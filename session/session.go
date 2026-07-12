// Package session orchestrates capture (spec §4.1): arm → speak → VAD →
// transcribe → parse → readback → confirm/correct → save. It owns the
// state machine, routes voice commands during review, applies the
// confidence thresholds, writes drafts to the store the moment they parse
// (crash safety), and handles audio-clip retention.
//
// Threading: all methods are synchronous and safe for concurrent use, but
// the library starts no goroutines of its own — the shell decides what runs
// off the audio thread. Listener callbacks are delivered after internal
// locks are released, so a listener may call back into the Session.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/computerscienceiscool/voice-inventory/asr"
	"github.com/computerscienceiscool/voice-inventory/audio"
	"github.com/computerscienceiscool/voice-inventory/config"
	"github.com/computerscienceiscool/voice-inventory/lang"
	"github.com/computerscienceiscool/voice-inventory/observation"
	"github.com/computerscienceiscool/voice-inventory/parser"
	"github.com/computerscienceiscool/voice-inventory/refdata"
	"github.com/computerscienceiscool/voice-inventory/store"
	"github.com/computerscienceiscool/voice-inventory/vad"
)

// State is the capture state machine position.
type State string

const (
	// StateIdle: not listening.
	StateIdle State = "idle"
	// StateArmed: listening for the next observation.
	StateArmed State = "armed"
	// StateReviewing: a pending record awaits confirm/correct (§4.1 step 6).
	StateReviewing State = "reviewing"
)

// Readback is what the shell shows and speaks back (§4.1 step 5).
type Readback struct {
	Observation   *observation.Observation
	Doubtful      []string // fields below their confidence threshold
	Text          string   // display/TTS line
	AutoConfirmed bool     // saved without explicit confirm (config opt-in)
}

// Listener receives session events. All methods are optional no-ops when a
// nil Listener is configured. OnSaved is the hook for the audible + haptic
// save cue (§4.3).
type Listener interface {
	OnState(s State)
	OnLevel(rms float64)
	OnSpeechStart()
	OnReadback(rb Readback)
	OnSaved(id string, status observation.Status)
	OnDiscarded(id string)
	OnError(msg string)
	OnSuggestion(msg string)
}

// Deps wires the session to its collaborators.
type Deps struct {
	Store       *store.Store
	Transcriber asr.Transcriber
	Listener    Listener // optional
	// AudioDir enables clip retention when set and cfg.Retention.Enabled.
	AudioDir string
	// Clock is a test hook; defaults to time.Now.
	Clock func() time.Time
}

// Session is the capture orchestrator.
type Session struct {
	mu   sync.Mutex
	cfg  config.Config
	deps Deps

	state        State
	detector     *vad.Detector // wake/continuous mode
	pttBuf       []float32
	inPTT        bool
	pttTruncated bool
	truncPending bool // the utterance being handled hit the length cap

	// Stateful stream conditioning (§8.3): phase-continuous resampling and
	// high-pass filtering across chunk boundaries. Rebuilt when the input
	// format changes.
	inRate     int
	inChannels int
	resampler  *audio.Resampler
	highPass   *audio.HighPassFilter

	pending     *observation.Observation
	lastSavedID string

	resolver     *refdata.Index
	hasLocations bool
	hasParts     bool
	extraUnits   map[lang.Code]map[string]string

	utterStart time.Time
	latencies  []time.Duration
	suggested  bool
}

