// Package asr defines the speech-to-text seam (spec §8.1): a Transcriber
// interface the rest of the app depends on, with a whisper.cpp CLI
// implementation for desktop/CI use and a scriptable mock for tests. The
// mobile builds bind whisper.cpp natively behind this same interface.
package asr

import (
	"context"
	"errors"
	"sync"
)

// Lang selects the transcription language.
type Lang string

const (
	LangEnglish Lang = "en"
	LangSpanish Lang = "es"
	LangAuto    Lang = "auto"
)

// Token is one recognized token with timing and confidence.
type Token struct {
	Text       string
	StartMS    int
	EndMS      int
	Confidence float64 // probability in [0,1]
}

// Result is a completed transcription.
type Result struct {
	Text       string
	Language   string  // language actually used/detected
	Confidence float64 // mean token probability in [0,1]
	Tokens     []Token
}

// ModelOpts tunes model loading.
type ModelOpts struct {
	Threads   int      // 0 = runtime default
	ExtraArgs []string // implementation-specific flags
}

// Transcriber converts 16 kHz mono PCM float32 into text (spec §8.1).
type Transcriber interface {
	// Transcribe returns text plus token-level timing/confidence for a mono
	// 16 kHz PCM buffer.
	Transcribe(ctx context.Context, pcm []float32, lang Lang) (Result, error)
	// LoadModel prepares the engine with the given weights.
	LoadModel(path string, opts ModelOpts) error
	// Close releases engine resources.
	Close() error
}

// ErrNoModel is returned when Transcribe is called before LoadModel.
var ErrNoModel = errors.New("asr: no model loaded")

// Mock is a scriptable Transcriber for tests: it returns queued results in
// order (repeating the last one) and records every call.
type Mock struct {
	mu      sync.Mutex
	Results []Result
	Err     error
	next    int

	Calls       [][]float32
	CallLangs   []Lang
	LoadedModel string
	Closed      bool
}

// Transcribe pops the next scripted result.
func (m *Mock) Transcribe(_ context.Context, pcm []float32, lang Lang) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]float32, len(pcm))
	copy(cp, pcm)
	m.Calls = append(m.Calls, cp)
	m.CallLangs = append(m.CallLangs, lang)
	if m.Err != nil {
		return Result{}, m.Err
	}
	if len(m.Results) == 0 {
		return Result{}, nil
	}
	r := m.Results[m.next]
	if m.next < len(m.Results)-1 {
		m.next++
	}
	return r, nil
}

// LoadModel records the model path.
func (m *Mock) LoadModel(path string, _ ModelOpts) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LoadedModel = path
	return nil
}

// Close marks the mock closed.
func (m *Mock) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Closed = true
	return nil
}

// TextResult builds a plausible Result from plain text with uniform token
// confidence — convenient for tests.
func TextResult(text string, lang string, conf float64) Result {
	return Result{Text: text, Language: lang, Confidence: conf}
}
