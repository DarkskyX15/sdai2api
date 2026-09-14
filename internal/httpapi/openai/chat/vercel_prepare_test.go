package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ds2api/internal/account"
	"ds2api/internal/auth"
	"ds2api/internal/config"
	dsclient "ds2api/internal/deepseek/client"
	"ds2api/internal/promptcompat"
)

func TestIsVercelStreamPrepareRequest(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions?__stream_prepare=1", nil)
	if !isVercelStreamPrepareRequest(req) {
		t.Fatalf("expected prepare request to be detected")
	}

	req2 := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	if isVercelStreamPrepareRequest(req2) {
		t.Fatalf("expected non-prepare request")
	}
}

func TestIsVercelStreamReleaseRequest(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions?__stream_release=1", nil)
	if !isVercelStreamReleaseRequest(req) {
		t.Fatalf("expected release request to be detected")
	}

	req2 := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	if isVercelStreamReleaseRequest(req2) {
		t.Fatalf("expected non-release request")
	}
}

func TestVercelInternalSecret(t *testing.T) {
	t.Run("prefer explicit secret", func(t *testing.T) {
		t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")
		t.Setenv("DS2API_ADMIN_KEY", "admin-fallback")
		if got := vercelInternalSecret(); got != "stream-secret" {
			t.Fatalf("expected explicit secret, got %q", got)
		}
	})

	t.Run("fallback to admin key", func(t *testing.T) {
		t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "")
		t.Setenv("DS2API_ADMIN_KEY", "admin-fallback")
		if got := vercelInternalSecret(); got != "admin-fallback" {
			t.Fatalf("expected admin key fallback, got %q", got)
		}
	})

	t.Run("default admin when env missing", func(t *testing.T) {
		t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "")
		t.Setenv("DS2API_ADMIN_KEY", "")
		if got := vercelInternalSecret(); got != "admin" {
			t.Fatalf("expected default admin fallback, got %q", got)
		}
	})
}

func TestStreamLeaseLifecycle(t *testing.T) {
	h := &Handler{}
	leaseID := h.holdStreamLease(&auth.RequestAuth{UseConfigToken: false}, promptcompat.StandardRequest{}, "test-session-id")
	if leaseID == "" {
		t.Fatalf("expected non-empty lease id")
	}
	if lease, ok := h.releaseStreamLease(leaseID); !ok {
		t.Fatalf("expected lease release success")
	} else if lease.SessionID != "test-session-id" {
		t.Fatalf("expected released session id, got %q", lease.SessionID)
	}
	if _, ok := h.releaseStreamLease(leaseID); ok {
		t.Fatalf("expected duplicate release to fail")
	}
}

func TestStreamLeaseTTL(t *testing.T) {
	t.Setenv("DS2API_VERCEL_STREAM_LEASE_TTL_SECONDS", "120")
	if got := streamLeaseTTL(); got != 120*time.Second {
		t.Fatalf("expected ttl=120s, got %v", got)
	}
	t.Setenv("DS2API_VERCEL_STREAM_LEASE_TTL_SECONDS", "invalid")
	if got := streamLeaseTTL(); got != 15*time.Minute {
		t.Fatalf("expected default ttl on invalid value, got %v", got)
	}
}

// vercelPrepareDSStub SDAI 版 prepare 测试桩。
type vercelPrepareDSStub struct {
	resp *http.Response
}

func (m *vercelPrepareDSStub) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "session-id", nil
}

func (m *vercelPrepareDSStub) CallCompletion(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ int) (*http.Response, error) {
	return m.resp, nil
}

func (m *vercelPrepareDSStub) DeleteSessionForToken(_ context.Context, _ string, _ string) (*dsclient.DeleteSessionResult, error) {
	return &dsclient.DeleteSessionResult{Success: true}, nil
}

func (m *vercelPrepareDSStub) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	return nil
}

