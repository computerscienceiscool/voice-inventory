package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/computerscienceiscool/voice-inventory/observation"
	"github.com/computerscienceiscool/voice-inventory/refdata"
)

func newObs(t *testing.T) *observation.Observation {
	t.Helper()
	o, err := observation.New("dev-1", "op-1")
	if err != nil {
		t.Fatal(err)
	}
	o.RawTranscript = "twelve boxes of RJ45 connectors in bin A-14"
	o.Parsed.ItemText = "RJ45 connectors"
	q := 12.0
	o.Parsed.Quantity = &q
	u := "boxes"
	o.Parsed.Unit = &u
	o.Parsed.LocationText = "A-14"
	o.Confidence = observation.Confidence{ASR: 0.95, Quantity: 0.95, Location: 0.9, Item: 0.8}
	return o
}

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestInsertGetRoundTrip(t *testing.T) {
	s := openTest(t)
	o := newObs(t)
	desc := "three damaged"
	o.Parsed.Description = &desc
	o.ReviewReasons = []string{"low quantity confidence"}
	o.NeedsReview = true
	o.ApplyCorrection("location", "A-40", "A-14", time.Now())

	if err := s.Insert(o); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != o.ID || got.DeviceID != "dev-1" || got.OperatorID != "op-1" {
		t.Errorf("identity fields wrong: %+v", got)
	}
	if got.Parsed.ItemText != "RJ45 connectors" || *got.Parsed.Quantity != 12 ||
		*got.Parsed.Unit != "boxes" || got.Parsed.LocationText != "A-14" ||
		*got.Parsed.Description != "three damaged" {
		t.Errorf("parsed fields wrong: %+v", got.Parsed)
	}
	if got.Confidence.ASR != 0.95 || !got.NeedsReview || len(got.ReviewReasons) != 1 {
		t.Errorf("meta wrong: %+v", got)
	}
	if len(got.Corrections) != 1 || got.Corrections[0].To != "A-14" {
		t.Errorf("corrections wrong: %+v", got.Corrections)
	}
	if !got.CapturedAt.Equal(o.CapturedAt.Truncate(time.Nanosecond)) {
		t.Errorf("captured_at drift: %v vs %v", got.CapturedAt, o.CapturedAt)
	}
}

