package lang

import (
	"math"
	"strconv"
	"strings"
)

// Number is one spoken quantity found in a token stream.
type Number struct {
	Value  float64 // NaN when Vague (no usable numeric value)
	Approx bool    // "about forty", "a couple", "unos cuarenta"
	Vague  bool    // "several", "varios" — quantity should stay null, flagged
	// Token span of the number words themselves: [Start, End).
	Start, End int
	// MarkStart extends Start to include approximation-marker tokens
	// ("about", "más o menos") so the caller can consume them too.
	MarkStart int
}

// ScanNumbers finds every spoken or numeric quantity in the folded token
// stream, left to right, longest-match first. It never overlaps results.
func (t *Table) ScanNumbers(tokens []string) []Number {
	var out []Number
	i := 0
	for i < len(tokens) {
		if entry, vlen := t.matchVague(tokens, i); vlen > 0 {
			next := i + vlen
			// "a couple hundred", "a few thousand": the vague value seeds
			// the scale words that follow.
			if !math.IsNaN(entry.value) && t.startsScale(tokens, next) {
				if num, ok := t.parseSeededRun(tokens, next, entry.value); ok {
					num.Start = i
					num.MarkStart = i
					num.Approx = true
					if m := t.MatchApproxEndingAt(tokens, i); m > 0 {
						num.MarkStart = i - m
					}
					out = append(out, num)
					i = num.End
					continue
				}
			}
			// "several hundred": no usable value, but the scale run belongs
			// to the vague phrase — consume it so it can't leak out as an
			// exact, full-confidence number.
			if math.IsNaN(entry.value) && t.startsScale(tokens, next) {
				if num, ok := t.parseRun(tokens, next); ok {
					v := Number{
						Value:     math.NaN(),
						Approx:    true,
						Vague:     true,
						Start:     i,
						End:       num.End,
						MarkStart: i,
					}
					if m := t.MatchApproxEndingAt(tokens, i); m > 0 {
						v.MarkStart = i - m
					}
					out = append(out, v)
					i = v.End
					continue
				}
			}
			// "some forty": the phrase is an approximation marker for the
			// number that follows — let the run path handle it.
			if !t.startsNumber(tokens, next) {
				num := Number{
					Value:     entry.value,
					Approx:    true,
					Vague:     math.IsNaN(entry.value),
					Start:     i,
					End:       next,
					MarkStart: i,
				}
				if m := t.MatchApproxEndingAt(tokens, i); m > 0 {
					num.MarkStart = i - m
				}
				out = append(out, num)
				i = num.End
				continue
			}
		}
		if num, ok := t.parseRun(tokens, i); ok {
			num.MarkStart = num.Start
			if m := t.MatchApproxEndingAt(tokens, num.Start); m > 0 {
				num.Approx = true
				num.MarkStart = num.Start - m
			}
			out = append(out, num)
			i = num.End
			continue
		}
		i++
	}
	return out
}

// startsScale reports whether tokens[i] is a scale word (hundred,
// thousand, dozen) that a preceding vague value can multiply.
func (t *Table) startsScale(tokens []string, i int) bool {
	if i >= len(tokens) {
		return false
	}
	tok := tokens[i]
	if t.ScaleHundred[tok] || t.DozenWords[tok] {
		return true
	}
	_, ok := t.ScaleBig[tok]
	return ok
}

// startsNumber reports whether tokens[i] can begin a numeric run.
func (t *Table) startsNumber(tokens []string, i int) bool {
	if i >= len(tokens) {
		return false
	}
	tok := tokens[i]
	if _, ok := t.Ones[tok]; ok {
		return true
	}
	if _, ok := t.Tens[tok]; ok {
		return true
	}
	if _, ok := t.Hundreds[tok]; ok {
		return true
	}
	if t.ScaleHundred[tok] || t.DozenWords[tok] {
		return true
	}
	if _, ok := t.ScaleBig[tok]; ok {
		return true
	}
	_, ok := parseDigits(tok)
	return ok
}

func (t *Table) matchVague(tokens []string, i int) (vagueEntry, int) {
	best := 0
	var bestEntry vagueEntry
	for _, v := range t.vagues {
		n := len(v.phrase)
		if n <= best || i+n > len(tokens) {
			continue
		}
		ok := true
		for k, w := range v.phrase {
			if tokens[i+k] != w {
				ok = false
				break
			}
		}
		if ok {
			best = n
			bestEntry = v
		}
	}
	return bestEntry, best
}

// parseRun consumes the longest well-formed number-word run starting at
// tokens[start] and returns its value.
func (t *Table) parseRun(tokens []string, start int) (Number, bool) {
	return t.parseRunFrom(tokens, start, 0, false)
}

