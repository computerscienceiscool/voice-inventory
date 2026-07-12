// Package lang holds the per-language vocabulary tables that drive the
// deterministic parser: anchor keywords, unit vocabulary, number words,
// correction/command verbs, and folding rules. New vocabulary is data, not
// code (§7 of the spec): adding a synonym means adding a table entry.
package lang

import (
	"strings"
)

// Code identifies a supported language.
type Code string

const (
	English Code = "en"
	Spanish Code = "es"
	Auto    Code = "auto" // resolve from ASR-detected language at runtime
)

// Known reports whether c is a concrete language with a rule table.
func Known(c Code) bool { return c == English || c == Spanish }

// Fold normalizes a token for table lookups: lower-case and diacritics
// stripped (á→a, ñ→n, …). It does not remove punctuation.
func Fold(s string) string {
	s = strings.ToLower(s)
	return strings.Map(func(r rune) rune {
		if d, ok := deaccent[r]; ok {
			return d
		}
		return r
	}, s)
}

var deaccent = map[rune]rune{
	'á': 'a', 'à': 'a', 'ä': 'a', 'â': 'a', 'ã': 'a',
	'é': 'e', 'è': 'e', 'ë': 'e', 'ê': 'e',
	'í': 'i', 'ì': 'i', 'ï': 'i', 'î': 'i',
	'ó': 'o', 'ò': 'o', 'ö': 'o', 'ô': 'o', 'õ': 'o',
	'ú': 'u', 'ù': 'u', 'ü': 'u', 'û': 'u',
	'ñ': 'n', 'ç': 'c',
}

// Phrase is a sequence of folded tokens matched as a unit.
type Phrase []string

// PhraseSet matches multi-token phrases at a position, longest-first.
type PhraseSet struct {
	byFirst map[string][]Phrase
}

// NewPhraseSet builds a PhraseSet from phrases; empty phrases are ignored.
func NewPhraseSet(phrases ...Phrase) *PhraseSet {
	ps := &PhraseSet{byFirst: map[string][]Phrase{}}
	for _, p := range phrases {
		if len(p) == 0 {
			continue
		}
		ps.byFirst[p[0]] = append(ps.byFirst[p[0]], p)
	}
	// Longest phrase first per bucket so MatchAt is greedy.
	for k, list := range ps.byFirst {
		sorted := append([]Phrase(nil), list...)
		for i := 1; i < len(sorted); i++ {
			for j := i; j > 0 && len(sorted[j]) > len(sorted[j-1]); j-- {
				sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			}
		}
		ps.byFirst[k] = sorted
	}
	return ps
}

// MatchAt returns the length of the longest phrase starting at tokens[i],
// or 0 if none matches.
func (ps *PhraseSet) MatchAt(tokens []string, i int) int {
	if ps == nil || i >= len(tokens) {
		return 0
	}
	for _, p := range ps.byFirst[tokens[i]] {
		if i+len(p) > len(tokens) {
			continue
		}
		match := true
		for k, w := range p {
			if tokens[i+k] != w {
				match = false
				break
			}
		}
		if match {
			return len(p)
		}
	}
	return 0
}

// vagueEntry maps a spoken vague-quantity phrase to a numeric value.
// A NaN value means "no usable number" (quantity stays null, flagged).
type vagueEntry struct {
	phrase Phrase
	value  float64 // math.NaN() for purely vague words
}

// Table is the full rule set for one language. All keys are folded tokens.
type Table struct {
	Code Code

	// Location grammar.
	LocationNouns   map[string]bool // bin, shelf, aisle, …
	KeepAnchorNouns map[string]bool // nouns kept in location_text when value is a bare number ("aisle 5")
	LocationPreps   map[string]bool // in, at / en
	Articles        map[string]bool // the / el, la, …

	// Quantity grammar.
	Ones          map[string]float64
	Tens          map[string]float64
	Hundreds      map[string]float64 // direct hundreds words (Spanish: quinientos, …)
	ScaleHundred  map[string]bool    // hundred / cien, ciento
	ScaleBig      map[string]float64 // thousand, million / mil, millón
	DozenWords    map[string]bool
	HalfWords     map[string]bool
	NumConnectors map[string]bool // and / y (only valid inside a number run)
	NumArticles   map[string]bool // a, an / un, una (run starters before scales/dozens)
	approxSet     *PhraseSet
	vagues        []vagueEntry
	vagueSet      *PhraseSet

	// Units: folded spoken form → canonical singular form.
	Units map[string]string
	Of    map[string]bool // of / de, del

	// Commands and corrections.
	ConfirmWords map[string]bool
	RejectWords  map[string]bool
	scratchSet   *PhraseSet
	negationSet  *PhraseSet
	FieldAliases map[string]string // spoken field name → canonical field
	IsWords      map[string]bool   // is / es
	ChangeWords  map[string]bool   // change, set / cambia, pon
	ToWords      map[string]bool   // to / a, por
	Conjunctions map[string]bool   // and / y (multi-item splitting)
}

// Get returns the rule table for a concrete language code (en, es).
// It returns nil for Auto or unknown codes.
func Get(c Code) *Table {
	switch c {
	case English:
		return englishTable
	case Spanish:
		return spanishTable
	}
	return nil
}

// GetOrDefault returns the table for c, falling back to English.
func GetOrDefault(c Code) *Table {
	if t := Get(c); t != nil {
		return t
	}
	return englishTable
}

// MatchScratch returns the token length of a "scratch that"-style phrase at
// position i, or 0.
func (t *Table) MatchScratch(tokens []string, i int) int { return t.scratchSet.MatchAt(tokens, i) }

// MatchNegation returns the token length of a correction-starter phrase
// ("no", "i mean", "digo", …) at position i, or 0.
func (t *Table) MatchNegation(tokens []string, i int) int { return t.negationSet.MatchAt(tokens, i) }

// MatchApproxEndingAt returns the token length of an approximation-marker
// phrase that ends immediately before position i ("about", "unos", …), or 0.
func (t *Table) MatchApproxEndingAt(tokens []string, i int) int {
	if t.approxSet == nil {
		return 0
	}
	// Try longest phrases first by probing every possible start.
	for start := i - maxApproxLen; start < i; start++ {
		if start < 0 {
			continue
		}
		if n := t.approxSet.MatchAt(tokens, start); n > 0 && start+n == i {
			return n
		}
	}
	return 0
}

const maxApproxLen = 3

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}
