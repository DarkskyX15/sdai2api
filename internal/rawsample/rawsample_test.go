package rawsample

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeSampleID(t *testing.T) {
	got := NormalizeSampleID("  Hello, World!  ")
	if got != "hello-world" {
		t.Fatalf("expected hello-world, got %q", got)
	}
}

func TestPersistWritesSampleFilesAndMeta(t *testing.T) {
	root := t.TempDir()
	saved, err := Persist(PersistOptions{
		RootDir:  root,
		SampleID: "My Sample! 01",
		Source:   "unit-test",
		Request: map[string]any{
			"model":  "deepseek-v4-flash",
			"stream": true,
			"messages": []any{
				map[string]any{"role": "user", "content": "广州天气"},
			},
		},
		Capture: CaptureSummary{
			Label:      "sdai_chat_start",
			URL:        "https://sdai.suda.edu.cn/backend/api/chat/start",
			StatusCode: 200,
		},
		UpstreamBody: []byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\",\"type\":\"text\"}}]}\n\n" +
			"data: DONE\n\n"),
	})
	if err != nil {
		t.Fatalf("Persist failed: %v", err)
	}

	if saved.SampleID != "my-sample-01" {
		t.Fatalf("expected normalized sample id, got %q", saved.SampleID)
	}
	if _, err := os.Stat(saved.Dir); err != nil {
		t.Fatalf("sample dir missing: %v", err)
	}
	if _, err := os.Stat(saved.UpstreamPath); err != nil {
		t.Fatalf("upstream file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(saved.Dir, "openai.stream.sse")); !os.IsNotExist(err) {
		t.Fatalf("unexpected processed stream file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(saved.Dir, "openai.output.txt")); !os.IsNotExist(err) {
		t.Fatalf("unexpected processed text file: %v", err)
	}

	metaBytes, err := os.ReadFile(saved.MetaPath)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var meta Meta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if meta.SampleID != saved.SampleID {
		t.Fatalf("expected meta sample id %q, got %q", saved.SampleID, meta.SampleID)
	}
	// SDAI 流无 [reference:N] / FINISHED 标记，分析器计数应为 0（保留作历史样本诊断）。
	if meta.Capture.ReferenceMarkerCount != 0 || meta.Capture.ContainsReferenceMarkers {
		t.Fatalf("expected no reference markers in SDAI sample, got %+v", meta.Capture)
	}
	if meta.Capture.FinishedTokenCount != 0 || meta.Capture.ContainsFinishedToken {
		t.Fatalf("expected no finished token in SDAI sample, got %+v", meta.Capture)
	}
	if strings.Contains(string(metaBytes), "\"processed\"") {
		t.Fatalf("meta should not include processed payload: %s", string(metaBytes))
	}
}
