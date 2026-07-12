// Package mobile is the gomobile-bind facade (spec §9.2): a thin,
// bind-friendly API over the Go core, built as an Android AAR / iOS
// xcframework with:
//
//	gomobile bind -target=android ./mobile
//	gomobile bind -target=ios     ./mobile
//
// Bind-friendly means: only strings, numbers, bools, []byte, errors, and
// small interfaces cross the boundary; structured data travels as JSON.
// The native shell implements Events (UI callbacks) and, on device,
// Transcriber (whisper.cpp via JNI/ObjC returning whisper.cpp JSON).
package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/computerscienceiscool/voice-inventory/asr"
	"github.com/computerscienceiscool/voice-inventory/audio"
	"github.com/computerscienceiscool/voice-inventory/config"
	"github.com/computerscienceiscool/voice-inventory/export"
	"github.com/computerscienceiscool/voice-inventory/observation"
	"github.com/computerscienceiscool/voice-inventory/session"
	"github.com/computerscienceiscool/voice-inventory/store"
	"github.com/computerscienceiscool/voice-inventory/syncer"
)

// Events is implemented by the native shell to receive session events.
// OnSaved is the cue for the audible + haptic save confirmation (§4.3).
type Events interface {
	OnState(state string)
	OnLevel(rms float64)
	OnSpeechStart()
	OnReadback(readbackJSON string)
	OnSaved(id string, status string)
	OnDiscarded(id string)
	OnError(message string)
	OnSuggestion(message string)
}

// Transcriber is implemented by the native shell around whisper.cpp: it
// receives 16 kHz mono WAV bytes and returns whisper.cpp -oj JSON.
type Transcriber interface {
	TranscribeWAV(wav []byte, lang string) (resultJSON string, err error)
}

// App is the bound application core. The session (a.s) and store (a.st)
// are created once in NewApp and never reassigned, so delegating methods
// may read them without the mutex; a.cfg and a.sync are guarded by a.mu.
type App struct {
	mu   sync.Mutex
	cfg  config.Config
	st   *store.Store
	s    *session.Session
	hold *swapTranscriber
	sync syncer.Syncer

	dataDir string
}

// NewApp opens the store under dataDir, applies the config JSON (empty
// string = defaults merged with any saved device profile), and builds the
// capture session. Events may be nil.
func NewApp(dataDir string, configJSON string, events Events) (*App, error) {
	cfgPath := filepath.Join(dataDir, "config.json")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, fmt.Errorf("mobile: config json: %w", err)
		}
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
	}
	st, err := store.Open(filepath.Join(dataDir, "observations.db"))
	if err != nil {
		return nil, err
	}
	hold := &swapTranscriber{}
	deps := session.Deps{
		Store:       st,
		Transcriber: hold,
		AudioDir:    filepath.Join(dataDir, "audio"),
	}
	if events != nil {
		deps.Listener = &listenerBridge{ev: events}
	}
	s, err := session.New(cfg, deps)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	return &App{cfg: cfg, st: st, s: s, hold: hold, dataDir: dataDir}, nil
}

// SetTranscriber installs the native whisper.cpp bridge.
func (a *App) SetTranscriber(t Transcriber) {
	a.hold.set(&bridgeTranscriber{impl: t})
}

// SetExecTranscriber uses the whisper.cpp CLI at binPath with the given
// weights — the desktop/testing path.
func (a *App) SetExecTranscriber(binPath, modelPath string, threads int) error {
	e := asr.NewExec(binPath)
	if err := e.LoadModel(modelPath, asr.ModelOpts{Threads: threads}); err != nil {
		return err
	}
	a.hold.set(e)
	return nil
}

// SaveConfig persists the active device profile.
func (a *App) SaveConfig() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Save(filepath.Join(a.dataDir, "config.json"))
}

// ConfigJSON returns the active device profile (§14) for a settings screen.
func (a *App) ConfigJSON() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return marshal(a.cfg)
}

// SetConfigJSON merges the given JSON over the active profile, validates,
// and persists it. The operator id applies immediately; capture-affecting
// fields (language, capture mode, thresholds, retention, sync endpoint)
// apply the next time the app starts — the session is deliberately not
// rebuilt mid-flight (see item 107). Returns an error on invalid config
// without mutating the active profile.
func (a *App) SetConfigJSON(configJSON string) error {
	// Deep-copy the active config through a JSON round-trip before merging:
	// a shallow struct copy would share slice/map backing arrays with
	// a.cfg, and json.Unmarshal reuses those in place — so a rejected merge
	// would still corrupt the live config (and race concurrent readers).
	a.mu.Lock()
	base, err := json.Marshal(a.cfg)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	var merged config.Config
	if err := json.Unmarshal(base, &merged); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(configJSON), &merged); err != nil {
		return fmt.Errorf("mobile: config json: %w", err)
	}
	if err := merged.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = merged
	a.sync = nil // endpoint/token may have changed; rebuild lazily
	a.mu.Unlock()
	a.s.SetOperator(merged.OperatorID)
	return a.SaveConfig()
}

