package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"ds2api/internal/config"
	trans "ds2api/internal/deepseek/transport"
)

// jsonRequest 执行一次带 JSON body 的上游请求并解析响应体。
// method 支持 POST/DELETE；body 为 nil 时不发送请求体。
func (c *Client) jsonRequest(ctx context.Context, doer trans.Doer, method, url string, headers map[string]string, payload any) (map[string]any, int, error) {
	var reader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	payloadBytes, err := readResponseBody(resp)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	out := map[string]any{}
	if len(payloadBytes) > 0 {
		if err := json.Unmarshal(payloadBytes, &out); err != nil {
			config.Logger.Warn("[sdai] json parse failed", "url", url, "status", resp.StatusCode, "preview", preview(payloadBytes))
		}
	}
	return out, resp.StatusCode, nil
}

func (c *Client) postJSON(ctx context.Context, doer trans.Doer, url string, headers map[string]string, payload any) (map[string]any, error) {
	body, status, err := c.jsonRequest(ctx, doer, http.MethodPost, url, headers, payload)
	if err != nil {
		return nil, err
	}
	if status == 0 {
		return nil, errors.New("request failed")
	}
	return body, nil
}

func (c *Client) postJSONWithStatus(ctx context.Context, doer trans.Doer, url string, headers map[string]string, payload any) (map[string]any, int, error) {
	return c.jsonRequest(ctx, doer, http.MethodPost, url, headers, payload)
}

func (c *Client) getJSONWithStatus(ctx context.Context, doer trans.Doer, url string, headers map[string]string) (map[string]any, int, error) {
	return c.jsonRequest(ctx, doer, http.MethodGet, url, headers, nil)
}

func (c *Client) deleteJSONWithStatus(ctx context.Context, doer trans.Doer, url string, headers map[string]string, payload any) (map[string]any, int, error) {
	return c.jsonRequest(ctx, doer, http.MethodDelete, url, headers, payload)
}
