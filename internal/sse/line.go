package sse

// LineResult is the normalized parse result for one SDAI SSE line.
type LineResult struct {
	Parsed                     bool
	Stop                       bool
	ContentFilter              bool
	ErrorMessage               string
	Parts                      []ContentPart
	ToolDetectionThinkingParts []ContentPart
	NextType                   string
	ResponseMessageID          int
}

// ParseSDAIContentLine centralizes one-line SDAI SSE parsing for both
// streaming and non-streaming handlers.
func ParseSDAIContentLine(raw []byte, thinkingEnabled bool, currentType string) LineResult {
	chunk, done, parsed := ParseSDAISSELine(raw)
	if !parsed {
		return LineResult{NextType: currentType}
	}
	if done {
		return LineResult{Parsed: true, Stop: true, NextType: currentType}
	}
	// finish 事件：携带 req_message_pk_id（上游消息 ID），用于日志与重试关联；
	// 真正的结束信号由 flag 事件的 DONE 给出。
	if pk := reqMessagePkID(chunk); pk > 0 {
		return LineResult{Parsed: true, NextType: currentType, ResponseMessageID: pk}
	}
	// 未知 data（如 cate 事件的 {"format":"STREAM"}）不产生内容。
	if _, hasChoices := chunk["choices"]; !hasChoices {
		return LineResult{NextType: currentType}
	}
	// think 增量无论 thinking 开关都必须进入 tool 检测通道：
	// 模型可能只在思考流里输出 DSML 工具调用块（正文为空），
	// 流式 finalize 依赖该通道提升工具调用（回归：仅关闭时填充会导致
	// thinking 开启场景误判 upstream_empty_output）。
	allParts, nextType := deltaParts(chunk, true, currentType)
	parts := make([]ContentPart, 0, len(allParts))
	detectionParts := make([]ContentPart, 0, len(allParts))
	for _, p := range allParts {
		if p.Type == "thinking" {
			detectionParts = append(detectionParts, p)
			if !thinkingEnabled {
				continue
			}
		}
		parts = append(parts, p)
	}
	return LineResult{
		Parsed:                     true,
		Parts:                      parts,
		ToolDetectionThinkingParts: detectionParts,
		NextType:                   nextType,
		ResponseMessageID:          0,
	}
}

func reqMessagePkID(chunk map[string]any) int {
	if v, ok := chunk["req_message_pk_id"].(float64); ok && v > 0 {
		return int(v)
	}
	return 0
}