// SetOperator records the logged-in operator id (§3). Capture state and
// any pending record are untouched — only new records pick up the id.
func (a *App) SetOperator(operatorID string) error {
	a.mu.Lock()
	a.cfg.OperatorID = operatorID
	a.mu.Unlock()
	a.s.SetOperator(operatorID)
	return a.SaveConfig()
}

// --- capture -------------------------------------------------------------

// Arm starts listening. Disarm stops.
func (a *App) Arm()    { a.s.Arm() }
func (a *App) Disarm() { a.s.Disarm() }

// State returns idle | armed | reviewing.
func (a *App) State() string { return string(a.s.State()) }

// BeginUtterance / EndUtterance bracket a push-to-talk press.
func (a *App) BeginUtterance() error { return a.s.BeginUtterance() }
func (a *App) EndUtterance()         { a.s.EndUtterance() }

// FeedPCM16 accepts little-endian 16-bit PCM from the mic.
func (a *App) FeedPCM16(data []byte, sampleRate int, channels int) {
	a.s.FeedPCM(audio.Int16ToFloat32(data), sampleRate, channels)
}

// HandleTranscript injects recognized text directly (testing, wake-word
// shells that do their own ASR).
func (a *App) HandleTranscript(text string, language string, confidence float64) {
	a.s.HandleTranscript(text, asr.Result{
		Text: text, Language: language, Confidence: confidence,
	}, nil)
}

// --- review --------------------------------------------------------------

// PendingJSON returns the record awaiting confirmation, or "".
func (a *App) PendingJSON() (string, error) {
	p := a.s.Pending()
	if p == nil {
		return "", nil
	}
	return marshal(p)
}

// Confirm saves the pending record; Scratch discards it (or the last-saved
// record when nothing is pending).
func (a *App) Confirm() error { return a.s.Confirm() }
func (a *App) Scratch()       { a.s.Scratch() }

// CorrectField sets one field of the pending record from text.
func (a *App) CorrectField(field, value string) error {
	return a.s.CorrectField(field, value)
}

// --- batch review (§4.2) ---------------------------------------------------

// ListJSON returns records as {"observations":[…]}; status filters when
// non-empty ("draft", "confirmed", "synced", "rejected").
func (a *App) ListJSON(status string, limit int) (string, error) {
	f := store.Filter{Limit: limit}
	if status != "" {
		st := observation.Status(status)
		if !st.Valid() {
			return "", fmt.Errorf("mobile: unknown status %q", status)
		}
		f.Status = st
	}
	obs, err := a.st.List(f)
	if err != nil {
		return "", err
	}
	if obs == nil {
		obs = []*observation.Observation{} // documented contract: an array
	}
	return marshal(map[string]any{"observations": obs})
}

// ListSyncRejectedJSON returns records the backend refused on push, for a
// persistent batch-review badge; each carries sync_rejected_reason.
func (a *App) ListSyncRejectedJSON(limit int) (string, error) {
	v := true
	obs, err := a.st.List(store.Filter{SyncRejected: &v, Limit: limit})
	if err != nil {
		return "", err
	}
	if obs == nil {
		obs = []*observation.Observation{}
	}
	return marshal(map[string]any{"observations": obs})
}

// ExportCSV returns the queue as CSV for the platform share sheet
// (docs/proposals.md item 072); status filters when non-empty. The shell
// writes the string to a temp file and hands it to the share intent.
func (a *App) ExportCSV(status string) (string, error) {
	f := store.Filter{}
	if status != "" {
		st := observation.Status(status)
		if !st.Valid() {
			return "", fmt.Errorf("mobile: unknown status %q", status)
		}
		f.Status = st
	}
	obs, err := a.st.List(f)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := export.CSV(&b, obs); err != nil {
		return "", err
	}
	return b.String(), nil
}

// ConfirmRecord / RejectRecord act on any queued record.
func (a *App) ConfirmRecord(id string) error { return a.st.Confirm(id) }
func (a *App) RejectRecord(id string) error  { return a.st.Reject(id) }

// EditRecord sets one field of any draft/confirmed record from text.
func (a *App) EditRecord(id, field, value string) error {
	return a.s.EditRecord(id, field, value)
}

// AddManual records a typed observation (mic-denied fallback, §13).
// parsedJSON is an observation "parsed" object.
func (a *App) AddManual(parsedJSON string, confirm bool) (string, error) {
	var p observation.Parsed
	if err := json.Unmarshal([]byte(parsedJSON), &p); err != nil {
		return "", fmt.Errorf("mobile: parsed json: %w", err)
	}
	a.mu.Lock()
	langCode := a.cfg.Language
	a.mu.Unlock()
	return a.s.AddManual(p, langCode, confirm)
}

// --- sync ------------------------------------------------------------------

