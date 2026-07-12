# How to speak to it — and how the parser thinks

This is both the operator phrasing guide (the source for the on-device
"how to speak to it" card, spec §5) and the engineering reference for the
deterministic slot filler (§7).

## Recommended phrasing

```
[quantity] [unit] [of] <item> [in|at] <location> [, <description>]
<location> , [quantity] [unit] <item> [, <description>]
```

Examples (all real golden-test cases):

| You say | quantity | unit | item | location | description |
|---------|----------|------|------|----------|-------------|
| "Twelve boxes of RJ45 connectors in bin A-14" | 12 | boxes | RJ45 connectors | A-14 | — |
| "Bin C-7, forty spools of Cat6, three have water damage" | 40 | spools | Cat6 | C-7 | three have water damage |
| "About a dozen assorted brackets, aisle five shelf two" | ~12 | — | assorted brackets | aisle 5 / shelf 2 | — |
| "a box of nails on shelf 4" | 1 | box | nails | shelf 4 | — |
| "cuarenta carretes de Cat6 en el bin C-7" | 40 | carretes | Cat6 | C-7 | — |

Useful voice moves:

- **Fix a slip mid-sentence:** "…in bin A-40, no, A-14" — the last value
  wins (works for location and quantity; Spanish: "…, digo, A-14").
- **While the readback is showing:** "yes"/"correct" saves; "location is
  A-40", "change quantity to 15", or just "no, A-14" fixes one field;
  "scratch that" discards; saying a whole new observation replaces the
  pending one.
- **After saving:** "scratch that" rejects the record you just saved.
- **Approximate counts are fine:** "about forty", "a couple hundred",
  "unos cuarenta" — saved with low confidence and flagged for review.
  "Several" saves with no number at all (also flagged).

## How the slot filler works (engineering)

Pipeline per utterance (`parser.Parse`):

1. **Tokenize.** Whitespace split; trailing `,.;:!?` end a *clause*;
   `forty-two` splits, codes like `a-14` don't; a bare `-` between tokens
   (Whisper's "A - 14") is dropped without breaking the clause.
2. **Scan numbers** (`lang.ScanNumbers`): composed number words
   ("three hundred forty two", "dos mil quinientos"), digits ("1,200",
   "12,5" Spanish decimal), dozens, and vague/approx phrases. "a couple
   hundred" seeds the scale (≈200, approximate); "several hundred" stays
   vague (no number, flagged).
3. **Extract the location**: the first group of anchor segments in one
   clause. Anchors are prepositions (in/at/en) and nouns (bin, aisle,
   shelf, pasillo, estante…). Values: codes (`A-14`, `c7` → `C-7`), spoken
   letter+number ("a fourteen" → `A-14`), bare numbers after keep-anchor
   nouns ("aisle 5"). Adjacent segments join: "aisle 5 / shelf 2".
4. **Apply mid-utterance corrections**: a negation ("no", "I mean",
   "digo") followed by a location-shaped or number(+unit) value overrides
   that slot; anything else ("no water damage") is left for the
   description.
5. **Pick the quantity**: the first number not consumed by the location.
   The unit is the next token if it's in the unit vocabulary — or any word
   before "of" ("five skids of cement" keeps `skids` raw, §5.2). "a box
   of…" means quantity 1. If nothing remains to be the item, the unit word
   *was* the item ("forty tubes in aisle 3" → item "tubes").
6. **Item** = the unconsumed run right after the quantity/unit (falling
   back to the first free run). **Description** = every other leftover
   run, verbatim.
7. **Resolve** location/item against reference data (exact → canonical
   code → Jaro-Winkler, threshold 0.85). Matching is suggestive: an
   unmatched value still saves as free text, flagged unresolved (§6.2).

Per-field certainty (exact 1.0, approximate 0.5, vague 0.35, resolver
score when matched, …) multiplies the ASR confidence to give the §6.1
confidence block; anything under its threshold is highlighted at readback
and sets `needs_review` with a reason.

## Adding vocabulary

- **Units/synonyms**: backend reference data (`units` in the refdata pull)
  — no code change, per language.
- **Anchor nouns, number words, command verbs**: table entries in
  `lang/tables_en.go` / `lang/tables_es.go`.
- **Part & location aliases**: backend reference data; give locations
  spoken aliases ("A fourteen") for reliable resolution.
- New languages = a new `lang.Table` plus golden cases in
  `parser/testdata/`.
