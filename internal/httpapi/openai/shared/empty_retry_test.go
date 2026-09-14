package shared

import "testing"

// 回归：SDAI 思考模型在 think=1 时对简单消息会把整段回答写进 reasoning
// 通道（正文为空），空输出重试必须翻转 think=0 才能打破 reasoning-only
// 循环（探针结论见 .local/findings.md）。
func TestClonePayloadForEmptyOutputRetryDisablesThinking(t *testing.T) {
	payload := map[string]any{
		"content": "你好。",
		"uuid":    "u1",
		"model_id": 10,
		"think":   1,
	}
	clone := ClonePayloadForEmptyOutputRetry(payload, 0)

	if got := clone["think"]; got != 0 {
		t.Fatalf("expected think=0 on retry payload, got %#v", got)
	}
	if got := clone["content"]; got == "你好。" {
		t.Fatalf("expected retry suffix appended to content, got %#v", got)
	}
	if got := clone["uuid"]; got != "u1" {
		t.Fatalf("expected uuid preserved, got %#v", got)
	}
	// 原始 payload 不被污染。
	if payload["think"] != 1 {
		t.Fatalf("original payload think must stay 1, got %#v", payload["think"])
	}
	if payload["content"] != "你好。" {
		t.Fatalf("original payload content must stay untouched, got %#v", payload["content"])
	}
}
