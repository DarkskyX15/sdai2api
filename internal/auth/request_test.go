package auth

import (
	"errors"
	"net/http"
	"testing"

	"ds2api/internal/account"
	"ds2api/internal/config"
)

func newTestResolver(t *testing.T) *Resolver {
	t.Helper()
	isolateTestConfigPath(t)
	isolateTestConfigPath(t)
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[{"email":"acc@example.com","token":"account-token"}]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	return NewResolver(store, pool)
}

// isolateTestConfigPath 防止本地 config.json 覆盖测试注入的 env 配置
// （loadConfig 在 env writeback 开启时优先读取 DS2API_CONFIG_PATH 文件）。
func isolateTestConfigPath(t *testing.T) {
	t.Helper()
	t.Setenv("DS2API_CONFIG_PATH", t.TempDir()+"/config.json")
}

func TestDetermineWithXAPIKeyUsesDirectToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	req.Header.Set("x-api-key", "direct-token")

	auth, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if auth.UseConfigToken {
		t.Fatalf("expected direct token mode")
	}
	if auth.DeepSeekToken != "direct-token" {
		t.Fatalf("unexpected token: %q", auth.DeepSeekToken)
	}
	if auth.CallerID == "" {
		t.Fatalf("expected caller id to be populated")
	}
}

func TestDetermineWithXAPIKeyManagedKeyAcquiresAccount(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	req.Header.Set("x-api-key", "managed-key")

	auth, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(auth)
	if !auth.UseConfigToken {
		t.Fatalf("expected managed key mode")
	}
	if auth.AccountID != "acc@example.com" {
		t.Fatalf("unexpected account id: %q", auth.AccountID)
	}
	// SDAI：token 直接来自账号配置，无登录/刷新。
	if auth.DeepSeekToken != "account-token" {
		t.Fatalf("unexpected account token: %q", auth.DeepSeekToken)
	}
	if auth.CallerID == "" {
		t.Fatalf("expected caller id to be populated")
	}
}

func TestDetermineCallerWithManagedKeySkipsAccountAcquire(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodGet, "/v1/responses/resp_1", nil)
	req.Header.Set("x-api-key", "managed-key")

	a, err := r.DetermineCaller(req)
	if err != nil {
		t.Fatalf("determine caller failed: %v", err)
	}
	if a.CallerID == "" {
		t.Fatalf("expected caller id to be populated")
	}
	if a.UseConfigToken {
		t.Fatalf("expected no config-token lease for caller-only auth")
	}
	if a.AccountID != "" {
		t.Fatalf("expected empty account id, got %q", a.AccountID)
	}
}

func TestCallerTokenIDStable(t *testing.T) {
	a := callerTokenID("token-a")
	b := callerTokenID("token-a")
	c := callerTokenID("token-b")
	if a == "" || b == "" || c == "" {
		t.Fatalf("expected non-empty caller ids")
	}
	if a != b {
		t.Fatalf("expected stable caller id, got %q and %q", a, b)
	}
	if a == c {
		t.Fatalf("expected different caller id for different tokens")
	}
}

func TestDetermineMissingToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	_, err := r.Determine(req)
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if err != ErrUnauthorized {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetermineWithQueryKeyUsesDirectToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?key=direct-query-key", nil)

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if a.UseConfigToken {
		t.Fatalf("expected direct token mode")
	}
	if a.DeepSeekToken != "direct-query-key" {
		t.Fatalf("unexpected token: %q", a.DeepSeekToken)
	}
}

func TestDetermineWithXGoogAPIKeyUsesDirectToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse", nil)
	req.Header.Set("x-goog-api-key", "goog-header-key")

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if a.UseConfigToken {
		t.Fatalf("expected direct token mode")
	}
	if a.DeepSeekToken != "goog-header-key" {
		t.Fatalf("unexpected token: %q", a.DeepSeekToken)
	}
}

func TestDetermineWithAPIKeyQueryParamUsesDirectToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?api_key=direct-api-key", nil)

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if a.UseConfigToken {
		t.Fatalf("expected direct token mode")
	}
	if a.DeepSeekToken != "direct-api-key" {
		t.Fatalf("unexpected token: %q", a.DeepSeekToken)
	}
}

func TestDetermineHeaderTokenPrecedenceOverQueryKey(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?key=query-key", nil)
	req.Header.Set("x-api-key", "managed-key")

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(a)
	if !a.UseConfigToken {
		t.Fatalf("expected managed key mode from header token")
	}
	if a.AccountID == "" {
		t.Fatalf("expected managed account to be acquired")
	}
}

func TestDetermineCallerMissingToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodGet, "/v1/responses/resp_1", nil)

	_, err := r.DetermineCaller(req)
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if err != ErrUnauthorized {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetermineManagedAccountRetriesOtherAccountOnMissingToken(t *testing.T) {
	isolateTestConfigPath(t)
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"bad@example.com"},
			{"email":"good@example.com","token":"good-token"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := NewResolver(store, pool)

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	a, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer resolver.Release(a)
	// 账号池轮转顺序不确定，但结果必须落在有 token 的账号上。
	if a.AccountID != "good@example.com" {
		t.Fatalf("expected fallback to good account, got %q", a.AccountID)
	}
	if a.DeepSeekToken != "good-token" {
		t.Fatalf("expected good token, got %q", a.DeepSeekToken)
	}
}

func TestDetermineTargetAccountDoesNotFallbackOnMissingToken(t *testing.T) {
	isolateTestConfigPath(t)
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"bad@example.com"},
			{"email":"good@example.com","token":"good-token"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := NewResolver(store, pool)

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")
	req.Header.Set("X-Ds2-Target-Account", "bad@example.com")

	_, err := resolver.Determine(req)
	if err == nil {
		t.Fatal("expected determine to fail for token-less target account")
	}
}

func TestDetermineManagedAccountReturnsLastEnsureErrorWhenAllFail(t *testing.T) {
	isolateTestConfigPath(t)
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"bad1@example.com"},
			{"email":"bad2@example.com"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := NewResolver(store, pool)

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	_, err := resolver.Determine(req)
	if err == nil {
		t.Fatal("expected determine to fail")
	}
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}
