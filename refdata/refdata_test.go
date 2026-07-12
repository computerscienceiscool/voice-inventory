package refdata

import (
	"testing"

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
