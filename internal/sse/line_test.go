package sse

import (
	"testing"
)

func TestLineResultContractOnUnparsedLine(t *testing.T) {
	result := ParseSDAIContentLine([]byte("event: message"), true, "thinking")
	if result.Parsed {
		t.Fatal("event-only line must not be parsed")
	}
	if result.NextType != "thinking" {
		t.Fatalf("current type must be preserved, got %q", result.NextType)
	}
	if result.Stop {
		t.Fatal("event-only line must not stop the stream")
	}
}

func TestLineResultBrokenJSONDoesNotStop(t *testing.T) {
	result := ParseSDAIContentLine([]byte("data: {broken json"), true, "text")
	if result.Parsed || result.Stop {
		t.Fatalf("broken json must be ignored, got %#v", result)
	}
}