// New builds a session. Store and Transcriber are required.
func New(cfg config.Config, deps Deps) (*Session, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if deps.Store == nil {
		return nil, fmt.Errorf("session: store is required")
	}
	if deps.Transcriber == nil {
		return nil, fmt.Errorf("session: transcriber is required")
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	s := &Session{
		cfg:      cfg,
		deps:     deps,
		state:    StateIdle,
		detector: vad.NewDetector(vad.Config{SampleRate: audio.WhisperRate}),
	}
	if err := s.RefreshRefData(); err != nil {
		return nil, err
	}
	return s, nil
}

// RefreshRefData reloads locations/parts/units from the store (call after a
// reference-data pull).
func (s *Session) RefreshRefData() error {
	locs, err := s.deps.Store.Locations()
	if err != nil {
		return fmt.Errorf("session: load locations: %w", err)
	}
	parts, err := s.deps.Store.Parts()
	if err != nil {
		return fmt.Errorf("session: load parts: %w", err)
	}
	units, err := s.deps.Store.Units()
	if err != nil {
		return fmt.Errorf("session: load units: %w", err)
	}
	s.mu.Lock()
	s.resolver = refdata.NewIndex(locs, parts)
	s.hasLocations = len(locs) > 0
	s.hasParts = len(parts) > 0
	s.extraUnits = map[lang.Code]map[string]string{
		lang.English: refdata.UnitMap(units, lang.English),
		lang.Spanish: refdata.UnitMap(units, lang.Spanish),
	}
	s.mu.Unlock()
	return nil
}

// events queues listener callbacks fired after the lock is released.
type events []func(Listener)

func (e *events) add(fn func(Listener)) { *e = append(*e, fn) }

func (s *Session) flush(ev events) {
	l := s.deps.Listener
	if l == nil {
		return
	}
	for _, fn := range ev {
		fn(l)
	}
}

// State returns the current state.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// SetOperator changes the operator stamped on new records (login flow, §3)
// without disturbing capture state or the pending record.
func (s *Session) SetOperator(operatorID string) {
	s.mu.Lock()
	s.cfg.OperatorID = operatorID
	s.mu.Unlock()
}

// identity snapshots the device/operator ids for a new record.
func (s *Session) identity() (deviceID, operatorID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.DeviceID, s.cfg.OperatorID
}

// Pending returns a deep copy of the record awaiting review, or nil.
func (s *Session) Pending() *observation.Observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending.Clone()
}

// Arm starts listening (§4.1 step 1).
func (s *Session) Arm() {
	var ev events
	s.mu.Lock()
	if s.state == StateIdle {
		s.setState(StateArmed, &ev)
	}
	s.mu.Unlock()
	s.flush(ev)
}

// Disarm stops listening and drops any partial buffer. A pending record
// stays pending.
func (s *Session) Disarm() {
	var ev events
	s.mu.Lock()
	s.pttBuf = nil
	s.inPTT = false
	s.detector = vad.NewDetector(vad.Config{SampleRate: audio.WhisperRate})
	if s.state == StateArmed {
		s.setState(StateIdle, &ev)
	}
	s.mu.Unlock()
	s.flush(ev)
}

func (s *Session) setState(st State, ev *events) {
	if s.state == st {
		return
	}
	s.state = st
	ev.add(func(l Listener) { l.OnState(st) })
}

// BeginUtterance starts push-to-talk capture (button down).
func (s *Session) BeginUtterance() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateIdle {
		return fmt.Errorf("session: not armed")
	}
	s.inPTT = true
	s.pttBuf = s.pttBuf[:0]
	s.pttTruncated = false
	return nil
}

// maxPTTSamples caps a held push-to-talk buffer at the §8.3 utterance
// limit (30 s at 16 kHz).
const maxPTTSamples = audio.WhisperRate * 30

// FeedPCM accepts interleaved samples at any rate/channel count and routes
// them into the active capture mode. In wake mode the VAD segments
// utterances; in push-to-talk the buffer accumulates until EndUtterance.
func (s *Session) FeedPCM(samples []float32, sampleRate, channels int) {
	var ev events
	var utterances []utterance
	s.mu.Lock()
	if s.state == StateIdle {
		s.mu.Unlock()
		return
	}
	if s.resampler == nil || sampleRate != s.inRate || channels != s.inChannels {
		s.inRate, s.inChannels = sampleRate, channels
		s.resampler = audio.NewResampler(sampleRate, audio.WhisperRate)
		s.highPass = audio.NewHighPass(audio.WhisperRate, float64(s.cfg.HighPassHz))
	}
	mono := audio.MonoFromInterleaved(samples, channels)
	pcm := s.resampler.Process(mono)
	// The passthrough paths can alias the caller's buffer; copy before the
	// in-place filter so we never mutate what the shell handed us.
	if len(pcm) > 0 && len(samples) > 0 && &pcm[0] == &samples[0] {
		pcm = append([]float32(nil), pcm...)
	}
	pcm = s.highPass.Process(pcm)
	if s.inPTT {
		if space := maxPTTSamples - len(s.pttBuf); space < len(pcm) {
			if space > 0 {
				s.pttBuf = append(s.pttBuf, pcm[:space]...)
			}
			s.pttTruncated = true
		} else {
			s.pttBuf = append(s.pttBuf, pcm...)
		}
		if rms := audio.RMS(pcm); len(pcm) > 0 {
			ev.add(func(l Listener) { l.OnLevel(rms) })
		}
	} else if s.cfg.CaptureMode == "wake" {
		for _, e := range s.detector.Process(pcm) {
			switch e.Kind {
			case vad.EventLevel:
				rms := e.RMS
				ev.add(func(l Listener) { l.OnLevel(rms) })
			case vad.EventSpeechStart:
				ev.add(func(l Listener) { l.OnSpeechStart() })
			case vad.EventUtterance:
				// §8.4 latency is measured from utterance END to readback
				s.utterStart = s.deps.Clock()
				utterances = append(utterances, utterance{pcm: e.Utterance, truncated: e.Truncated})
			}
		}
	}
	s.mu.Unlock()
	s.flush(ev)
	for _, u := range utterances {
		s.handleUtterance(u.pcm, u.truncated)
	}
}

