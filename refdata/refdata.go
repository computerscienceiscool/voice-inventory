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

// key is one reference name/alias with its comparison forms precomputed
// once at index-build time — resolution runs on every utterance all day
// (§12), so re-folding per query would be wasted battery.
type key struct {
	fold  string // lower-cased, accent-stripped
	canon string // fold with spaces/dashes removed ("a14")
	deplu string // fold with simple plurals stripped
}

type entry struct {
	id   string
	keys []key
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

func foldKeys(raw []string) []key {
	var keys []key
	seen := map[string]bool{}
	for _, r := range raw {
		f := lang.Fold(strings.TrimSpace(r))
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		keys = append(keys, key{fold: f, canon: canon(f), deplu: depluralize(f)})
	}
	return keys
}

// CanonCode collapses a location code for comparison: folded, with spaces
// and dashes removed, so "A-14" == "a 14" == "a14".
func CanonCode(s string) string { return canon(lang.Fold(s)) }

// canon removes separators from an already-folded string.
func canon(folded string) string {
	if !strings.ContainsAny(folded, " -./") {
		return folded
	}
	var b strings.Builder
	b.Grow(len(folded))
	for _, r := range folded {
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

// query holds the caller's spoken text in the same precomputed forms as a
// key, so bestScore does zero string allocation per reference entry.
type query struct {
	fold, canon, deplu string
}

func newQuery(text string) (query, bool) {
	f := lang.Fold(strings.TrimSpace(text))
	if f == "" {
		return query{}, false
	}
	return query{fold: f, canon: canon(f), deplu: depluralize(f)}, true
}

func bestScore(q query, e entry, code bool) float64 {
	best := 0.0
	for i := range e.keys {
		k := &e.keys[i]
		if k.fold == q.fold {
			return 1
		}
		if code && q.canon != "" && k.canon == q.canon {
			if 0.98 > best {
				best = 0.98
			}
			continue
		}
		s := fuzzy.JaroWinkler(q.fold, k.fold)
		if d := fuzzy.JaroWinkler(q.deplu, k.deplu); d > s {
			s = d
		}
		if s > best {
			best = s
		}
	}
	return best
}

// HasLocations reports whether any location reference data is loaded.
func (x *Index) HasLocations() bool { return x != nil && len(x.locations) > 0 }

// HasParts reports whether any part reference data is loaded.
func (x *Index) HasParts() bool { return x != nil && len(x.parts) > 0 }

// ResolveLocation maps spoken location text to a known location ID.
// ok is true only when the best score clears the threshold.
func (x *Index) ResolveLocation(text string) (id string, score float64, ok bool) {
	if x == nil {
		return "", 0, false
	}
	q, ok := newQuery(text)
	if !ok {
		return "", 0, false
	}
	for i := range x.locations {
		if s := bestScore(q, x.locations[i], true); s > score {
			score, id = s, x.locations[i].id
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
	q, ok := newQuery(text)
	if !ok {
		return "", 0, false
	}
	for i := range x.parts {
		if s := bestScore(q, x.parts[i], false); s > score {
			score, partNumber = s, x.parts[i].id
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
