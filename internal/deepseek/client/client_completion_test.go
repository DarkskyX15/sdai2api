package client

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"ds2api/internal/auth"
	dsprotocol "ds2api/internal/deepseek/protocol"
)

func TestCallCompletionRejectsOverlongContentBeforeSend(t *testing.T) {
	var called bool
	client := &Client{
		stream: doerFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("should not be called")
		}),
	}
	payload := map[string]any{"content": string(make([]byte, dsprotocol.SDAIContentMaxBytes+1))}
	_, err := client.CallCompletion(
		context.Background(),
		&auth.RequestAuth{DeepSeekToken: "token"},
		payload,
		3,
	)
	if err == nil {
		t.Fatal("expected content-too-long failure")
	}
	if called {
		t.Fatal("upstream must not be called when content exceeds limit")
	}
}

func TestCallCompletionPropagatesTransportError(t *testing.T) {
	client := &Client{
		stream: doerFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("ambiguous completion write failure")
		}),
	}
	_, err := client.CallCompletion(
		context.Background(),
		&auth.RequestAuth{DeepSeekToken: "token"},
		map[string]any{"content": "hello"},
		3,
	)
	if err == nil {
		t.Fatal("expected completion error")
	}
}

func TestIsTokenInvalidSDAISemantics(t *testing.T) {
	cases := []struct {
		name   string
		status int
		code   int
		msg    string
		want   bool
	}{
		{"http 200 with json code 401", http.StatusOK, 401, "Unauthorized", true},
		{"http 401", http.StatusUnauthorized, 0, "", true},
		{"http 403", http.StatusForbidden, 0, "", true},
		{"business ok", http.StatusOK, 200, "success", false},
		{"not found", http.StatusOK, 404, "对话不存在", false},
		{"msg keyword", http.StatusOK, 0, "please login first", true},
		{"msg keyword chinese", http.StatusOK, 0, "请先登录", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTokenInvalid(tc.status, tc.code, tc.msg); got != tc.want {
				t.Fatalf("isTokenInvalid(%d,%d,%q)=%v want %v", tc.status, tc.code, tc.msg, got, tc.want)
			}
		})
	}
}
