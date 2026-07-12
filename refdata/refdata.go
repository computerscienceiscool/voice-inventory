// Package refdata holds reference data synced down from the backend
// (spec §6.2) — known locations, part vocabulary, unit synonyms — and the
// fuzzy resolvers that map spoken text onto it. Matching is suggestive,
// never blocking: an unmatched value simply resolves to nothing.
package refdata

import (
	"strings"

	"github.com/computerscienceiscool/voice-inventory/fuzzy"
	"github.com/computerscienceiscool/voice-inventory/lang"
)

// Location is a known bin/shelf/aisle with spoken aliases.
type Location struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

// Part is a known part number with spoken names.
type Part struct {
	PartNumber string   `json:"part_number"`
	Name       string   `json:"name"`
	Aliases    []string `json:"aliases"`
}

// Unit is an extra recognized unit word (extends the built-in table).
type Unit struct {
	Name     string   `json:"name"`
	Language string   `json:"language"`
	Aliases  []string `json:"aliases"`
}

// Default acceptance thresholds for fuzzy resolution.
const (
	DefaultLocationThreshold = 0.85
	DefaultPartThreshold     = 0.85
)

type entry struct {
	id   string
	keys []string // folded name/aliases
}

// Index resolves spoken location and item text against reference data.
type Index struct {
	locations         []entry
	parts             []entry
	LocationThreshold float64
	PartThreshold     float64
}

// NewIndex builds a resolver index. Nil/empty slices are fine — resolution
// just never matches.
func NewIndex(locs []Location, parts []Part) *Index {
	x := &Index{
		LocationThreshold: DefaultLocationThreshold,
		PartThreshold:     DefaultPartThreshold,
	}
	for _, l := range locs {
		keys := foldKeys(append([]string{l.Name, l.ID}, l.Aliases...))
		x.locations = append(x.locations, entry{id: l.ID, keys: keys})
	}
	for _, p := range parts {
		keys := foldKeys(append([]string{p.Name, p.PartNumber}, p.Aliases...))
		x.parts = append(x.parts, entry{id: p.PartNumber, keys: keys})
	}
	return x
}

func foldKeys(raw []string) []string {
	var keys []string
	seen := map[string]bool{}
	for _, r := range raw {
		k := lang.Fold(strings.TrimSpace(r))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	return keys
}

// CanonCode collapses a location code for comparison: folded, with spaces
// and dashes removed, so "A-14" == "a 14" == "a14".
func CanonCode(s string) string {
	s = lang.Fold(s)
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '-' || r == '.' || r == '/' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// depluralize strips a simple plural suffix from each token; enough for
// "connectors" vs "connector" style variation in part names.
func depluralize(s string) string {
	tokens := strings.Fields(s)
	for i, t := range tokens {
		if strings.HasSuffix(t, "es") && len(t) > 4 {
			tokens[i] = t[:len(t)-2]
		} else if strings.HasSuffix(t, "s") && len(t) > 3 {
			tokens[i] = t[:len(t)-1]
		}
	}
	return strings.Join(tokens, " ")
}

func bestScore(text string, e entry, code bool) float64 {
	folded := lang.Fold(strings.TrimSpace(text))
	if folded == "" {
		return 0
	}
	canon := CanonCode(folded)
	deplu := depluralize(folded)
	best := 0.0
	for _, k := range e.keys {
		if k == folded {
			return 1
		}
		if code && canon != "" && CanonCode(k) == canon {
			if 0.98 > best {
				best = 0.98
			}
			continue
		}
		s := fuzzy.JaroWinkler(folded, k)
		if d := fuzzy.JaroWinkler(deplu, depluralize(k)); d > s {
			s = d
		}
		if s > best {
			best = s
		}
	}
	return best
}

// ResolveLocation maps spoken location text to a known location ID.
// ok is true only when the best score clears the threshold.
func (x *Index) ResolveLocation(text string) (id string, score float64, ok bool) {
	if x == nil {
		return "", 0, false
	}
	for _, e := range x.locations {
		if s := bestScore(text, e, true); s > score {
			score, id = s, e.id
		}
	}
	if score < x.LocationThreshold {
		return "", score, false
	}
	return id, score, true
}

// ResolvePart maps spoken item text to a known part number.
func (x *Index) ResolvePart(text string) (partNumber string, score float64, ok bool) {
	if x == nil {
		return "", 0, false
	}
	for _, e := range x.parts {
		if s := bestScore(text, e, false); s > score {
			score, partNumber = s, e.id
		}
	}
	if score < x.PartThreshold {
		return "", score, false
	}
	return partNumber, score, true
}

// UnitMap converts backend unit rows for one language into the folded
// spoken-form → canonical map the parser consumes. Rows for other languages
// are skipped; rows with an empty language apply to all languages.
func UnitMap(units []Unit, code lang.Code) map[string]string {
	m := map[string]string{}
	for _, u := range units {
		if u.Language != "" && u.Language != string(code) {
			continue
		}
		canon := lang.Fold(strings.TrimSpace(u.Name))
		if canon == "" {
			continue
		}
		m[canon] = canon
		for _, a := range u.Aliases {
			f := lang.Fold(strings.TrimSpace(a))
			if f != "" {
				m[f] = canon
			}
		}
	}
	return m
}
