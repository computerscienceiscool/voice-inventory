package parser

import (
	"testing"

	"github.com/computerscienceiscool/voice-inventory/lang"
	"github.com/computerscienceiscool/voice-inventory/refdata"
)

func enOpts() Options { return Options{Lang: lang.English} }
func esOpts() Options { return Options{Lang: lang.Spanish} }

func fp(v float64) *float64 { return &v }

func checkQty(t *testing.T, r Result, want *float64) {
	t.Helper()
	if want == nil {
		if r.Parsed.Quantity != nil {
			t.Errorf("quantity = %v, want nil", *r.Parsed.Quantity)
		}
		return
	}
	if r.Parsed.Quantity == nil {
		t.Errorf("quantity = nil, want %v", *want)
		return
	}
	if *r.Parsed.Quantity != *want {
		t.Errorf("quantity = %v, want %v", *r.Parsed.Quantity, *want)
	}
}

func checkStr(t *testing.T, name string, got *string, want string) {
	t.Helper()
	if want == "" {
		if got != nil {
			t.Errorf("%s = %q, want nil", name, *got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s = nil, want %q", name, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %q, want %q", name, *got, want)
	}
}

// The three §5.3 examples from the spec must parse exactly.
func TestSpecExamples(t *testing.T) {
	t.Run("twelve boxes", func(t *testing.T) {
		r := Parse("Twelve boxes of RJ45 connectors in bin A-14", enOpts())
		checkQty(t, r, fp(12))
		checkStr(t, "unit", r.Parsed.Unit, "boxes")
		if r.Parsed.ItemText != "RJ45 connectors" {
			t.Errorf("item = %q", r.Parsed.ItemText)
		}
		if r.Parsed.LocationText != "A-14" {
			t.Errorf("location = %q", r.Parsed.LocationText)
		}
		checkStr(t, "description", r.Parsed.Description, "")
		if r.QuantityApprox {
			t.Error("quantity should not be approximate")
		}
	})

	t.Run("bin C-7 first", func(t *testing.T) {
		r := Parse("Bin C-7, forty spools of Cat6, three have water damage", enOpts())
		checkQty(t, r, fp(40))
		checkStr(t, "unit", r.Parsed.Unit, "spools")
		if r.Parsed.ItemText != "Cat6" {
			t.Errorf("item = %q", r.Parsed.ItemText)
		}
		if r.Parsed.LocationText != "C-7" {
			t.Errorf("location = %q", r.Parsed.LocationText)
		}
		checkStr(t, "description", r.Parsed.Description, "three have water damage")
	})

	t.Run("about a dozen", func(t *testing.T) {
		r := Parse("About a dozen assorted brackets, aisle five shelf two", enOpts())
		checkQty(t, r, fp(12))
		if !r.QuantityApprox {
			t.Error("quantity should be approximate (low confidence)")
		}
		checkStr(t, "unit", r.Parsed.Unit, "")
		if r.Parsed.ItemText != "assorted brackets" {
			t.Errorf("item = %q", r.Parsed.ItemText)
		}
		if r.Parsed.LocationText != "aisle 5 / shelf 2" {
			t.Errorf("location = %q", r.Parsed.LocationText)
		}
	})
}

func TestParseVariants(t *testing.T) {
	t.Run("spoken letter-number bin", func(t *testing.T) {
		r := Parse("three bags of washers in bin a 14", enOpts())
		checkQty(t, r, fp(3))
		if r.Parsed.LocationText != "A-14" {
			t.Errorf("location = %q", r.Parsed.LocationText)
		}
		if r.Parsed.ItemText != "washers" {
			t.Errorf("item = %q", r.Parsed.ItemText)
		}
	})

	t.Run("no quantity", func(t *testing.T) {
		r := Parse("RJ45 connectors in bin A-14", enOpts())
		checkQty(t, r, nil)
		if r.CertQuantity != 0 {
			t.Errorf("cert quantity = %v, want 0", r.CertQuantity)
		}
		if r.Parsed.ItemText != "RJ45 connectors" {
			t.Errorf("item = %q", r.Parsed.ItemText)
		}
	})

	t.Run("vague quantity", func(t *testing.T) {
		r := Parse("several boxes of hinges in bin B-2", enOpts())
		checkQty(t, r, nil)
		if !r.QuantityVague {
			t.Error("expected vague quantity")
		}
		checkStr(t, "unit", r.Parsed.Unit, "boxes")
		if r.Parsed.ItemText != "hinges" {
			t.Errorf("item = %q", r.Parsed.ItemText)
		}
	})

	t.Run("unknown unit kept raw", func(t *testing.T) {
		r := Parse("five skids of cement in bay 3", enOpts())
		checkQty(t, r, fp(5))
		checkStr(t, "unit", r.Parsed.Unit, "skids")
		if r.Parsed.ItemText != "cement" {
			t.Errorf("item = %q", r.Parsed.ItemText)
		}
		if r.Parsed.LocationText != "bay 3" {
			t.Errorf("location = %q", r.Parsed.LocationText)
		}
	})

	t.Run("article as quantity one", func(t *testing.T) {
		r := Parse("a box of nails on shelf 4", enOpts())
		checkQty(t, r, fp(1))
		checkStr(t, "unit", r.Parsed.Unit, "box")
		if r.Parsed.ItemText != "nails" {
			t.Errorf("item = %q", r.Parsed.ItemText)
		}
		if r.Parsed.LocationText != "shelf 4" {
			t.Errorf("location = %q", r.Parsed.LocationText)
		}
	})

	t.Run("trailing description", func(t *testing.T) {
		r := Parse("forty spools of Cat6 in bin A-14 near the door", enOpts())
		checkStr(t, "description", r.Parsed.Description, "near the door")
	})

	t.Run("code without dash", func(t *testing.T) {
		r := Parse("two reels of wire in bin c7", enOpts())
		if r.Parsed.LocationText != "C-7" {
			t.Errorf("location = %q", r.Parsed.LocationText)
		}
	})

	t.Run("item prefers quantity clause", func(t *testing.T) {
		r := Parse("damaged stuff, twelve boxes of RJ45 connectors in bin A-14", enOpts())
		if r.Parsed.ItemText != "RJ45 connectors" {
			t.Errorf("item = %q, want the span after the unit", r.Parsed.ItemText)
		}
		checkStr(t, "description", r.Parsed.Description, "damaged stuff")
		checkQty(t, r, fp(12))
	})

	t.Run("empty resolver scores like no resolver", func(t *testing.T) {
		opts := enOpts()
		opts.Resolver = refdata.NewIndex(nil, nil)
		r := Parse("twelve boxes of RJ45 in bin A-14", opts)
		if r.CertLocation != 0.75 || r.CertItem != 0.8 {
			t.Errorf("certainties with empty refdata: loc %.2f item %.2f, want 0.75/0.80",
				r.CertLocation, r.CertItem)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		r := Parse("", enOpts())
		if r.Parsed.ItemText != "" || r.Parsed.Quantity != nil {
			t.Errorf("empty parse should be empty: %+v", r)
		}
	})
}

func TestMidUtteranceCorrection(t *testing.T) {
	t.Run("location corrected", func(t *testing.T) {
		r := Parse("Twelve boxes of RJ45 in bin A-40, no, A-14", enOpts())
		if r.Parsed.LocationText != "A-14" {
			t.Errorf("location = %q, want A-14 (last value wins)", r.Parsed.LocationText)
		}
		if len(r.Overridden) != 1 || r.Overridden[0] != "location" {
			t.Errorf("overridden = %v", r.Overridden)
		}
		checkQty(t, r, fp(12))
	})

	t.Run("quantity corrected", func(t *testing.T) {
		r := Parse("Twelve boxes of RJ45 in bin A-14, I mean fifteen", enOpts())
		checkQty(t, r, fp(15))
		checkStr(t, "unit", r.Parsed.Unit, "boxes")
	})

	t.Run("negation without value stays description", func(t *testing.T) {
		r := Parse("forty spools of Cat6 in C-7, no water damage", enOpts())
		checkQty(t, r, fp(40))
		checkStr(t, "description", r.Parsed.Description, "no water damage")
		if len(r.Overridden) != 0 {
			t.Errorf("nothing should be overridden: %v", r.Overridden)
		}
	})
}

func TestSpanishParsing(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		r := Parse("cuarenta carretes de Cat6 en el bin C-7", esOpts())
		checkQty(t, r, fp(40))
		checkStr(t, "unit", r.Parsed.Unit, "carretes")
		if r.Parsed.ItemText != "Cat6" {
			t.Errorf("item = %q", r.Parsed.ItemText)
		}
		if r.Parsed.LocationText != "C-7" {
			t.Errorf("location = %q", r.Parsed.LocationText)
		}
	})

	t.Run("dozen and shelf", func(t *testing.T) {
		r := Parse("una docena de conectores RJ45 en el estante cinco", esOpts())
		checkQty(t, r, fp(12))
		if r.Parsed.ItemText != "conectores RJ45" {
			t.Errorf("item = %q", r.Parsed.ItemText)
		}
		if r.Parsed.LocationText != "estante 5" {
			t.Errorf("location = %q", r.Parsed.LocationText)
		}
	})

	t.Run("correction in spanish", func(t *testing.T) {
		r := Parse("doce cajas de tornillos en el bin A-40, digo, A-14", esOpts())
		if r.Parsed.LocationText != "A-14" {
			t.Errorf("location = %q", r.Parsed.LocationText)
		}
		checkQty(t, r, fp(12))
	})

	t.Run("approx", func(t *testing.T) {
		r := Parse("unos cuarenta tubos en el pasillo tres", esOpts())
		checkQty(t, r, fp(40))
		if !r.QuantityApprox {
			t.Error("expected approximate quantity")
		}
		if r.Parsed.LocationText != "pasillo 3" {
			t.Errorf("location = %q", r.Parsed.LocationText)
		}
	})
}

