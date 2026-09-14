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
	parts, nextType := deltaParts(chunk, thinkingEnabled, currentType)
	// thinking 关闭时 think 增量不出现在可见 parts，但仍需进入
	// tool 检测通道（隐藏思考中的 DSML 工具调用要能被提升）。
	detectionParts := make([]ContentPart, 0, len(parts))
	if !thinkingEnabled {
		visibleParts, _ := deltaParts(chunk, true, currentType)
		for _, p := range visibleParts {
			if p.Type == "thinking" {
				detectionParts = append(detectionParts, p)
			}
		}
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