func TestGetNotFound(t *testing.T) {
	s := openTest(t)
	if _, err := s.Get("00000000-0000-7000-8000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestStatusLifecycle(t *testing.T) {
	s := openTest(t)
	o := newObs(t)
	if err := s.Insert(o); err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkSynced([]string{o.ID}, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(o.ID)
	if got.Status != observation.StatusSynced {
		t.Errorf("status = %s, want synced", got.Status)
	}
	// synced is terminal
	if err := s.Confirm(o.ID); !errors.Is(err, ErrBadTransition) {
		t.Errorf("expected ErrBadTransition, got %v", err)
	}
	if err := s.Reject(o.ID); !errors.Is(err, ErrBadTransition) {
		t.Errorf("expected ErrBadTransition, got %v", err)
	}
	// synced is immutable
	got.Parsed.ItemText = "changed"
	if err := s.Update(got); !errors.Is(err, ErrImmutable) {
		t.Errorf("expected ErrImmutable, got %v", err)
	}
}

func TestRejectDraft(t *testing.T) {
	s := openTest(t)
	o := newObs(t)
	if err := s.Insert(o); err != nil {
		t.Fatal(err)
	}
	if err := s.Reject(o.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(o.ID)
	if got.Status != observation.StatusRejected {
		t.Errorf("status = %s", got.Status)
	}
}

func TestUpdate(t *testing.T) {
	s := openTest(t)
	o := newObs(t)
	if err := s.Insert(o); err != nil {
		t.Fatal(err)
	}
	q := 15.0
	o.Parsed.Quantity = &q
	o.Parsed.LocationText = "A-40"
	if err := s.Update(o); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(o.ID)
	if *got.Parsed.Quantity != 15 || got.Parsed.LocationText != "A-40" {
		t.Errorf("update not applied: %+v", got.Parsed)
	}
}

func TestUnsyncedAndMarkSynced(t *testing.T) {
	s := openTest(t)
	var ids []string
	for i := 0; i < 3; i++ {
		o := newObs(t)
		if err := s.Insert(o); err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			if err := s.Confirm(o.ID); err != nil {
				t.Fatal(err)
			}
		}
		ids = append(ids, o.ID)
	}
	unsynced, err := s.UnsyncedConfirmed("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(unsynced) != 2 {
		t.Fatalf("unsynced = %d, want 2 (draft must not sync)", len(unsynced))
	}
	if unsynced[0].ID > unsynced[1].ID {
		t.Error("unsynced should be oldest-first")
	}
	n, err := s.MarkSynced([]string{ids[0], ids[1], ids[2]}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("marked = %d, want 2 (draft skipped)", n)
	}
	counts, err := s.CountsByStatus()
	if err != nil {
		t.Fatal(err)
	}
	if counts[observation.StatusSynced] != 2 || counts[observation.StatusDraft] != 1 {
		t.Errorf("counts = %v", counts)
	}
}

func TestListFilters(t *testing.T) {
	s := openTest(t)
	for i := 0; i < 5; i++ {
		o := newObs(t)
		o.NeedsReview = i%2 == 0
		if err := s.Insert(o); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.List(Filter{})
	if err != nil || len(all) != 5 {
		t.Fatalf("list all: %d, %v", len(all), err)
	}
	if all[0].ID < all[4].ID {
		t.Error("List should be newest-first")
	}
	nr := true
	flagged, err := s.List(Filter{NeedsReview: &nr})
	if err != nil || len(flagged) != 3 {
		t.Fatalf("needs_review filter: %d, %v", len(flagged), err)
	}
	limited, err := s.List(Filter{Limit: 2, Offset: 1})
	if err != nil || len(limited) != 2 {
		t.Fatalf("limit/offset: %d, %v", len(limited), err)
	}
}

func TestLastActive(t *testing.T) {
	s := openTest(t)
	if _, err := s.LastActive(); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty store should be ErrNotFound, got %v", err)
	}
	o1 := newObs(t)
	o2 := newObs(t)
	_ = s.Insert(o1)
	_ = s.Insert(o2)
	id, err := s.LastActive()
	if err != nil || id != o2.ID {
		t.Errorf("last active = %s, want %s (err %v)", id, o2.ID, err)
	}
	_ = s.Reject(o2.ID)
	id, _ = s.LastActive()
	if id != o1.ID {
		t.Errorf("after reject, last active = %s, want %s", id, o1.ID)
	}
}

func TestAudioPurge(t *testing.T) {
	s := openTest(t)
	o := newObs(t)
	ref := "audio/clip1.wav"
	o.AudioRef = &ref
	_ = s.Insert(o)
	_ = s.Confirm(o.ID)
	if _, err := s.MarkSynced([]string{o.ID}, time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	cands, err := s.AudioToPurge(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].AudioRef != ref {
		t.Fatalf("purge candidates = %+v", cands)
	}
	if err := s.ClearAudioRef(o.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(o.ID)
	if got.AudioRef != nil {
		t.Error("audio_ref should be cleared")
	}
	cands, _ = s.AudioToPurge(time.Now())
	if len(cands) != 0 {
		t.Errorf("nothing left to purge, got %+v", cands)
	}
}

func TestRefData(t *testing.T) {
	s := openTest(t)
	locs := []refdata.Location{
		{ID: "L1", Name: "Bin A-14", Aliases: []string{"A-14"}},
		{ID: "L2", Name: "Bin C-7", Aliases: []string{"C-7"}},
	}
	parts := []refdata.Part{{PartNumber: "PN-1", Name: "RJ45", Aliases: []string{"connectors"}}}
	units := []refdata.Unit{{Name: "skid", Language: "en", Aliases: []string{"skids"}}}
	if err := s.ReplaceLocations(locs); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceParts(parts); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceUnits(units); err != nil {
		t.Fatal(err)
	}
	gl, err := s.Locations()
	if err != nil || len(gl) != 2 || gl[0].Aliases[0] != "A-14" {
		t.Fatalf("locations round trip: %+v, %v", gl, err)
	}
	gp, _ := s.Parts()
	if len(gp) != 1 || gp[0].PartNumber != "PN-1" {
		t.Fatalf("parts round trip: %+v", gp)
	}
	gu, _ := s.Units()
	if len(gu) != 1 || gu[0].Name != "skid" {
		t.Fatalf("units round trip: %+v", gu)
	}
	// replace-all really replaces
	if err := s.ReplaceLocations(locs[:1]); err != nil {
		t.Fatal(err)
	}
	gl, _ = s.Locations()
	if len(gl) != 1 {
		t.Fatalf("replace should leave 1, got %d", len(gl))
	}
}

func TestSyncState(t *testing.T) {
	s := openTest(t)
	v, err := s.GetSyncState("etag")
	if err != nil || v != "" {
		t.Fatalf("missing key should be empty: %q %v", v, err)
	}
	_ = s.SetSyncState("etag", "abc")
	_ = s.SetSyncState("etag", "def")
	v, _ = s.GetSyncState("etag")
	if v != "def" {
		t.Errorf("etag = %q, want def", v)
	}
}

// TestFileDurability simulates a force-quit: write, close nothing, reopen
// the same file, and confirm the confirmed record is present (§12).
func TestFileDurability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "obs.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	o := newObs(t)
	if err := s.Insert(o); err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm(o.ID); err != nil {
		t.Fatal(err)
	}
	// no clean Close — reopen and read
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.Get(o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != observation.StatusConfirmed {
		t.Errorf("status after reopen = %s", got.Status)
	}
	_ = s.Close()
}

func TestInsertValidation(t *testing.T) {
	s := openTest(t)
	o := newObs(t)
	o.ID = "not-a-uuid"
	if err := s.Insert(o); err == nil {
		t.Error("invalid id should fail validation")
	}
}

// Sub-second timestamps must compare correctly in SQL: time.RFC3339Nano
// trims trailing zeros, which broke lexicographic ordering ("…00.5Z" sorts
// before "…00Z"). The store uses a fixed-width format instead.
func TestAudioPurgeSubsecondTimestamps(t *testing.T) {
	s := openTest(t)

	// synced_at strictly AFTER the cutoff (by 0.5 s) → must NOT purge
	oA := newObs(t)
	refA := "a.wav"
	oA.AudioRef = &refA
	_ = s.Insert(oA)
	_ = s.Confirm(oA.ID)
	cutoff := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.MarkSynced([]string{oA.ID}, cutoff.Add(500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	cands, err := s.AudioToPurge(cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 0 {
		t.Errorf("record synced after cutoff must not purge: %+v", cands)
	}

	// synced_at strictly BEFORE a sub-second cutoff → must purge
	cands, err = s.AudioToPurge(cutoff.Add(600 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Errorf("record synced before cutoff must purge: %+v", cands)
	}
}

// Offset must work without an explicit Limit (SQLite needs LIMIT -1).
func TestListOffsetWithoutLimit(t *testing.T) {
	s := openTest(t)
	for i := 0; i < 4; i++ {
		if err := s.Insert(newObs(t)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.List(Filter{Offset: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("offset-only list returned %d records, want 1", len(got))
	}
}

// Hostile field content must round-trip byte-for-byte: transcripts from
// ASR can contain anything.
func TestNastyStringRoundTrip(t *testing.T) {
	s := openTest(t)
	nasty := []string{
		"NUL\x00inside",
		"quotes ' \" `` and; DROP TABLE observations; --",
		"emoji 📦🔊 and ünïcödé ñ",
		"newline\nand\ttabs\r\n",
		string([]byte{0xff, 0xfe, 'x'}), // invalid UTF-8
		strings.Repeat("long ", 5000),
	}
	for i, v := range nasty {
		o := newObs(t)
		o.RawTranscript = v
		o.Parsed.ItemText = v
		desc := v
		o.Parsed.Description = &desc
		if err := s.Insert(o); err != nil {
			t.Fatalf("case %d: insert: %v", i, err)
		}
		got, err := s.Get(o.ID)
		if err != nil {
			t.Fatalf("case %d: get: %v", i, err)
		}
		if got.RawTranscript != v || got.Parsed.ItemText != v || *got.Parsed.Description != v {
			t.Errorf("case %d: round trip mangled the value (len %d→%d)",
				i, len(v), len(got.RawTranscript))
		}
	}
}
