package client

import (
	"context"
	"fmt"
	"net/http"

	dsprotocol "ds2api/internal/deepseek/protocol"
)

// SessionStats 会话统计结果
type SessionStats struct {
	AccountID      string // 账号标识
	FirstPageCount int    // 第一页会话数量（当 HasMore 为 true 时，真实总数可能更大）
	PinnedCount    int    // 保留字段：SDAI 会话无置顶语义，恒为 0
	HasMore        bool   // 是否还有更多页
	TotalItems     int    // SDAI 会话总数（msg_title/list total_items）
	Success        bool   // 请求是否成功
	ErrorMessage   string // 错误信息
}

// GetSessionCountForToken 使用 token 获取会话数量（msg_title/list 第一页）。
func (c *Client) GetSessionCountForToken(ctx context.Context, token string) (*SessionStats, error) {
	clients := c.requestClientsFromContext(ctx)
	headers := c.authHeaders(token)

	reqURL := dsprotocol.SDAIMsgTitleListURL + "?page=1&page_size=100"
	resp, status, err := c.getJSONWithStatus(ctx, clients.regular, reqURL, headers)
	if err != nil {
		return nil, err
	}
	code, msg := extractResponseStatus(resp)
	if status != http.StatusOK || code != 200 {
		return nil, fmt.Errorf("request failed: status=%d, code=%d, message=%s", status, code, msg)
	}

	data, _ := resp["data"].(map[string]any)
	results, _ := data["results"].([]any)
	totalItems := intFrom(data["total_items"])
	totalPages := intFrom(data["total_pages"])

	return &SessionStats{
		FirstPageCount: len(results),
		HasMore:        totalPages > 1,
		TotalItems:     totalItems,
		Success:        true,
	}, nil
}