type utterance struct {
	pcm       []float32
	truncated bool
}

// EndUtterance finishes push-to-talk capture (button up), trims silence,
// and processes the utterance.
func (s *Session) EndUtterance() {
	s.mu.Lock()
	if !s.inPTT {
		s.mu.Unlock()
		return
	}
	s.inPTT = false
	buf := s.pttBuf
	truncated := s.pttTruncated
	s.pttBuf = nil
	s.pttTruncated = false
	s.utterStart = s.deps.Clock()
	s.mu.Unlock()

	trimmed, ok := vad.Trim(buf, vad.Config{SampleRate: audio.WhisperRate})
	if !ok {
		s.emitError("no speech detected")
		return
	}
	s.handleUtterance(trimmed, truncated)
}

func (s *Session) emitError(msg string) {
	if l := s.deps.Listener; l != nil {
		l.OnError(msg)
	}
}

// handleUtterance transcribes and routes one segmented utterance.
func (s *Session) handleUtterance(pcm []float32, truncated bool) {
	langOpt := asr.Lang(s.cfg.Language)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	res, err := s.deps.Transcriber.Transcribe(ctx, pcm, langOpt)
	cancel()
	if err != nil {
		s.emitError("transcription failed: " + err.Error())
		return
	}
	text := strings.TrimSpace(res.Text)
	if text == "" {
		s.emitError("heard nothing intelligible")
		return
	}
	s.mu.Lock()
	s.truncPending = truncated
	s.mu.Unlock()
	s.HandleTranscript(text, res, pcm)
}

// HandleTranscript routes recognized text through the state machine. The
// pcm buffer (optional) is retained per the audio policy. Exposed for
// text-path tools and tests.
func (s *Session) HandleTranscript(text string, res asr.Result, pcm []float32) {
	langCode := s.parseLang(res.Language)
	opts := s.parserOptions(langCode)

	s.mu.Lock()
	state := s.state
	truncated := s.truncPending
	s.truncPending = false
	s.mu.Unlock()

	if cmd, ok := parser.ParseCommand(text, opts); ok {
		switch state {
		case StateReviewing:
			s.applyCommand(cmd, langCode)
			return
		case StateArmed:
			if cmd.Kind == parser.CmdScratch {
				s.Scratch()
				return
			}
			// "yes"/"no"/"location is …" with nothing under review would
			// otherwise be recorded as a garbage observation
			s.emitError("no record under review — dictate an observation or say 'scratch that'")
			return
		}
	}

	switch state {
	case StateReviewing:
		s.redictate(text, res, langCode, opts)
	case StateArmed:
		s.newObservation(text, res, pcm, langCode, opts, truncated)
	default:
		s.emitError("not armed")
	}
}

func (s *Session) parseLang(detected string) lang.Code {
	if s.cfg.Language == "auto" {
		if c := lang.Code(detected); lang.Known(c) {
			return c
		}
		return lang.English
	}
	return lang.Code(s.cfg.Language)
}

func (s *Session) parserOptions(code lang.Code) parser.Options {
	s.mu.Lock()
	defer s.mu.Unlock()
	return parser.Options{
		Lang:       code,
		Resolver:   s.resolver,
		ExtraUnits: s.extraUnits[code],
		MultiItem:  s.cfg.MultiItem,
	}
}

