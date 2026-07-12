// Package parser implements the deterministic slot filler (spec §7): it
// turns a transcript into item / quantity / unit / location / description
// slots using per-language anchor keywords and number-word tables, applies
// mid-utterance corrections ("…A-40, no, A-14"), and resolves location and
// item text against reference data. Everything the slots don't claim
// becomes the description.
package parser

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/computerscienceiscool/voice-inventory/lang"
	"github.com/computerscienceiscool/voice-inventory/observation"
	"github.com/computerscienceiscool/voice-inventory/refdata"
)

// Options configures a parse.
type Options struct {
	Lang       lang.Code         // concrete language (en/es); unknown falls back to en
	Resolver   *refdata.Index    // optional reference-data resolver
	ExtraUnits map[string]string // extra folded unit → canonical (backend refdata)
	MultiItem  bool              // enable "…and…" multi-item splitting (P4)
}

func (o Options) table() *lang.Table { return lang.GetOrDefault(o.Lang) }

func (o Options) unitOf(fold string) (string, bool) {
	if c, ok := o.table().Units[fold]; ok {
		return c, true
	}
	if o.ExtraUnits != nil {
		if c, ok := o.ExtraUnits[fold]; ok {
			return c, true
		}
	}
	return "", false
}

// Result is the outcome of parsing one utterance into one observation.
type Result struct {
	Parsed observation.Parsed

	QuantityApprox bool // "about forty", "a couple"
	QuantityVague  bool // "several" — no usable number

	LocationScore float64 // resolver similarity when matched
	ItemScore     float64

	// Parse-certainty factors in [0,1], before ASR confidence is applied.
	CertQuantity float64
	CertLocation float64
	CertItem     float64

	Overridden []string // slots replaced by a mid-utterance correction
}

// Parse extracts one observation from the transcript.
func Parse(text string, opts Options) Result {
	return parseTokens(tokenize(text), opts)
}

// ParseAll extracts one or more observations. With opts.MultiItem it splits
// "…and three reels of…" style utterances; a later item without a location
// inherits the previous item's location (same-bin phrasing).
func ParseAll(text string, opts Options) []Result {
	toks := tokenize(text)
	if !opts.MultiItem {
		return []Result{parseTokens(toks, opts)}
	}
	cuts := findItemCuts(toks, opts)
	if len(cuts) == 0 {
		return []Result{parseTokens(toks, opts)}
	}
	var out []Result
	start := 0
	bounds := append(cuts, len(toks))
	for _, end := range bounds {
		part := cloneSlice(toks[start:end])
		if len(part) > 0 {
			part[len(part)-1].ClauseEnd = true
			out = append(out, parseTokens(part, opts))
		}
		start = end + 1 // skip the conjunction token itself
	}
	for i := 1; i < len(out); i++ {
		if out[i].Parsed.LocationText == "" && out[i-1].Parsed.LocationText != "" {
			out[i].Parsed.LocationText = out[i-1].Parsed.LocationText
			out[i].Parsed.LocationID = out[i-1].Parsed.LocationID
			out[i].LocationScore = out[i-1].LocationScore
			out[i].CertLocation = out[i-1].CertLocation * 0.9
		}
	}
	return out
}

func cloneSlice(t []token) []token {
	c := make([]token, len(t))
	copy(c, t)
	return c
}

// findItemCuts locates conjunctions that begin a new item: "…and <number>
// <unit>…" (or "and a <unit> of…"). Conjunctions inside a number run
// ("one hundred and five") are never cuts.
func findItemCuts(toks []token, opts Options) []int {
	t := opts.table()
	folds := foldsOf(toks)
	nums := t.ScanNumbers(folds)
	inNum := make([]bool, len(toks))
	numStartEnd := map[int]int{}
	for _, n := range nums {
		for i := n.MarkStart; i < n.End; i++ {
			inNum[i] = true
		}
		numStartEnd[n.Start] = n.End
	}
	var cuts []int
	for i := 1; i < len(toks)-1; i++ {
		if !t.Conjunctions[folds[i]] || inNum[i] {
			continue
		}
		// "and <number> <unit-ish>"
		if end, ok := numStartEnd[i+1]; ok {
			if end < len(folds) {
				if _, isUnit := opts.unitOf(folds[end]); isUnit {
					cuts = append(cuts, i)
					continue
				}
				if end+1 < len(folds) && t.Of[folds[end+1]] {
					cuts = append(cuts, i)
					continue
				}
			}
			continue
		}
		// "and a <unit> of"
		if t.NumArticles[folds[i+1]] && i+2 < len(folds) {
			if _, isUnit := opts.unitOf(folds[i+2]); isUnit {
				cuts = append(cuts, i)
			}
		}
	}
	return cuts
}