func (a *App) ensureSyncer() (syncer.Syncer, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sync != nil {
		return a.sync, nil
	}
	if a.cfg.Sync.Endpoint == "" {
		return nil, errors.New("mobile: no sync endpoint configured")
	}
	h, err := syncer.NewHTTP(a.st, syncer.Options{
		BaseURL:       a.cfg.Sync.Endpoint,
		Token:         a.cfg.Sync.Token,
		DeviceID:      a.cfg.DeviceID,
		BatchSize:     a.cfg.Sync.BatchSize,
		AllowInsecure: a.cfg.Sync.AllowInsecure,
	})
	if err != nil {
		return nil, err
	}
	a.sync = h
	return h, nil
}

// SyncPush uploads confirmed records; returns a JSON report.
func (a *App) SyncPush() (string, error) {
	sy, err := a.ensureSyncer()
	if err != nil {
		return "", err
	}
	report, err := sy.Push(context.Background())
	if err != nil {
		return "", err
	}
	return marshal(report)
}

// SyncPull refreshes reference data and reloads the resolvers.
func (a *App) SyncPull() (string, error) {
	sy, err := a.ensureSyncer()
	if err != nil {
		return "", err
	}
	report, err := sy.PullRefData(context.Background())
	if err != nil {
		return "", err
	}
	if err := a.s.RefreshRefData(); err != nil {
		return "", err
	}
	return marshal(report)
}

// --- maintenance -----------------------------------------------------------

// PurgeAudio applies the retention policy; returns clips removed.
func (a *App) PurgeAudio() (int, error) { return a.s.PurgeAudio() }

// StatsJSON reports latency and queue counts.
func (a *App) StatsJSON() (string, error) {
	st, err := a.s.Stats()
	if err != nil {
		return "", err
	}
	return marshal(st)
}

// Close releases the store.
func (a *App) Close() error { return a.st.Close() }

func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// --- bridges ---------------------------------------------------------------

// swapTranscriber lets the shell install the engine after construction.
type swapTranscriber struct {
	mu    sync.Mutex
	inner asr.Transcriber
}

func (s *swapTranscriber) set(t asr.Transcriber) {
	s.mu.Lock()
	s.inner = t
	s.mu.Unlock()
}

func (s *swapTranscriber) get() asr.Transcriber {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner
}

func (s *swapTranscriber) Transcribe(ctx context.Context, pcm []float32, lang asr.Lang) (asr.Result, error) {
	t := s.get()
	if t == nil {
		return asr.Result{}, errors.New("mobile: no transcriber configured (model not ready)")
	}
	return t.Transcribe(ctx, pcm, lang)
}

func (s *swapTranscriber) LoadModel(path string, opts asr.ModelOpts) error {
	t := s.get()
	if t == nil {
		return errors.New("mobile: no transcriber configured")
	}
	return t.LoadModel(path, opts)
}

func (s *swapTranscriber) Close() error {
	t := s.get()
	if t == nil {
		return nil
	}
	return t.Close()
}

// bridgeTranscriber adapts the native Transcriber (WAV in, whisper JSON
// out) to asr.Transcriber.
type bridgeTranscriber struct {
	impl Transcriber
}

func (b *bridgeTranscriber) Transcribe(_ context.Context, pcm []float32, lang asr.Lang) (asr.Result, error) {
	var buf wavBuffer
	if err := audio.EncodeWAV16(&buf, pcm, audio.WhisperRate); err != nil {
		return asr.Result{}, err
	}
	out, err := b.impl.TranscribeWAV(buf.b, string(lang))
	if err != nil {
		return asr.Result{}, err
	}
	return asr.ParseWhisperJSON([]byte(out))
}

func (b *bridgeTranscriber) LoadModel(string, asr.ModelOpts) error { return nil }
func (b *bridgeTranscriber) Close() error                          { return nil }

type wavBuffer struct{ b []byte }

func (w *wavBuffer) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}

// listenerBridge adapts session events to the bound Events interface.
type listenerBridge struct {
	ev Events
}

func (l *listenerBridge) OnState(s session.State) { l.ev.OnState(string(s)) }
func (l *listenerBridge) OnLevel(rms float64)     { l.ev.OnLevel(rms) }
func (l *listenerBridge) OnSpeechStart()          { l.ev.OnSpeechStart() }
func (l *listenerBridge) OnReadback(rb session.Readback) {
	b, err := json.Marshal(map[string]any{
		"observation":    rb.Observation,
		"doubtful":       rb.Doubtful,
		"text":           rb.Text,
		"auto_confirmed": rb.AutoConfirmed,
	})
	if err != nil {
		l.ev.OnError("readback encode: " + err.Error())
		return
	}
	l.ev.OnReadback(string(b))
}
func (l *listenerBridge) OnSaved(id string, st observation.Status) { l.ev.OnSaved(id, string(st)) }
func (l *listenerBridge) OnDiscarded(id string)                    { l.ev.OnDiscarded(id) }
func (l *listenerBridge) OnError(msg string)                       { l.ev.OnError(msg) }
func (l *listenerBridge) OnSuggestion(msg string)                  { l.ev.OnSuggestion(msg) }