// newObservation parses, persists a draft, and enters review.
func (s *Session) newObservation(text string, res asr.Result, pcm []float32,
	langCode lang.Code, opts parser.Options, truncated bool) {

	results := parser.ParseAll(text, opts)
	deviceID, operatorID := s.identity()
	for i, r := range results {
		last := i == len(results)-1
		obs, err := observation.New(deviceID, operatorID)
		if err != nil {
			s.emitError(err.Error())
			return
		}
		obs.Language = string(langCode)
		obs.RawTranscript = text
		obs.Parsed = r.Parsed
		obs.Confidence = s.confidence(r, res.Confidence)
		obs.NeedsReview, obs.ReviewReasons = s.review(r, obs.Confidence)
		if truncated {
			obs.NeedsReview = true
			obs.ReviewReasons = append(obs.ReviewReasons,
				"utterance hit the 30-second cap; tail may be missing")
		}
		if pcm != nil && s.cfg.Retention.Enabled && s.deps.AudioDir != "" {
			if ref, err := s.saveClip(obs.ID, pcm); err != nil {
				s.emitError("audio retention failed: " + err.Error())
			} else {
				obs.AudioRef = &ref
			}
		}
		if err := s.deps.Store.Insert(obs); err != nil {
			s.emitError("save failed: " + err.Error())
			return
		}

		if !last {
			// Multi-item: earlier records auto-confirm; the final one gets
			// the interactive review.
			if err := s.deps.Store.Confirm(obs.ID); err != nil {
				s.emitError(err.Error())
				return
			}
			obs.Status = observation.StatusConfirmed
			s.mu.Lock()
			s.lastSavedID = obs.ID
			s.mu.Unlock()
			if l := s.deps.Listener; l != nil {
				l.OnSaved(obs.ID, observation.StatusConfirmed)
			}
			continue
		}
		s.enterReview(obs, langCode)
	}
}

func (s *Session) enterReview(obs *observation.Observation, langCode lang.Code) {
	var ev events
	s.mu.Lock()
	s.pending = obs
	s.recordLatency(&ev)
	auto := s.cfg.AutoConfirmHighConfidence && !obs.NeedsReview
	rb := Readback{
		Observation:   obs,
		Doubtful:      s.doubtful(obs),
		Text:          obs.Readback(string(langCode)),
		AutoConfirmed: auto,
	}
	if auto {
		s.pending = nil
		s.lastSavedID = obs.ID
		s.setState(StateArmed, &ev)
	} else {
		s.setState(StateReviewing, &ev)
	}
	s.mu.Unlock()

	if auto {
		if err := s.deps.Store.Confirm(obs.ID); err != nil {
			s.emitError(err.Error())
			return
		}
		obs.Status = observation.StatusConfirmed
	}
	s.flush(ev)
	if l := s.deps.Listener; l != nil {
		l.OnReadback(rb)
		if auto {
			l.OnSaved(obs.ID, observation.StatusConfirmed)
		}
	}
}

// redictate replaces the pending record's fields from a fresh utterance.
// Like CorrectField, it mutates a deep copy and swaps it in.
func (s *Session) redictate(text string, res asr.Result, langCode lang.Code, opts parser.Options) {
	s.mu.Lock()
	pending := s.pending.Clone()
	s.mu.Unlock()
	if pending == nil {
		return
	}
	r := parser.Parse(text, opts)
	now := s.deps.Clock()
	logChange := func(field, from, to string) {
		if from != to {
			pending.ApplyCorrection(field, from, to, now)
		}
	}
	logChange("item", pending.Parsed.ItemText, r.Parsed.ItemText)
	logChange("quantity", observation.FormatQuantity(pending.Parsed.Quantity),
		observation.FormatQuantity(r.Parsed.Quantity))
	logChange("location", pending.Parsed.LocationText, r.Parsed.LocationText)

	pending.Parsed = r.Parsed
	pending.RawTranscript = text
	pending.Language = string(langCode)
	pending.Confidence = s.confidence(r, res.Confidence)
	pending.NeedsReview, pending.ReviewReasons = s.review(r, pending.Confidence)
	if err := s.deps.Store.Update(pending); err != nil {
		if errors.Is(err, store.ErrImmutable) || errors.Is(err, store.ErrNotFound) {
			// the record changed out-of-band; don't wedge in review
			s.clearPending(pending.ID)
			s.emitError("record changed elsewhere; speak the observation again")
			return
		}
		s.emitError("save failed: " + err.Error())
		return
	}
	for _, c := range pending.Corrections[max(0, len(pending.Corrections)-3):] {
		if c.At.Equal(now) {
			_ = s.deps.Store.AddCorrection(pending.ID, c)
		}
	}
	s.mu.Lock()
	if s.pending != nil && s.pending.ID == pending.ID {
		s.pending = pending
	}
	s.mu.Unlock()
	s.emitReadback(pending, langCode)
}

