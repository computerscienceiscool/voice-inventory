package export

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/computerscienceiscool/voice-inventory/observation"
)

func obs(t *testing.T, mutate func(*observation.Observation)) *observation.Observation {
	t.Helper()
	o, err := observation.New("dev-1", "op-1")
	if err != nil {
		t.Fatal(err)
	}
	o.CapturedAt = time.Date(2026, 7, 12, 9, 30, 0, 0, time.UTC)
	o.Parsed.ItemText = "RJ45 connectors"
	q := 12.0
	o.Parsed.Quantity = &q
	u := "boxes"
	o.Parsed.Unit = &u
	o.Parsed.LocationText = "A-14"
	o.Status = observation.StatusConfirmed
	if mutate != nil {
		mutate(o)
	}
	return o
}

func TestCSVRoundTrip(t *testing.T) {
	pn := "PN-1001"
	loc := "LOC-A14"
	desc := "three wet"
	o := obs(t, func(o *observation.Observation) {
		o.Parsed.PartNumber = &pn
		o.Parsed.LocationID = &loc
		o.Parsed.Description = &desc
		o.NeedsReview = true
		o.SyncRejectedReason = "duplicate"
		o.RawTranscript = "twelve boxes of RJ45 in A-14"
	})
	var buf bytes.Buffer
	if err := CSV(&buf, []*observation.Observation{o}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want header + 1", len(rows))
	}
	if len(rows[0]) != len(Columns) || rows[0][0] != "id" {
		t.Errorf("header = %v", rows[0])
	}
	got := rows[1]
	want := map[string]string{
		"captured_at":          "2026-07-12T09:30:00Z",
		"operator_id":          "op-1",
		"quantity":             "12",
		"unit":                 "boxes",
		"item_text":            "RJ45 connectors",
		"part_number":          "PN-1001",
		"location_id":          "LOC-A14",
		"status":               "confirmed",
		"needs_review":         "true",
		"sync_rejected_reason": "duplicate",
		"description":          "three wet",
	}
	col := map[string]int{}
	for i, c := range Columns {
		col[c] = i
	}
	for k, v := range want {
		if got[col[k]] != v {
			t.Errorf("%s = %q, want %q", k, got[col[k]], v)
		}
	}
}

func TestCSVEmptyFields(t *testing.T) {
	o := obs(t, func(o *observation.Observation) {
		o.Parsed.Quantity = nil // no quantity
		o.Parsed.Unit = nil
	})
	var buf bytes.Buffer
	if err := CSV(&buf, []*observation.Observation{o}); err != nil {
		t.Fatal(err)
	}
	rows, _ := csv.NewReader(&buf).ReadAll()
	col := map[string]int{}
	for i, c := range Columns {
		col[c] = i
	}
	if rows[1][col["quantity"]] != "" || rows[1][col["unit"]] != "" {
		t.Errorf("nil fields should be empty: %v", rows[1])
	}
}

// Hostile transcript content must not break the CSV structure or enable
// injection; encoding/csv quotes it.
func TestCSVHostileContent(t *testing.T) {
	// encoding/csv's Reader normalizes \r\n to \n inside quoted fields, so
	// `want` reflects the post-round-trip value; the point is that hostile
	// content never breaks the CSV structure or escapes its field.
	nasty := []struct{ name, val, want string }{
		{"comma", "a, b, c", "a, b, c"},
		{"quote", `say "hi"`, `say "hi"`},
		{"newline", "line1\nline2", "line1\nline2"},
		{"formula", "=SUM(A1:A9)", "=SUM(A1:A9)"}, // injection attempt, stays inert text
		{"crlf", "x\r\ny", "x\ny"},
	}
	var all []*observation.Observation
	for _, c := range nasty {
		v := c.val
		all = append(all, obs(t, func(o *observation.Observation) {
			o.Parsed.ItemText = v
			o.RawTranscript = v
		}))
	}
	var buf bytes.Buffer
	if err := CSV(&buf, all); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("hostile content broke CSV parsing: %v", err)
	}
	if len(rows) != len(nasty)+1 {
		t.Fatalf("rows = %d, want %d", len(rows), len(nasty)+1)
	}
	col := map[string]int{}
	for i, c := range Columns {
		col[c] = i
	}
	for i, c := range nasty {
		if rows[i+1][col["item_text"]] != c.want {
			t.Errorf("%s: item = %q, want %q", c.name, rows[i+1][col["item_text"]], c.want)
		}
	}
}

func TestCSVEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := CSV(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "id,captured_at") {
		t.Errorf("empty export should still write the header: %q", buf.String())
	}
	rows, _ := csv.NewReader(&buf).ReadAll()
	if len(rows) != 1 {
		t.Errorf("empty export rows = %d, want 1 (header)", len(rows))
	}
}
