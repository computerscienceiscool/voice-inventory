package asr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

const fakeWhisperJSON = `{
  "result": {"language": "en"},
  "transcription": [
    {
      "text": " Twelve boxes of RJ45 connectors in bin A-14.",
      "offsets": {"from": 0, "to": 3000},
      "tokens": [
        {"text": "[_BEG_]", "p": 0.99, "offsets": {"from": 0, "to": 0}},
        {"text": " Twelve", "p": 0.95, "offsets": {"from": 0, "to": 400}},
        {"text": " boxes", "p": 0.90, "offsets": {"from": 400, "to": 800}},
        {"text": " of", "p": 0.99, "offsets": {"from": 800, "to": 900}},
        {"text": " RJ", "p": 0.80, "offsets": {"from": 900, "to": 1100}},
        {"text": "45", "p": 0.85, "offsets": {"from": 1100, "to": 1300}}
      ]
    }
  ]
}`

// writeFakeWhisper creates a shell script that mimics whisper.cpp's CLI:
// it finds the -of argument and writes canned JSON to <prefix>.json.
func writeFakeWhisper(t *testing.T, dir, payload string, exitCode int) string {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
prev=""
of=""
for a in "$@"; do
  if [ "$prev" = "-of" ]; then of="$a"; fi
  prev="$a"
done
if [ %d -ne 0 ]; then echo "boom" >&2; exit %d; fi
cat > "$of.json" <<'EOF'
%s
EOF
`, exitCode, exitCode, payload)
	path := filepath.Join(dir, "fake-whisper")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExecTranscriber(t *testing.T) {
	dir := t.TempDir()
	bin := writeFakeWhisper(t, dir, fakeWhisperJSON, 0)
	model := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(model, []byte("weights"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := NewExec(bin)
	e.TempDir = dir
	if err := e.LoadModel(model, ModelOpts{Threads: 2}); err != nil {
		t.Fatal(err)
	}
	pcm := make([]float32, 16000)
	res, err := e.Transcribe(context.Background(), pcm, LangEnglish)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Twelve boxes of RJ45 connectors in bin A-14." {
		t.Errorf("text = %q", res.Text)
	}
	if res.Language != "en" {
		t.Errorf("language = %q", res.Language)
	}
	if len(res.Tokens) != 5 {
		t.Errorf("tokens = %d, want 5 (special filtered)", len(res.Tokens))
	}
	want := (0.95 + 0.90 + 0.99 + 0.80 + 0.85) / 5
	if diff := res.Confidence - want; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("confidence = %v, want %v", res.Confidence, want)
	}
	if res.Tokens[0].EndMS != 400 {
		t.Errorf("token timing wrong: %+v", res.Tokens[0])
	}
}

func TestExecTranscriberErrors(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "model.bin")
	_ = os.WriteFile(model, []byte("w"), 0o644)

	e := NewExec(writeFakeWhisper(t, dir, "", 3))
	e.TempDir = dir
	if _, err := e.Transcribe(context.Background(), make([]float32, 100), LangAuto); err != ErrNoModel {
		t.Errorf("expected ErrNoModel, got %v", err)
	}
	if err := e.LoadModel(model, ModelOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Transcribe(context.Background(), nil, LangAuto); err == nil {
		t.Error("empty buffer should error")
	}
	if _, err := e.Transcribe(context.Background(), make([]float32, 100), LangAuto); err == nil {
		t.Error("nonzero exit should error")
	}
	if err := e.LoadModel(filepath.Join(dir, "missing.bin"), ModelOpts{}); err == nil {
		t.Error("missing model should error")
	}
}

func TestMock(t *testing.T) {
	m := &Mock{Results: []Result{
		TextResult("first", "en", 0.9),
		TextResult("second", "en", 0.8),
	}}
	r1, _ := m.Transcribe(context.Background(), []float32{1}, LangEnglish)
	r2, _ := m.Transcribe(context.Background(), []float32{2}, LangEnglish)
	r3, _ := m.Transcribe(context.Background(), []float32{3}, LangEnglish)
	if r1.Text != "first" || r2.Text != "second" || r3.Text != "second" {
		t.Errorf("mock sequencing wrong: %q %q %q", r1.Text, r2.Text, r3.Text)
	}
	if len(m.Calls) != 3 {
		t.Errorf("calls = %d", len(m.Calls))
	}
}

func TestEnsureModel(t *testing.T) {
	payload := []byte("pretend these are ggml weights")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	spec := ModelSpec{Name: "ggml-test.bin", URL: srv.URL, SHA256: digest}

	var sawProgress bool
	path, err := EnsureModel(context.Background(), dir, spec, func(got, total int64) {
		sawProgress = true
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawProgress {
		t.Error("progress callback never fired")
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(payload) {
		t.Error("cached content mismatch")
	}

	// second call: cache hit, no new download
	if _, err := EnsureModel(context.Background(), dir, spec, nil); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Errorf("expected 1 download, got %d", hits.Load())
	}

	// corrupt the cache → refetch
	if err := os.WriteFile(path, []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureModel(context.Background(), dir, spec, nil); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Errorf("expected refetch after corruption, got %d hits", hits.Load())
	}

	// wrong digest from server → error, nothing cached
	bad := spec
	bad.Name = "bad.bin"
	bad.SHA256 = "00000000000000000000000000000000000000000000000000000000deadbeef"
	if _, err := EnsureModel(context.Background(), dir, bad, nil); err == nil {
		t.Error("checksum mismatch should fail")
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.bin")); !os.IsNotExist(err) {
		t.Error("failed download must not leave a model file")
	}

	// missing cache + no URL → clear error
	if _, err := EnsureModel(context.Background(), dir, ModelSpec{Name: "nourl.bin"}, nil); err == nil {
		t.Error("no URL should fail")
	}

	// download without a checksum is refused unless explicitly allowed
	noSum := ModelSpec{Name: "nosum.bin", URL: srv.URL}
	if _, err := EnsureModel(context.Background(), dir, noSum, nil); err == nil {
		t.Error("unverified download should be refused by default")
	}
	noSum.AllowUnverified = true
	if _, err := EnsureModel(context.Background(), dir, noSum, nil); err != nil {
		t.Errorf("AllowUnverified should permit the fetch: %v", err)
	}
}

// Concurrent EnsureModel calls must share one download and never remove a
// verified install (the download is verified before the rename).
func TestEnsureModelConcurrent(t *testing.T) {
	payload := []byte("concurrent ggml weights")
	sum := sha256.Sum256(payload)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	dir := t.TempDir()
	spec := ModelSpec{Name: "conc.bin", URL: srv.URL, SHA256: hex.EncodeToString(sum[:])}

	const n = 6
	errs := make(chan error, n)
	paths := make(chan string, n)
	for i := 0; i < n; i++ {
		go func() {
			p, err := EnsureModel(context.Background(), dir, spec, nil)
			paths <- p
			errs <- err
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("goroutine error: %v", err)
		}
		if p := <-paths; p != "" {
			if _, err := os.Stat(p); err != nil {
				t.Errorf("returned path missing: %v", err)
			}
		}
	}
	if hits.Load() != 1 {
		t.Errorf("downloads = %d, want 1 (serialized)", hits.Load())
	}
}