func (s *Session) emitReadback(obs *observation.Observation, langCode lang.Code) {
	if l := s.deps.Listener; l != nil {
		l.OnReadback(Readback{
			Observation: obs,
			Doubtful:    s.doubtful(obs),
			Text:        obs.Readback(string(langCode)),
		})
	}
}

// applyCommand executes a review-state voice command.
func (s *Session) applyCommand(cmd parser.Command, langCode lang.Code) {
	switch cmd.Kind {
	case parser.CmdConfirm:
		if err := s.Confirm(); err != nil {
			s.emitError(err.Error())
		}
	case parser.CmdScratch:
		s.Scratch()
	case parser.CmdReject:
		// stay in review; re-read the record so the operator can fix a field
		s.mu.Lock()
		pending := s.pending
		s.mu.Unlock()
		if pending != nil {
			s.emitReadback(pending, langCode)
		}
	case parser.CmdSetField:
		if err := s.CorrectField(cmd.Field, cmd.Value); err != nil {
			s.emitError(err.Error())
		}
	}
}

// clearPending drops the pending record (it changed out-of-band — batch
// review, sync — and can no longer be acted on) and re-arms so the session
// never wedges in review.
func (s *Session) clearPending(id string) {
	var ev events
	s.mu.Lock()
	if s.pending != nil && s.pending.ID == id {
		s.pending = nil
	}
	if s.lastSavedID == id {
		s.lastSavedID = ""
	}
	if s.state == StateReviewing {
		s.setState(StateArmed, &ev)
	}
	s.mu.Unlock()
	s.flush(ev)
}

// Confirm saves the pending record (§4.1 step 7) and re-arms. When the
// record's status was changed elsewhere (batch review confirmed or
// rejected it, sync pushed it) the session reconciles instead of wedging.
func (s *Session) Confirm() error {
	var ev events
	s.mu.Lock()
	pending := s.pending
	s.mu.Unlock()
	if pending == nil {
		return fmt.Errorf("session: nothing to confirm")
	}
	if err := s.deps.Store.Confirm(pending.ID); err != nil {
		cur, gerr := s.deps.Store.Get(pending.ID)
		switch {
		case gerr == nil && (cur.Status == observation.StatusConfirmed ||
			cur.Status == observation.StatusSynced):
			// already saved out-of-band — the operator's intent holds
		case gerr == nil && cur.Status == observation.StatusRejected:
			s.clearPending(pending.ID)
			if l := s.deps.Listener; l != nil {
				l.OnDiscarded(pending.ID)
			}
			return fmt.Errorf("session: record was discarded in batch review; nothing to confirm")
		case errors.Is(gerr, store.ErrNotFound):
			s.clearPending(pending.ID)
			return fmt.Errorf("session: record no longer exists")
		default:
			return err // transient store failure: keep pending, retry later
		}
	}
	s.mu.Lock()
	s.pending = nil
	s.lastSavedID = pending.ID
	s.setState(StateArmed, &ev)
	s.mu.Unlock()
	s.flush(ev)
	if l := s.deps.Listener; l != nil {
		l.OnSaved(pending.ID, observation.StatusConfirmed)
	}
	return nil
}

// Scratch discards the pending record, or the last-saved one when nothing
// is pending ("scratch that", §13). Records are marked rejected, not
// deleted, so the action is auditable.
func (s *Session) Scratch() {
	var ev events
	s.mu.Lock()
	pending := s.pending
	last := s.lastSavedID
	s.mu.Unlock()

	target := ""
	if pending != nil {
		target = pending.ID
	} else if last != "" {
		target = last
	} else if id, err := s.deps.Store.LastActive(); err == nil {
		target = id
	}
	if target == "" {
		s.emitError("nothing to scratch")
		return
	}
	if err := s.deps.Store.Reject(target); err != nil {
		cur, gerr := s.deps.Store.Get(target)
		switch {
		case gerr == nil && cur.Status == observation.StatusRejected:
			// already discarded out-of-band — same outcome, proceed
		case gerr == nil && cur.Status == observation.StatusSynced:
			s.clearPending(target)
			s.emitError("record already synced; it must be voided on the backend")
			return
		case errors.Is(gerr, store.ErrNotFound):
			s.clearPending(target)
			s.emitError("record no longer exists")
			return
		default:
			s.emitError(err.Error())
			return
		}
	}
	s.mu.Lock()
	if pending != nil {
		s.pending = nil
	}
	if s.lastSavedID == target {
		s.lastSavedID = ""
	}
	// Leaving review resumes listening; scratching from idle (batch
	// review, disarmed) must NOT arm the mic behind the operator's back.
	if s.state == StateReviewing {
		s.setState(StateArmed, &ev)
	}
	s.mu.Unlock()
	s.flush(ev)
	if l := s.deps.Listener; l != nil {
		l.OnDiscarded(target)
	}
}

