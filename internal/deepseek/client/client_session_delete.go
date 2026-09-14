package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"ds2api/internal/config"
	dsprotocol "ds2api/internal/deepseek/protocol"
)

// DeleteSessionResult 删除会话结果
type DeleteSessionResult struct {
	SessionID    string // 会话 ID（SDAI 会话 uuid）
	Success      bool   // 是否成功
	ErrorMessage string // 错误信息
}

// DeleteSessionForToken 删除单个 SDAI 会话（DELETE /msg_title/del，JSON body {"uuid"}）。
// 上游返回 404"对话不存在"时视为幂等成功。
func (c *Client) DeleteSessionForToken(ctx context.Context, token string, sessionID string) (*DeleteSessionResult, error) {
	clients := c.requestClientsFromContext(ctx)
	result := &DeleteSessionResult{SessionID: sessionID}
	if sessionID == "" {
		result.ErrorMessage = "session_id is required"
		return result, errors.New(result.ErrorMessage)
	}

	headers := c.authHeaders(token)
	resp, status, err := c.deleteJSONWithStatus(ctx, clients.regular, dsprotocol.SDAIMsgTitleDelURL, headers, map[string]any{"uuid": sessionID})
	if err != nil {
		result.ErrorMessage = err.Error()
		return result, err
	}
	code, msg := extractResponseStatus(resp)
	if status != http.StatusOK || (code != 200 && code != 404) {
		if code == 404 {
			// 对话不存在：目标已消失，视为成功。
			result.Success = true
			return result, nil
		}
		result.ErrorMessage = fmt.Sprintf("request failed: status=%d, code=%d, message=%s", status, code, msg)
		return result, errors.New(result.ErrorMessage)
	}
	result.Success = true
	return result, nil
}

// DeleteAllSessionsForToken 删除 token 名下全部 SDAI 会话。
// SDAI 无 delete-all 端点：通过 msg_title/list 分页枚举后逐个删除。
// 注意：这会删除该账号在 SDAI 网页端的全部对话（含真实使用记录）。
func (c *Client) DeleteAllSessionsForToken(ctx context.Context, token string) error {
	const pageSize = 100
	headers := c.authHeaders(token)
	clients := c.requestClientsFromContext(ctx)

	for page := 1; ; page++ {
		reqURL := fmt.Sprintf("%s?page=%d&page_size=%d", dsprotocol.SDAIMsgTitleListURL, page, pageSize)
		resp, status, err := c.getJSONWithStatus(ctx, clients.regular, reqURL, headers)
		if err != nil {
			return err
		}
		code, msg := extractResponseStatus(resp)
		if status != http.StatusOK || code != 200 {
			return fmt.Errorf("list sessions failed: status=%d, code=%d, message=%s", status, code, msg)
		}
		uuids := extractSessionUUIDs(resp)
		for _, id := range uuids {
			result, delErr := c.DeleteSessionForToken(ctx, token, id)
			if delErr != nil {
				config.Logger.Warn("[delete_all_sessions] delete one failed", "uuid", id, "error", delErr)
			} else if result != nil && !result.Success {
				config.Logger.Warn("[delete_all_sessions] delete one not success", "uuid", id, "message", result.ErrorMessage)
			}
		}
		totalPages := intFrom(resp["total_pages"])
		if data, ok := resp["data"].(map[string]any); ok {
			totalPages = intFrom(data["total_pages"])
		}
		if len(uuids) == 0 || page >= maxInt(totalPages, 1) {
			return nil
		}
	}
}

func extractSessionUUIDs(resp map[string]any) []string {
	data, _ := resp["data"].(map[string]any)
	results, _ := data["results"].([]any)
	uuids := make([]string, 0, len(results))
	for _, item := range results {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := m["message_id"].(string); ok && id != "" {
			uuids = append(uuids, id)
		}
	}
	return uuids
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