func TestResolverIntegration(t *testing.T) {
	x := refdata.NewIndex(
		[]refdata.Location{
			{ID: "LOC-A14", Name: "Bin A-14", Aliases: []string{"A-14", "A fourteen"}},
		},
		[]refdata.Part{
			{PartNumber: "PN-1001", Name: "RJ45 connector", Aliases: []string{"RJ45 connectors"}},
		},
	)
	opts := Options{Lang: lang.English, Resolver: x}
	r := Parse("Twelve boxes of RJ45 connectors in bin A-14", opts)
	checkStr(t, "location_id", r.Parsed.LocationID, "LOC-A14")
	checkStr(t, "part_number", r.Parsed.PartNumber, "PN-1001")
	if r.CertLocation < 0.9 || r.CertItem < 0.9 {
		t.Errorf("resolver certainties too low: loc %.2f item %.2f", r.CertLocation, r.CertItem)
	}

	r2 := Parse("Twelve boxes of mystery widgets in bin Z-99", opts)
	if r2.Parsed.LocationID != nil {
		t.Errorf("unknown location should not resolve: %v", *r2.Parsed.LocationID)
	}
	if r2.Parsed.PartNumber != nil {
		t.Errorf("unknown part should not resolve: %v", *r2.Parsed.PartNumber)
	}
	if r2.CertLocation != 0.6 || r2.CertItem != 0.7 {
		t.Errorf("unresolved certainties: loc %.2f item %.2f", r2.CertLocation, r2.CertItem)
	}
}