func foldsOf(toks []token) []string {
	f := make([]string, len(toks))
	for i, t := range toks {
		f[i] = t.Fold
	}
	return f
}

// ---------------------------------------------------------------------------
// Tokenizer

type token struct {
	Orig      string
	Fold      string
	ClauseEnd bool // a clause boundary (comma, period, …) follows this token
}

const leadPunct = "([{\"'¿¡«“‘"
const trailPunct = ")]}\"'»”’"

func tokenize(text string) []token {
	var toks []token
	for _, f := range strings.Fields(text) {
		// A bare ASCII dash is how Whisper often renders a spoken code
		// ("bin A - 14") — drop it without breaking the clause. Typographic
		// dashes are prose separators and do end the clause.
		if f == "-" {
			continue
		}
		if f == "—" || f == "–" {
			if len(toks) > 0 {
				toks[len(toks)-1].ClauseEnd = true
			}
			continue
		}
		f = strings.TrimLeft(f, leadPunct)
		clauseEnd := false
		for f != "" {
			r, size := utf8.DecodeLastRuneInString(f)
			if strings.ContainsRune(trailPunct, r) {
				f = f[:len(f)-size]
				continue
			}
			if r == ',' || r == ';' || r == '.' || r == '!' || r == '?' || r == ':' || r == '…' {
				f = f[:len(f)-size]
				clauseEnd = true
				continue
			}
			break
		}
		if f == "" {
			if clauseEnd && len(toks) > 0 {
				toks[len(toks)-1].ClauseEnd = true
			}
			continue
		}
		parts := splitAlphaHyphen(f)
		for i, p := range parts {
			tk := token{Orig: p, Fold: lang.Fold(p)}
			if i == len(parts)-1 {
				tk.ClauseEnd = clauseEnd
			}
			toks = append(toks, tk)
		}
	}
	if n := len(toks); n > 0 {
		toks[n-1].ClauseEnd = true
	}
	return toks
}

// splitAlphaHyphen splits "forty-two" into ["forty","two"] but keeps
// alphanumeric codes like "a-14" or "rj-45" intact.
func splitAlphaHyphen(s string) []string {
	if !strings.Contains(s, "-") {
		return []string{s}
	}
	parts := strings.Split(s, "-")
	for _, p := range parts {
		if p == "" {
			return []string{s}
		}
		for _, r := range p {
			if !unicode.IsLetter(r) {
				return []string{s}
			}
		}
	}
	return parts
}

// ---------------------------------------------------------------------------
// Parse state

type parse struct {
	t        *lang.Table
	opts     Options
	toks     []token
	folds    []string
	clause   []int // clause index of each token
	consumed []bool
	nums     []lang.Number
	numStart map[int]int // token index → nums index of a span starting there
}

func newParse(toks []token, opts Options) *parse {
	p := &parse{
		t:        opts.table(),
		opts:     opts,
		toks:     toks,
		folds:    foldsOf(toks),
		clause:   make([]int, len(toks)),
		consumed: make([]bool, len(toks)),
		numStart: map[int]int{},
	}
	c := 0
	for i, tk := range toks {
		p.clause[i] = c
		if tk.ClauseEnd {
			c++
		}
	}
	p.nums = p.t.ScanNumbers(p.folds)
	for i, n := range p.nums {
		p.numStart[n.Start] = i
	}
	return p
}

// clauseEnd returns the index one past the last token of the clause
// containing token i.
func (p *parse) clauseEnd(i int) int {
	c := p.clause[i]
	j := i
	for j < len(p.toks) && p.clause[j] == c {
		j++
	}
	return j
}

func (p *parse) consume(start, end int) {
	for i := start; i < end && i < len(p.consumed); i++ {
		p.consumed[i] = true
	}
}

