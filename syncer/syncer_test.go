package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/computerscienceiscool/voice-inventory/observation"
	"github.com/computerscienceiscool/voice-inventory/refdata"
	"github.com/computerscienceiscool/voice-inventory/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func addConfirmed(t *testing.T, s *store.Store, n int) []string {
	t.Helper()
	var ids []string
	for i := 0; i < n; i++ {
		o, err := observation.New("dev-1", "op-1")
		if err != nil {
			t.Fatal(err)
		}
		o.RawTranscript = "test"
		o.Parsed.ItemText = "widgets"
		if err := s.Insert(o); err != nil {
			t.Fatal(err)
		}
		if err := s.Confirm(o.ID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, o.ID)
	}
	return ids
}

func noBackoff(int) time.Duration { return 0 }

func TestPushBatches(t *testing.T) {
	s := testStore(t)
	addConfirmed(t, s, 5)

	var batches atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/observations:batch" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-1" {
			t.Errorf("auth = %q", got)
		}
		var req PushRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req.DeviceID != "dev-1" {
			t.Errorf("device = %q", req.DeviceID)
		}
		batches.Add(1)
		var resp PushResponse
		for _, o := range req.Observations {
			if o.Status != observation.StatusConfirmed {
				t.Errorf("pushed record has status %s", o.Status)
			}
			resp.Accepted = append(resp.Accepted, o.ID)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	h, err := NewHTTP(s, Options{
		BaseURL: srv.URL, Token: "tok-1", DeviceID: "dev-1",
		BatchSize: 2, AllowInsecure: true, Backoff: noBackoff,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := h.Push(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Pushed != 5 {
		t.Errorf("pushed = %d, want 5", report.Pushed)
	}
	if batches.Load() != 3 {
		t.Errorf("batches = %d, want 3 (2+2+1)", batches.Load())
	}
	left, _ := s.UnsyncedConfirmed("", 0)
	if len(left) != 0 {
		t.Errorf("unsynced left = %d", len(left))
	}
	// idempotent second pass: nothing to do
	report, err = h.Push(context.Background())
	if err != nil || report.Pushed != 0 || report.Batches != 0 {
		t.Errorf("second push = %+v, %v", report, err)
	}
}

func TestPushRejectedNotMarked(t *testing.T) {
	s := testStore(t)
	ids := addConfirmed(t, s, 2)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req PushRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := PushResponse{
			Accepted: []string{req.Observations[0].ID},
			Rejected: []RejectedRecord{{ID: req.Observations[1].ID, Reason: "duplicate"}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	h, _ := NewHTTP(s, Options{BaseURL: srv.URL, DeviceID: "d",
		AllowInsecure: true, Backoff: noBackoff})
	report, err := h.Push(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Pushed != 1 || len(report.Rejected) != 1 || report.Rejected[0].ID != ids[1] {
		t.Errorf("report = %+v", report)
	}
	got, _ := s.Get(ids[1])
	if got.Status != observation.StatusConfirmed {
		t.Errorf("rejected record should stay confirmed, got %s", got.Status)
	}
	// A rerun must terminate even though the rejected record is still queued.
	done := make(chan struct{})
	go func() {
		_, _ = h.Push(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("push loops forever on rejected records")
	}
}

func TestPushRetriesOn5xx(t *testing.T) {
	s := testStore(t)
	addConfirmed(t, s, 1)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "flaky", http.StatusInternalServerError)
			return
		}
		var req PushRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(PushResponse{Accepted: []string{req.Observations[0].ID}})
	}))
	defer srv.Close()

	h, _ := NewHTTP(s, Options{BaseURL: srv.URL, DeviceID: "d",
		AllowInsecure: true, Backoff: noBackoff})
	report, err := h.Push(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Pushed != 1 || hits.Load() != 2 {
		t.Errorf("pushed=%d hits=%d", report.Pushed, hits.Load())
	}
}

func TestPushAuthFailureIsFatal(t *testing.T) {
	s := testStore(t)
	addConfirmed(t, s, 1)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "bad token", http.StatusUnauthorized)
	}))
	defer srv.Close()

	h, _ := NewHTTP(s, Options{BaseURL: srv.URL, DeviceID: "d",
		AllowInsecure: true, Backoff: noBackoff})
	_, err := h.Push(context.Background())
	var fatal *FatalError
	if !errors.As(err, &fatal) || fatal.Status != 401 {
		t.Fatalf("expected FatalError 401, got %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("401 must not retry, hits=%d", hits.Load())
	}
}

func TestPullRefDataWithETag(t *testing.T) {
	s := testStore(t)
	payload := RefDataResponse{
		Locations: []refdata.Location{{ID: "L1", Name: "Bin A-14", Aliases: []string{"A-14"}}},
		Parts:     []refdata.Part{{PartNumber: "P1", Name: "RJ45"}},
		Units:     []refdata.Unit{{Name: "skid", Language: "en"}},
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/v1/refdata" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	h, _ := NewHTTP(s, Options{BaseURL: srv.URL, DeviceID: "d",
		AllowInsecure: true, Backoff: noBackoff})
	report, err := h.PullRefData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Locations != 1 || report.Parts != 1 || report.Units != 1 || report.NotModified {
		t.Errorf("report = %+v", report)
	}
	locs, _ := s.Locations()
	if len(locs) != 1 || locs[0].ID != "L1" {
		t.Errorf("locations not cached: %+v", locs)
	}

	report, err = h.PullRefData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.NotModified {
		t.Errorf("second pull should be 304: %+v", report)
	}
	locs, _ = s.Locations()
	if len(locs) != 1 {
		t.Error("304 must keep cached data")
	}
}