func TestMultiItem(t *testing.T) {
	opts := enOpts()
	opts.MultiItem = true
	rs := ParseAll("twelve boxes of RJ45 in bin A-14 and three reels of Cat6", opts)
	if len(rs) != 2 {
		t.Fatalf("expected 2 items, got %d: %+v", len(rs), rs)
	}
	checkQty(t, rs[0], fp(12))
	checkQty(t, rs[1], fp(3))
	if rs[1].Parsed.ItemText != "Cat6" {
		t.Errorf("second item = %q", rs[1].Parsed.ItemText)
	}
	if rs[1].Parsed.LocationText != "A-14" {
		t.Errorf("second location should inherit: %q", rs[1].Parsed.LocationText)
	}

	// "and" inside a description must NOT split.
	rs2 := ParseAll("forty spools of Cat6 in C-7, three are wet and two are missing ends", opts)
	if len(rs2) != 1 {
		t.Fatalf("description 'and' should not split: got %d results", len(rs2))
	}

	// MultiItem off → always one result.
	rs3 := ParseAll("twelve boxes of RJ45 in A-14 and three reels of Cat6", enOpts())
	if len(rs3) != 1 {
		t.Fatalf("multi-item disabled should yield 1, got %d", len(rs3))
	}
}

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in       string
		lang     lang.Code
		wantKind CommandKind
		wantFld  string
		wantVal  string
		wantOK   bool
	}{
		{"yes", lang.English, CmdConfirm, "", "", true},
		{"correct", lang.English, CmdConfirm, "", "", true},
		{"yes correct", lang.English, CmdConfirm, "", "", true},
		{"sí", lang.Spanish, CmdConfirm, "", "", true},
		{"scratch that", lang.English, CmdScratch, "", "", true},
		{"delete", lang.English, CmdScratch, "", "", true},
		{"borra eso", lang.Spanish, CmdScratch, "", "", true},
		{"no", lang.English, CmdReject, "", "", true},
		{"wrong", lang.English, CmdReject, "", "", true},
		{"location is A-40", lang.English, CmdSetField, "location", "A-40", true},
		{"the location is A-40", lang.English, CmdSetField, "location", "A-40", true},
		{"change quantity to 15", lang.English, CmdSetField, "quantity", "15", true},
		{"quantity is fifteen", lang.English, CmdSetField, "quantity", "fifteen", true},
		{"la ubicación es B-2", lang.Spanish, CmdSetField, "location", "B-2", true},
		{"cambia la cantidad a quince", lang.Spanish, CmdSetField, "quantity", "quince", true},
		{"no, A-14", lang.English, CmdSetField, "location", "A-14", true},
		{"no, fifteen", lang.English, CmdSetField, "quantity", "15", true},
		{"twelve boxes of RJ45 in A-14", lang.English, 0, "", "", false},
		{"", lang.English, 0, "", "", false},
	}
	for _, c := range cases {
		cmd, ok := ParseCommand(c.in, Options{Lang: c.lang})
		if ok != c.wantOK {
			t.Errorf("ParseCommand(%q) ok=%v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if cmd.Kind != c.wantKind {
			t.Errorf("ParseCommand(%q) kind=%v, want %v", c.in, cmd.Kind, c.wantKind)
		}
		if cmd.Field != c.wantFld || cmd.Value != c.wantVal {
			t.Errorf("ParseCommand(%q) = %q/%q, want %q/%q", c.in, cmd.Field, cmd.Value, c.wantFld, c.wantVal)
		}
	}
}

