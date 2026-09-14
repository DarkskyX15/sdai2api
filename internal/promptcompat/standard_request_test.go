package promptcompat

import "testing"

// SDAI payload 六字段契约：uuid/content/from_uuid/kb_tid_list/model_id/think。
func TestStandardRequestCompletionPayloadSDAIShape(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		thinking bool
		modelID  int
		think    int
	}{
		{name: "default_flash", model: "deepseek-v4-flash", thinking: false, modelID: 10, think: 0},
		{name: "default_flash_thinking", model: "deepseek-v4-flash", thinking: true, modelID: 10, think: 1},
		{name: "default_nothinking", model: "deepseek-v4-flash-nothinking", thinking: false, modelID: 10, think: 0},
		{name: "pro", model: "deepseek-v4-pro", thinking: true, modelID: 8, think: 1},
		{name: "v32", model: "deepseek-v3.2", thinking: false, modelID: 9, think: 0},
		{name: "doubao", model: "doubao-1-5-pro-32k-250115", thinking: false, modelID: 6, think: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := StandardRequest{
				ResolvedModel: tc.model,
				FinalPrompt:   "hello",
				Thinking:      tc.thinking,
			}

			payload := req.CompletionPayload("session-123")

			if got := payload["uuid"]; got != "session-123" {
				t.Fatalf("unexpected uuid: %#v", got)
			}
			if got := payload["content"]; got != "hello" {
				t.Fatalf("unexpected content: %#v", got)
			}
			if got := payload["model_id"]; got != tc.modelID {
				t.Fatalf("expected model_id %d, got %#v", tc.modelID, got)
			}
			if got := payload["think"]; got != tc.think {
				t.Fatalf("expected think %d, got %#v", tc.think, got)
			}
			if got := payload["from_uuid"]; got != "" {
				t.Fatalf("expected empty from_uuid, got %#v", got)
			}
			if _, ok := payload["kb_tid_list"].([]any); !ok {
				t.Fatalf("expected kb_tid_list slice, got %#v", payload["kb_tid_list"])
			}
			// DeepSeek 专属字段必须不再出现。
			for _, legacy := range []string{"chat_session_id", "model_type", "prompt", "ref_file_ids", "thinking_enabled", "search_enabled", "parent_message_id"} {
				if _, has := payload[legacy]; has {
					t.Fatalf("expected legacy field %q to be removed from SDAI payload", legacy)
				}
			}
		})
	}
}

func TestStandardRequestCompletionPayloadPassthrough(t *testing.T) {
	req := StandardRequest{
		ResolvedModel: "deepseek-v4-flash",
		FinalPrompt:   "hello",
		PassThrough: map[string]any{
			"temperature": 0.3,
		},
	}
	payload := req.CompletionPayload("session-123")
	if got := payload["temperature"]; got != 0.3 {
		t.Fatalf("expected passthrough temperature, got %#v", got)
	}
}

func TestStandardRequestCompletionPayloadUnknownModelFallsBackToFlash(t *testing.T) {
	req := StandardRequest{
		ResolvedModel: "totally-unknown-model",
		FinalPrompt:   "hello",
	}
	payload := req.CompletionPayload("session-123")
	if got := payload["model_id"]; got != 10 {
		t.Fatalf("expected fallback model_id 10, got %#v", got)
	}
}