// CorrectField replaces one field of the pending record from spoken or
// typed text ("location is A-40", §4.1 step 6) and re-reads it back.
//
// The pending record is never mutated in place: edits work on a deep copy
// that is swapped in under the lock, so snapshots handed to other
// goroutines (Pending, readback listeners) stay immutable.
func (s *Session) CorrectField(field, value string) error {
	s.mu.Lock()
	work := s.pending.Clone()
	s.mu.Unlock()
	if work == nil {
		return fmt.Errorf("session: nothing to correct")
	}
	langCode := obsLang(work)
	if err := s.editObservation(work, field, value); err != nil {
		if errors.Is(err, store.ErrImmutable) || errors.Is(err, store.ErrNotFound) {
			s.clearPending(work.ID)
			return fmt.Errorf("session: record changed elsewhere; speak the observation again")
		}
		return err
	}
	s.mu.Lock()
	if s.pending != nil && s.pending.ID == work.ID {
		s.pending = work
	}
	s.mu.Unlock()
	s.emitReadback(work, langCode)
	return nil
}

// EditRecord applies the same field-correction logic to any stored draft or
// confirmed record (batch review screen, §4.2).
func (s *Session) EditRecord(id, field, value string) error {
	obs, err := s.deps.Store.Get(id)
	if err != nil {
		return err
	}
	return s.editObservation(obs, field, value)
}

func obsLang(o *observation.Observation) lang.Code {
	c := lang.Code(o.Language)
	if !lang.Known(c) {
		return lang.English
	}
	return c
}

// editObservation mutates one field, recomputes review flags, and persists
// the change plus its correction-log entry.
func (s *Session) editObservation(pending *observation.Observation, field, value string) error {
	langCode := obsLang(pending)
	opts := s.parserOptions(langCode)
	now := s.deps.Clock()

	switch field {
	case "location":
		text, id, score, ok := parser.ResolveLocationText(value, opts)
		if !ok {
			return fmt.Errorf("session: couldn't understand location %q", value)
		}
		pending.ApplyCorrection("location", pending.Parsed.LocationText, text, now)
		pending.Parsed.LocationText = text
		pending.Parsed.LocationID = id
		cert := 0.9
		if id != nil {
			cert = score
		}
		pending.Confidence.Location = cert
	case "quantity":
		q, approx, ok := parser.ParseQuantityText(value, opts)
		if !ok {
			return fmt.Errorf("session: couldn't understand quantity %q", value)
		}
		pending.ApplyCorrection("quantity",
			observation.FormatQuantity(pending.Parsed.Quantity),
			observation.FormatQuantity(q), now)
		pending.Parsed.Quantity = q
		if approx {
			pending.Confidence.Quantity = 0.5
		} else {
			pending.Confidence.Quantity = 0.95
		}
	case "unit":
		v := strings.ToLower(strings.TrimSpace(value))
		if v == "" {
			return fmt.Errorf("session: empty unit")
		}
		old := ""
		if pending.Parsed.Unit != nil {
			old = *pending.Parsed.Unit
		}
		pending.ApplyCorrection("unit", old, v, now)
		pending.Parsed.Unit = &v
	case "item":
		v := strings.TrimSpace(value)
		if v == "" {
			return fmt.Errorf("session: empty item")
		}
		pending.ApplyCorrection("item", pending.Parsed.ItemText, v, now)
		pending.Parsed.ItemText = v
		pending.Parsed.PartNumber = nil
		if pn, score, ok := opts.Resolver.ResolvePart(v); ok {
			pending.Parsed.PartNumber = &pn
			pending.Confidence.Item = score
		} else {
			pending.Confidence.Item = 0.9
		}
	case "description":
		v := strings.TrimSpace(value)
		old := ""
		if pending.Parsed.Description != nil {
			old = *pending.Parsed.Description
		}
		pending.ApplyCorrection("description", old, v, now)
		if v == "" {
			pending.Parsed.Description = nil
		} else {
			pending.Parsed.Description = &v
		}
	default:
		return fmt.Errorf("session: unknown field %q", field)
	}

	pending.NeedsReview, pending.ReviewReasons = s.reviewObs(pending)
	if err := s.deps.Store.Update(pending); err != nil {
		return err
	}
	if n := len(pending.Corrections); n > 0 {
		if err := s.deps.Store.AddCorrection(pending.ID, pending.Corrections[n-1]); err != nil {
			return err
		}
	}
	return nil
}

