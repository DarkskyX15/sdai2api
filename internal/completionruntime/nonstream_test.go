package completionruntime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"ds2api/internal/account"
	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

type fakeDeepSeekCaller struct {
	responses          []*http.Response
	payloads           []map[string]any
	completionAccounts []string
	sessionByAccount   bool
}

type currentInputRuntimeConfig struct{}

func (currentInputRuntimeConfig) CurrentInputFileEnabled() bool { return true }
func (currentInputRuntimeConfig) CurrentInputFileMinChars() int { return 0 }

func (f *fakeDeepSeekCaller) CreateSession(_ context.Context, a *auth.RequestAuth, _ int) (string, error) {
	if f.sessionByAccount && a != nil && a.AccountID != "" {
		return "session-" + a.AccountID, nil
	}
	return "session-1", nil
}

func (f *fakeDeepSeekCaller) CallCompletion(_ context.Context, a *auth.RequestAuth, payload map[string]any, _ int) (*http.Response, error) {
	f.payloads = append(f.payloads, payload)
	if a != nil {
		f.completionAccounts = append(f.completionAccounts, a.AccountID)
	}
	if len(f.responses) == 0 {
		return sseHTTPResponse(http.StatusOK, sdaiTextDelta("fallback")), nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

// sdaiDeltaLine 构造一行 SDAI 增量（JSON 安全编码）。
func sdaiDeltaLine(content, deltaType string) string {
	line, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{"content": content, "type": deltaType},
		}},
	})
	if err != nil {
		panic("test helper: marshal delta failed: " + err.Error())
	}
	return "data: " + string(line)
}

// sdaiTextDelta 构造一行 SDAI text 增量。
func sdaiTextDelta(content string) string {
	return sdaiDeltaLine(content, "text")
}

// sdaiThinkDelta 构造一行 SDAI think 增量（用于构造空输出场景）。
func sdaiThinkDelta(content string) string {
	return sdaiDeltaLine(content, "think")
}

// sdaiFinish 构造 SDAI finish 事件行。
func sdaiFinish(id int) string {
	return `data: {"req_message_pk_id": ` + intToString(id) + `}`
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestExecuteNonStreamWithRetryBuildsCanonicalTurn(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(
		http.StatusOK,
		sdaiFinish(42),
		sdaiTextDelta(`<tool_calls><invoke name="Write"><parameter name="content">{"x":1}</parameter></invoke></tool_calls>`),
		"data: DONE",
	)}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
		ToolNames:       []string{"Write"},
		ToolsRaw: []any{map[string]any{
			"name": "Write",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
				},
			},
		}},
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, stdReq, Options{})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if result.SessionID != "session-1" {
		t.Fatalf("session mismatch: %q", result.SessionID)
	}
	if got := result.Turn.ResponseMessageID; got != 42 {
		t.Fatalf("response message id mismatch: %d", got)
	}
	if len(result.Turn.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(result.Turn.ToolCalls))
	}
	if _, ok := result.Turn.ToolCalls[0].Input["content"].(string); !ok {
		t.Fatalf("expected schema-normalized string argument, got %#v", result.Turn.ToolCalls[0].Input["content"])
	}
	if result.Turn.Usage.InputTokens == 0 || result.Turn.Usage.TotalTokens == 0 {
		t.Fatalf("expected usage to be populated, got %#v", result.Turn.Usage)
	}
}

func sdaiTestAccountsConfig() string {
	return `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"acc1@test.com","token":"token-acc1"},
			{"email":"acc2@test.com","token":"token-acc2"}
		]
	}`
}

func sdaiManagedAuth(t *testing.T) *auth.RequestAuth {
	t.Helper()
	store := config.LoadStore()
	resolver := auth.NewResolver(store, account.NewPool(store))
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	a, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	t.Cleanup(func() { resolver.Release(a) })
	return a
}

