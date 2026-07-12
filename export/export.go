// Package export renders observations to operator-facing formats. CSV via
// the platform share sheet is the recommended batch-review export
// (docs/proposals.md, TODO item 072); the JSON path stays available for
// integrations. Kept separate from store so both the CLI and the mobile
// facade share one column definition.
package export

import (
	"encoding/csv"
	"io"
	"strconv"

	"github.com/computerscienceiscool/voice-inventory/observation"
)

// Columns is the fixed CSV header, in order. Stable across releases so
// downstream spreadsheets don't break.
var Columns = []string{
	"id",
	"captured_at",
	"operator_id",
	"device_id",
	"quantity",
	"unit",
	"item_text",
	"part_number",
	"location_text",
	"location_id",
	"status",
	"needs_review",
	"sync_rejected_reason",
	"description",
	"raw_transcript",
}

// CSV writes observations as RFC 4180 CSV with the Columns header. The
// encoding/csv writer quotes and escapes every field, so hostile
// transcripts (commas, quotes, newlines, injection strings) are safe.
func CSV(w io.Writer, obs []*observation.Observation) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(Columns); err != nil {
		return err
	}
	for _, o := range obs {
		if err := cw.Write(row(o)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func row(o *observation.Observation) []string {
	return []string{
		o.ID,
		o.CapturedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		o.OperatorID,
		o.DeviceID,
		observation.FormatQuantity(o.Parsed.Quantity),
		deref(o.Parsed.Unit),
		o.Parsed.ItemText,
		deref(o.Parsed.PartNumber),
		o.Parsed.LocationText,
		deref(o.Parsed.LocationID),
		string(o.Status),
		strconv.FormatBool(o.NeedsReview),
		o.SyncRejectedReason,
		deref(o.Parsed.Description),
		o.RawTranscript,
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