func TestNewHTTPValidation(t *testing.T) {
	s := testStore(t)
	if _, err := NewHTTP(s, Options{BaseURL: "http://example.com"}); !errors.Is(err, ErrInsecureEndpoint) {
		t.Errorf("plain http should be rejected: %v", err)
	}
	if _, err := NewHTTP(s, Options{BaseURL: "https://example.com"}); err != nil {
		t.Errorf("https should be accepted: %v", err)
	}
	if _, err := NewHTTP(s, Options{BaseURL: "not a url"}); err == nil {
		t.Error("garbage URL should be rejected")
	}
	if _, err := NewHTTP(nil, Options{BaseURL: "https://example.com"}); err == nil {
		t.Error("nil store should be rejected")
	}
}

// A record the backend keeps rejecting must not starve the records queued
// behind it (cursor pagination walks the whole queue each pass).
func TestPushRejectedDoesNotStarve(t *testing.T) {
	s := testStore(t)
	ids := addConfirmed(t, s, 3)
	rejectID := ids[0]

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req PushRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		var resp PushResponse
		for _, o := range req.Observations {
			if o.ID == rejectID {
				resp.Rejected = append(resp.Rejected, RejectedRecord{ID: o.ID, Reason: "nope"})
			} else {
				resp.Accepted = append(resp.Accepted, o.ID)
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// batch size 1 puts the rejected record alone at the head of the queue
	h, _ := NewHTTP(s, Options{BaseURL: srv.URL, DeviceID: "d",
		BatchSize: 1, AllowInsecure: true, Backoff: noBackoff})
	report, err := h.Push(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Pushed != 2 {
		t.Errorf("pushed = %d, want 2 (records behind the reject must sync)", report.Pushed)
	}
	for _, id := range ids[1:] {
		got, _ := s.Get(id)
		if got.Status != observation.StatusSynced {
			t.Errorf("%s status = %s, want synced", id, got.Status)
		}
	}
	got, _ := s.Get(rejectID)
	if got.Status != observation.StatusConfirmed {
		t.Errorf("rejected record status = %s, want confirmed (retry later)", got.Status)
	}
}