func TestExecuteNonStreamWithRetrySwitchesManagedAccountBeforeFinal429(t *testing.T) {
	isolateTestConfig(t)
	t.Setenv("DS2API_CONFIG_JSON", sdaiTestAccountsConfig())
	a := sdaiManagedAuth(t)

	ds := &fakeDeepSeekCaller{
		sessionByAccount: true,
		responses: []*http.Response{
			sseHTTPResponse(http.StatusOK, sdaiFinish(11), sdaiThinkDelta("first empty"), "data: DONE"),
			sseHTTPResponse(http.StatusOK, sdaiFinish(12), sdaiThinkDelta("retry empty"), "data: DONE"),
			sseHTTPResponse(http.StatusOK, sdaiFinish(21), sdaiTextDelta("ok from second account"), "data: DONE"),
		},
	}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
		Thinking:        true,
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, a, stdReq, Options{RetryEnabled: true})
	if outErr != nil {
		t.Fatalf("unexpected output error after account switch retry: %#v", outErr)
	}
	if result.Turn.Text != "ok from second account" {
		t.Fatalf("text mismatch after switch retry: %q", result.Turn.Text)
	}
	if result.SessionID != "session-acc2@test.com" {
		t.Fatalf("expected switched account session, got %q", result.SessionID)
	}
	if got := ds.payloads[2]["uuid"]; got != "session-acc2@test.com" {
		t.Fatalf("switched payload uuid mismatch: %#v", got)
	}
	if content, _ := ds.payloads[2]["content"].(string); strings.Contains(content, "Previous reply had no visible output") {
		t.Fatalf("expected fresh switched-account content without empty-output suffix, got %q", content)
	}
	// SDAI 切号重试是 fresh retry：每次调用都带账号身份。
	wantLastAccount := "acc2@test.com"
	if got := ds.completionAccounts[len(ds.completionAccounts)-1]; got != wantLastAccount {
		t.Fatalf("expected last completion on %q, got %q (all=%v)", wantLastAccount, got, ds.completionAccounts)
	}
}

func TestExecuteNonStreamWithRetryCurrentInputFileIsNoop(t *testing.T) {
	// SDAI 无文件上传通道：current_input_file 短路，payload 直接透传全量上下文。
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(http.StatusOK, sdaiTextDelta("ok"), "data: DONE")}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test_adapter",
		RequestedModel:  "deepseek-v4-flash",
		ResolvedModel:   "deepseek-v4-flash",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "first user turn",
		FinalPrompt:     "first user turn",
		Messages: []any{
			map[string]any{"role": "user", "content": "first user turn"},
		},
	}

	start, outErr := StartCompletion(context.Background(), ds, &auth.RequestAuth{DeepSeekToken: "token"}, stdReq, Options{
		CurrentInputFile: currentInputRuntimeConfig{},
	})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if start.Request.CurrentInputFileApplied {
		t.Fatal("expected current input file to be a no-op under SDAI")
	}
	if len(ds.payloads) != 1 {
		t.Fatalf("expected one completion payload, got %d", len(ds.payloads))
	}
	if content, _ := ds.payloads[0]["content"].(string); content != "first user turn" {
		t.Fatalf("expected passthrough content, got %q", content)
	}
	if _, has := ds.payloads[0]["model_id"]; !has {
		t.Fatal("expected model_id in SDAI payload")
	}
}

func TestExecuteNonStreamWithRetryUsesContentSuffixForEmptyRetry(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{
		sseHTTPResponse(http.StatusOK, sdaiFinish(77), sdaiThinkDelta("plan"), "data: DONE"),
		sseHTTPResponse(http.StatusOK, sdaiFinish(78), sdaiTextDelta("ok"), "data: DONE"),
	}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, stdReq, Options{RetryEnabled: true})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if result.Attempts != 1 {
		t.Fatalf("expected one retry, got %d", result.Attempts)
	}
	if len(ds.payloads) != 2 {
		t.Fatalf("expected two completion calls, got %d", len(ds.payloads))
	}
	// SDAI 无 parent_message_id：fresh retry 在 content 上追加重试后缀。
	if content, _ := ds.payloads[1]["content"].(string); !strings.Contains(content, "Previous reply had no visible output") {
		t.Fatalf("expected retry suffix in payload content, got %q", content)
	}
	if _, has := ds.payloads[1]["parent_message_id"]; has {
		t.Fatal("expected no parent_message_id in SDAI payload")
	}
	if result.Turn.Text != "ok" {
		t.Fatalf("retry text mismatch: %q", result.Turn.Text)
	}
}

func TestExecuteNonStreamWithRetryPlainText(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(
		http.StatusOK,
		sdaiTextDelta("答案。"),
		"data: DONE",
	)}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, stdReq, Options{})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if result.Turn.Text != "答案。" {
		t.Fatalf("text mismatch: got %q", result.Turn.Text)
	}
}

// isolateTestConfig 防止本地 config.json 覆盖测试注入的 env 配置。
func isolateTestConfig(t *testing.T) {
	t.Helper()
	t.Setenv("DS2API_CONFIG_PATH", t.TempDir()+"/config.json")
}

func sseHTTPResponse(status int, lines ...string) *http.Response {
	body := strings.Join(lines, "\n")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