// AddManual records a typed observation for the mic-denied fallback (§13).
// Typed fields carry full confidence.
func (s *Session) AddManual(p observation.Parsed, langCode string, confirm bool) (string, error) {
	deviceID, operatorID := s.identity()
	obs, err := observation.New(deviceID, operatorID)
	if err != nil {
		return "", err
	}
	if !lang.Known(lang.Code(langCode)) {
		langCode = "en"
	}
	obs.Language = langCode
	obs.Parsed = p
	obs.Confidence = observation.Confidence{ASR: 1, Quantity: 1, Location: 1, Item: 1}
	obs.NeedsReview, obs.ReviewReasons = s.reviewObs(obs)
	if err := s.deps.Store.Insert(obs); err != nil {
		return "", err
	}
	if confirm {
		if err := s.deps.Store.Confirm(obs.ID); err != nil {
			return "", err
		}
		s.mu.Lock()
		s.lastSavedID = obs.ID
		s.mu.Unlock()
		if l := s.deps.Listener; l != nil {
			l.OnSaved(obs.ID, observation.StatusConfirmed)
		}
	}
	return obs.ID, nil
}

// PurgeAudio deletes retained clips for synced records older than the
// retention window and clears their references (§6.3). It returns how many
// clips were removed.
func (s *Session) PurgeAudio() (int, error) {
	if s.deps.AudioDir == "" {
		return 0, nil
	}
	keepDays := s.cfg.Retention.KeepDays
	if keepDays < 0 {
		keepDays = 0 // defense in depth; Validate rejects negatives
	}
	keep := time.Duration(keepDays) * 24 * time.Hour
	cutoff := s.deps.Clock().Add(-keep)
	cands, err := s.deps.Store.AudioToPurge(cutoff)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range cands {
		path := filepath.Join(s.deps.AudioDir, filepath.Base(c.AudioRef))
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return n, err
		}
		if err := s.deps.Store.ClearAudioRef(c.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Stats reports capture health for the shell.
type Stats struct {
	Utterances      int
	LastLatencyMS   int
	MedianLatencyMS int
	Counts          map[observation.Status]int
}

// Stats returns latency and queue statistics.
func (s *Session) Stats() (Stats, error) {
	counts, err := s.deps.Store.CountsByStatus()
	if err != nil {
		return Stats{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Stats{Utterances: len(s.latencies), Counts: counts}
	if n := len(s.latencies); n > 0 {
		st.LastLatencyMS = int(s.latencies[n-1].Milliseconds())
		sorted := append([]time.Duration(nil), s.latencies...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		st.MedianLatencyMS = int(sorted[n/2].Milliseconds())
	}
	return st, nil
}

// ---------------------------------------------------------------------------
// internals

func (s *Session) saveClip(id string, pcm []float32) (string, error) {
	if err := os.MkdirAll(s.deps.AudioDir, 0o755); err != nil {
		return "", err
	}
	name := id + ".wav"
	f, err := os.Create(filepath.Join(s.deps.AudioDir, name))
	if err != nil {
		return "", err
	}
	if err := audio.EncodeWAV16(f, pcm, audio.WhisperRate); err != nil {
		_ = f.Close()
		return "", err
	}
	return name, f.Close()
}

// confidence combines parse certainty with ASR confidence (§6.1).
func (s *Session) confidence(r parser.Result, asrConf float64) observation.Confidence {
	return observation.Confidence{
		ASR:      clamp01(asrConf),
		Quantity: clamp01(r.CertQuantity * asrConf),
		Location: clamp01(r.CertLocation * asrConf),
		Item:     clamp01(r.CertItem * asrConf),
	}
}

// requiredSet expands cfg.RequiredFields for membership checks.
func (s *Session) requiredSet() map[string]bool {
	m := make(map[string]bool, len(s.cfg.RequiredFields))
	for _, f := range s.cfg.RequiredFields {
		m[f] = true
	}
	return m
}

// review derives the NeedsReview flag and its reasons (§13). Which missing
// fields count is the admin's RequiredFields choice (§14).
func (s *Session) review(r parser.Result, conf observation.Confidence) (bool, []string) {
	s.mu.Lock()
	hasLocations, hasParts := s.hasLocations, s.hasParts
	s.mu.Unlock()
	req := s.requiredSet()
	var reasons []string
	if r.Parsed.ItemText == "" && req["item"] {
		reasons = append(reasons, "no item")
	}
	if r.Parsed.Quantity == nil && req["quantity"] {
		if r.QuantityVague {
			reasons = append(reasons, "vague quantity")
		} else {
			reasons = append(reasons, "no quantity spoken")
		}
	}
	if r.Parsed.LocationText == "" && req["location"] {
		reasons = append(reasons, "no location spoken")
	}
	reasons = append(reasons, s.confidenceReasons(conf, r.Parsed)...)
	if r.Parsed.LocationText != "" && r.Parsed.LocationID == nil && hasLocations {
		reasons = append(reasons, "location not in known locations")
	}
	if r.Parsed.ItemText != "" && r.Parsed.PartNumber == nil && hasParts {
		reasons = append(reasons, "item not in part vocabulary")
	}
	return len(reasons) > 0, reasons
}

// reviewObs recomputes review reasons from a stored record (corrections).
func (s *Session) reviewObs(o *observation.Observation) (bool, []string) {
	req := s.requiredSet()
	var reasons []string
	if o.Parsed.ItemText == "" && req["item"] {
		reasons = append(reasons, "no item")
	}
	if o.Parsed.Quantity == nil && req["quantity"] {
		reasons = append(reasons, "no quantity spoken")
	}
	if o.Parsed.LocationText == "" && req["location"] {
		reasons = append(reasons, "no location spoken")
	}
	reasons = append(reasons, s.confidenceReasons(o.Confidence, o.Parsed)...)
	return len(reasons) > 0, reasons
}

func (s *Session) confidenceReasons(conf observation.Confidence, p observation.Parsed) []string {
	t := s.cfg.Thresholds
	var reasons []string
	if conf.ASR > 0 && conf.ASR < t.ASR {
		reasons = append(reasons, "low speech confidence")
	}
	if p.Quantity != nil && conf.Quantity < t.Quantity {
		reasons = append(reasons, "low quantity confidence")
	}
	if p.LocationText != "" && conf.Location < t.Location {
		reasons = append(reasons, "low location confidence")
	}
	if p.ItemText != "" && conf.Item < t.Item {
		reasons = append(reasons, "low item confidence")
	}
	return reasons
}

// doubtful lists fields to highlight on the readback screen (§13).
func (s *Session) doubtful(o *observation.Observation) []string {
	t := s.cfg.Thresholds
	var fields []string
	if o.Parsed.Quantity == nil || o.Confidence.Quantity < t.Quantity {
		fields = append(fields, "quantity")
	}
	if o.Parsed.LocationText == "" || o.Confidence.Location < t.Location {
		fields = append(fields, "location")
	}
	if o.Parsed.ItemText == "" || o.Confidence.Item < t.Item {
		fields = append(fields, "item")
	}
	return fields
}

// recordLatency captures utterance-end → readback latency (§8.4) and
// suggests a smaller model when the median exceeds the target.
func (s *Session) recordLatency(ev *events) {
	if s.utterStart.IsZero() {
		return
	}
	d := s.deps.Clock().Sub(s.utterStart)
	s.utterStart = time.Time{}
	if d < 0 {
		return
	}
	s.latencies = append(s.latencies, d)
	if len(s.latencies) > 50 {
		s.latencies = s.latencies[len(s.latencies)-50:]
	}
	target := time.Duration(s.cfg.TargetLatencyMS) * time.Millisecond
	if s.suggested || target <= 0 || len(s.latencies) < 3 {
		return
	}
	sorted := append([]time.Duration(nil), s.latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if sorted[len(sorted)/2] > target {
		s.suggested = true
		ev.add(func(l Listener) {
			l.OnSuggestion("transcription is slower than target; consider the base model (§8.4)")
		})
	}
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
