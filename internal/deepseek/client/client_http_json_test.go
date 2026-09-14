package client

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPostJSONWithStatusParsesBody(t *testing.T) {
	client := &Client{}
	primary := doerFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":200,"message":"success"}`)),
			Request:    req,
		}, nil
	})

	resp, status, err := client.postJSONWithStatus(
		context.Background(),
		primary,
		"https://example.com/api",
		map[string]string{"x-test": "1"},
		map[string]any{"foo": "bar"},
	)
	if err != nil {
		t.Fatalf("postJSONWithStatus error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d want=%d", status, http.StatusOK)
	}
	if code, _ := resp["code"].(float64); int(code) != 200 {
		t.Fatalf("unexpected response body: %#v", resp)
	}
}

func TestDeleteJSONWithStatusSendsDeleteMethod(t *testing.T) {
	client := &Client{}
	var gotMethod string
	primary := doerFunc(func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":200}`)),
			Request:    req,
		}, nil
	})

	if _, _, err := client.deleteJSONWithStatus(
		context.Background(), primary, "https://example.com/del",
		map[string]string{}, map[string]any{"uuid": "x"},
	); err != nil {
		t.Fatalf("deleteJSONWithStatus error: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("expected DELETE method, got %q", gotMethod)
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}