// intNumSpanAt returns (numsIndex, end) when a non-vague integer number in
// [0,9999] starts exactly at token i.
func (p *parse) intNumSpanAt(i int) (int, int, bool) {
	idx, ok := p.numStart[i]
	if !ok {
		return 0, 0, false
	}
	n := p.nums[idx]
	if n.Vague || n.Value != math.Trunc(n.Value) || n.Value < 0 || n.Value > 9999 {
		return 0, 0, false
	}
	return idx, n.End, true
}

func isSingleLetter(fold string) bool {
	return len(fold) == 1 && fold[0] >= 'a' && fold[0] <= 'z'
}

// isCodeTok reports whether a folded token looks like a location code:
// letters and digits mixed, e.g. "a-14", "c7", "14b".
func isCodeTok(fold string) bool {
	if len(fold) == 0 || len(fold) > 10 {
		return false
	}
	hasLetter, hasDigit := false, false
	for _, r := range fold {
		switch {
		case r >= 'a' && r <= 'z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '-':
		default:
			return false
		}
	}
	return hasLetter && hasDigit
}

// ---------------------------------------------------------------------------
// Location extraction

type locSeg struct {
	start, end       int // consumed token span, incl. prep/article/anchor
	anchorIdx        int // -1 when a bare code follows a preposition
	keepAnchor       bool
	valStart, valEnd int
}

// segAt tries to read one location segment whose anchor noun or preposition
// starts at token i. claimedBefore is where consumption may start (the
// preposition when present).
func (p *parse) segAt(i int) (locSeg, bool) {
	start := i
	j := i
	if p.t.LocationPreps[p.folds[j]] {
		j++
		for j < len(p.toks) && p.t.Articles[p.folds[j]] && p.clause[j] == p.clause[i] {
			j++
		}
		if j >= len(p.toks) || p.clause[j] != p.clause[i] {
			return locSeg{}, false
		}
		if p.t.LocationNouns[p.folds[j]] {
			return p.segFromNoun(j, start)
		}
		if isCodeTok(p.folds[j]) && !p.consumed[j] {
			return locSeg{start: start, end: j + 1, anchorIdx: -1, valStart: j, valEnd: j + 1}, true
		}
		return locSeg{}, false
	}
	if p.t.LocationNouns[p.folds[j]] {
		return p.segFromNoun(j, start)
	}
	return locSeg{}, false
}

func (p *parse) segFromNoun(noun, start int) (locSeg, bool) {
	v := noun + 1
	end := p.clauseEnd(noun)
	if v >= end || p.consumed[v] {
		return locSeg{}, false
	}
	seg := locSeg{start: start, anchorIdx: noun, valStart: v}
	switch {
	case isCodeTok(p.folds[v]):
		seg.valEnd = v + 1
	case isSingleLetter(p.folds[v]):
		if _, numEnd, ok := p.intNumSpanAt(v + 1); ok && numEnd <= end {
			seg.valEnd = numEnd // "bin a 14"
		} else {
			seg.valEnd = v + 1 // "section b"
		}
	default:
		if _, numEnd, ok := p.intNumSpanAt(v); ok && numEnd <= end {
			seg.valEnd = numEnd // "aisle 5"
		} else {
			return locSeg{}, false
		}
	}
	seg.end = seg.valEnd
	seg.keepAnchor = p.t.KeepAnchorNouns[p.folds[noun]]
	return seg, true
}

// extractLocation finds the primary location: the first group of adjacent
// segments ("aisle 5 shelf 2"). Only the first group is consumed; later
// location-ish text stays available for the description.
func (p *parse) extractLocation() []locSeg {
	for i := 0; i < len(p.toks); i++ {
		if p.consumed[i] {
			continue
		}
		seg, ok := p.segAt(i)
		if !ok {
			continue
		}
		group := []locSeg{seg}
		next := seg.end
		for next < len(p.toks) && p.clause[next] == p.clause[seg.start] {
			s2, ok2 := p.segAt(next)
			if !ok2 || s2.start != next {
				break
			}
			group = append(group, s2)
			next = s2.end
		}
		for _, s := range group {
			p.consume(s.start, s.end)
		}
		return group
	}
	return nil
}

