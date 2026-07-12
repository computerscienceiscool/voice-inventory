package parser

import (
	"testing"

	"github.com/computerscienceiscool/voice-inventory/lang"
	"github.com/computerscienceiscool/voice-inventory/refdata"
)

// FuzzParse asserts the parser never panics and keeps basic invariants,
// whatever the transcript looks like.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"Twelve boxes of RJ45 connectors in bin A-14",
		"Bin C-7, forty spools of Cat6, three have water damage",
		"About a dozen assorted brackets, aisle five shelf two",
		"doce cajas de conectores RJ45 en el bin A-14",
		"a couple hundred, no, fifteen",
		"in in in bin bin - - , , and and a a",
		"1,200 12,5 12. .5 - — – ¿¡ «»",
		"no",
		"scratch that",
		"twelve boxes and three reels and a box of nails and several",
		"aisle five shelf two rack 9 bin a 14 row 3",
		"半ダース the ünïcödé bin Ω-14",
	}
	for _, s := range seeds {
		f.Add(s, false)
		f.Add(s, true)
	}
	resolver := refdata.NewIndex(
		[]refdata.Location{{ID: "L1", Name: "Bin A-14", Aliases: []string{"A-14"}}},
		[]refdata.Part{{PartNumber: "P1", Name: "RJ45 connector"}},
	)
	f.Fuzz(func(t *testing.T, text string, spanish bool) {
		code := lang.English
		if spanish {
			code = lang.Spanish
		}
		opts := Options{Lang: code, Resolver: resolver, MultiItem: true}
		for _, r := range ParseAll(text, opts) {
			if q := r.Parsed.Quantity; q != nil && (*q < 0 || *q != *q) {
				t.Fatalf("bad quantity %v from %q", *q, text)
			}
			for _, c := range []float64{r.CertQuantity, r.CertLocation, r.CertItem} {
				if c < 0 || c > 1 {
					t.Fatalf("certainty %v out of range from %q", c, text)
				}
			}
		}
		_, _ = ParseCommand(text, opts)
		_, _, _, _ = ResolveLocationText(text, opts)
		_, _, _ = ParseQuantityText(text, opts)
	})
}
