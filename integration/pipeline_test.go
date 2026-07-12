// Package integration exercises the whole capture→sync→export chain with
// every real component except the ASR engine (mocked, since whisper.cpp
// isn't in CI). It's the end-to-end regression guard the per-package tests
// can't be: it proves the seams compose.
package integration

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/computerscienceiscool/voice-inventory/asr"
	"github.com/computerscienceiscool/voice-inventory/config"
	"github.com/computerscienceiscool/voice-inventory/export"
	"github.com/computerscienceiscool/voice-inventory/observation"
	"github.com/computerscienceiscool/voice-inventory/refdata"
	"github.com/computerscienceiscool/voice-inventory/session"
	"github.com/computerscienceiscool/voice-inventory/store"
	"github.com/computerscienceiscool/voice-inventory/syncer"
)

// tone builds a loud tone the energy VAD treats as speech.
func tone(seconds float64) []float32 {
	n := int(16000 * seconds)
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(0.3 * math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	return out
}

// TestFullPipeline drives one observation from microphone samples all the
// way to a backend and a CSV export, touching every real component.
func TestFullPipeline(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Reference data as if pulled from the backend.
	if err := st.ReplaceLocations([]refdata.Location{
		{ID: "LOC-A14", Name: "Bin A-14", Aliases: []string{"A-14", "A fourteen"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceParts([]refdata.Part{
		{PartNumber: "PN-1001", Name: "RJ45 connector", Aliases: []string{"RJ45 connectors"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Backend: accept the batch, serve refdata.
	var received []observation.Observation
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/observations:batch":
			var req syncer.PushRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			received = append(received, req.Observations...)
			var resp syncer.PushResponse
			for _, o := range req.Observations {
				resp.Accepted = append(resp.Accepted, o.ID)
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/v1/refdata":
			_ = json.NewEncoder(w).Encode(syncer.RefDataResponse{})
		}
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.DeviceID = "dev-int"
	cfg.OperatorID = "op-int"
	mock := &asr.Mock{Results: []asr.Result{
		asr.TextResult("Twelve boxes of RJ45 connectors in bin A-14", "en", 0.95),
	}}
	s, err := session.New(cfg, session.Deps{Store: st, Transcriber: mock})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Capture: audio → VAD trim → mock ASR → parser → draft in store.
	s.Arm()
	if err := s.BeginUtterance(); err != nil {
		t.Fatal(err)
	}
	s.FeedPCM(tone(1.0), 16000, 1)
	s.EndUtterance()

	pending := s.Pending()
	if pending == nil {
		t.Fatal("no pending record after capture")
	}
	if pending.Parsed.LocationID == nil || *pending.Parsed.LocationID != "LOC-A14" {
		t.Errorf("location not resolved through the chain: %+v", pending.Parsed)
	}
	if pending.Parsed.PartNumber == nil || *pending.Parsed.PartNumber != "PN-1001" {
		t.Errorf("part not resolved through the chain: %+v", pending.Parsed)
	}

	// 2. Confirm.
	if err := s.Confirm(); err != nil {
		t.Fatal(err)
	}
	if s.State() != session.StateArmed {
		t.Errorf("state after confirm = %s", s.State())
	}

	// 3. Sync push.
	sync, err := syncer.NewHTTP(st, syncer.Options{
		BaseURL: srv.URL, DeviceID: "dev-int", AllowInsecure: true,
		Backoff: func(int) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := sync.Push(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Pushed != 1 {
		t.Fatalf("pushed = %d, want 1", report.Pushed)
	}
	if len(received) != 1 || received[0].Parsed.ItemText != "RJ45 connectors" {
		t.Fatalf("backend received wrong data: %+v", received)
	}
	if received[0].Parsed.Quantity == nil || *received[0].Parsed.Quantity != 12 {
		t.Errorf("quantity lost in transit: %+v", received[0].Parsed)
	}

	// 4. Local record is now synced.
	got, err := st.Get(pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != observation.StatusSynced {
		t.Errorf("status after sync = %s", got.Status)
	}

	// 5. Export the queue to CSV.
	all, err := st.List(store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := export.CSV(&buf, all); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 { // header + record
		t.Fatalf("CSV rows = %d, want 2", len(rows))
	}
	if !strings.Contains(buf.String(), "RJ45 connectors") ||
		!strings.Contains(buf.String(), "synced") {
		t.Errorf("CSV missing expected content:\n%s", buf.String())
	}
}

// TestPipelineOfflineThenSync proves capture works with the backend down,
// then syncs cleanly when it returns (offline-first, §10.2).
func TestPipelineOfflineThenSync(t *testing.T) {
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := config.Default()
	cfg.DeviceID = "dev-off"
	mock := &asr.Mock{}
	s, err := session.New(cfg, session.Deps{Store: st, Transcriber: mock})
	if err != nil {
		t.Fatal(err)
	}

	// Capture three records fully offline (no server exists yet).
	s.Arm()
	for i, text := range []string{
		"five bags of washers in bin A-1",
		"ten reels of wire in bin A-2",
		"forty spools of Cat6 in bin A-3",
	} {
		*mock = asr.Mock{Results: []asr.Result{asr.TextResult(text, "en", 0.9)}}
		if err := s.BeginUtterance(); err != nil {
			t.Fatal(err)
		}
		s.FeedPCM(tone(0.6), 16000, 1)
		s.EndUtterance()
		if s.Pending() == nil {
			t.Fatalf("record %d not captured offline", i)
		}
		if err := s.Confirm(); err != nil {
			t.Fatal(err)
		}
	}
	unsynced, _ := st.UnsyncedConfirmed("", 0)
	if len(unsynced) != 3 {
		t.Fatalf("offline captures = %d, want 3", len(unsynced))
	}

	// Backend comes online; everything syncs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req syncer.PushRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		var resp syncer.PushResponse
		for _, o := range req.Observations {
			resp.Accepted = append(resp.Accepted, o.ID)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	sync, _ := syncer.NewHTTP(st, syncer.Options{
		BaseURL: srv.URL, DeviceID: "dev-off", AllowInsecure: true,
		BatchSize: 2, Backoff: func(int) time.Duration { return 0 },
	})
	report, err := sync.Push(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Pushed != 3 {
		t.Errorf("synced on reconnect = %d, want 3", report.Pushed)
	}
	leftover, _ := st.UnsyncedConfirmed("", 0)
	if len(leftover) != 0 {
		t.Errorf("unsynced after reconnect = %d", len(leftover))
	}
}