func TestHandleVercelStreamPreparePassesThroughNormalizedContext(t *testing.T) {
	// SDAI：current_input_file 短路，归一化上下文直接进入 payload.content。
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	ds := &vercelPrepareDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{
			currentInputEnabled: true,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
		"stream":   true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	payload, _ := body["payload"].(map[string]any)
	if payload == nil {
		t.Fatalf("expected payload object, got %#v", body["payload"])
	}
	content, _ := payload["content"].(string)
	if !strings.Contains(content, "latest user turn") {
		t.Fatalf("expected normalized context in payload content, got %s", content)
	}
	if _, has := payload["ref_file_ids"]; has {
		t.Fatalf("expected no ref_file_ids under SDAI, got %#v", payload["ref_file_ids"])
	}
}

func TestHandleVercelStreamPrepareUsesHalfwidthDSMLToolPrompt(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	h := &Handler{
		Store: mockOpenAIConfig{},
		Auth:  streamStatusAuthStub{},
		DS:    &vercelPrepareDSStub{},
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "search docs"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "search",
					"description": "search docs",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{"type": "string"},
						},
						"required": []any{"query"},
					},
				},
			},
		},
		"stream": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	finalPrompt, _ := body["final_prompt"].(string)
	payload, _ := body["payload"].(map[string]any)
	payloadPrompt, _ := payload["content"].(string)
	for label, promptText := range map[string]string{"final_prompt": finalPrompt, "payload.content": payloadPrompt} {
		if !strings.Contains(promptText, "<|DSML|tool_calls>") || !strings.Contains(promptText, "Tag punctuation alphabet: ASCII < > / = \" plus the halfwidth pipe |.") {
			t.Fatalf("expected %s to contain halfwidth DSML tool instructions, got %q", label, promptText)
		}
		if strings.Contains(promptText, "\uff5c") || strings.Contains(promptText, "full"+"width vertical bar") {
			t.Fatalf("expected %s not to contain legacy pipe guidance, got %q", label, promptText)
		}
	}
	toolNames, _ := body["tool_names"].([]any)
	if len(toolNames) != 1 || toolNames[0] != "search" {
		t.Fatalf("expected prepared tool names to align with request tools, got %#v", body["tool_names"])
	}
}

type vercelReleaseAutoDeleteDSStub struct {
	resp             *http.Response
	deleteCallCount  int
	deletedSessionID string
	deletedToken     string
	deleteErr        error
	events           *[]string
}

func (m *vercelReleaseAutoDeleteDSStub) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "session-id", nil
}

func (m *vercelReleaseAutoDeleteDSStub) CallCompletion(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ int) (*http.Response, error) {
	return m.resp, nil
}

func (m *vercelReleaseAutoDeleteDSStub) DeleteSessionForToken(_ context.Context, token string, sessionID string) (*dsclient.DeleteSessionResult, error) {
	if m.events != nil {
		*m.events = append(*m.events, "delete")
	}
	m.deleteCallCount++
	m.deletedSessionID = sessionID
	m.deletedToken = token
	if m.deleteErr != nil {
		return nil, m.deleteErr
	}
	return &dsclient.DeleteSessionResult{SessionID: sessionID, Success: true}, nil
}

func (m *vercelReleaseAutoDeleteDSStub) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	return nil
}

type vercelReleaseAuthStub struct {
	events *[]string
}

func (a *vercelReleaseAuthStub) Determine(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{DeepSeekToken: "test-token", AccountID: "test-account"}, nil
}

func (a *vercelReleaseAuthStub) DetermineCaller(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{DeepSeekToken: "test-token", AccountID: "test-account"}, nil
}

func (a *vercelReleaseAuthStub) Release(_ *auth.RequestAuth) {
	if a.events != nil {
		*a.events = append(*a.events, "release")
	}
}

