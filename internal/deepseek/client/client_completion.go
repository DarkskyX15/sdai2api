package client

import (
	"context"
	dsprotocol "ds2api/internal/deepseek/protocol"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"ds2api/internal/auth"
	trans "ds2api/internal/deepseek/transport"
)

// CallCompletion 调用 SDAI chat/start 并返回上游 SSE 响应。
// payload 由调用方（promptcompat.CompletionPayload）注入，包含六字段：
// content/from_uuid/uuid/kb_tid_list/model_id/think。
func (c *Client) CallCompletion(ctx context.Context, a *auth.RequestAuth, payload map[string]any, maxAttempts int) (*http.Response, error) {
	_ = maxAttempts
	if err := validateContentLength(payload); err != nil {
		return nil, err
	}
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken)
	captureSession := c.capture.Start("sdai_chat_start", dsprotocol.SDAIChatStartURL, a.AccountID, payload)
	resp, err := c.streamPostOnce(ctx, clients.stream, dsprotocol.SDAIChatStartURL, headers, payload)
	if err != nil {
		return nil, err
	}
	if captureSession != nil {
		resp.Body = captureSession.WrapBody(resp.Body, resp.StatusCode)
	}
	// SDAI 失败形态：HTTP 200 + application/json 业务错误体（非 SSE 流）。
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		defer func() { _ = resp.Body.Close() }()
		bodyBytes, readErr := readResponseBody(resp)
		if readErr != nil {
			return nil, fmt.Errorf("read chat/start error body: %w", readErr)
		}
		parsed := map[string]any{}
		_ = json.Unmarshal(bodyBytes, &parsed)
		code, msg := extractResponseStatus(parsed)
		return nil, sdaiBusinessFailure("chat start", resp.StatusCode, code, msg, a.UseConfigToken)
	}
	return resp, nil
}

// validateContentLength 预校验 content 长度。
// SDAI 上游 content 列 ≈65535 bytes，超限时以"正常 SSE + MySQL 1406 错误文本"返回，
// 无法从流上区分成败，因此必须在发送前拦截。
func validateContentLength(payload map[string]any) error {
	content, _ := payload["content"].(string)
	if len(content) <= dsprotocol.SDAIContentMaxBytes {
		return nil
	}
	return &RequestFailure{
		Op:      "chat start",
		Message: fmt.Sprintf("content too long for upstream: %d bytes (limit ~65535)", len(content)),
	}
}

func (c *Client) streamPostOnce(ctx context.Context, doer trans.Doer, url string, headers map[string]string, payload any) (*http.Response, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return doer.Do(req)
}