// renderLocValue canonicalizes a location value span: "a 14" → "A-14",
// "c7" → "C-7", "a-14" → "A-14", "5" → "5".
func (p *parse) renderLocValue(seg locSeg) string {
	if seg.valEnd-seg.valStart >= 2 && isSingleLetter(p.folds[seg.valStart]) {
		if idx, _, ok := p.intNumSpanAt(seg.valStart + 1); ok {
			return strings.ToUpper(p.folds[seg.valStart]) + "-" +
				strconv.FormatFloat(p.nums[idx].Value, 'f', -1, 64)
		}
	}
	if idx, end, ok := p.intNumSpanAt(seg.valStart); ok && end == seg.valEnd {
		return strconv.FormatFloat(p.nums[idx].Value, 'f', -1, 64)
	}
	tok := p.folds[seg.valStart]
	if isCodeTok(tok) {
		return canonCodeDisplay(tok)
	}
	return strings.ToUpper(tok)
}

// canonCodeDisplay uppercases a code and inserts a dash at the first
// letter→digit boundary when none is present: "c7" → "C-7".
func canonCodeDisplay(fold string) string {
	up := strings.ToUpper(fold)
	if strings.Contains(up, "-") {
		return up
	}
	for i := 1; i < len(up); i++ {
		if up[i-1] >= 'A' && up[i-1] <= 'Z' && up[i] >= '0' && up[i] <= '9' {
			return up[:i] + "-" + up[i:]
		}
	}
	return up
}

func (p *parse) renderLocation(group []locSeg) string {
	var parts []string
	for _, s := range group {
		val := p.renderLocValue(s)
		if s.keepAnchor && s.anchorIdx >= 0 {
			parts = append(parts, p.folds[s.anchorIdx]+" "+val)
		} else {
			parts = append(parts, val)
		}
	}
	return strings.Join(parts, " / ")
}

// locationFromSpan parses [start,end) as a standalone location value
// ("bin a-14", "a-14", "aisle 5 shelf 2"). ok requires full coverage.
func (p *parse) locationFromSpan(start, end int) (string, bool) {
	if start >= end {
		return "", false
	}
	// Bare code or bare "letter number".
	if end-start == 1 && isCodeTok(p.folds[start]) {
		return canonCodeDisplay(p.folds[start]), true
	}
	if isSingleLetter(p.folds[start]) {
		if idx, numEnd, ok := p.intNumSpanAt(start + 1); ok && numEnd == end {
			return strings.ToUpper(p.folds[start]) + "-" +
				strconv.FormatFloat(p.nums[idx].Value, 'f', -1, 64), true
		}
	}
	var group []locSeg
	i := start
	for i < end {
		seg, ok := p.segAt(i)
		if !ok || seg.start != i || seg.end > end {
			return "", false
		}
		group = append(group, seg)
		i = seg.end
	}
	if len(group) == 0 {
		return "", false
	}
	return p.renderLocation(group), true
}

// ---------------------------------------------------------------------------
// Mid-utterance corrections ("…, no, A-14" / "… I mean fifteen")

type override struct {
	locText  string
	qty      *float64
	qtyAprox bool
	unit     *string
}

func (p *parse) applyNegations() override {
	var ov override
	for i := 1; i < len(p.toks); i++ {
		if p.consumed[i] {
			continue
		}
		n := p.t.MatchNegation(p.folds, i)
		if n == 0 {
			continue
		}
		valStart := i + n
		valEnd := p.clauseEnd(i)
		if valStart >= valEnd {
			// negation at end of clause: value is the whole next clause
			if valStart >= len(p.toks) {
				continue
			}
			valEnd = p.clauseEnd(valStart)
		}
		if p.anyConsumed(valStart, valEnd) {
			continue
		}
		if text, ok := p.locationFromSpan(valStart, valEnd); ok {
			ov.locText = text
			p.consume(i, valEnd)
			continue
		}
		if q, approx, unit, ok := p.qtyFromSpan(valStart, valEnd); ok {
			ov.qty, ov.qtyAprox, ov.unit = q, approx, unit
			p.consume(i, valEnd)
		}
	}
	return ov
}

func (p *parse) anyConsumed(start, end int) bool {
	for i := start; i < end; i++ {
		if p.consumed[i] {
			return true
		}
	}
	return false
}

// qtyFromSpan accepts a span that is exactly a number, optionally followed
// by a single known unit token.
func (p *parse) qtyFromSpan(start, end int) (*float64, bool, *string, bool) {
	idx, ok := p.numStart[start]
	if !ok {
		return nil, false, nil, false
	}
	n := p.nums[idx]
	if n.Vague {
		return nil, false, nil, false
	}
	if n.End == end {
		v := n.Value
		return &v, n.Approx, nil, true
	}
	if n.End == end-1 {
		if _, isUnit := p.opts.unitOf(p.folds[end-1]); isUnit {
			v := n.Value
			u := strings.ToLower(p.toks[end-1].Orig)
			return &v, n.Approx, &u, true
		}
	}
	return nil, false, nil, false
}

