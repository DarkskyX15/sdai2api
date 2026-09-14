package client

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// ─── NewClient ───────────────────────────────────────────────────────

func TestNewClientInitialState(t *testing.T) {
	client := NewClient(nil, nil)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// ─── CreateSession（SDAI：本地 UUID 生成）────────────────────────────

func TestCreateSessionReturnsFreshUUID(t *testing.T) {
	client := NewClient(nil, nil)
	first, err := client.CreateSession(context.Background(), nil, 3)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	second, err := client.CreateSession(context.Background(), nil, 3)
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	if first == "" || second == "" {
		t.Fatal("expected non-empty session ids")
	}
	if first == second {
		t.Fatal("expected unique session ids per request")
	}
	if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("expected valid uuid, got %q: %v", first, err)
	}
}
