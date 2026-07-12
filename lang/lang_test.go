package lang

import (
	"math"
	"strings"
	"testing"
)

func TestFold(t *testing.T) {
	cases := map[string]string{
		"Ubicación":  "ubicacion",
		"MÁS":        "mas",
		"año":        "ano",
		"Boxes":      "boxes",
		"cañería":    "caneria",
		"dieciséis":  "dieciseis",
		"plain":      "plain",
		"A-14":       "a-14",
		"Über":       "uber",
	}
	for in, want := range cases {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPhraseSet(t *testing.T) {
	ps := NewPhraseSet(Phrase{"scratch", "that"}, Phrase{"scratch"}, Phrase{"never", "mind"})
	toks := []string{"scratch", "that", "please"}
	if n := ps.MatchAt(toks, 0); n != 2 {
		t.Errorf("expected longest match 2, got %d", n)
	}
	if n := ps.MatchAt([]string{"scratch"}, 0); n != 1 {
		t.Errorf("expected match 1, got %d", n)
	}
	if n := ps.MatchAt(toks, 1); n != 0 {
		t.Errorf("expected no match, got %d", n)
	}
	if n := ps.MatchAt(toks, 5); n != 0 {
		t.Errorf("out of range should be 0, got %d", n)
	}
}

type numCase struct {
	in     string
	value  float64 // NaN → expect vague
	approx bool
	count  int // expected number of results (default 1; 0 = none)
}

func runNumCases(t *testing.T, table *Table, cases []numCase) {
	t.Helper()
	for _, c := range cases {
		tokens := strings.Fields(c.in)
		for i := range tokens {
			tokens[i] = Fold(tokens[i])
		}
		nums := table.ScanNumbers(tokens)
		wantCount := c.count
		if wantCount == 0 && !math.IsNaN(c.value) && c.value == -1 {
			wantCount = 0
		} else if wantCount == 0 {
			wantCount = 1
		}
		if c.value == -1 && !math.IsNaN(c.value) { // sentinel: expect no numbers
			if len(nums) != 0 {
				t.Errorf("%q: expected no numbers, got %+v", c.in, nums)
			}
			continue
		}
		if len(nums) != wantCount {
			t.Errorf("%q: expected %d numbers, got %+v", c.in, wantCount, nums)
			continue
		}
		n := nums[0]
		if math.IsNaN(c.value) {
			if !n.Vague {
				t.Errorf("%q: expected vague, got %+v", c.in, n)
			}
		} else if n.Value != c.value {
			t.Errorf("%q: value = %v, want %v", c.in, n.Value, c.value)
		}
		if n.Approx != c.approx {
			t.Errorf("%q: approx = %v, want %v", c.in, n.Approx, c.approx)
		}
	}
}

func TestScanNumbersEnglish(t *testing.T) {
	en := Get(English)
	runNumCases(t, en, []numCase{
		{in: "twelve", value: 12},
		{in: "forty two", value: 42},
		{in: "a dozen", value: 12},
		{in: "half a dozen", value: 6},
		{in: "two dozen", value: 24},
		{in: "one hundred and five", value: 105},
		{in: "twenty five hundred", value: 2500},
		{in: "three thousand two hundred and ten", value: 3210},
		{in: "about forty", value: 40, approx: true},
		{in: "a couple", value: 2, approx: true},
		{in: "a few", value: 3, approx: true},
		{in: "several", value: math.NaN(), approx: true},
		{in: "some forty", value: 40, approx: true},
		{in: "14", value: 14},
		{in: "12.5", value: 12.5},
		{in: "3 dozen", value: 36},
		{in: "a hundred", value: 100},
		{in: "a thousand", value: 1000},
		{in: "aisle five shelf two", value: 5, count: 2},
		{in: "a box", value: -1},
		{in: "and", value: -1},
		{in: "nothing here", value: -1},
	})
}

func TestScanNumbersSpanish(t *testing.T) {
	es := Get(Spanish)
	runNumCases(t, es, []numCase{
		{in: "cuarenta", value: 40},
		{in: "cuarenta y dos", value: 42},
		{in: "veintidós", value: 22},
		{in: "dos mil quinientos cuarenta y dos", value: 2542},
		{in: "una docena", value: 12},
		{in: "media docena", value: 6},
		{in: "un par", value: 2, approx: true},
		{in: "unos cuarenta", value: 40, approx: true},
		{in: "más o menos diez", value: 10, approx: true},
		{in: "varios", value: math.NaN(), approx: true},
		{in: "cien", value: 100},
		{in: "ciento cinco", value: 105},
		{in: "quinientos tres", value: 503},
		{in: "un carrete", value: 1},
		{in: "mil", value: 1000},
		{in: "12,5", value: 12.5},
		{in: "la caja", value: -1},
	})
}

func TestScanNumbersSpans(t *testing.T) {
	en := Get(English)
	tokens := []string{"about", "a", "dozen", "assorted", "brackets"}
	nums := en.ScanNumbers(tokens)
	if len(nums) != 1 {
		t.Fatalf("expected 1 number, got %+v", nums)
	}
	n := nums[0]
	if n.Value != 12 || !n.Approx {
		t.Errorf("value/approx wrong: %+v", n)
	}
	if n.MarkStart != 0 || n.Start != 1 || n.End != 3 {
		t.Errorf("span wrong: %+v", n)
	}
}

func TestGetTables(t *testing.T) {
	if Get(English) == nil || Get(Spanish) == nil {
		t.Fatal("expected tables for en and es")
	}
	if Get(Auto) != nil || Get("fr") != nil {
		t.Error("auto/unknown should return nil")
	}
	if GetOrDefault("zz").Code != English {
		t.Error("GetOrDefault should fall back to English")
	}
	if !Known(English) || !Known(Spanish) || Known(Auto) {
		t.Error("Known() wrong")
	}
}
