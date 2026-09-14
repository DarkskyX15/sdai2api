package sse

import (
	"encoding/json"
	"strings"
)

type ContentPart struct {
	Text string
	Type string
}

// ParseSDAISSELine 解析一行 SDAI SSE。
//
// SDAI 事件结构（见 tests/raw_stream_samples/sdai-*）：
//
//	event: message / data: {"choices":[{"index":0,"delta":{"content":"...","type":"think|text"}}]}
//	event: finish  / data: {"req_message_pk_id":123}
//	event: flag    / data: DONE
//
// 返回值：(解析出的 JSON chunk, 是否为结束信号, 是否成功解析)。
// 仅处理 data: 行；event: 行与空行由行扫描器天然跳过（未知事件如 cate 容忍）。
func ParseSDAISSELine(raw []byte) (map[string]any, bool, bool) {
	line := strings.TrimSpace(string(raw))
	if line == "" || !strings.HasPrefix(line, "data:") {
		return nil, false, false
	}
	dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if dataStr == "DONE" {
		return nil, true, true
	}
	chunk := map[string]any{}
	if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
		return nil, false, false
	}
	return chunk, false, true
}

// deltaParts 从 SDAI message chunk 提取增量内容。
// delta.type == "think" → thinking 通道；"text"（或其他）→ text 通道。
func deltaParts(chunk map[string]any, thinkingEnabled bool, currentType string) ([]ContentPart, string) {
	parts := make([]ContentPart, 0, 4)
	nextType := currentType
	choices, ok := chunk["choices"].([]any)
	if !ok {
		return parts, nextType
	}
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]any)
		if !ok {
			continue
		}
		delta, ok := choice["delta"].(map[string]any)
		if !ok {
			continue
		}
		content, _ := delta["content"].(string)
		if content == "" {
			continue
		}
		deltaType, _ := delta["type"].(string)
		partType := "text"
		if strings.EqualFold(deltaType, "think") {
			partType = "thinking"
		}
		if partType == "thinking" {
			nextType = "thinking"
		} else {
			nextType = "text"
		}
		if partType == "thinking" && !thinkingEnabled {
			continue
		}
		parts = append(parts, ContentPart{Text: content, Type: partType})
	}
	return parts, nextType
}
