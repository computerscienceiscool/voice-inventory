package asr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/computerscienceiscool/voice-inventory/audio"
)

// ExecTranscriber runs the whisper.cpp CLI (whisper-cli / main) as a
// subprocess and parses its JSON output. It is the desktop/CI
// implementation of Transcriber, used by the golden-audio suite (§15) and
// the vinv command-line tool; mobile builds link whisper.cpp natively.
type ExecTranscriber struct {
	// Bin is the whisper.cpp CLI binary.
	Bin string
	// TempDir overrides the scratch directory (defaults to os.TempDir()).
	TempDir string

	mu        sync.Mutex
	modelPath string
	threads   int
	extraArgs []string
}

// NewExec creates an ExecTranscriber for the given whisper.cpp binary.
func NewExec(bin string) *ExecTranscriber {
	return &ExecTranscriber{Bin: bin}
}

// LoadModel verifies the weights file exists and remembers it.
func (e *ExecTranscriber) LoadModel(path string, opts ModelOpts) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("asr: model weights: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("asr: model path %q is a directory", path)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.modelPath = path
	e.threads = opts.Threads
	e.extraArgs = append([]string(nil), opts.ExtraArgs...)
	return nil
}

// Close releases nothing for the subprocess runner.
func (e *ExecTranscriber) Close() error { return nil }

// Transcribe writes the PCM to a temp WAV, runs whisper.cpp with JSON
// output, and parses the result.
func (e *ExecTranscriber) Transcribe(ctx context.Context, pcm []float32, lang Lang) (Result, error) {
	e.mu.Lock()
	model := e.modelPath
	threads := e.threads
	extra := append([]string(nil), e.extraArgs...)
	e.mu.Unlock()
	if model == "" {
		return Result{}, ErrNoModel
	}
	if len(pcm) == 0 {
		return Result{}, errors.New("asr: empty audio buffer")
	}

	dir := e.TempDir
	if dir == "" {
		dir = os.TempDir()
	}
	tmp, err := os.MkdirTemp(dir, "vinv-asr-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(tmp)

	wavPath := filepath.Join(tmp, "utterance.wav")
	wf, err := os.Create(wavPath)
	if err != nil {
		return Result{}, err
	}
	if err := audio.EncodeWAV16(wf, pcm, audio.WhisperRate); err != nil {
		_ = wf.Close()
		return Result{}, err
	}
	if err := wf.Close(); err != nil {
		return Result{}, err
	}

	outPrefix := filepath.Join(tmp, "out")
	args := []string{
		"-m", model,
		"-f", wavPath,
		"-oj",
		"-of", outPrefix,
		"-np",
	}
	if lang == "" {
		lang = LangAuto
	}
	args = append(args, "-l", string(lang))
	if threads > 0 {
		args = append(args, "-t", strconv.Itoa(threads))
	}
	args = append(args, extra...)

	cmd := exec.CommandContext(ctx, e.Bin, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("asr: whisper.cpp failed: %w (%s)",
			err, strings.TrimSpace(tail(stderr.String(), 400)))
	}

	data, err := os.ReadFile(outPrefix + ".json")
	if err != nil {
		return Result{}, fmt.Errorf("asr: whisper.cpp produced no JSON: %w", err)
	}
	return ParseWhisperJSON(data)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// whisperJSON mirrors whisper.cpp's -oj output.
type whisperJSON struct {
	Result struct {
		Language string `json:"language"`
	} `json:"result"`
	Transcription []struct {
		Text    string `json:"text"`
		Offsets struct {
			From int `json:"from"`
			To   int `json:"to"`
		} `json:"offsets"`
		Tokens []struct {
			Text    string  `json:"text"`
			P       float64 `json:"p"`
			Offsets struct {
				From int `json:"from"`
				To   int `json:"to"`
			} `json:"offsets"`
		} `json:"tokens"`
	} `json:"transcription"`
}

// ParseWhisperJSON parses whisper.cpp's -oj JSON output into a Result.
// Exported for native-shell bridges that run whisper.cpp themselves and
// hand back its JSON.
func ParseWhisperJSON(data []byte) (Result, error) {
	var wj whisperJSON
	if err := json.Unmarshal(data, &wj); err != nil {
		return Result{}, fmt.Errorf("asr: parse whisper JSON: %w", err)
	}
	var res Result
	res.Language = wj.Result.Language
	var textParts []string
	var confSum float64
	for _, seg := range wj.Transcription {
		textParts = append(textParts, seg.Text)
		for _, tok := range seg.Tokens {
			if isSpecialToken(tok.Text) {
				continue
			}
			res.Tokens = append(res.Tokens, Token{
				Text:       tok.Text,
				StartMS:    tok.Offsets.From,
				EndMS:      tok.Offsets.To,
				Confidence: clamp01(tok.P),
			})
			confSum += clamp01(tok.P)
		}
	}
	res.Text = strings.TrimSpace(strings.Join(textParts, ""))
	if n := len(res.Tokens); n > 0 {
		res.Confidence = confSum / float64(n)
	}
	return res, nil
}

// isSpecialToken filters whisper.cpp markers like "[_BEG_]" or
// "<|endoftext|>".
func isSpecialToken(text string) bool {
	t := strings.TrimSpace(text)
	return strings.HasPrefix(t, "[_") || strings.HasPrefix(t, "<|")
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
