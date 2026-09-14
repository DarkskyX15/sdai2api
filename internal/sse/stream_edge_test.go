package sse

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStartParsedLinePumpEmptyBody(t *testing.T) {
	body := strings.NewReader("")
	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	collected := make([]LineResult, 0)
	for r := range results {
		collected = append(collected, r)
	}
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(collected) != 0 {
		t.Fatalf("expected no results for empty body, got %d", len(collected))
	}
}

func TestStartParsedLinePumpTypeTracking(t *testing.T) {
	body := strings.NewReader(
		sdaiDeltaLine(t, "思", "think") +
			sdaiDeltaLine(t, "考", "think") +
			sdaiDeltaLine(t, "答", "text") +
			sdaiDeltaLine(t, "案", "text") +
			"event: finish\ndata: {\"req_message_pk_id\": 42}\n" +
			"event: flag\ndata: DONE\n",
	)
	results, done := StartParsedLinePump(context.Background(), body, true, "thinking")

	types := make([]string, 0)
	var sawMessageID int
	for r := range results {
		if r.ResponseMessageID > 0 {
			sawMessageID = r.ResponseMessageID
		}
		for _, p := range r.Parts {
			types = append(types, p.Type)
		}
	}
	<-done

	if len(types) == 0 {
		t.Fatal("expected some parts, got none")
	}
	hasThinking := false
	hasText := false
	for _, tp := range types {
		if tp == "thinking" {
			hasThinking = true
		}
		if tp == "text" {
			hasText = true
		}
	}
	if !hasThinking {
		t.Fatalf("expected thinking type in results, got %v", types)
	}
	if !hasText {
		t.Fatalf("expected text type in results, got %v", types)
	}
	if sawMessageID != 42 {
		t.Fatalf("expected finish event message id 42, got %d", sawMessageID)
	}
}

func TestStartParsedLinePumpContextCancellation(t *testing.T) {
	pr, pw := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	results, done := StartParsedLinePump(ctx, pr, false, "text")

	go func() {
		_, _ = io.WriteString(pw, sdaiDeltaLine(t, "hello", "text"))
		time.Sleep(50 * time.Millisecond)
		_ = pw.Close()
	}()

	r := <-results
	if !r.Parsed || len(r.Parts) == 0 {
		t.Fatalf("expected first parsed result, got %#v", r)
	}

	cancel()

	for range results {
	}

	err := <-done
	if err != nil && err != context.Canceled {
		t.Fatalf("expected context.Canceled or nil error, got %v", err)
	}
}

func TestStartParsedLinePumpOnlyDONE(t *testing.T) {
	body := strings.NewReader("event: flag\ndata: DONE\n")
	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	collected := make([]LineResult, 0)
	for r := range results {
		collected = append(collected, r)
	}
	<-done

	if len(collected) != 1 {
		t.Fatalf("expected 1 result, got %d", len(collected))
	}
	if !collected[0].Stop {
		t.Fatal("expected stop on DONE")
	}
}

func TestStartParsedLinePumpUnknownEventsIgnored(t *testing.T) {
	body := strings.NewReader(
		"event: cate\n" +
			"data: {\"format\": \"STREAM\"}\n" +
			"event: message\n" +
			sdaiDeltaLine(t, "valid", "text") +
			"event: flag\n" +
			"data: DONE\n",
	)
	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	var validCount int
	for r := range results {
		if r.Parsed && len(r.Parts) > 0 {
			validCount++
		}
	}
	<-done

	if validCount != 1 {
		t.Fatalf("expected 1 valid result, got %d", validCount)
	}
}

func TestStartParsedLinePumpThinkingDisabled(t *testing.T) {
	body := strings.NewReader(
		sdaiDeltaLine(t, "思", "think") +
			sdaiDeltaLine(t, "答", "text") +
			"data: DONE\n",
	)
	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	var parts []ContentPart
	for r := range results {
		parts = append(parts, r.Parts...)
	}
	<-done

	got := strings.Builder{}
	for _, p := range parts {
		if p.Type != "text" {
			t.Fatalf("expected only text parts with thinking disabled, got %#v", parts)
		}
		got.WriteString(p.Text)
	}
	if got.String() != "答" {
		t.Fatalf("expected hidden thinking to be dropped, got %q from %#v", got.String(), parts)
	}
}

func TestStartParsedLinePumpAccumulatesSmallChunks(t *testing.T) {
	body := strings.NewReader(
		sdaiDeltaLine(t, "h", "text") +
			sdaiDeltaLine(t, "i", "text") +
			"data: DONE\n",
	)

	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	collected := make([]LineResult, 0)
	for r := range results {
		collected = append(collected, r)
	}
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	last := collected[len(collected)-1]
	if !last.Stop {
		t.Fatal("expected last result to stop")
	}

	allText := strings.Builder{}
	for _, r := range collected {
		for _, p := range r.Parts {
			allText.WriteString(p.Text)
		}
	}
	if allText.String() != "hi" {
		t.Fatalf("expected accumulated text 'hi', got %q", allText.String())
	}
}
