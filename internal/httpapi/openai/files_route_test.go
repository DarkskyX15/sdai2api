package openai

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/auth"
	dsclient "ds2api/internal/deepseek/client"
)

// SDAI 上游没有文件上传端点：/v1/files 上传/检索一律 501。
type filesRouteDSStub struct{}

type managedFilesAuthStub struct{}

func (managedFilesAuthStub) Determine(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{
		UseConfigToken: true,
		DeepSeekToken:  "managed-token",
		CallerID:       "caller:test",
		AccountID:      "acct-123",
		TriedAccounts:  map[string]bool{},
	}, nil
}

func (managedFilesAuthStub) DetermineCaller(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{
		UseConfigToken: true,
		DeepSeekToken:  "managed-token",
		CallerID:       "caller:test",
		AccountID:      "acct-123",
		TriedAccounts:  map[string]bool{},
	}, nil
}

func (managedFilesAuthStub) Release(_ *auth.RequestAuth) {}

func (m *filesRouteDSStub) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "", nil
}

func (m *filesRouteDSStub) CallCompletion(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ int) (*http.Response, error) {
	return nil, nil
}

func (m *filesRouteDSStub) DeleteSessionForToken(_ context.Context, _ string, _ string) (*dsclient.DeleteSessionResult, error) {
	return &dsclient.DeleteSessionResult{Success: true}, nil
}

func (m *filesRouteDSStub) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	return nil
}

func TestFilesRouteUploadReturns501(t *testing.T) {
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: streamStatusAuthStub{}, DS: &filesRouteDSStub{}}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	req := httptest.NewRequest(http.MethodPost, "/v1/files", bytes.NewBufferString(`{"purpose":"assistants"}`))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFilesRouteRetrieveReturns501(t *testing.T) {
	h := &openAITestSurface{Store: mockOpenAIConfig{}, Auth: managedFilesAuthStub{}, DS: &filesRouteDSStub{}}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	req := httptest.NewRequest(http.MethodGet, "/v1/files/file-123", nil)
	req.Header.Set("Authorization", "Bearer direct-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d body=%s", rec.Code, rec.Body.String())
	}
}
