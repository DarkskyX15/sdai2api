package sse

import (
	"io"
	"net/http"
	"strings"
)

// makeSSEResponse 构造一个用于 CollectStream 测试的 http.Response。
func makeSSEResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
