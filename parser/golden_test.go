package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/computerscienceiscool/voice-inventory/lang"
)

// goldenCase is one transcript with its expected parse (spec §15: the
// parse half of the golden suite; the audio half lives in asr/golden).
type goldenCase struct {
	Transcript  string   `json:"transcript"`
	Quantity    *float64 `json:"quantity"`
	Approx      bool     `json:"approx"`
	Unit        *string  `json:"unit"`
	Item        string   `json:"item"`
	Location    string   `json:"location"`
	Description string   `json:"description"`
	Note        string   `json:"note"`
}

func runGolden(t *testing.T, file string, code lang.Code) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Transcript, func(t *testing.T) {
			r := Parse(c.Transcript, Options{Lang: code})
			if c.Quantity == nil {
				if r.Parsed.Quantity != nil {
					t.Errorf("quantity = %v, want none", *r.Parsed.Quantity)
				}
			} else if r.Parsed.Quantity == nil || *r.Parsed.Quantity != *c.Quantity {
				t.Errorf("quantity = %v, want %v", r.Parsed.Quantity, *c.Quantity)
			}
			if c.Approx != r.QuantityApprox {
				t.Errorf("approx = %v, want %v", r.QuantityApprox, c.Approx)
			}
			if c.Unit != nil {
				if *c.Unit == "" {
					if r.Parsed.Unit != nil {
						t.Errorf("unit = %q, want none", *r.Parsed.Unit)
					}
				} else if r.Parsed.Unit == nil || *r.Parsed.Unit != *c.Unit {
					t.Errorf("unit = %v, want %q", r.Parsed.Unit, *c.Unit)
				}
			}
			if r.Parsed.ItemText != c.Item {
				t.Errorf("item = %q, want %q", r.Parsed.ItemText, c.Item)
			}
			if r.Parsed.LocationText != c.Location {
				t.Errorf("location = %q, want %q", r.Parsed.LocationText, c.Location)
			}
			if c.Description != "" {
				if r.Parsed.Description == nil || *r.Parsed.Description != c.Description {
					t.Errorf("description = %v, want %q", r.Parsed.Description, c.Description)
				}
			}
		})
	}
}

func TestGoldenEnglish(t *testing.T) { runGolden(t, "golden_en.json", lang.English) }
func TestGoldenSpanish(t *testing.T) { runGolden(t, "golden_es.json", lang.Spanish) }
