package refdata

import (
	"strings"
	"testing"

	"github.com/computerscienceiscool/voice-inventory/fuzzy"
	"github.com/computerscienceiscool/voice-inventory/lang"
)

func testIndex() *Index {
	locs := []Location{
		{ID: "LOC-A14", Name: "Bin A-14", Aliases: []string{"A-14", "A fourteen", "bin A 14"}},
		{ID: "LOC-C7", Name: "Bin C-7", Aliases: []string{"C-7"}},
		{ID: "LOC-AISLE5-S2", Name: "Aisle 5 Shelf 2", Aliases: []string{"aisle 5 shelf 2"}},
	}
	parts := []Part{
		{PartNumber: "PN-1001", Name: "RJ45 connector", Aliases: []string{"RJ45 connectors", "ethernet connector"}},
		{PartNumber: "PN-2002", Name: "Cat6 cable", Aliases: []string{"Cat6", "category 6 cable"}},
	}
	return NewIndex(locs, parts)
}

func TestResolveLocation(t *testing.T) {
	x := testIndex()
	cases := []struct {
		in     string
		wantID string
		wantOK bool
	}{
		{"A-14", "LOC-A14", true},
		{"a 14", "LOC-A14", true},
		{"a14", "LOC-A14", true},
		{"bin a-14", "LOC-A14", true},
		{"C-7", "LOC-C7", true},
		{"aisle 5 / shelf 2", "LOC-AISLE5-S2", true},
		{"zz-99", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		id, score, ok := x.ResolveLocation(c.in)
		if ok != c.wantOK {
			t.Errorf("ResolveLocation(%q) ok=%v (score %.2f), want %v", c.in, ok, score, c.wantOK)
			continue
		}
		if ok && id != c.wantID {
			t.Errorf("ResolveLocation(%q) = %q, want %q", c.in, id, c.wantID)
		}
	}
}

func TestResolvePart(t *testing.T) {
	x := testIndex()
	cases := []struct {
		in     string
		wantPN string
		wantOK bool
	}{
		{"RJ45 connectors", "PN-1001", true},
		{"rj45 connector", "PN-1001", true},
		{"Cat6", "PN-2002", true},
		{"cat 6 cable", "PN-2002", true},
		{"mystery widget", "", false},
	}
	for _, c := range cases {
		pn, score, ok := x.ResolvePart(c.in)
		if ok != c.wantOK {
			t.Errorf("ResolvePart(%q) ok=%v (score %.2f), want %v", c.in, ok, score, c.wantOK)
			continue
		}
		if ok && pn != c.wantPN {
			t.Errorf("ResolvePart(%q) = %q, want %q", c.in, pn, c.wantPN)
		}
	}
}

func TestNilIndex(t *testing.T) {
	var x *Index
	if _, _, ok := x.ResolveLocation("A-14"); ok {
		t.Error("nil index should not resolve")
	}
	if _, _, ok := x.ResolvePart("thing"); ok {
		t.Error("nil index should not resolve")
	}
}

func TestCanonCode(t *testing.T) {
	for _, c := range [][2]string{
		{"A-14", "a14"}, {"a 14", "a14"}, {"A.14", "a14"}, {"a/14", "a14"},
	} {
		if got := CanonCode(c[0]); got != c[1] {
			t.Errorf("CanonCode(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

func TestUnitMap(t *testing.T) {
	units := []Unit{
		{Name: "skid", Language: "en", Aliases: []string{"skids"}},
		{Name: "tarima", Language: "es", Aliases: []string{"tarimas"}},
		{Name: "lot", Language: "", Aliases: []string{"lots"}},
	}
	m := UnitMap(units, lang.English)
	if m["skid"] != "skid" || m["skids"] != "skid" {
		t.Errorf("en units missing: %v", m)
	}
	if _, ok := m["tarima"]; ok {
		t.Error("es unit should be filtered out for en")
	}
	if m["lot"] != "lot" {
		t.Error("language-neutral unit should apply")
	}
}

// Locks the resolver optimization's behavior: precomputed key forms must
// resolve identically to naive per-query folding across tricky inputs
// (case, accents, separators, plurals, unicode, ties, near-threshold).
func TestResolverBehaviorPreserved(t *testing.T) {
	x := NewIndex(
		[]Location{
			{ID: "L1", Name: "Bin A-14", Aliases: []string{"A-14", "A fourteen"}},
			{ID: "L2", Name: "Pasillo Cinco", Aliases: []string{"pasillo 5"}},
			{ID: "L3", Name: "Zona Ñ", Aliases: []string{"zona n"}},
		},
		[]Part{
			{PartNumber: "P1", Name: "RJ45 connector", Aliases: []string{"RJ45 connectors"}},
			{PartNumber: "P2", Name: "cable", Aliases: nil},
		},
	)
	locInputs := []string{
		"A-14", "a 14", "A14", "a-14", "A FOURTEEN", "bin a-14",
		"pasillo cinco", "PASILLO 5", "zona ñ", "zona Ñ", "ZONA N",
		"", "   ", "zz-99", "a-140", "cinco",
	}
	for _, in := range locInputs {
		gotID, gotScore, gotOK := x.ResolveLocation(in)
		wantID, wantScore, wantOK := naiveResolveLocation(x, in)
		if gotOK != wantOK || gotID != wantID ||
			(gotScore-wantScore > 1e-9 || wantScore-gotScore > 1e-9) {
			t.Errorf("ResolveLocation(%q) = (%q,%.6f,%v), naive = (%q,%.6f,%v)",
				in, gotID, gotScore, gotOK, wantID, wantScore, wantOK)
		}
	}
	partInputs := []string{
		"RJ45 connector", "rj45 connectors", "RJ45 CONNECTOR", "cable",
		"cables", "connector", "", "widget",
	}
	for _, in := range partInputs {
		gotPN, gotScore, gotOK := x.ResolvePart(in)
		wantPN, wantScore, wantOK := naiveResolvePart(x, in)
		if gotOK != wantOK || gotPN != wantPN ||
			(gotScore-wantScore > 1e-9 || wantScore-gotScore > 1e-9) {
			t.Errorf("ResolvePart(%q) = (%q,%.6f,%v), naive = (%q,%.6f,%v)",
				in, gotPN, gotScore, gotOK, wantPN, wantScore, wantOK)
		}
	}
}

// naiveResolveLocation reimplements resolution the old per-query way, from
// the raw folded strings, as an independent oracle.
func naiveResolveLocation(x *Index, text string) (string, float64, bool) {
	return naiveResolve(x.locations, text, true, x.LocationThreshold)
}
func naiveResolvePart(x *Index, text string) (string, float64, bool) {
	return naiveResolve(x.parts, text, false, x.PartThreshold)
}

func naiveResolve(entries []entry, text string, code bool, threshold float64) (string, float64, bool) {
	// Fold the query fresh and re-derive canon/deplu from each key's fold
	// on the fly (not the precomputed key.canon/key.deplu), so this is an
	// independent oracle for the optimization.
	folded := lang.Fold(strings.TrimSpace(text))
	if folded == "" {
		return "", 0, false
	}
	qCanon := canon(folded)
	qDeplu := depluralize(folded)
	bestID, best := "", 0.0
	for _, e := range entries {
		s := 0.0
		for _, k := range e.keys {
			kf := k.fold
			switch {
			case kf == folded:
				s = 1
			case code && qCanon != "" && canon(kf) == qCanon:
				if 0.98 > s {
					s = 0.98
				}
			default:
				v := fuzzy.JaroWinkler(folded, kf)
				if d := fuzzy.JaroWinkler(qDeplu, depluralize(kf)); d > v {
					v = d
				}
				if v > s {
					s = v
				}
			}
		}
		if s > best {
			best, bestID = s, e.id
		}
	}
	if best < threshold {
		return "", best, false
	}
	return bestID, best, true
}
