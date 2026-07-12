package mobile

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// fakeEvents implements Events.
type fakeEvents struct {
	mu        sync.Mutex
	states    []string
	readbacks []string
	saved     []string
	errors    []string
}

func (f *fakeEvents) OnState(s string) { f.mu.Lock(); f.states = append(f.states, s); f.mu.Unlock() }
func (f *fakeEvents) OnLevel(float64)  {}
func (f *fakeEvents) OnSpeechStart()   {}
func (f *fakeEvents) OnReadback(j string) {
	f.mu.Lock()
	f.readbacks = append(f.readbacks, j)
	f.mu.Unlock()
}
func (f *fakeEvents) OnSaved(id, _ string) {
	f.mu.Lock()
	f.saved = append(f.saved, id)
	f.mu.Unlock()
}
func (f *fakeEvents) OnDiscarded(string)  {}
func (f *fakeEvents) OnError(m string)    { f.mu.Lock(); f.errors = append(f.errors, m); f.mu.Unlock() }
func (f *fakeEvents) OnSuggestion(string) {}

func TestAppTextFlow(t *testing.T) {
	ev := &fakeEvents{}
	app, err := NewApp(t.TempDir(), `{"device_id":"dev-9","operator_id":"op-9"}`, ev)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	app.Arm()
	if app.State() != "armed" {
		t.Fatalf("state = %s", app.State())
	}
	app.HandleTranscript("Twelve boxes of RJ45 connectors in bin A-14", "en", 0.95)
	if app.State() != "reviewing" {
		t.Fatalf("state = %s", app.State())
	}
	pj, err := app.PendingJSON()
	if err != nil || pj == "" {
		t.Fatalf("pending: %q, %v", pj, err)
	}
	var pending map[string]any
	if err := json.Unmarshal([]byte(pj), &pending); err != nil {
		t.Fatal(err)
	}
	parsed := pending["parsed"].(map[string]any)
	if parsed["item_text"] != "RJ45 connectors" || parsed["quantity"].(float64) != 12 {
		t.Errorf("parsed = %v", parsed)
	}
	if pending["device_id"] != "dev-9" || pending["operator_id"] != "op-9" {
		t.Errorf("identity = %v/%v", pending["device_id"], pending["operator_id"])
	}

	if err := app.CorrectField("location", "B-2"); err != nil {
		t.Fatal(err)
	}
	if err := app.Confirm(); err != nil {
		t.Fatal(err)
	}
	if app.State() != "armed" {
		t.Errorf("state after confirm = %s", app.State())
	}

	list, err := app.ListJSON("confirmed", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list, "B-2") {
		t.Errorf("list = %s", list)
	}

	ev.mu.Lock()
	if len(ev.readbacks) < 2 || len(ev.saved) != 1 {
		t.Errorf("events: readbacks=%d saved=%d", len(ev.readbacks), len(ev.saved))
	}
	ev.mu.Unlock()

	stats, err := app.StatsJSON()
	if err != nil || !strings.Contains(stats, "Counts") {
		t.Errorf("stats = %q, %v", stats, err)
	}
}

func TestAppManualAndEdit(t *testing.T) {
	app, err := NewApp(t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	id, err := app.AddManual(`{"item_text":"hex nuts","quantity":7,"location_text":"B-2"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.EditRecord(id, "quantity", "9"); err != nil {
		t.Fatal(err)
	}
	if err := app.ConfirmRecord(id); err != nil {
		t.Fatal(err)
	}
	list, err := app.ListJSON("confirmed", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list, `"quantity":9`) {
		t.Errorf("edit not applied: %s", list)
	}
	if err := app.RejectRecord(id); err != nil {
		t.Fatal(err)
	}
}

func TestAppNoTranscriber(t *testing.T) {
	ev := &fakeEvents{}
	app, err := NewApp(t.TempDir(), "", ev)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.Arm()
	_ = app.BeginUtterance()
	app.FeedPCM16(make([]byte, 32000), 16000, 1)
	app.EndUtterance()
	// silence → "no speech detected"; with tone it would surface the
	// no-transcriber error. Either way the app must not panic and must
	// report through OnError.
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if len(ev.errors) == 0 {
		t.Error("expected an error event")
	}
}

func TestAppBadConfig(t *testing.T) {
	if _, err := NewApp(t.TempDir(), `{"language":"xx"}`, nil); err == nil {
		t.Error("bad config should fail")
	}
	if _, err := NewApp(t.TempDir(), `{not json`, nil); err == nil {
		t.Error("bad json should fail")
	}
}

func TestAppSyncUnconfigured(t *testing.T) {
	app, err := NewApp(t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if _, err := app.SyncPush(); err == nil {
		t.Error("push without endpoint should fail")
	}
}

// SetOperator must not disturb capture state, the pending record, or race
// concurrent session access (it previously rebuilt the session).
func TestSetOperatorPreservesCapture(t *testing.T) {
	ev := &fakeEvents{}
	app, err := NewApp(t.TempDir(), `{"device_id":"d1","operator_id":"op-1"}`, ev)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	app.Arm()
	app.HandleTranscript("Twelve boxes of RJ45 connectors in bin A-14", "en", 0.95)
	if app.State() != "reviewing" {
		t.Fatalf("precondition: state = %s", app.State())
	}
	done := make(chan struct{})
	go func() { // exercise concurrent reads during the operator change
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = app.State()
			_, _ = app.PendingJSON()
		}
	}()
	if err := app.SetOperator("op-2"); err != nil {
		t.Fatal(err)
	}
	<-done
	if app.State() != "reviewing" {
		t.Errorf("state after SetOperator = %s, want reviewing", app.State())
	}
	pj, _ := app.PendingJSON()
	if pj == "" {
		t.Fatal("pending record lost by SetOperator")
	}
	if err := app.Confirm(); err != nil {
		t.Fatal(err)
	}
	// records dictated after the change carry the new operator
	app.HandleTranscript("five bags of washers in bin C-7", "en", 0.9)
	pj, _ = app.PendingJSON()
	if !strings.Contains(pj, `"operator_id":"op-2"`) {
		t.Errorf("new record should carry op-2: %s", pj)
	}
}

// Documented contract: ListJSON always returns an array.
func TestListJSONEmptyIsArray(t *testing.T) {
	app, err := NewApp(t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	out, err := app.ListJSON("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"observations":[]`) {
		t.Errorf("empty list = %s, want observations:[]", out)
	}
}