// ---------------------------------------------------------------------------
// Main slot assembly

func parseTokens(toks []token, opts Options) Result {
	var res Result
	if len(toks) == 0 {
		return res
	}
	p := newParse(toks, opts)

	// 1. Primary location.
	locGroup := p.extractLocation()
	locText := ""
	if len(locGroup) > 0 {
		locText = p.renderLocation(locGroup)
	}

	// 2. Mid-utterance corrections (last value for a slot wins, §13).
	ov := p.applyNegations()
	if ov.locText != "" {
		if locText != "" {
			res.Overridden = append(res.Overridden, "location")
		}
		locText = ov.locText
	}

	// 3. Quantity + unit from the first unconsumed number.
	var qty *float64
	qtyApprox, qtyVague, qtyFound := false, false, false
	var unit *string
	afterQty := -1 // token position right after the quantity/unit span
	unitIdx := -1  // token index the unit came from
	for _, n := range p.nums {
		if p.anyConsumed(n.MarkStart, n.End) {
			continue
		}
		p.consume(n.MarkStart, n.End)
		qtyFound = true
		if n.Vague {
			qtyVague = true
		} else {
			v := n.Value
			qty = &v
			qtyApprox = n.Approx
		}
		// unit directly after the number, within the same clause:
		// [of]* [unit] [of]*
		j := n.End
		end := p.clauseEnd(n.End - 1)
		for j < end && p.t.Of[p.folds[j]] {
			p.consume(j, j+1)
			j++
		}
		if j < end && !p.consumed[j] {
			if _, isUnit := p.opts.unitOf(p.folds[j]); isUnit {
				u := strings.ToLower(p.toks[j].Orig)
				unit = &u
				unitIdx = j
				p.consume(j, j+1)
				j++
			} else if j+1 < end && p.t.Of[p.folds[j+1]] {
				// unknown unit kept as raw text (§5.2)
				u := strings.ToLower(p.toks[j].Orig)
				unit = &u
				unitIdx = j
				p.consume(j, j+1)
				j++
			}
			for j < end && p.t.Of[p.folds[j]] {
				p.consume(j, j+1)
				j++
			}
		}
		afterQty = j
		break
	}

	// 3b. English "a box of nails" → quantity 1.
	articleQty := false
	if !qtyFound && ov.qty == nil {
		for i := 0; i+1 < len(p.toks); i++ {
			if p.consumed[i] || p.consumed[i+1] {
				continue
			}
			if p.t.NumArticles[p.folds[i]] && p.clause[i] == p.clause[i+1] {
				if _, isUnit := p.opts.unitOf(p.folds[i+1]); isUnit {
					one := 1.0
					qty = &one
					qtyFound, articleQty = true, true
					u := strings.ToLower(p.toks[i+1].Orig)
					unit = &u
					unitIdx = i + 1
					p.consume(i, i+2)
					j := i + 2
					end := p.clauseEnd(i)
					for j < end && p.t.Of[p.folds[j]] {
						p.consume(j, j+1)
						j++
					}
					afterQty = j
				}
			}
			if qtyFound {
				break
			}
		}
	}

	// 4. Correction override wins.
	unitFromOverride := false
	if ov.qty != nil {
		if qty != nil || qtyVague {
			res.Overridden = append(res.Overridden, "quantity")
		}
		qty = ov.qty
		qtyApprox = ov.qtyAprox
		qtyVague = false
		if ov.unit != nil {
			unit = ov.unit
			unitFromOverride = true
		}
	}

	// 5. Item: prefer the first unconsumed run at/after the quantity+unit
	// ("…, twelve boxes of RJ45 …" → the item follows the unit even when an
	// earlier clause holds free text); fall back to the first unconsumed
	// run anywhere when no quantity was spoken or nothing follows it.
	itemStart := len(p.toks)
	if afterQty >= 0 {
		for i := afterQty; i < len(p.toks); i++ {
			if !p.consumed[i] {
				itemStart = i
				break
			}
		}
	}
	if itemStart == len(p.toks) {
		itemStart = 0
		for itemStart < len(p.toks) && p.consumed[itemStart] {
			itemStart++
		}
	}
	itemText := ""
	if itemStart < len(p.toks) {
		itemEnd := itemStart
		c := p.clause[itemStart]
		for itemEnd < len(p.toks) && !p.consumed[itemEnd] && p.clause[itemEnd] == c {
			itemEnd++
		}
		var words []string
		for i := itemStart; i < itemEnd; i++ {
			words = append(words, p.toks[i].Orig)
		}
		itemText = strings.Join(words, " ")
		p.consume(itemStart, itemEnd)
	}

	// "forty tubes in aisle 3": when nothing is left to be the item, the
	// unit word was the item all along — demote it. Never demote a unit the
	// operator supplied via a correction ("…, no, fifty reels"): that would
	// resurrect the corrected-away word.
	if itemText == "" && unit != nil && unitIdx >= 0 && !unitFromOverride {
		itemText = p.toks[unitIdx].Orig
		unit = nil
	}

	// 6. Description: every remaining unconsumed run.
	var descRuns []string
	i := 0
	for i < len(p.toks) {
		if p.consumed[i] {
			i++
			continue
		}
		j := i
		for j < len(p.toks) && !p.consumed[j] {
			j++
		}
		start := i
		for start < j && p.t.Conjunctions[p.folds[start]] {
			start++
		}
		if start < j {
			var words []string
			for k := start; k < j; k++ {
				words = append(words, p.toks[k].Orig)
			}
			descRuns = append(descRuns, strings.Join(words, " "))
		}
		i = j
	}
	var desc *string
	if len(descRuns) > 0 {
		d := strings.Join(descRuns, ", ")
		desc = &d
	}

	// 7. Assemble + resolve + certainties.
	res.Parsed.ItemText = itemText
	res.Parsed.Quantity = qty
	res.Parsed.Unit = unit
	res.Parsed.LocationText = locText
	res.Parsed.Description = desc
	res.QuantityApprox = qtyApprox
	res.QuantityVague = qtyVague

	switch {
	case qty == nil && !qtyVague:
		res.CertQuantity = 0
	case qtyVague:
		res.CertQuantity = 0.35
	case qtyApprox:
		res.CertQuantity = 0.5
	case articleQty:
		res.CertQuantity = 0.85
	default:
		res.CertQuantity = 1.0
	}

	if locText != "" {
		if id, score, ok := opts.Resolver.ResolveLocation(locText); ok {
			lid := id
			res.Parsed.LocationID = &lid
			res.LocationScore = score
			res.CertLocation = score
		} else if opts.Resolver.HasLocations() {
			res.LocationScore = score
			res.CertLocation = 0.6 // reference data exists but no match
		} else {
			res.CertLocation = 0.75 // nothing to match against
		}
	}

	if itemText != "" {
		if pn, score, ok := opts.Resolver.ResolvePart(itemText); ok {
			pnCopy := pn
			res.Parsed.PartNumber = &pnCopy
			res.ItemScore = score
			res.CertItem = score
		} else if opts.Resolver.HasParts() {
			res.ItemScore = score
			res.CertItem = 0.7 // reference data exists but no match
		} else {
			res.CertItem = 0.8 // nothing to match against
		}
	}
	return res
}

// ResolveLocationText parses a spoken location value on its own (used for
// voice corrections like "location is A-14").
func ResolveLocationText(value string, opts Options) (text string, id *string, score float64, ok bool) {
	toks := tokenize(value)
	if len(toks) == 0 {
		return "", nil, 0, false
	}
	p := newParse(toks, opts)
	text, ok = p.locationFromSpan(0, len(toks))
	if !ok {
		// fall back: accept the raw text as the location
		text = strings.TrimSpace(value)
		if text == "" {
			return "", nil, 0, false
		}
	}
	if rid, s, matched := opts.Resolver.ResolveLocation(text); matched {
		id = &rid
		score = s
	}
	return text, id, score, true
}

// ParseQuantityText parses a spoken quantity value ("fifteen", "12").
func ParseQuantityText(value string, opts Options) (*float64, bool, bool) {
	toks := tokenize(value)
	if len(toks) == 0 {
		return nil, false, false
	}
	p := newParse(toks, opts)
	for _, n := range p.nums {
		if n.Vague {
			continue
		}
		v := n.Value
		return &v, n.Approx, true
	}
	return nil, false, false
}
