package shared

import "strings"

const EmptyOutputRetrySuffix = "Previous reply had no visible output. Please regenerate the visible final answer or tool call now."

func EmptyOutputRetryEnabled() bool {
	return true
}

func EmptyOutputRetryMaxAttempts() int {
	return 1
}

func ClonePayloadWithEmptyOutputRetryPrompt(payload map[string]any) map[string]any {
	return ClonePayloadForEmptyOutputRetry(payload, 0)
}

// ClonePayloadForEmptyOutputRetry creates a retry payload with the retry
// suffix appended to the upstream content field. parentMessageID 保留参数以
// 兼容旧调用点，但 SDAI 无 parent_message_id 语义，恒不写入。
//
// SDAI 思考模型怪癖（见 .local/findings.md）：think=1 时对简单/社交类消息，
// 模型会把最终回答整段写进 reasoning 通道、不发 text，仅追加后缀的重试
// 不稳定。重试时强制 think=0（该轮放弃思考换可见输出），实测可稳定打破
// reasoning-only 死循环；对 nothinking 模型 payload 本就 think=0，无影响。
func ClonePayloadForEmptyOutputRetry(payload map[string]any, parentMessageID int) map[string]any {
	clone := make(map[string]any, len(payload))
	for k, v := range payload {
		clone[k] = v
	}
	original, _ := payload["content"].(string)
	clone["content"] = AppendEmptyOutputRetrySuffix(original)
	clone["think"] = 0
	return clone
}

func AppendEmptyOutputRetrySuffix(prompt string) string {
	prompt = strings.TrimRight(prompt, "\r\n\t ")
	if prompt == "" {
		return EmptyOutputRetrySuffix
	}
	return prompt + "\n\n" + EmptyOutputRetrySuffix
}

func UsagePromptWithEmptyOutputRetry(originalPrompt string, retryAttempts int) string {
	if retryAttempts <= 0 {
		return originalPrompt
	}
	parts := make([]string, 0, retryAttempts+1)
	parts = append(parts, originalPrompt)
	next := originalPrompt
	for i := 0; i < retryAttempts; i++ {
		next = AppendEmptyOutputRetrySuffix(next)
		parts = append(parts, next)
	}
	return strings.Join(parts, "\n")
}
