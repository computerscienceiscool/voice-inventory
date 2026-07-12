package observation

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func valid(t *testing.T) *Observation {
	t.Helper()
	o, err := New("dev-1", "op-1")
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestNew(t *testing.T) {
	o := valid(t)
	u, err := uuid.Parse(o.ID)
	if err != nil {
		t.Fatalf("id not a uuid: %v", err)
	}
	if u.Version() != 7 {
		t.Errorf("uuid version = %d, want 7", u.Version())
	}
	if o.Status != StatusDraft || o.SchemaVersion != SchemaVersion {
		t.Errorf("defaults wrong: %+v", o)
	}
	if o.CapturedAt.Location() != time.UTC {
		t.Error("captured_at should be UTC")
	}
	// ids are time-ordered
	o2 := valid(t)
	if !(o.ID < o2.ID) {
		t.Errorf("uuidv7 ordering: %s !< %s", o.ID, o2.ID)
	}
}

func TestValidate(t *testing.T) {
	o := valid(t)
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Observation)
	}{
		{"nil", nil},
		{"bad id", func(o *Observation) { o.ID = "nope" }},
		{"zero time", func(o *Observation) { o.CapturedAt = time.Time{} }},
		{"bad status", func(o *Observation) { o.Status = "meh" }},
		{"empty language", func(o *Observation) { o.Language = "" }},
		{"zero schema", func(o *Observation) { o.SchemaVersion = 0 }},
		{"negative quantity", func(o *Observation) { q := -1.0; o.Parsed.Quantity = &q }},
		{"nan quantity", func(o *Observation) { q := math.NaN(); o.Parsed.Quantity = &q }},
		{"confidence range", func(o *Observation) { o.Confidence.ASR = 1.5 }},
		{"confidence nan", func(o *Observation) { o.Confidence.Item = math.NaN() }},
	}
	for _, c := range cases {
		if c.mutate == nil {
			var nilObs *Observation
			if err := nilObs.Validate(); err == nil {
				t.Error("nil observation should fail")
			}
			continue
		}
		o := valid(t)
		c.mutate(o)
		if err := o.Validate(); err == nil {
			t.Errorf("%s should fail validation", c.name)
		}
	}
}

func TestTransitions(t *testing.T) {
	allowed := map[[2]Status]bool{
		{StatusDraft, StatusConfirmed}:    true,
		{StatusDraft, StatusRejected}:     true,
		{StatusConfirmed, StatusSynced}:   true,
		{StatusConfirmed, StatusRejected}: true,
	}
	all := []Status{StatusDraft, StatusConfirmed, StatusSynced, StatusRejected}
	for _, from := range all {
		for _, to := range all {
			want := allowed[[2]Status{from, to}]
			if got := CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
	if StatusDraft.Valid() != true || Status("junk").Valid() != false {
		t.Error("Valid() wrong")
	}
}

func TestReadback(t *testing.T) {
	o := valid(t)
	q := 12.0
	u := "boxes"
	o.Parsed.Quantity = &q
	o.Parsed.Unit = &u
	o.Parsed.ItemText = "RJ45 connectors"
	o.Parsed.LocationText = "A-14"
	got := o.Readback("en")
	if got != "12 boxes, RJ45 connectors, A-14. Correct?" {
		t.Errorf("readback = %q", got)
	}
	es := o.Readback("es")
	if !strings.HasSuffix(es, "¿Correcto?") {
		t.Errorf("spanish readback = %q", es)
	}
	// missing fields are called out
	empty := valid(t)
	got = empty.Readback("en")
	for _, want := range []string{"no quantity", "no item", "no location"} {
		if !strings.Contains(got, want) {
			t.Errorf("readback %q missing %q", got, want)
		}
	}
	// fractional quantities render minimally
	q2 := 2.5
	o.Parsed.Quantity = &q2
	if !strings.HasPrefix(o.Readback("en"), "2.5 boxes") {
		t.Errorf("fractional readback = %q", o.Readback("en"))
	}
}

// TestWireShape locks the JSON encoding to the spec §6.1 field names.
func TestWireShape(t *testing.T) {
	o := valid(t)
	q := 40.0
	o.Parsed.Quantity = &q
	o.Parsed.ItemText = "Cat6"
	o.ApplyCorrection("location", "A-40", "A-14", time.Now())
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "device_id", "operator_id", "captured_at",
		"language", "raw_transcript", "audio_ref", "parsed", "confidence",
		"status", "corrections", "schema_version"} {
		if _, ok := m[key]; !ok {
			t.Errorf("wire JSON missing spec key %q", key)
		}
	}
	parsed := m["parsed"].(map[string]any)
	for _, key := range []string{"item_text", "part_number", "quantity", "unit",
		"location_text", "location_id", "description"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("parsed JSON missing spec key %q", key)
		}
	}
	conf := m["confidence"].(map[string]any)
	for _, key := range []string{"asr", "quantity", "location", "item"} {
		if _, ok := conf[key]; !ok {
			t.Errorf("confidence JSON missing spec key %q", key)
		}
	}
	corr := m["corrections"].([]any)[0].(map[string]any)
	for _, key := range []string{"field", "from", "to", "at"} {
		if _, ok := corr[key]; !ok {
			t.Errorf("correction JSON missing spec key %q", key)
		}
	}
}

func TestFormatQuantity(t *testing.T) {
	if FormatQuantity(nil) != "" {
		t.Error("nil should be empty")
	}
	for v, want := range map[float64]string{12: "12", 2.5: "2.5", 1200: "1200"} {
		vv := v
		if got := FormatQuantity(&vv); got != want {
			t.Errorf("FormatQuantity(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestApplyCorrection(t *testing.T) {
	o := valid(t)
	at := time.Date(2026, 7, 11, 10, 0, 0, 0, time.FixedZone("X", 3600))
	o.ApplyCorrection("location", "A-40", "A-14", at)
	if len(o.Corrections) != 1 {
		t.Fatal("correction not recorded")
	}
	c := o.Corrections[0]
	if c.Field != "location" || c.From != "A-40" || c.To != "A-14" {
		t.Errorf("correction = %+v", c)
	}
	if c.At.Location() != time.UTC {
		t.Error("correction time should be stored UTC")
	}
}
