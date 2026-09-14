package sse

import (
	"net/http"
	"strings"

	dsprotocol "ds2api/internal/deepseek/protocol"
)

// CollectResult holds the aggregated text and thinking content from an
// SDAI SSE stream, consumed to completion (non-streaming use case).
type CollectResult struct {
	Text                  string
	Thinking              string
	ToolDetectionThinking string
	ContentFilter         bool
	CitationLinks         map[int]string
	ResponseMessageID     int
}

// CollectStream fully consumes an SDAI SSE response and separates
// thinking content from text content.
//
// The caller is responsible for closing resp.Body unless closeBody is true.
func CollectStream(resp *http.Response, thinkingEnabled bool, closeBody bool) CollectResult {
	if closeBody {
		defer func() { _ = resp.Body.Close() }()
	}
	text := strings.Builder{}
	thinking := strings.Builder{}
	detectionThinking := strings.Builder{}
	stopped := false
	responseMessageID := 0
	currentType := "text"
	if thinkingEnabled {
		currentType = "thinking"
	}
	_ = dsprotocol.ScanSSELines(resp, func(line []byte) bool {
		result := ParseSDAIContentLine(line, thinkingEnabled, currentType)
		if result.ResponseMessageID > 0 {
			responseMessageID = result.ResponseMessageID
		}
		if result.Stop {
			stopped = true
			return false
		}
		if stopped || !result.Parsed {
			return true
		}
		currentType = result.NextType
		for _, p := range result.Parts {
			if p.Type == "thinking" {
				thinking.WriteString(p.Text)
			} else {
				text.WriteString(p.Text)
			}
		}
		// thinking 关闭时 think 增量仅进入 tool 检测通道。
		for _, p := range result.ToolDetectionThinkingParts {
			detectionThinking.WriteString(p.Text)
		}
		return true
	})
	thinkingText := thinking.String()
	return CollectResult{
		Text:                  text.String(),
		Thinking:              thinkingText,
		ToolDetectionThinking: thinkingText + detectionThinking.String(),
		ContentFilter:         false,
		CitationLinks:         nil,
		ResponseMessageID:     responseMessageID,
	}
}
