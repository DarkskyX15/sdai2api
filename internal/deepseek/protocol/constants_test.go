package protocol

import (
	"strings"
	"testing"
)

func TestSDAIBaseHeaders(t *testing.T) {
	if BaseHeaders["Content-Type"] != "application/json" {
		t.Fatalf("unexpected Content-Type header: %q", BaseHeaders["Content-Type"])
	}
	if BaseHeaders["Referer"] != SDAIChatStartReferer {
		t.Fatalf("unexpected Referer header: %q", BaseHeaders["Referer"])
	}
	if !strings.Contains(BaseHeaders["User-Agent"], "Mozilla/5.0") {
		t.Fatalf("unexpected User-Agent header: %q", BaseHeaders["User-Agent"])
	}
}

func TestSDAIEndpoints(t *testing.T) {
	cases := map[string]string{
		SDAIChatStartURL:    "/chat/start",
		SDAIMsgTitleDelURL:  "/msg_title/del",
		SDAIMsgTitleListURL: "/msg_title/list",
		SDAIModelListURL:    "/model/list",
	}
	for url, suffix := range cases {
		if !strings.HasSuffix(url, suffix) {
			t.Fatalf("unexpected endpoint %q, want suffix %q", url, suffix)
		}
		if !strings.HasPrefix(url, SDAIBaseURL) {
			t.Fatalf("endpoint %q must live under base url %q", url, SDAIBaseURL)
		}
	}
}
