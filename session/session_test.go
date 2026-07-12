package session

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/computerscienceiscool/voice-inventory/asr"
	"github.com/computerscienceiscool/voice-inventory/config"
	"github.com/computerscienceiscool/voice-inventory/observation"
	"github.com/computerscienceiscool/voice-inventory/refdata"
	"github.com/computerscienceiscool/voice-inventory/store"
)

// recorder captures listener events.
type recorder struct {
	mu          sync.Mutex
	states      []State
	readbacks   []Readback
	saved       []string
	savedStatus []observation.Status
	discarded   []string
	errors      []string
	suggestions []string
	levels      int
	speechStart int
}

func (r *recorder) OnState(s State) { r.mu.Lock(); r.states = append(r.states, s); r.mu.Unlock() }
func (r *recorder) OnLevel(float64) { r.mu.Lock(); r.levels++; r.mu.Unlock() }
func (r *recorder) OnSpeechStart()  { r.mu.Lock(); r.speechStart++; r.mu.Unlock() }
func (r *recorder) OnReadback(rb Readback) {
	r.mu.Lock()
	r.readbacks = append(r.readbacks, rb)
	r.mu.Unlock()
}
func (r *recorder) OnSaved(id string, st observation.Status) {
	r.mu.Lock()
	r.saved = append(r.saved, id)
	r.savedStatus = append(r.savedStatus, st)
	r.mu.Unlock()
}
func (r *recorder) OnDiscarded(id string) {
	r.mu.Lock()
	r.discarded = append(r.discarded, id)
	r.mu.Unlock()
}
func (r *recorder) OnError(msg string) { r.mu.Lock(); r.errors = append(r.errors, msg); r.mu.Unlock() }
func (r *recorder) OnSuggestion(msg string) {
	r.mu.Lock()
	r.suggestions = append(r.suggestions, msg)
	r.mu.Unlock()
}

func (r *recorder) lastReadback(t *testing.T) Readback {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.readbacks) == 0 {
		t.Fatal("no readback emitted")
	}
	return r.readbacks[len(r.readbacks)-1]
}

type fixture struct {
	s    *Session
	st   *store.Store
	mock *asr.Mock
	rec  *recorder
	now  *time.Time
}