func TestHandleVercelStreamReleaseTriggersAutoDelete(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	events := []string{}
	ds := &vercelReleaseAutoDeleteDSStub{events: &events}
	h := &Handler{
		Store: mockOpenAIConfig{
			autoDeleteMode: "single",
		},
		Auth: &vercelReleaseAuthStub{events: &events},
		DS:   ds,
	}

	leaseID := h.holdStreamLease(&auth.RequestAuth{DeepSeekToken: "test-token", AccountID: "test-account"}, promptcompat.StandardRequest{}, "session-to-delete")
	if leaseID == "" {
		t.Fatalf("expected non-empty lease id")
	}

	reqBody := map[string]any{"lease_id": leaseID}
	reqJSON, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_release=1", strings.NewReader(string(reqJSON)))
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.handleVercelStreamRelease(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ds.deleteCallCount != 1 {
		t.Fatalf("expected auto delete call count=1, got %d", ds.deleteCallCount)
	}
	if ds.deletedSessionID != "session-to-delete" {
		t.Fatalf("expected deleted session id=session-to-delete, got %q", ds.deletedSessionID)
	}
	if got, want := strings.Join(events, ","), "delete,release"; got != want {
		t.Fatalf("expected auto-delete before auth release, got %s", got)
	}
}

func TestHandleVercelStreamPrepareInlinesToolsPrompt(t *testing.T) {
	// SDAI：无文件上传，tool schema 以提示词内联方式进入 final_prompt 与 payload.content。
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	ds := &vercelPrepareDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{currentInputEnabled: true},
		Auth:  streamStatusAuthStub{},
		DS:    ds,
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "search docs"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "search",
					"description": "search docs",
					"parameters":  map[string]any{"type": "object"},
				},
			},
		},
		"stream": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	finalPrompt, _ := body["final_prompt"].(string)
	payload, _ := body["payload"].(map[string]any)
	payloadContent, _ := payload["content"].(string)
	for label, text := range map[string]string{"final_prompt": finalPrompt, "payload.content": payloadContent} {
		if !strings.Contains(text, "TOOL CALL FORMAT") {
			t.Fatalf("expected %s to retain tool call format instructions, got %q", label, text)
		}
		if !strings.Contains(text, "search docs") {
			t.Fatalf("expected %s to inline tool schema, got %q", label, text)
		}
	}
}

func TestHandleVercelStreamPrepareMapsCurrentInputFileManagedAuthFailureTo401(t *testing.T) {
	// SDAI：current_input_file 短路，不再产生上传类 401；此场景由 token 失效路径覆盖。
	t.Skip("current_input_file is a no-op under SDAI upstream")
}

func TestHandleVercelStreamSwitchIssuesFreshPayload(t *testing.T) {
	// SDAI：切号 = 新 uuid + 原始 payload，无 current_input_file 重传。
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")
	t.Setenv("DS2API_CONFIG_PATH", t.TempDir()+"/config.json")
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"acc1@test.com","token":"token-acc1"},
			{"email":"acc2@test.com","token":"token-acc2"}
		]
	}`)
	store := config.LoadStore()
	resolver := auth.NewResolver(store, account.NewPool(store))
	authReq := httptest.NewRequest(http.MethodPost, "/", nil)
	authReq.Header.Set("Authorization", "Bearer managed-key")
	a, err := resolver.Determine(authReq)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer resolver.Release(a)

	ds := &vercelPrepareDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{currentInputEnabled: true},
		Auth:  resolver,
		DS:    ds,
	}
	stdReq := promptcompat.StandardRequest{
		RequestedModel:          "deepseek-v4-flash",
		ResolvedModel:           "deepseek-v4-flash",
		ResponseModel:           "deepseek-v4-flash",
		FinalPrompt:             "Continue from the latest state in the attached DS2API_HISTORY.txt context. Available tool descriptions and parameter schemas are attached in DS2API_TOOLS.txt; use only those tools and follow the tool-call format rules in this prompt.",
		PromptTokenText:         "# DS2API_HISTORY.txt\n\n=== 1. USER ===\nhello\n\n# DS2API_TOOLS.txt\nAvailable tool descriptions and parameter schemas for this request.\n\nYou have access to these tools:\n\nTool: search\nDescription: search docs\nParameters: {\"type\":\"object\"}\n",
		HistoryText:             "# DS2API_HISTORY.txt\n\n=== 1. USER ===\nhello\n",
		CurrentInputFileApplied: true,
		CurrentInputFileID:      "file-old",
		CurrentToolsFileID:      "file-old-tools",
		ToolsRaw: []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "search",
					"description": "search docs",
					"parameters":  map[string]any{"type": "object"},
				},
			},
		},
		RefFileIDs: []string{"file-old", "file-old-tools", "client-file"},
		Thinking:   true,
	}
	leaseID := h.holdStreamLease(a, stdReq, "")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_switch=1", strings.NewReader(`{"lease_id":"`+leaseID+`"}`))
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamSwitch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if body["deepseek_token"] != "token-acc2" {
		t.Fatalf("expected switched account token, got %#v", body["deepseek_token"])
	}
	payload, _ := body["payload"].(map[string]any)
	newUUID, _ := payload["uuid"].(string)
	if newUUID == "" {
		t.Fatalf("expected non-empty uuid in switched payload, got %#v", payload["uuid"])
	}
	contentText, _ := payload["content"].(string)
	if !strings.Contains(contentText, "DS2API_TOOLS.txt") {
		t.Fatalf("expected switched payload content to retain tools file reference, got %q", contentText)
	}
}
