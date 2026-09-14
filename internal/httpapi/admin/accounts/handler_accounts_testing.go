package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	authn "ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/prompt"
	"ds2api/internal/promptcompat"
	"ds2api/internal/sse"
)

type modelAliasSnapshotReader struct {
	aliases map[string]string
}

func (m modelAliasSnapshotReader) ModelAliases() map[string]string {
	return m.aliases
}

func (h *Handler) testSingleAccount(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	identifier, _ := req["identifier"].(string)
	if strings.TrimSpace(identifier) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "需要账号标识（identifier / email / mobile）"})
		return
	}
	acc, ok := findAccountByIdentifier(h.Store, identifier)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "账号不存在"})
		return
	}
	model, _ := req["model"].(string)
	if model == "" {
		model = "deepseek-v4-flash"
	}
	message, _ := req["message"].(string)
	result := h.testAccount(r.Context(), acc, model, message)
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) testAllAccounts(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	model, _ := req["model"].(string)
	if model == "" {
		model = "deepseek-v4-flash"
	}
	accounts := h.Store.Snapshot().Accounts
	if len(accounts) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"total": 0, "success": 0, "failed": 0, "results": []any{}})
		return
	}

	// Concurrent testing with a semaphore to limit parallelism.
	const maxConcurrency = 5
	results := runAccountTestsConcurrently(accounts, maxConcurrency, func(_ int, account config.Account) map[string]any {
		return h.testAccount(r.Context(), account, model, "")
	})

	success := 0
	for _, res := range results {
		if ok, _ := res["success"].(bool); ok {
			success++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(accounts), "success": success, "failed": len(accounts) - success, "results": results})
}

func runAccountTestsConcurrently(accounts []config.Account, maxConcurrency int, testFn func(int, config.Account) map[string]any) []map[string]any {
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	sem := make(chan struct{}, maxConcurrency)
	results := make([]map[string]any, len(accounts))
	var wg sync.WaitGroup
	for i, acc := range accounts {
		wg.Add(1)
		go func(idx int, account config.Account) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release
			results[idx] = testFn(idx, account)
		}(i, acc)
	}
	wg.Wait()
	return results
}

func (h *Handler) testAccount(ctx context.Context, acc config.Account, model, message string) map[string]any {
	start := time.Now()
	identifier := acc.Identifier()
	result := map[string]any{
		"account":         identifier,
		"success":         false,
		"response_time":   0,
		"message":         "",
		"model":           model,
		"session_count":   0,
		"config_writable": !h.Store.IsEnvBacked(),
		"config_warning":  "",
	}
	defer func() {
		status := "failed"
		if ok, _ := result["success"].(bool); ok {
			status = "ok"
		}
		_ = h.Store.UpdateAccountTestStatus(identifier, status)
	}()
	// SDAI 上游以配置的 Bearer token 为唯一凭据，无自动登录。
	token := strings.TrimSpace(acc.Token)
	if token == "" {
		result["message"] = "账号未配置 token：SDAI 上游需要在账号设置中填入 Bearer token"
		return result
	}
	authCtx := &authn.RequestAuth{UseConfigToken: false, DeepSeekToken: token, AccountID: identifier, Account: acc}
	proxyCtx := authn.WithAuth(ctx, authCtx)
	sessionID, err := h.DS.CreateSession(proxyCtx, authCtx, 1)
	if err != nil {
		result["message"] = "创建会话失败: " + err.Error()
		return result
	}

	// 获取会话数量
	sessionStats, sessionErr := h.DS.GetSessionCountForToken(proxyCtx, token)
	if sessionErr == nil && sessionStats != nil {
		result["session_count"] = sessionStats.FirstPageCount
	}

	if strings.TrimSpace(message) == "" {
		result["success"] = true
		result["message"] = "Token 有效（会话创建成功）"
		result["response_time"] = int(time.Since(start).Milliseconds())
		return result
	}
	thinking, _, ok := config.GetModelConfig(model)
	resolvedModel, resolved := config.ResolveModel(modelAliasSnapshotReader{
		aliases: h.Store.Snapshot().ModelAliases,
	}, model)
	if resolved {
		model = resolvedModel
		thinking, _, ok = config.GetModelConfig(model)
	}
	if !ok {
		thinking = false
	}
	payload := promptcompat.StandardRequest{
		ResolvedModel: model,
		FinalPrompt:   prompt.MessagesPrepare([]map[string]any{{"role": "user", "content": message}}),
		Thinking:      thinking,
	}.CompletionPayload(sessionID)
	resp, err := h.DS.CallCompletion(proxyCtx, authCtx, payload, 1)
	if err != nil {
		result["message"] = "请求失败: " + err.Error()
		return result
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		result["message"] = fmt.Sprintf("请求失败: HTTP %d", resp.StatusCode)
		return result
	}
	collected := sse.CollectStream(resp, thinking, true)
	result["success"] = true
	result["response_time"] = int(time.Since(start).Milliseconds())
	if collected.Text != "" {
		result["message"] = collected.Text
	} else {
		result["message"] = "（无回复内容）"
	}
	if collected.Thinking != "" {
		result["thinking"] = collected.Thinking
	}
	return result
}

func (h *Handler) testAPI(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	model, _ := req["model"].(string)
	message, _ := req["message"].(string)
	apiKey, _ := req["api_key"].(string)
	if model == "" {
		model = "deepseek-v4-flash"
	}
	if message == "" {
		message = "你好"
	}
	if apiKey == "" {
		keys := h.Store.Snapshot().Keys
		if len(keys) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "没有可用的 API Key"})
			return
		}
		apiKey = keys[0]
	}
	host := r.Host
	scheme := "http"
	if strings.Contains(strings.ToLower(host), "vercel") || strings.Contains(strings.ToLower(r.Header.Get("X-Forwarded-Proto")), "https") {
		scheme = "https"
	}
	payload := map[string]any{"model": model, "messages": []map[string]any{{"role": "user", "content": message}}, "stream": false}
	b, _ := json.Marshal(payload)
	request, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, fmt.Sprintf("%s://%s/v1/chat/completions", scheme, host), bytes.NewReader(b))
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(request)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		var parsed any
		_ = json.Unmarshal(body, &parsed)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "status_code": resp.StatusCode, "response": parsed})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": false, "status_code": resp.StatusCode, "response": string(body)})
}

func (h *Handler) deleteAllSessions(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	identifier, _ := req["identifier"].(string)
	if strings.TrimSpace(identifier) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "需要账号标识（identifier / email / mobile）"})
		return
	}
	acc, ok := findAccountByIdentifier(h.Store, identifier)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "账号不存在"})
		return
	}

	// SDAI 无自动登录：直接使用账号配置的 token。
	token := strings.TrimSpace(acc.Token)
	if token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "账号未配置 token"})
		return
	}

	// 删除所有会话（注意：会连 SDAI 网页端的真实对话一并删除）。
	if err := h.DS.DeleteAllSessionsForToken(r.Context(), token); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "删除失败: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "删除成功"})
}
