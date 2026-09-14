package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	dsclient "ds2api/internal/deepseek/client"
)

func loadTestStore(t *testing.T) *config.Store {
	t.Helper()
	return config.LoadStore()
}

type testingDSMock struct {
	createSessionCalls     int
	callCompletionCalls    int
	deleteAllSessionsCalls int
	deleteAllSessionsError error
}

func (m *testingDSMock) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	m.createSessionCalls++
	return "session-id", nil
}

func (m *testingDSMock) CallCompletion(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ int) (*http.Response, error) {
	m.callCompletionCalls++
	return nil, errors.New("should not call CallCompletion in this test")
}

func (m *testingDSMock) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	m.deleteAllSessionsCalls++
	if m.deleteAllSessionsError != nil {
		return m.deleteAllSessionsError
	}
	return nil
}

func (m *testingDSMock) GetSessionCountForToken(_ context.Context, _ string) (*dsclient.SessionStats, error) {
	return &dsclient.SessionStats{Success: true}, nil
}

func TestTestAccount_TokenlessAccountFails(t *testing.T) {
	t.Setenv("DS2API_CONFIG_PATH", t.TempDir()+"/config.json")
	t.Setenv("DS2API_CONFIG_JSON", `{"accounts":[{"email":"batch@example.com","token":""}]}`)
	store := loadTestStore(t)
	ds := &testingDSMock{}
	h := &Handler{Store: store, DS: ds}
	acc, ok := store.FindAccount("batch@example.com")
	if !ok {
		t.Fatal("expected test account")
	}

	result := h.testAccount(context.Background(), acc, "deepseek-v4-flash", "")

	if ok, _ := result["success"].(bool); ok {
		t.Fatalf("expected token-less account test to fail, got %#v", result)
	}
	msg, _ := result["message"].(string)
	if !strings.Contains(msg, "token") {
		t.Fatalf("expected token hint in message, got %q", msg)
	}
	if ds.createSessionCalls != 0 {
		t.Fatalf("expected no session creation for token-less account, got %d", ds.createSessionCalls)
	}
}

func TestTestAccount_TokenOnlyBatchModeCreatesSession(t *testing.T) {
	t.Setenv("DS2API_CONFIG_PATH", t.TempDir()+"/config.json")
	t.Setenv("DS2API_CONFIG_JSON", `{"accounts":[{"email":"batch@example.com","token":"configured-token"}]}`)
	store := loadTestStore(t)
	ds := &testingDSMock{}
	h := &Handler{Store: store, DS: ds}
	acc, ok := store.FindAccount("batch@example.com")
	if !ok {
		t.Fatal("expected test account")
	}

	result := h.testAccount(context.Background(), acc, "deepseek-v4-flash", "")

	if ok, _ := result["success"].(bool); !ok {
		t.Fatalf("expected success=true, got %#v", result)
	}
	msg, _ := result["message"].(string)
	if !strings.Contains(msg, "Token") {
		t.Fatalf("expected token-valid success message, got %q", msg)
	}
	if ds.createSessionCalls != 1 || ds.callCompletionCalls != 0 {
		t.Fatalf("unexpected CreateSession/CallCompletion calls: createSession=%d callCompletion=%d", ds.createSessionCalls, ds.callCompletionCalls)
	}
	testStatus, ok := store.AccountTestStatus("batch@example.com")
	if !ok || testStatus != "ok" {
		t.Fatalf("expected runtime test status ok, got %q (ok=%v)", testStatus, ok)
	}
}

func TestDeleteAllSessions_FailureReportedWithoutRelogin(t *testing.T) {
	t.Setenv("DS2API_CONFIG_PATH", t.TempDir()+"/config.json")
	t.Setenv("DS2API_CONFIG_JSON", `{"accounts":[{"email":"batch@example.com","token":"configured-token"}]}`)
	store := loadTestStore(t)
	ds := &testingDSMock{deleteAllSessionsError: errors.New("token expired")}
	h := &Handler{Store: store, DS: ds}

	req := httptest.NewRequest(http.MethodPost, "/delete-all", bytes.NewBufferString(`{"identifier":"batch@example.com"}`))
	rec := httptest.NewRecorder()
	h.deleteAllSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if ok, _ := resp["success"].(bool); ok {
		t.Fatalf("expected failure response, got %#v", resp)
	}
	if ds.deleteAllSessionsCalls != 1 {
		t.Fatalf("expected single delete call (no relogin), got %d", ds.deleteAllSessionsCalls)
	}
}

type completionPayloadDSMock struct {
	payload map[string]any
}

func (m *completionPayloadDSMock) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "session-id", nil
}

func (m *completionPayloadDSMock) CallCompletion(_ context.Context, _ *auth.RequestAuth, payload map[string]any, _ int) (*http.Response, error) {
	m.payload = payload
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: DONE\n\n")),
	}, nil
}

func (m *completionPayloadDSMock) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	return nil
}

func (m *completionPayloadDSMock) GetSessionCountForToken(_ context.Context, _ string) (*dsclient.SessionStats, error) {
	return &dsclient.SessionStats{Success: true}, nil
}

func TestTestAccount_MessageModeUsesSDAIPayload(t *testing.T) {
	t.Setenv("DS2API_CONFIG_PATH", t.TempDir()+"/config.json")
	t.Setenv("DS2API_CONFIG_JSON", `{"accounts":[{"email":"batch@example.com","token":"seed-token"}]}`)
	store := loadTestStore(t)
	ds := &completionPayloadDSMock{}
	h := &Handler{Store: store, DS: ds}
	acc, ok := store.FindAccount("batch@example.com")
	if !ok {
		t.Fatal("expected test account")
	}

	result := h.testAccount(context.Background(), acc, "deepseek-v4-pro", "hello")

	if ok, _ := result["success"].(bool); !ok {
		t.Fatalf("expected success=true, got %#v", result)
	}
	if got := ds.payload["uuid"]; got != "session-id" {
		t.Fatalf("unexpected uuid: %#v", got)
	}
	if got := ds.payload["model_id"]; got != 8 {
		t.Fatalf("expected model_id 8 (deepseek-v4-pro), got %#v", got)
	}
	if content, _ := ds.payload["content"].(string); !strings.Contains(content, "hello") {
		t.Fatalf("expected content to contain prompt, got %q", content)
	}
	if _, has := ds.payload["think"]; !has {
		t.Fatal("expected think field in SDAI payload")
	}
}
