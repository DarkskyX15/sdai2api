package claude

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ds2api/internal/auth"
	"ds2api/internal/chathistory"
)

type claudeCurrentInputAuth struct{}

type claudeHistoryConfig struct {
	aliases map[string]string
}

func (m claudeHistoryConfig) ModelAliases() map[string]string { return m.aliases }
func (claudeHistoryConfig) CurrentInputFileEnabled() bool     { return false }
func (claudeHistoryConfig) CurrentInputFileMinChars() int     { return 0 }

func (claudeCurrentInputAuth) Determine(*http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{
		DeepSeekToken: "direct-token",
		CallerID:      "caller:test",
		TriedAccounts: map[string]bool{},
	}, nil
}

func TestClaudeDirectRecordsResponseHistory(t *testing.T) {
	ds := &claudeCurrentInputDS{}
	historyStore := chathistory.New(filepath.Join(t.TempDir(), "history.json"))
	h := &Handler{
		Store:       claudeHistoryConfig{aliases: map[string]string{"claude-sonnet-4-6": "deepseek-v4-flash"}},
		Auth:        claudeCurrentInputAuth{},
		DS:          ds,
		ChatHistory: historyStore,
	}
	reqBody := `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello from claude"}],"max_tokens":1024}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Messages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot history: %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one history item, got %d", len(snapshot.Items))
	}
	item, err := historyStore.Get(snapshot.Items[0].ID)
	if err != nil {
		t.Fatalf("get history item: %v", err)
	}
	if item.Surface != "claude.messages" {
		t.Fatalf("unexpected surface: %q", item.Surface)
	}
	if item.Model != "claude-sonnet-4-6" {
		t.Fatalf("unexpected model: %q", item.Model)
	}
	if item.UserInput != "hello from claude" {
		t.Fatalf("unexpected user input: %q", item.UserInput)
	}
	if item.Content != "ok" {
		t.Fatalf("expected raw upstream content, got %q", item.Content)
	}
}

func (claudeCurrentInputAuth) Release(*auth.RequestAuth) {}

type claudeCurrentInputDS struct {
	payload map[string]any
}

func (d *claudeCurrentInputDS) CreateSession(context.Context, *auth.RequestAuth, int) (string, error) {
	return "session-id", nil
}

func (d *claudeCurrentInputDS) CallCompletion(_ context.Context, _ *auth.RequestAuth, payload map[string]any, _ int) (*http.Response, error) {
	d.payload = payload
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\",\"type\":\"text\"}}]}\ndata: DONE\n")),
	}, nil
}

func TestClaudeDirectAppliesCurrentInputFile(t *testing.T) {
	ds := &claudeCurrentInputDS{}
	historyStore := chathistory.New(filepath.Join(t.TempDir(), "history.json"))
	h := &Handler{
		Store:       mockClaudeConfig{aliases: map[string]string{"claude-sonnet-4-6": "deepseek-v4-flash"}},
		Auth:        claudeCurrentInputAuth{},
		DS:          ds,
		ChatHistory: historyStore,
	}
	reqBody := `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello from claude"}],"max_tokens":1024}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Messages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	// SDAI：current_input_file 短路，上下文直接透传 content。
	content, _ := ds.payload["content"].(string)
	if !strings.Contains(content, "hello from claude") {
		t.Fatalf("expected passthrough content to include user text, got %q", content)
	}
	if _, has := ds.payload["ref_file_ids"]; has {
		t.Fatalf("expected no ref_file_ids under SDAI, got %#v", ds.payload["ref_file_ids"])
	}
	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot history: %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one history item, got %d", len(snapshot.Items))
	}
	full, err := historyStore.Get(snapshot.Items[0].ID)
	if err != nil {
		t.Fatalf("get history item: %v", err)
	}
	if full.Content != "ok" {
		t.Fatalf("expected raw upstream content, got %q", full.Content)
	}
}

func TestClaudeCurrentInputFileDisabledPassthroughToolsPrompt(t *testing.T) {
	ds := &claudeCurrentInputDS{}
	h := &Handler{
		Store: mockClaudeConfig{aliases: map[string]string{"claude-sonnet-4-6": "deepseek-v4-flash"}},
		Auth:  claudeCurrentInputAuth{},
		DS:    ds,
	}
	reqBody := `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello from claude"}],"tools":[{"name":"search","description":"Search docs","input_schema":{"type":"object"}}],"max_tokens":1024}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Messages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	// SDAI：无文件上传，tool schema 以提示词内联方式进入 content。
	content, _ := ds.payload["content"].(string)
	if !strings.Contains(content, "TOOL CALL FORMAT") || !strings.Contains(content, "Search docs") {
		t.Fatalf("expected live content to inline tool format and schema, got %q", content)
	}
}
