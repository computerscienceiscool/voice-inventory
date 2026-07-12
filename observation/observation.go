// Package observation defines the core inventory record produced by one
// spoken utterance (spec §6.1), its status lifecycle, and validation.
package observation

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// SchemaVersion is the current record schema version.
const SchemaVersion = 1

// Status is the lifecycle state of an observation.
//
// Transitions: draft → confirmed → synced; draft|confirmed → rejected.
// Synced records are immutable on the device (the backend owns them after
// upload); "scratch that" marks a record rejected rather than deleting it so
// the action is auditable.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusConfirmed Status = "confirmed"
	StatusSynced    Status = "synced"
	StatusRejected  Status = "rejected"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusConfirmed, StatusSynced, StatusRejected:
		return true
	}
	return false
}

// CanTransition reports whether a record may move from → to.
func CanTransition(from, to Status) bool {
	switch from {
	case StatusDraft:
		return to == StatusConfirmed || to == StatusRejected
	case StatusConfirmed:
		return to == StatusSynced || to == StatusRejected
	}
	return false
}

// Correction is one field change made after the initial parse (spec §6.1).
type Correction struct {
	Field string    `json:"field"`
	From  string    `json:"from"`
	To    string    `json:"to"`
	At    time.Time `json:"at"`
}

// Parsed holds the structured fields extracted from the transcript.
type Parsed struct {
	ItemText     string   `json:"item_text"`
	PartNumber   *string  `json:"part_number"`
	Quantity     *float64 `json:"quantity"`
	Unit         *string  `json:"unit"`
	LocationText string   `json:"location_text"`
	LocationID   *string  `json:"location_id"`
	Description  *string  `json:"description"`
}

// Confidence carries per-field confidence in [0,1] (spec §6.1).
type Confidence struct {
	ASR      float64 `json:"asr"`
	Quantity float64 `json:"quantity"`
	Location float64 `json:"location"`
	Item     float64 `json:"item"`
}

// Observation is one captured inventory record.
type Observation struct {
	ID            string       `json:"id"`
	DeviceID      string       `json:"device_id"`
	OperatorID    string       `json:"operator_id"`
	CapturedAt    time.Time    `json:"captured_at"`
	Language      string       `json:"language"`
	RawTranscript string       `json:"raw_transcript"`
	AudioRef      *string      `json:"audio_ref"`
	Parsed        Parsed       `json:"parsed"`
	Confidence    Confidence   `json:"confidence"`
	Status        Status       `json:"status"`
	Corrections   []Correction `json:"corrections"`
	// NeedsReview flags records with any doubtful or missing field so the
	// batch-review screen and backend can filter them (extension to §6.1).
	NeedsReview   bool     `json:"needs_review"`
	ReviewReasons []string `json:"review_reasons,omitempty"`
	SchemaVersion int      `json:"schema_version"`
}

// New creates a draft observation with a fresh UUIDv7 id and the capture
// time stamped in UTC.
func New(deviceID, operatorID string) (*Observation, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate uuidv7: %w", err)
	}
	return &Observation{
		ID:            id.String(),
		DeviceID:      deviceID,
		OperatorID:    operatorID,
		CapturedAt:    time.Now().UTC(),
		Language:      "en",
		Status:        StatusDraft,
		SchemaVersion: SchemaVersion,
	}, nil
}

// Validate checks structural invariants before persisting or syncing.
func (o *Observation) Validate() error {
	if o == nil {
		return errors.New("observation is nil")
	}
	if _, err := uuid.Parse(o.ID); err != nil {
		return fmt.Errorf("invalid id %q: %w", o.ID, err)
	}
	if o.CapturedAt.IsZero() {
		return errors.New("captured_at is zero")
	}
	if !o.Status.Valid() {
		return fmt.Errorf("invalid status %q", o.Status)
	}
	if o.Language == "" {
		return errors.New("language is empty")
	}
	if o.SchemaVersion <= 0 {
		return errors.New("schema_version must be positive")
	}
	if q := o.Parsed.Quantity; q != nil {
		if math.IsNaN(*q) || math.IsInf(*q, 0) {
			return errors.New("quantity is not a finite number")
		}
		if *q < 0 {
			return errors.New("quantity is negative")
		}
	}
	for _, c := range []struct {
		name string
		v    float64
	}{
		{"asr", o.Confidence.ASR},
		{"quantity", o.Confidence.Quantity},
		{"location", o.Confidence.Location},
		{"item", o.Confidence.Item},
	} {
		if c.v < 0 || c.v > 1 || math.IsNaN(c.v) {
			return fmt.Errorf("confidence.%s out of range: %v", c.name, c.v)
		}
	}
	return nil
}

// Clone returns a deep copy safe to hand to another goroutine: mutating
// the clone (or the original) never writes through shared slices or
// pointer fields.
func (o *Observation) Clone() *Observation {
	if o == nil {
		return nil
	}
	cp := *o
	cp.Corrections = append([]Correction(nil), o.Corrections...)
	cp.ReviewReasons = append([]string(nil), o.ReviewReasons...)
	cp.AudioRef = cloneStr(o.AudioRef)
	cp.Parsed.PartNumber = cloneStr(o.Parsed.PartNumber)
	cp.Parsed.Unit = cloneStr(o.Parsed.Unit)
	cp.Parsed.LocationID = cloneStr(o.Parsed.LocationID)
	cp.Parsed.Description = cloneStr(o.Parsed.Description)
	if o.Parsed.Quantity != nil {
		q := *o.Parsed.Quantity
		cp.Parsed.Quantity = &q
	}
	return &cp
}

func cloneStr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// ApplyCorrection records a field change in the corrections log.
func (o *Observation) ApplyCorrection(field, from, to string, at time.Time) {
	o.Corrections = append(o.Corrections, Correction{
		Field: field, From: from, To: to, At: at.UTC(),
	})
}

// FormatQuantity renders a quantity for display: integers without decimals,
// otherwise a minimal decimal form.
func FormatQuantity(q *float64) string {
	if q == nil {
		return ""
	}
	return strconv.FormatFloat(*q, 'f', -1, 64)
}

// Readback builds the confirmation line shown and spoken back to the
// operator (spec §4.1 step 5), e.g.
// "12 boxes, RJ45 connectors, A-14. Correct?".
func (o *Observation) Readback(langCode string) string {
	es := langCode == "es"
	var parts []string
	if o.Parsed.Quantity != nil {
		q := FormatQuantity(o.Parsed.Quantity)
		if o.Parsed.Unit != nil {
			q += " " + *o.Parsed.Unit
		}
		parts = append(parts, q)
	} else {
		if es {
			parts = append(parts, "sin cantidad")
		} else {
			parts = append(parts, "no quantity")
		}
	}
	if o.Parsed.ItemText != "" {
		parts = append(parts, o.Parsed.ItemText)
	} else {
		if es {
			parts = append(parts, "sin artículo")
		} else {
			parts = append(parts, "no item")
		}
	}
	if o.Parsed.LocationText != "" {
		parts = append(parts, o.Parsed.LocationText)
	} else {
		if es {
			parts = append(parts, "sin ubicación")
		} else {
			parts = append(parts, "no location")
		}
	}
	line := ""
	for i, p := range parts {
		if i > 0 {
			line += ", "
		}
		line += p
	}
	if es {
		return line + ". ¿Correcto?"
	}
	return line + ". Correct?"
}
