package client

import (
	"context"
	dsprotocol "ds2api/internal/deepseek/protocol"
	"fmt"
	"net/http"
	"strings"

	"ds2api/internal/auth"

	"github.com/google/uuid"
)

// CreateSession 为每次对外请求生成一个全新的随机会话 UUID。
// SDAI 会话由客户端生成 uuid 直接使用，无需上游创建端点；
// 本服务采用无状态策略，每个 uuid 只用一次。
func (c *Client) CreateSession(ctx context.Context, a *auth.RequestAuth, maxAttempts int) (string, error) {
	_ = ctx
	_ = a
	_ = maxAttempts
	return uuid.NewString(), nil
}

func (c *Client) authHeaders(token string) map[string]string {
	headers := make(map[string]string, len(dsprotocol.BaseHeaders)+1)
	for k, v := range dsprotocol.BaseHeaders {
		headers[k] = v
	}
	headers["authorization"] = "Bearer " + token
	return streamAcceptHeaders(headers)
}

// streamAcceptHeaders 为 SSE 请求补充 accept 头。
func streamAcceptHeaders(headers map[string]string) map[string]string {
	out := cloneStringMap(headers)
	if _, ok := out["Accept"]; !ok {
		out["Accept"] = "text/event-stream"
	}
	return out
}

// isTokenInvalid 判定上游失败是否为 token 失效。
// SDAI 失败形态（实测）：HTTP 401/403，或 HTTP 200 + body {"code":401,"message":"Unauthorized"}。
func isTokenInvalid(status int, code int, msg string) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	if code == 401 || code == 403 {
		return true
	}
	combined := strings.ToLower(msg)
	return strings.Contains(combined, "unauthorized") ||
		strings.Contains(combined, "token") ||
		strings.Contains(combined, "login") ||
		strings.Contains(combined, "登录") ||
		strings.Contains(combined, "未登录") ||
		strings.Contains(combined, "认证")
}

func authFailureKind(useConfigToken bool) FailureKind {
	if useConfigToken {
		return FailureManagedUnauthorized
	}
	return FailureDirectUnauthorized
}

// extractResponseStatus 解析 SDAI 业务响应体 {"code","message","data"}。
// SDAI 约定：HTTP 状态恒 200，业务结果看 body code（200 成功）。
func extractResponseStatus(resp map[string]any) (code int, msg string) {
	code = intFrom(resp["code"])
	msg, _ = resp["message"].(string)
	if strings.TrimSpace(msg) == "" {
		msg, _ = resp["msg"].(string)
	}
	return code, msg
}

// sdaiBusinessFailure 把上游业务失败转换为统一错误。
func sdaiBusinessFailure(op string, status int, code int, msg string, useConfigToken bool) error {
	message := strings.TrimSpace(msg)
	if message == "" {
		message = fmt.Sprintf("status=%d, code=%d", status, code)
	}
	if isTokenInvalid(status, code, message) {
		return &RequestFailure{Op: op, Kind: authFailureKind(useConfigToken), Message: message}
	}
	return fmt.Errorf("%s failed: %s", op, message)
}
