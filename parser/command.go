package parser

import (
	"strconv"
	"strings"
)

// CommandKind classifies a voice command spoken during review or capture.
type CommandKind int

const (
	// CmdConfirm — "yes", "correct", "sí": save the pending record.
	CmdConfirm CommandKind = iota + 1
	// CmdReject — bare "no", "wrong": the readback is wrong; stay in review.
	CmdReject
	// CmdScratch — "scratch that", "delete": discard the pending or
	// last-saved record (§13).
	CmdScratch
	// CmdSetField — "location is A-40", "change quantity to 15",
	// "no, A-14": replace a single field.
	CmdSetField
)

// Command is one recognized voice command.
type Command struct {
	Kind  CommandKind
	Field string // canonical field for CmdSetField: location|quantity|item|unit|description
	Value string // spoken value text for CmdSetField
}

// ParseCommand recognizes an utterance as a command rather than a new
// observation. It returns false when the utterance should be treated as
// dictation.
func ParseCommand(text string, opts Options) (Command, bool) {
	t := opts.table()
	toks := tokenize(text)
	if len(toks) == 0 {
		return Command{}, false
	}
	folds := foldsOf(toks)

	// "scratch that" / "delete" — must cover the whole utterance.
	if n := t.MatchScratch(folds, 0); n == len(folds) {
		return Command{Kind: CmdScratch}, true
	}

	// Every token a confirm word → confirm ("yes", "yes correct").
	if allIn(folds, t.ConfirmWords) {
		return Command{Kind: CmdConfirm}, true
	}

	// Explicit field set: "[change] [the] <field> [is|to] <value>".
	if cmd, ok := parseSetField(toks, folds, opts); ok {
		return cmd, true
	}

	// Negation shorthand: "no, A-14" / "no, fifteen" / bare "no".
	if n := t.MatchNegation(folds, 0); n > 0 {
		rest := toks[n:]
		if len(rest) == 0 {
			if allIn(folds, t.RejectWords) {
				return Command{Kind: CmdReject}, true
			}
			return Command{}, false
		}
		p := newParse(cloneAsUtterance(rest), opts)
		if text, ok := p.locationFromSpan(0, len(rest)); ok {
			return Command{Kind: CmdSetField, Field: "location", Value: text}, true
		}
		if q, _, _, ok := p.qtyFromSpan(0, len(rest)); ok && q != nil {
			return Command{Kind: CmdSetField, Field: "quantity",
				Value: trimFloat(*q)}, true
		}
		if allIn(folds, t.RejectWords) {
			return Command{Kind: CmdReject}, true
		}
		return Command{}, false
	}

	// Every token a reject word → reject ("wrong").
	if allIn(folds, t.RejectWords) {
		return Command{Kind: CmdReject}, true
	}
	return Command{}, false
}

func cloneAsUtterance(toks []token) []token {
	c := cloneSlice(toks)
	if len(c) > 0 {
		c[len(c)-1].ClauseEnd = true
	}
	return c
}

func parseSetField(toks []token, folds []string, opts Options) (Command, bool) {
	t := opts.table()
	i := 0
	change := false
	if i < len(folds) && t.ChangeWords[folds[i]] {
		change = true
		i++
	}
	if i < len(folds) && t.Articles[folds[i]] {
		i++
	}
	if i >= len(folds) {
		return Command{}, false
	}
	field, ok := t.FieldAliases[folds[i]]
	if !ok {
		return Command{}, false
	}
	j := i + 1
	sep := false
	if j < len(folds) && (t.IsWords[folds[j]] || t.ToWords[folds[j]]) {
		sep = true
		j++
	}
	// Plain form requires "is" ("location is A-14"); the change form may
	// omit "to" ("change location A-14" is unusual but unambiguous).
	if !sep && !change {
		return Command{}, false
	}
	if j >= len(toks) {
		return Command{}, false
	}
	var words []string
	for _, tk := range toks[j:] {
		words = append(words, tk.Orig)
	}
	value := strings.TrimSpace(strings.Join(words, " "))
	if value == "" {
		return Command{}, false
	}
	return Command{Kind: CmdSetField, Field: field, Value: value}, true
}

func allIn(folds []string, set map[string]bool) bool {
	if len(folds) == 0 {
		return false
	}
	for _, f := range folds {
		if !set[f] {
			return false
		}
	}
	return true
}

func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