func TestFieldValueHelpers(t *testing.T) {
	text, _, _, ok := ResolveLocationText("bin a 14", enOpts())
	if !ok || text != "A-14" {
		t.Errorf("ResolveLocationText = %q ok=%v", text, ok)
	}
	text, _, _, ok = ResolveLocationText("the back corner", enOpts())
	if !ok || text != "the back corner" {
		t.Errorf("free-text location fallback = %q ok=%v", text, ok)
	}
	q, approx, ok := ParseQuantityText("about forty", enOpts())
	if !ok || q == nil || *q != 40 || !approx {
		t.Errorf("ParseQuantityText failed: %v %v %v", q, approx, ok)
	}
	if _, _, ok := ParseQuantityText("banana", enOpts()); ok {
		t.Error("non-number should not parse")
	}
}

func TestTokenizer(t *testing.T) {
	toks := tokenize("Bin C-7, forty spools.")
	if len(toks) != 4 {
		t.Fatalf("token count = %d: %+v", len(toks), toks)
	}
	if !toks[1].ClauseEnd {
		t.Error("comma should end clause after C-7")
	}
	if toks[1].Fold != "c-7" {
		t.Errorf("fold = %q", toks[1].Fold)
	}
	// hyphenated number words split; codes do not
	toks = tokenize("forty-two a-14")
	if len(toks) != 3 || toks[0].Fold != "forty" || toks[1].Fold != "two" || toks[2].Fold != "a-14" {
		t.Errorf("hyphen splitting wrong: %+v", toks)
	}
	// thousands separator survives; trailing comma is a clause end
	toks = tokenize("1,200, more")
	if toks[0].Fold != "1,200" || !toks[0].ClauseEnd {
		t.Errorf("numeric comma handling wrong: %+v", toks)
	}
}

// Whisper often renders spoken codes with a spaced dash: "bin A - 14".
func TestSpacedDashCode(t *testing.T) {
	r := Parse("nails in bin A - 14", enOpts())
	if r.Parsed.LocationText != "A-14" {
		t.Errorf("location = %q, want A-14", r.Parsed.LocationText)
	}
	checkQty(t, r, nil)
	if r.Parsed.ItemText != "nails" {
		t.Errorf("item = %q", r.Parsed.ItemText)
	}
}

// "several hundred" must stay a vague, flagged quantity — not an exact 100.
func TestVagueScaleQuantity(t *testing.T) {
	r := Parse("several hundred bolts in bin B-2", enOpts())
	checkQty(t, r, nil)
	if !r.QuantityVague {
		t.Error("expected vague quantity")
	}
	if r.Parsed.ItemText != "bolts" {
		t.Errorf("item = %q", r.Parsed.ItemText)
	}
	if r.CertQuantity >= 0.5 {
		t.Errorf("cert quantity = %v, want low", r.CertQuantity)
	}
}

// A corrected unit must not be demoted away, and the corrected-away word
// must not come back as the item.
func TestOverrideUnitNotDemoted(t *testing.T) {
	r := Parse("forty tubes in aisle 3, no, fifty reels", enOpts())
	checkQty(t, r, fp(50))
	checkStr(t, "unit", r.Parsed.Unit, "reels")
	if r.Parsed.ItemText == "tubes" {
		t.Errorf("corrected-away unit resurrected as item: %q", r.Parsed.ItemText)
	}
}
