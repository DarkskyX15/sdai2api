package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"ds2api/internal/account"
	"ds2api/internal/config"
)

type ctxKey string

const authCtxKey ctxKey = "auth_context"

var (
	ErrUnauthorized = errors.New("unauthorized: missing auth token")
	ErrNoAccount    = errors.New("no accounts configured or all accounts are busy")
	ErrNoToken      = errors.New("account has no SDAI token configured")
)

type RequestAuth struct {
	UseConfigToken bool
	// DeepSeekToken 承载上游凭据（SDAI Bearer token），字段名保留以兼容既有调用点。
	DeepSeekToken  string
	CallerID       string
	AccountID      string
	TargetAccount  string
	Account        config.Account
	TriedAccounts  map[string]bool
	resolver       *Resolver
}

type Resolver struct {
	Store *config.Store
	Pool  *account.Pool
}

func NewResolver(store *config.Store, pool *account.Pool) *Resolver {
	return &Resolver{
		Store: store,
		Pool:  pool,
	}
}

func (r *Resolver) Determine(req *http.Request) (*RequestAuth, error) {
	callerKey := extractCallerToken(req)
	if callerKey == "" {
		return nil, ErrUnauthorized
	}
	callerID := callerTokenID(callerKey)
	ctx := req.Context()
	if !r.Store.HasAPIKey(callerKey) {
		return &RequestAuth{
			UseConfigToken: false,
			DeepSeekToken:  callerKey,
			CallerID:       callerID,
			resolver:       r,
			TriedAccounts:  map[string]bool{},
		}, nil
	}
	target := strings.TrimSpace(req.Header.Get("X-Ds2-Target-Account"))
	a, err := r.acquireManagedRequestAuth(ctx, callerID, target)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *Resolver) acquireManagedRequestAuth(ctx context.Context, callerID, target string) (*RequestAuth, error) {
	tried := map[string]bool{}
	var lastEnsureErr error
	for {
		if target == "" && len(tried) >= len(r.Store.Accounts()) {
			if lastEnsureErr != nil {
				return nil, lastEnsureErr
			}
			return nil, ErrNoAccount
		}
		acc, ok := r.Pool.AcquireWait(ctx, target, tried)
		if !ok {
			if lastEnsureErr != nil {
				return nil, lastEnsureErr
			}
			return nil, ErrNoAccount
		}

		a := &RequestAuth{
			UseConfigToken: true,
			CallerID:       callerID,
			AccountID:      acc.Identifier(),
			TargetAccount:  target,
			Account:        acc,
			TriedAccounts:  tried,
			resolver:       r,
		}

		if err := r.ensureManagedToken(a); err != nil {
			lastEnsureErr = err
			tried[a.AccountID] = true
			r.Pool.Release(a.AccountID)
			if target != "" {
				return nil, err
			}
			continue
		}
		return a, nil
	}
}

// DetermineCaller resolves caller identity without acquiring any pooled account.
// Use this for local-cache lookup routes that only need tenant isolation.
func (r *Resolver) DetermineCaller(req *http.Request) (*RequestAuth, error) {
	callerKey := extractCallerToken(req)
	if callerKey == "" {
		return nil, ErrUnauthorized
	}
	callerID := callerTokenID(callerKey)
	a := &RequestAuth{
		UseConfigToken: false,
		CallerID:       callerID,
		resolver:       r,
		TriedAccounts:  map[string]bool{},
	}
	if r == nil || r.Store == nil || !r.Store.HasAPIKey(callerKey) {
		a.DeepSeekToken = callerKey
	}
	return a, nil
}

func WithAuth(ctx context.Context, a *RequestAuth) context.Context {
	return context.WithValue(ctx, authCtxKey, a)
}

func FromContext(ctx context.Context) (*RequestAuth, bool) {
	v := ctx.Value(authCtxKey)
	a, ok := v.(*RequestAuth)
	return a, ok
}

// ensureManagedToken SDAI 上游以配置中的 Bearer token 为唯一凭据：
// 无自动登录、无刷新。token 为空的账号直接判为不可用。
func (r *Resolver) ensureManagedToken(a *RequestAuth) error {
	token := strings.TrimSpace(a.Account.Token)
	if token == "" {
		return ErrNoToken
	}
	a.DeepSeekToken = token
	return nil
}

func (r *Resolver) SwitchAccount(ctx context.Context, a *RequestAuth) bool {
	if !a.UseConfigToken {
		return false
	}
	if strings.TrimSpace(a.TargetAccount) != "" {
		return false
	}
	if a.TriedAccounts == nil {
		a.TriedAccounts = map[string]bool{}
	}
	if a.AccountID != "" {
		a.TriedAccounts[a.AccountID] = true
		r.Pool.Release(a.AccountID)
	}
	for {
		acc, ok := r.Pool.Acquire("", a.TriedAccounts)
		if !ok {
			return false
		}
		a.Account = acc
		a.AccountID = acc.Identifier()
		if err := r.ensureManagedToken(a); err != nil {
			a.TriedAccounts[a.AccountID] = true
			r.Pool.Release(a.AccountID)
			continue
		}
		return true
	}
}

func (a *RequestAuth) SwitchAccount(ctx context.Context) bool {
	if a == nil || a.resolver == nil {
		return false
	}
	return a.resolver.SwitchAccount(ctx, a)
}

func (r *Resolver) Release(a *RequestAuth) {
	if a == nil || !a.UseConfigToken || a.AccountID == "" {
		return
	}
	r.Pool.Release(a.AccountID)
}

func extractCallerToken(req *http.Request) string {
	authHeader := strings.TrimSpace(req.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token := strings.TrimSpace(authHeader[7:])
		if token != "" {
			return token
		}
	}
	if key := strings.TrimSpace(req.Header.Get("x-api-key")); key != "" {
		return key
	}
	// Gemini/Google clients commonly send API key via x-goog-api-key.
	if key := strings.TrimSpace(req.Header.Get("x-goog-api-key")); key != "" {
		return key
	}
	// Gemini AI Studio compatibility: allow query key fallback only when no
	// header-based credential is present.
	if key := strings.TrimSpace(req.URL.Query().Get("key")); key != "" {
		return key
	}
	return strings.TrimSpace(req.URL.Query().Get("api_key"))
}

func callerTokenID(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return "caller:" + hex.EncodeToString(sum[:8])
}
