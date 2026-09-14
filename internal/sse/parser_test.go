package sse

import (
	"strings"
	"testing"
)

func TestParseSDAISSELine(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		parsed bool
		done   bool
	}{
		{name: "message delta", raw: `data: {"choices":[{"index":0,"delta":{"content":"你","type":"text"}}]}`, parsed: true},
		{name: "finish event", raw: `data: {"req_message_pk_id": 13856, "mtid": 3058}`, parsed: true},
		{name: "flag DONE", raw: "data: DONE", parsed: true, done: true},
		{name: "event line ignored", raw: "event: message"},
		{name: "empty line ignored", raw: ""},
		{name: "cate header ignored", raw: `data: {"format": "STREAM"}`, parsed: true},
		{name: "invalid json ignored", raw: "data: {broken"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunk, done, parsed := ParseSDAISSELine([]byte(tc.raw))
			if parsed != tc.parsed || done != tc.done {
				t.Fatalf("ParseSDAISSELine(%q) = parsed=%v done=%v, want parsed=%v done=%v", tc.raw, parsed, done, tc.parsed, tc.done)
			}
			if tc.parsed && !tc.done && chunk == nil {
				t.Fatal("expected non-nil chunk for parsed data line")
			}
		})
	}
}

func TestParseSDAISSELineToleratesCRLF(t *testing.T) {
	_, done, parsed := ParseSDAISSELine([]byte("data: DONE\r"))
	if !parsed || !done {
		t.Fatalf("expected CRLF-terminated DONE to parse, got parsed=%v done=%v", parsed, done)
	}
}

func TestParseSDAIContentLineThinkTextChannels(t *testing.T) {
	think := ParseSDAIContentLine([]byte(`data: {"choices":[{"index":0,"delta":{"content":"好的","type":"think"}}]}`), true, "thinking")
	if len(think.Parts) != 1 || think.Parts[0].Type != "thinking" || think.Parts[0].Text != "好的" {
		t.Fatalf("unexpected think part: %#v", think.Parts)
	}
	if think.NextType != "thinking" {
		t.Fatalf("expected NextType thinking, got %q", think.NextType)
	}

	text := ParseSDAIContentLine([]byte(`data: {"choices":[{"index":0,"delta":{"content":"答案","type":"text"}}]}`), true, "thinking")
	if len(text.Parts) != 1 || text.Parts[0].Type != "text" || text.Parts[0].Text != "答案" {
		t.Fatalf("unexpected text part: %#v", text.Parts)
	}
	if text.NextType != "text" {
		t.Fatalf("expected NextType text, got %q", text.NextType)
	}
}

func TestParseSDAIContentLineFinishCarriesMessageID(t *testing.T) {
	result := ParseSDAIContentLine([]byte(`data: {"req_message_pk_id": 13864, "mtid": 3061}`), true, "text")
	if !result.Parsed {
		t.Fatal("expected finish line to parse")
	}
	if result.Stop {
		t.Fatal("finish event must not stop the stream; DONE does")
	}
	if result.ResponseMessageID != 13864 {
		t.Fatalf("expected ResponseMessageID=13864, got %d", result.ResponseMessageID)
	}
}

func TestParseSDAIContentLineDone(t *testing.T) {
	result := ParseSDAIContentLine([]byte("data: DONE"), true, "text")
	if !result.Parsed || !result.Stop {
		t.Fatalf("expected DONE to produce parsed stop, got %#v", result)
	}
}

func TestParseSDAIContentLineIgnoresUnknown(t *testing.T) {
	result := ParseSDAIContentLine([]byte(`data: {"format": "STREAM"}`), true, "text")
	if result.Parsed {
		t.Fatal("expected unknown cate payload to be ignored")
	}
	if result.NextType != "text" {
		t.Fatalf("expected current type preserved, got %q", result.NextType)
	}
}

func TestParseSDAIContentLineEmptyContent(t *testing.T) {
	result := ParseSDAIContentLine([]byte(`data: {"choices":[{"index":0,"delta":{"content":"","type":"text"}}]}`), true, "text")
	if len(result.Parts) != 0 {
		t.Fatalf("expected no parts for empty content, got %#v", result.Parts)
	}
}

func TestCollectStreamSDAI(t *testing.T) {
	sseBody := strings.Join([]string{
		`event: message`,
		`data: {"choices":[{"index":0,"delta":{"content":"好的，我们","type":"think"}}]}`,
		``,
		`event: message`,
		`data: {"choices":[{"index":0,"delta":{"content":"首先","type":"think"}}]}`,
		``,
		`event: message`,
		`data: {"choices":[{"index":0,"delta":{"content":"9.9 更大。","type":"text"}}]}`,
		``,
		`event: finish`,
		`data: {"req_message_pk_id": 13864, "mtid": 3061}`,
		``,
		`event: flag`,
		`data: DONE`,
		``,
	}, "\n")
	resp := makeSSEResponse(sseBody)
	result := CollectStream(resp, true, true)
	if result.Thinking != "好的，我们首先" {
		t.Fatalf("unexpected thinking: %q", result.Thinking)
	}
	if result.Text != "9.9 更大。" {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if result.ToolDetectionThinking != result.Thinking {
		t.Fatalf("expected detection thinking to mirror thinking")
	}
	if result.ResponseMessageID != 13864 {
		t.Fatalf("expected ResponseMessageID 13864, got %d", result.ResponseMessageID)
	}
	if result.ContentFilter {
		t.Fatal("SDAI has no content filter signal")
	}
	if result.CitationLinks != nil {
		t.Fatal("SDAI has no citation links")
	}
}