func newFixture(t *testing.T, mutate func(*config.Config), deps func(*Deps)) *fixture {
	t.Helper()
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	_ = st.ReplaceLocations([]refdata.Location{
		{ID: "LOC-A14", Name: "Bin A-14", Aliases: []string{"A-14", "A fourteen"}},
		{ID: "LOC-A40", Name: "Bin A-40", Aliases: []string{"A-40"}},
	})
	_ = st.ReplaceParts([]refdata.Part{
		{PartNumber: "PN-1001", Name: "RJ45 connector", Aliases: []string{"RJ45 connectors"}},
	})

	cfg := config.Default()
	cfg.DeviceID = "dev-1"
	cfg.OperatorID = "op-1"
	if mutate != nil {
		mutate(&cfg)
	}
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	mock := &asr.Mock{}
	rec := &recorder{}
	d := Deps{
		Store:       st,
		Transcriber: mock,
		Listener:    rec,
		Clock:       func() time.Time { return now },
	}
	if deps != nil {
		deps(&d)
	}
	s, err := New(cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{s: s, st: st, mock: mock, rec: rec, now: &now}
}

func tone(seconds float64) []float32 {
	n := int(16000 * seconds)
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(0.3 * math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	return out
}

func TestHappyPathPTT(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.mock.Results = []asr.Result{
		asr.TextResult("Twelve boxes of RJ45 connectors in bin A-14", "en", 0.95),
		asr.TextResult("yes", "en", 0.99),
	}

	f.s.Arm()
	if f.s.State() != StateArmed {
		t.Fatalf("state = %s", f.s.State())
	}
	if err := f.s.BeginUtterance(); err != nil {
		t.Fatal(err)
	}
	f.s.FeedPCM(tone(1.0), 16000, 1)
	f.s.EndUtterance()

	if f.s.State() != StateReviewing {
		t.Fatalf("state after utterance = %s", f.s.State())
	}
	rb := f.rec.lastReadback(t)
	if !strings.Contains(rb.Text, "12 boxes") || !strings.Contains(rb.Text, "A-14") {
		t.Errorf("readback = %q", rb.Text)
	}
	pending := f.s.Pending()
	if pending == nil {
		t.Fatal("no pending record")
	}
	if pending.Parsed.LocationID == nil || *pending.Parsed.LocationID != "LOC-A14" {
		t.Errorf("location not resolved: %+v", pending.Parsed)
	}
	if pending.Parsed.PartNumber == nil || *pending.Parsed.PartNumber != "PN-1001" {
		t.Errorf("part not resolved: %+v", pending.Parsed)
	}
	// draft persisted before confirmation (crash safety)
	got, err := f.st.Get(pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != observation.StatusDraft {
		t.Errorf("stored status = %s, want draft", got.Status)
	}

	// voice confirm: "yes"
	f.s.BeginUtterance()
	f.s.FeedPCM(tone(0.5), 16000, 1)
	f.s.EndUtterance()

	if f.s.State() != StateArmed {
		t.Fatalf("state after confirm = %s", f.s.State())
	}
	got, _ = f.st.Get(pending.ID)
	if got.Status != observation.StatusConfirmed {
		t.Errorf("status = %s, want confirmed", got.Status)
	}
	if len(f.rec.saved) != 1 || f.rec.saved[0] != pending.ID {
		t.Errorf("OnSaved = %v", f.rec.saved)
	}
	if f.rec.levels == 0 {
		t.Error("no level events during PTT")
	}
}

func TestVoiceFieldCorrection(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.mock.Results = []asr.Result{
		asr.TextResult("Twelve boxes of RJ45 connectors in bin A-14", "en", 0.95),
		asr.TextResult("location is A-40", "en", 0.97),
		asr.TextResult("correct", "en", 0.99),
	}
	f.s.Arm()
	speak := func() {
		f.s.BeginUtterance()
		f.s.FeedPCM(tone(0.8), 16000, 1)
		f.s.EndUtterance()
	}
	speak() // dictate
	speak() // correct location
	pending := f.s.Pending()
	if pending == nil {
		t.Fatal("pending vanished")
	}
	if pending.Parsed.LocationText != "A-40" {
		t.Errorf("location = %q, want A-40", pending.Parsed.LocationText)
	}
	if pending.Parsed.LocationID == nil || *pending.Parsed.LocationID != "LOC-A40" {
		t.Errorf("location id = %v", pending.Parsed.LocationID)
	}
	if len(pending.Corrections) != 1 || pending.Corrections[0].From != "A-14" {
		t.Errorf("corrections = %+v", pending.Corrections)
	}
	speak() // confirm
	got, _ := f.st.Get(pending.ID)
	if got.Status != observation.StatusConfirmed {
		t.Errorf("status = %s", got.Status)
	}
	if len(got.Corrections) != 1 || got.Corrections[0].To != "A-40" {
		t.Errorf("persisted corrections = %+v", got.Corrections)
	}
}

func TestScratchPendingAndLast(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.mock.Results = []asr.Result{
		asr.TextResult("five bags of washers in bin A-14", "en", 0.9),
		asr.TextResult("scratch that", "en", 0.99),
	}
	f.s.Arm()
	speak := func() {
		f.s.BeginUtterance()
		f.s.FeedPCM(tone(0.6), 16000, 1)
		f.s.EndUtterance()
	}
	speak()
	pending := f.s.Pending()
	speak() // scratch the pending record
	if f.s.State() != StateArmed || f.s.Pending() != nil {
		t.Fatalf("scratch should re-arm: state=%s", f.s.State())
	}
	got, _ := f.st.Get(pending.ID)
	if got.Status != observation.StatusRejected {
		t.Errorf("status = %s, want rejected", got.Status)
	}

	// now scratch the last-saved record while armed
	f.mock.Results = []asr.Result{
		asr.TextResult("five bags of washers in bin A-14", "en", 0.9),
		asr.TextResult("yes", "en", 0.99),
		asr.TextResult("scratch that", "en", 0.99),
	}
	f.mock.Err = nil
	*f.mock = asr.Mock{Results: f.mock.Results}
	speak()
	saved := f.s.Pending()
	speak() // yes
	speak() // scratch that (armed)
	got, _ = f.st.Get(saved.ID)
	if got.Status != observation.StatusRejected {
		t.Errorf("last-saved should be rejected, got %s", got.Status)
	}
}

func TestRedictation(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.mock.Results = []asr.Result{
		asr.TextResult("five bags of washers in bin A-14", "en", 0.9),
		asr.TextResult("twelve boxes of RJ45 connectors in bin A-40", "en", 0.92),
	}
	f.s.Arm()
	speak := func() {
		f.s.BeginUtterance()
		f.s.FeedPCM(tone(0.6), 16000, 1)
		f.s.EndUtterance()
	}
	speak()
	first := f.s.Pending()
	speak() // full re-dictation replaces pending
	second := f.s.Pending()
	if second == nil || second.ID != first.ID {
		t.Fatal("re-dictation should keep the same record")
	}
	if second.Parsed.ItemText != "RJ45 connectors" || *second.Parsed.Quantity != 12 {
		t.Errorf("fields not replaced: %+v", second.Parsed)
	}
	if len(second.Corrections) == 0 {
		t.Error("re-dictation should log corrections")
	}
}

func TestReviewFlags(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.mock.Results = []asr.Result{
		asr.TextResult("several boxes of mystery widgets", "en", 0.5),
	}
	f.s.Arm()
	f.s.BeginUtterance()
	f.s.FeedPCM(tone(0.6), 16000, 1)
	f.s.EndUtterance()
	p := f.s.Pending()
	if p == nil {
		t.Fatal("no pending")
	}
	if !p.NeedsReview {
		t.Error("record should need review")
	}
	joined := strings.Join(p.ReviewReasons, "; ")
	for _, want := range []string{"vague quantity", "no location spoken", "low speech confidence"} {
		if !strings.Contains(joined, want) {
			t.Errorf("reasons %q missing %q", joined, want)
		}
	}
	rb := f.rec.lastReadback(t)
	if len(rb.Doubtful) == 0 {
		t.Error("doubtful fields should be highlighted")
	}
}

func TestAutoConfirm(t *testing.T) {
	f := newFixture(t, func(c *config.Config) {
		c.AutoConfirmHighConfidence = true
	}, nil)
	f.mock.Results = []asr.Result{
		asr.TextResult("Twelve boxes of RJ45 connectors in bin A-14", "en", 0.98),
	}
	f.s.Arm()
	f.s.BeginUtterance()
	f.s.FeedPCM(tone(0.6), 16000, 1)
	f.s.EndUtterance()
	if f.s.State() != StateArmed {
		t.Fatalf("auto-confirm should re-arm, state=%s", f.s.State())
	}
	if len(f.rec.saved) != 1 {
		t.Fatalf("saved = %v", f.rec.saved)
	}
	got, _ := f.st.Get(f.rec.saved[0])
	if got.Status != observation.StatusConfirmed {
		t.Errorf("status = %s", got.Status)
	}
	if !f.rec.lastReadback(t).AutoConfirmed {
		t.Error("readback should mark auto-confirmed")
	}
}

func TestAudioRetentionAndPurge(t *testing.T) {
	dir := t.TempDir()
	f := newFixture(t, nil, func(d *Deps) { d.AudioDir = dir })
	f.mock.Results = []asr.Result{
		asr.TextResult("Twelve boxes of RJ45 connectors in bin A-14", "en", 0.95),
	}
	f.s.Arm()
	f.s.BeginUtterance()
	f.s.FeedPCM(tone(0.6), 16000, 1)
	f.s.EndUtterance()
	p := f.s.Pending()
	if p.AudioRef == nil {
		t.Fatal("audio_ref not set")
	}
	clip := filepath.Join(dir, *p.AudioRef)
	if _, err := os.Stat(clip); err != nil {
		t.Fatalf("clip missing: %v", err)
	}
	_ = f.s.Confirm()
	if _, err := f.st.MarkSynced([]string{p.ID}, f.now.Add(-10*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	n, err := f.s.PurgeAudio()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("purged = %d", n)
	}
	if _, err := os.Stat(clip); !os.IsNotExist(err) {
		t.Error("clip should be deleted")
	}
	got, _ := f.st.Get(p.ID)
	if got.AudioRef != nil {
		t.Error("audio_ref should be cleared")
	}
}

func TestAddManual(t *testing.T) {
	f := newFixture(t, nil, nil)
	q := 7.0
	id, err := f.s.AddManual(observation.Parsed{
		ItemText: "hex nuts", Quantity: &q, LocationText: "B-2",
	}, "en", true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != observation.StatusConfirmed || got.Parsed.ItemText != "hex nuts" {
		t.Errorf("manual record wrong: %+v", got)
	}
	if got.RawTranscript != "" {
		t.Error("manual entry has no transcript")
	}
}

func TestLatencySuggestion(t *testing.T) {
	// Every Clock() call advances 200 ms, so utterance-end → readback
	// always measures 200 ms against a 100 ms target.
	base := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	var calls int
	f := newFixture(t,
		func(c *config.Config) { c.TargetLatencyMS = 100 },
		func(d *Deps) {
			d.Clock = func() time.Time {
				calls++
				return base.Add(time.Duration(calls) * 200 * time.Millisecond)
			}
		})
	f.s.Arm()
	for i := 0; i < 3; i++ {
		*f.mock = asr.Mock{Results: []asr.Result{
			asr.TextResult("five bags of washers in bin A-14", "en", 0.9),
		}}
		f.s.BeginUtterance()
		f.s.FeedPCM(tone(0.4), 16000, 1)
		f.s.EndUtterance()
		f.s.Scratch()
	}
	f.rec.mu.Lock()
	n := len(f.rec.suggestions)
	f.rec.mu.Unlock()
	if n != 1 {
		t.Errorf("suggestions = %d, want exactly 1", n)
	}
	st, err := f.s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Utterances != 3 || st.MedianLatencyMS < 100 {
		t.Errorf("stats = %+v", st)
	}
	if st.Counts[observation.StatusRejected] != 3 {
		t.Errorf("counts = %v", st.Counts)
	}
}

func TestWakeModeVAD(t *testing.T) {
	f := newFixture(t, func(c *config.Config) { c.CaptureMode = "wake" }, nil)
	f.mock.Results = []asr.Result{
		asr.TextResult("five bags of washers in bin A-14", "en", 0.9),
	}
	f.s.Arm()
	quiet := make([]float32, 8000)
	for i := range quiet {
		quiet[i] = float32(0.002 * math.Sin(2*math.Pi*120*float64(i)/16000))
	}
	f.s.FeedPCM(quiet, 16000, 1)
	f.s.FeedPCM(tone(1.0), 16000, 1)
	f.s.FeedPCM(make([]float32, 16000), 16000, 1) // silence closes the utterance
	if f.s.State() != StateReviewing {
		t.Fatalf("wake mode should reach review, state=%s", f.s.State())
	}
	if f.rec.speechStart == 0 {
		t.Error("no speech-start event")
	}
	if len(f.mock.Calls) != 1 {
		t.Errorf("transcriber calls = %d, want 1", len(f.mock.Calls))
	}
}

func TestArmRequired(t *testing.T) {
	f := newFixture(t, nil, nil)
	if err := f.s.BeginUtterance(); err == nil {
		t.Error("BeginUtterance while idle should fail")
	}
	if err := f.s.Confirm(); err == nil {
		t.Error("Confirm with nothing pending should fail")
	}
	if err := f.s.CorrectField("location", "A-1"); err == nil {
		t.Error("CorrectField with nothing pending should fail")
	}
}

// "yes" spoken while armed (nothing under review) must not become a
// garbage observation.
func TestArmedCommandIsNotDictation(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.mock.Results = []asr.Result{asr.TextResult("yes", "en", 0.99)}
	f.s.Arm()
	f.s.BeginUtterance()
	f.s.FeedPCM(tone(0.4), 16000, 1)
	f.s.EndUtterance()
	if f.s.State() != StateArmed {
		t.Errorf("state = %s", f.s.State())
	}
	counts, _ := f.st.CountsByStatus()
	if len(counts) != 0 {
		t.Errorf("no record should be created, got %v", counts)
	}
	f.rec.mu.Lock()
	defer f.rec.mu.Unlock()
	if len(f.rec.errors) == 0 || !strings.Contains(f.rec.errors[0], "no record under review") {
		t.Errorf("expected guidance error, got %v", f.rec.errors)
	}
}

func TestNoSpeechDetected(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.s.Arm()
	f.s.BeginUtterance()
	f.s.FeedPCM(make([]float32, 16000), 16000, 1) // pure silence
	f.s.EndUtterance()
	f.rec.mu.Lock()
	defer f.rec.mu.Unlock()
	if len(f.rec.errors) == 0 || !strings.Contains(f.rec.errors[0], "no speech") {
		t.Errorf("expected no-speech error, got %v", f.rec.errors)
	}
	if len(f.mock.Calls) != 0 {
		t.Error("transcriber should not be called for silence")
	}
}

// A held button past the 30 s cap truncates the buffer and flags the record.
func TestPTTCapTruncates(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.mock.Results = []asr.Result{
		asr.TextResult("five bags of washers in bin A-14", "en", 0.9),
	}
	f.s.Arm()
	_ = f.s.BeginUtterance()
	chunk := tone(1.0)
	for i := 0; i < 35; i++ { // 35 s of audio
		f.s.FeedPCM(chunk, 16000, 1)
	}
	f.s.EndUtterance()
	p := f.s.Pending()
	if p == nil {
		t.Fatal("no pending record")
	}
	found := false
	for _, r := range p.ReviewReasons {
		if strings.Contains(r, "30-second cap") {
			found = true
		}
	}
	if !found {
		t.Errorf("truncation not flagged: %v", p.ReviewReasons)
	}
	if len(f.mock.Calls) != 1 || len(f.mock.Calls[0]) > 16000*31 {
		t.Errorf("transcriber received %d samples; cap not applied", len(f.mock.Calls[0]))
	}
}

// Scratch from idle (batch review, disarmed) must not arm the microphone.
func TestScratchFromIdleStaysIdle(t *testing.T) {
	f := newFixture(t, nil, nil)
	q := 5.0
	id, err := f.s.AddManual(observation.Parsed{ItemText: "washers", Quantity: &q}, "en", true)
	if err != nil {
		t.Fatal(err)
	}
	if f.s.State() != StateIdle {
		t.Fatalf("precondition: state = %s", f.s.State())
	}
	f.s.Scratch()
	if f.s.State() != StateIdle {
		t.Errorf("state after idle scratch = %s, want idle", f.s.State())
	}
	got, _ := f.st.Get(id)
	if got.Status != observation.StatusRejected {
		t.Errorf("record status = %s, want rejected", got.Status)
	}
}

// The UI thread reads Pending() while the capture thread applies voice
// corrections; the pending record must be swapped, never mutated in place.
// (Meaningful under -race, which CI runs.)
func TestPendingConcurrentWithCorrections(t *testing.T) {
	f := newFixture(t, nil, nil)
	f.mock.Results = []asr.Result{
		asr.TextResult("Twelve boxes of RJ45 connectors in bin A-14", "en", 0.95),
	}
	f.s.Arm()
	_ = f.s.BeginUtterance()
	f.s.FeedPCM(tone(0.5), 16000, 1)
	f.s.EndUtterance()
	if f.s.Pending() == nil {
		t.Fatal("no pending record")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			if p := f.s.Pending(); p != nil {
				_ = p.Parsed.LocationText
				_ = len(p.Corrections)
			}
		}
	}()
	for i := 0; i < 50; i++ {
		loc := "A-40"
		if i%2 == 0 {
			loc = "A-14"
		}
		if err := f.s.CorrectField("location", loc); err != nil {
			t.Fatal(err)
		}
	}
	<-done
}

// Batch review acting on the record under live review must never wedge the
// session: it reconciles and re-arms.
func TestOutOfBandStatusChanges(t *testing.T) {
	dictate := func(f *fixture) *observation.Observation {
		t.Helper()
		*f.mock = asr.Mock{Results: []asr.Result{
			asr.TextResult("five bags of washers in bin A-14", "en", 0.9),
		}}
		f.s.Arm()
		_ = f.s.BeginUtterance()
		f.s.FeedPCM(tone(0.5), 16000, 1)
		f.s.EndUtterance()
		p := f.s.Pending()
		if p == nil {
			t.Fatal("no pending record")
		}
		return p
	}

	t.Run("rejected elsewhere then confirm", func(t *testing.T) {
		f := newFixture(t, nil, nil)
		p := dictate(f)
		if err := f.st.Reject(p.ID); err != nil { // batch review discards it
			t.Fatal(err)
		}
		if err := f.s.Confirm(); err == nil {
			t.Error("confirm should report the out-of-band discard")
		}
		if f.s.State() != StateArmed || f.s.Pending() != nil {
			t.Errorf("session wedged: state=%s pending=%v", f.s.State(), f.s.Pending())
		}
	})

	t.Run("confirmed elsewhere then voice yes", func(t *testing.T) {
		f := newFixture(t, nil, nil)
		p := dictate(f)
		if err := f.st.Confirm(p.ID); err != nil { // batch review confirms it
			t.Fatal(err)
		}
		if err := f.s.Confirm(); err != nil {
			t.Errorf("confirming an already-confirmed record should succeed: %v", err)
		}
		if f.s.State() != StateArmed {
			t.Errorf("state = %s", f.s.State())
		}
	})

	t.Run("rejected elsewhere then scratch", func(t *testing.T) {
		f := newFixture(t, nil, nil)
		p := dictate(f)
		_ = f.st.Reject(p.ID)
		f.s.Scratch() // same outcome the operator wanted
		if f.s.State() != StateArmed || f.s.Pending() != nil {
			t.Errorf("session wedged after scratch: state=%s", f.s.State())
		}
	})

	t.Run("rejected elsewhere then correction and redictate", func(t *testing.T) {
		f := newFixture(t, nil, nil)
		p := dictate(f)
		_ = f.st.Reject(p.ID)
		if err := f.s.CorrectField("location", "B-2"); err == nil {
			t.Error("correction on a discarded record should error")
		}
		if f.s.State() != StateArmed || f.s.Pending() != nil {
			t.Errorf("session wedged after correction: state=%s", f.s.State())
		}
		// and the next utterance dictates a fresh record normally
		p2 := dictate(f)
		if p2.ID == p.ID {
			t.Error("new dictation should be a new record")
		}
	})
}

// The admin's RequiredFields choice must actually drive missing-field flags.
func TestRequiredFieldsConfigurable(t *testing.T) {
	f := newFixture(t, func(c *config.Config) {
		c.RequiredFields = []string{"item"} // location/quantity optional
	}, nil)
	f.mock.Results = []asr.Result{
		asr.TextResult("RJ45 connectors", "en", 0.95), // item only
	}
	f.s.Arm()
	_ = f.s.BeginUtterance()
	f.s.FeedPCM(tone(0.5), 16000, 1)
	f.s.EndUtterance()
	p := f.s.Pending()
	if p == nil {
		t.Fatal("no pending")
	}
	for _, r := range p.ReviewReasons {
		if strings.Contains(r, "no quantity") || strings.Contains(r, "no location") {
			t.Errorf("optional field flagged anyway: %v", p.ReviewReasons)
		}
	}
	// defaults still flag all three
	f2 := newFixture(t, nil, nil)
	*f2.mock = asr.Mock{Results: []asr.Result{asr.TextResult("RJ45 connectors", "en", 0.95)}}
	f2.s.Arm()
	_ = f2.s.BeginUtterance()
	f2.s.FeedPCM(tone(0.5), 16000, 1)
	f2.s.EndUtterance()
	p2 := f2.s.Pending()
	joined := strings.Join(p2.ReviewReasons, "; ")
	if !strings.Contains(joined, "no quantity") || !strings.Contains(joined, "no location") {
		t.Errorf("defaults should flag missing quantity+location: %v", p2.ReviewReasons)
	}
}