// parseSeededRun parses a run whose small-value accumulator starts at seed
// ("a couple" = 2 before "hundred").
func (t *Table) parseSeededRun(tokens []string, start int, seed float64) (Number, bool) {
	return t.parseRunFrom(tokens, start, seed, true)
}

func (t *Table) parseRunFrom(tokens []string, start int, seed float64, seeded bool) (Number, bool) {
	i := start
	total, cur := 0.0, seed
	started := seeded
	onesSet, tensSet, hundSet := seeded, false, false
	halfPending := false

	for i < len(tokens) {
		tok := tokens[i]

		// "half a dozen" / "media docena"
		if !started && t.HalfWords[tok] {
			j := i + 1
			if j < len(tokens) && t.NumArticles[tokens[j]] {
				j++
			}
			if j < len(tokens) && t.DozenWords[tokens[j]] {
				halfPending = true
				i++
				continue
			}
			break
		}

		// Indefinite article before a scale word: "a dozen", "a hundred",
		// "un millón". Spanish "un/una" as the number 1 falls through to Ones.
		if t.NumArticles[tok] && !onesSet && !tensSet && cur == 0 {
			j := i + 1
			if j < len(tokens) {
				next := tokens[j]
				_, big := t.ScaleBig[next]
				if t.DozenWords[next] || t.ScaleHundred[next] || big {
					cur = 1
					started = true
					onesSet = true
					i++
					continue
				}
			}
			if _, isOne := t.Ones[tok]; !isOne {
				break
			}
		}

		if v, ok := t.Ones[tok]; ok {
			if onesSet {
				break
			}
			cur += v
			onesSet = true
			started = true
			i++
			continue
		}
		if v, ok := t.Tens[tok]; ok {
			if tensSet || onesSet {
				break
			}
			cur += v
			tensSet = true
			started = true
			i++
			continue
		}
		if v, ok := t.Hundreds[tok]; ok { // Spanish direct hundreds: quinientos…
			if hundSet || tensSet || onesSet {
				break
			}
			cur += v
			hundSet = true
			started = true
			i++
			continue
		}
		if t.ScaleHundred[tok] {
			if hundSet {
				break
			}
			if cur == 0 {
				cur = 1
			}
			cur *= 100
			started = true
			onesSet, tensSet, hundSet = false, false, true
			i++
			continue
		}
		if v, ok := t.ScaleBig[tok]; ok {
			if cur == 0 {
				if started {
					break
				}
				cur = 1
			}
			total += cur * v
			cur = 0
			started = true
			onesSet, tensSet, hundSet = false, false, false
			i++
			continue
		}
		if t.DozenWords[tok] {
			mult := cur
			if mult == 0 {
				mult = 1
			}
			cur = mult * 12
			if halfPending {
				cur /= 2
			}
			started = true
			i++
			break // a dozen-word completes the run
		}
		if t.NumConnectors[tok] {
			if !started {
				break
			}
			j := i + 1
			if j < len(tokens) {
				next := tokens[j]
				_, o := t.Ones[next]
				_, tn := t.Tens[next]
				_, h := t.Hundreds[next]
				if o || tn || h {
					i++
					continue
				}
			}
			break
		}
		if v, ok := parseDigits(tok); ok {
			if started {
				break // a fresh digit token is a separate number
			}
			cur = v
			started = true
			// Block further small words ("14 five") but allow scales:
			// "3 hundred", "3 thousand", "3 dozen".
			onesSet, tensSet, hundSet = true, true, false
			i++
			continue
		}
		break
	}

	if !started || (seeded && i == start) {
		return Number{}, false
	}
	return Number{Value: total + cur, Start: start, End: i}, true
}

// parseDigits parses a numeric token: "14", "12.5", "12,5" (Spanish decimal
// comma), "1,200" (thousands separators).
func parseDigits(tok string) (float64, bool) {
	if tok == "" {
		return 0, false
	}
	for _, r := range tok {
		if (r < '0' || r > '9') && r != '.' && r != ',' {
			return 0, false
		}
	}
	if strings.Count(tok, ".") > 1 {
		return 0, false
	}
	// Decide what commas mean: thousands separators ("1,200") when every
	// group after the first has exactly 3 digits, else a decimal comma
	// ("12,5" — Spanish).
	if strings.Contains(tok, ",") {
		parts := strings.Split(tok, ",")
		thousands := !strings.Contains(tok, ".") && parts[0] != "" && len(parts[0]) <= 3
		for _, p := range parts[1:] {
			if len(p) != 3 {
				thousands = false
			}
		}
		switch {
		case thousands:
			tok = strings.ReplaceAll(tok, ",", "")
		case len(parts) == 2 && !strings.Contains(tok, "."):
			tok = parts[0] + "." + parts[1]
		default:
			return 0, false
		}
	}
	v, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
