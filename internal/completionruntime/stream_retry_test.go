package completionruntime

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"ds2api/internal/auth"
	"ds2api/internal/httpapi/openai/shared"
)

func TestExecuteStreamWithRetryUsesSharedRetryPayloadAndUsagePrompt(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{
		sseHTTPResponse(http.StatusOK, sdaiTextDelta("ok"), "data: DONE"),
	}}
	initial := sseHTTPResponse(http.StatusOK, sdaiFinish(77), sdaiThinkDelta("plan"), "data: DONE")
	payload := map[string]any{"content": "original prompt", "uuid": "session-1"}
	attemptsSeen := 0
	retryPrompt := ""

	ExecuteStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, initial, payload, StreamRetryOptions{
		Surface:      "test.stream",
		Stream:       true,
		RetryEnabled: true,
		UsagePrompt:  "original prompt",
	}, StreamRetryHooks{
		ConsumeAttempt: func(resp *http.Response, allowDeferEmpty bool) (bool, bool) {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Fatalf("close failed: %v", err)
				}
			}()
			_, _ = io.ReadAll(resp.Body)
			attemptsSeen++
			return attemptsSeen == 2, attemptsSeen == 1 && allowDeferEmpty
		},
		ParentMessageID: func() int {
			return 77
		},
		OnRetryPrompt: func(prompt string) {
			retryPrompt = prompt
		},
	})

	if attemptsSeen != 2 {
		t.Fatalf("expected two stream attempts, got %d", attemptsSeen)
	}
	if len(ds.payloads) != 1 {
		t.Fatalf("expected one retry completion call, got %d", len(ds.payloads))
	}
	if _, has := ds.payloads[0]["parent_message_id"]; has {
		t.Fatal("expected no parent_message_id in SDAI retry payload")
	}
	if content, _ := ds.payloads[0]["content"].(string); !strings.Contains(content, shared.EmptyOutputRetrySuffix) {
		t.Fatalf("expected retry suffix in payload content, got %q", content)
	}
	if !strings.Contains(retryPrompt, shared.EmptyOutputRetrySuffix) {
		t.Fatalf("expected retry suffix in usage prompt, got %q", retryPrompt)
	}
}

func TestExecuteStreamWithRetrySwitchesManagedAccountBeforeFinal429(t *testing.T) {
	isolateTestConfig(t)
	t.Setenv("DS2API_CONFIG_JSON", sdaiTestAccountsConfig())
	a := sdaiManagedAuth(t)

	ds := &fakeDeepSeekCaller{
		sessionByAccount: true,
		responses: []*http.Response{
			sseHTTPResponse(http.StatusOK, sdaiFinish(12), sdaiThinkDelta("retry empty"), "data: DONE"),
			sseHTTPResponse(http.StatusOK, sdaiFinish(21), sdaiTextDelta("ok from second account"), "data: DONE"),
		},
	}
	initial := sseHTTPResponse(http.StatusOK, sdaiFinish(11), sdaiThinkDelta("first empty"), "data: DONE")
	payload := map[string]any{"content": "original prompt", "uuid": "session-acc1@test.com"}
	attemptsSeen := 0
	switchedSession := ""

	ExecuteStreamWithRetry(context.Background(), ds, a, initial, payload, StreamRetryOptions{
		Surface:          "test.stream",
		Stream:           true,
		RetryEnabled:     true,
		RetryMaxAttempts: 1,
		UsagePrompt:      "original prompt",
	}, StreamRetryHooks{
		ConsumeAttempt: func(resp *http.Response, allowDeferEmpty bool) (bool, bool) {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Fatalf("close failed: %v", err)
				}
			}()
			body, _ := io.ReadAll(resp.Body)
			attemptsSeen++
			if strings.Contains(string(body), "ok from second account") {
				return true, false
			}
			if !allowDeferEmpty {
				t.Fatalf("expected empty attempt %d to be deferred before final 429", attemptsSeen)
			}
			return false, true
		},
		ParentMessageID: func() int {
			return 11 + attemptsSeen
		},
		OnAccountSwitch: func(sessionID string) {
			switchedSession = sessionID
		},
	})

	if attemptsSeen != 3 {
		t.Fatalf("expected three stream attempts, got %d", attemptsSeen)
	}
	if switchedSession != "session-acc2@test.com" {
		t.Fatalf("expected switched session id, got %q", switchedSession)
	}
	if got := ds.payloads[1]["uuid"]; got != "session-acc2@test.com" {
		t.Fatalf("switched payload uuid mismatch: %#v", got)
	}
	if content, _ := ds.payloads[1]["content"].(string); strings.Contains(content, shared.EmptyOutputRetrySuffix) {
		t.Fatalf("expected switched-account content without empty-output suffix, got %q", content)
	}
}
