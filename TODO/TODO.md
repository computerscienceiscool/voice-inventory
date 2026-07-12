# Voice Inventory Indexer — TODO

Source: [voice-inventory-indexer-spec.md](../voice-inventory-indexer-spec.md) (Draft v0.1)

Convention: one numbered item per feature/issue, numbers start at 001 and are never
reused (new items append at the next number). Mark an item done by checking it and
crossing it off: `- [x] ~~001 - example~~`

Status notes: "core ✔" = the Go-core side is implemented and tested in this repo;
the remainder is native-shell / device / backend work outside the Go core.

## P0 — Core capture (English, push-to-talk, Android-first)

- [ ] 001 - Go core scaffold packaged via gomobile bind (Android AAR + iOS xcframework) with thin native shell (§9, §17) — core + `mobile` bind facade ✔; AAR/xcframework build + Compose shell need the mobile toolchain
- [x] ~~002 - Audio capture pipeline: mic at native rate → downsample to 16 kHz mono PCM float32 (§8.3)~~ — `audio` pkg; shell feeds mic PCM at any rate/channels
- [x] ~~003 - VAD utterance segmentation (energy-based MVP), silence trimming, ~30 s utterance cap (§8.3)~~ — `vad` pkg (energy + zero-crossing, pre-roll, hangover, cap)
- [x] ~~004 - Transcriber interface + whisper.cpp cgo backend returning text + token timing/confidence (§8.1)~~ — `asr` pkg: interface, whisper.cpp CLI runner (desktop/CI), JSON parser, mock; on-device binding plugs in via `mobile.Transcriber`
- [x] ~~005 - Model lifecycle: bundle or first-run fetch, on-device cache, missing/corrupt → re-fetch with progress, capture disabled until ready (§8.2, §13)~~ — `asr.EnsureModel` (SHA-256 verify, atomic install, progress); capture blocked until a transcriber is configured
- [x] ~~006 - Push-to-talk capture: hold or tap-start/tap-stop; armed/idle session state machine (§4.1, §4.2)~~ — `session` Begin/EndUtterance + state machine
- [ ] 007 - Visible mic level meter while capturing (§4.1) — core emits per-frame RMS level events ✔; meter widget is shell work
- [x] ~~008 - Deterministic slot-filler parser: tokenize, anchor keywords, span→slot assignment (§5, §7)~~ — `parser` pkg; spec §5.3 examples are golden tests
- [x] ~~009 - Number-word normalization: "forty"→40, "a dozen"→12, "a couple"→2 low-confidence (§5.2)~~ — `lang` number engine (en+es, hundreds/thousands, dozens, digits)
- [x] ~~010 - Unit vocabulary, extensible; unknown units stored as raw text (§5.2)~~ — built-in tables + backend units via refdata; unknown "X of" kept raw
- [x] ~~011 - Leftover tokens → description field (§7)~~
- [x] ~~012 - Per-field confidence scores; below-threshold fields flagged for confirmation (§6.1, §7)~~ — parse certainty × ASR confidence; doubtful fields highlighted in readback
- [x] ~~013 - Observation record per §6.1: UUIDv7 id, device/operator ids, captured_at, raw_transcript always retained, corrections log, schema_version~~ — `observation` pkg; wire-shape locked by test
- [x] ~~014 - SQLite local store for observation queue + sync state; WAL/crash-safe so confirmed records survive force-quit (§10.1, §12)~~ — `store` pkg (WAL + synchronous FULL, durability test)
- [ ] 015 - Readback screen: parsed fields, doubtful-field highlighting, high-contrast glanceable layout (§4.1, §4.3, §13) — core provides readback text + doubtful-field list ✔; screen is shell work
- [ ] 016 - Confirm/correct interactions: tap ✓ to confirm, tap a field to edit or re-dictate (§4.1) — core APIs Confirm/CorrectField/EditRecord ✔; touch UI is shell work
- [x] ~~017 - Record status lifecycle draft → confirmed on save; auto-return to armed/idle (§4.1)~~ — drafts persist at parse time (crash safety), confirm promotes, session re-arms
- [ ] 018 - Audible + haptic confirmation on save (§4.3) — core fires OnSaved event ✔; sound/vibration is shell work
- [ ] 019 - Glove-friendly large-button, one-handed UI (§4.3) — shell work
- [x] ~~020 - "Scratch that" / "delete" voice command discards in-progress or last-saved record (§5.2, §13)~~ — marks records rejected (auditable)
- [x] ~~021 - Mid-utterance self-correction: last value spoken for a slot wins ("…A-40, no, A-14") (§13)~~ — negation corrections for location and quantity, en+es
- [x] ~~022 - Manual-entry fallback when mic permission denied / no mic present (§13)~~ — session.AddManual + mobile/CLI surfaces; entry form is shell work
- [x] ~~023 - Missing quantity → save with quantity:null, flagged (§13)~~ — review reason "no quantity spoken"; vague quantities ("several") also flagged

## P1 — Usability + Spanish

- [ ] 024 - TTS spoken readback (optional) (§4.1, §17) — readback text generated per language ✔; TTS engine choice is item 070
- [x] ~~025 - Voice confirmation: "yes" / "correct" (§4.1)~~ — plus sí/correcto/dale…
- [x] ~~026 - Voice field corrections: "location is A-40", "change … to …" (§4.1, §5.2)~~ — including "no, A-14" shorthand
- [x] ~~027 - Spanish support: multilingual model config + Spanish rule/vocab tables (§2.1, §7)~~ — full es tables (numbers, anchors, units, commands); golden suite
- [x] ~~028 - Language auto-detect mode (en|es|auto) (§6.1)~~ — whisper-detected language selects the rule table; record stores the language used
- [x] ~~029 - Table-driven per-language rulesets; new vocabulary is data, not code (§7)~~ — `lang.Table`; backend-extensible units via refdata
- [x] ~~030 - Locations reference data with spoken aliases + fuzzy resolver location_text → location_id (§6.2)~~ — exact/code-canonical/Jaro-Winkler with threshold
- [x] ~~031 - Part vocabulary + fuzzy resolver item_text → part_number; suggestive, never blocks capture (§6.2)~~
- [x] ~~032 - Unresolved part/location flagging for later supervisor/backend resolution (§13)~~ — review reasons when reference data exists but doesn't match
- [ ] 033 - Batch review screen: session record list, bulk review/edit/delete, export + sync trigger (§4.2) — core list/filter/edit/confirm/reject APIs + CLI ✔; screen is shell work
- [x] ~~034 - Audio clip retention + purge policy (default: delete after sync + N days; configurable, can disable) (§6.3)~~ — WAV per utterance, PurgeAudio clears refs
- [ ] 035 - On-device "how to speak to it" help card with recommended phrasing (§5) — shell work (content can come from README quick-start)

## P2 — Sync (HTTPS) + iOS

- [x] ~~036 - Syncer interface with HTTPS MVP implementation (§10.2, §11 Phase A)~~ — TLS enforced unless AllowInsecure; bounded retries with backoff; 4xx fail fast
- [x] ~~037 - Opportunistic resumable sync: batch-push confirmed records → synced; idempotent retry by UUIDv7 id (§10.2)~~ — drafts never sync; server rejects don't wedge the loop
- [x] ~~038 - Reference-data pull (locations/parts/units) cached for offline matching (§10.2)~~ — ETag/304 aware; resolvers hot-reload
- [x] ~~039 - Manual sync trigger for operators (§3)~~ — mobile SyncPush/SyncPull + `vinv sync`
- [x] ~~040 - Operator login + per-device identity; stamp operator_id/device_id on records (§3)~~ — SetOperator + device profile; real authentication is item 063
- [x] ~~041 - Admin device-profile config: model/quant/language, capture mode + wake phrase text, vocab tables, retention, sync endpoint + credentials, confidence thresholds (§14)~~ — `config` pkg; anchor-keyword tables are code-side data (unit/synonym tables are remotely extensible)
- [ ] 042 - iOS build with Metal + CoreML-converted encoder (§8.5) — needs Xcode toolchain
- [ ] 043 - Android acceleration: ARM NEON baseline, GPU backends where supported, clean CPU fallback (§8.5) — needs NDK; see item 066 for the corrected backend list
- [x] ~~044 - Latency instrumentation vs §8.4 targets; auto-suggest base model when target missed~~ — utterance-end → readback, rolling median, one-shot suggestion
- [x] ~~045 - Low-end device profile: quantized base/tiny, English-only option (§8.2)~~ — config-level (model name/path + language per profile); device benchmarking is 055
- [x] ~~046 - Optional noise suppression / high-pass filter pipeline stage (§8.3)~~ — first-order high-pass in `audio`

## P3 — Grid-native

- [x] ~~047 - CBOR grid-message encoding for observations (protocol referenced by piece, ex1 conventions) (§11 Phase B)~~ — `grid.Message` with integer keys, protocol by reference
- [x] ~~048 - Capability tokens (CWT/COSE, ECDSA) carrying device identity + operator authority (§11)~~ — ES256 COSE_Sign1 CWT; claims must match payload; expiry enforced
- [ ] 049 - PromiseGrid agent as sync target behind the Syncer interface (§11) — message format + tokens ready; transport awaits the upstream agent protocol

## P4 — Enhancements

- [ ] 050 - Wake-phrase capture mode: keyword spotter, configurable phrase (default "log item"), opt-in (§4.2, §17) — continuous VAD mode exists; keyword spotting not implemented
- [x] ~~051 - Multi-item utterance splitter ("…and…") (§13)~~ — opt-in (config multi_item); later items inherit the location; earlier items auto-confirm
- [ ] 052 - Optional on-device small-LLM parse assist; deterministic path remains fallback (§7)

## Testing / acceptance

- [x] ~~053 - Unit suites: table-driven parser per language, number normalization, fuzzy resolvers, DB layer (§15)~~ — 15 packages, race-clean
- [ ] 054 - Golden-audio CI suite: recorded en+es utterances, quiet + noisy, expected parsed output (§15) — transcript-level goldens run in CI ✔; audio harness ready (`asr.TestGoldenAudio`, env-gated) — needs real recordings
- [ ] 055 - Device-matrix verification: low/mid/flagship per OS, latency + thermal (§15) — needs devices
- [ ] 056 - Battery test: 8-hour intermittent-capture shift on a typical phone (§12) — needs devices
- [ ] 057 - Field trial: ≥50 consecutive voice captures while walking, ≥95% qty+location accuracy, offline + force-quit resilience, time-per-item vs paper (§15)

## Spec issues found in review (fix the spec / decide, then cross off)

- [x] ~~058 - Draft records never sync: define draft visibility + end-of-shift handling~~ — decided in code: drafts persist immediately (crash safety), appear in batch review/list, never sync until confirmed
- [x] ~~059 - `rejected` status is orphaned: define its transitions~~ — decided: "scratch that" and batch-review delete mark records rejected (auditable), from draft or confirmed
- [ ] 060 - Audio re-verification contradiction: clips exist for later human re-verification (§6.3) but audio never syncs (§10.2) — decide audio upload for low-confidence records vs device-only review
- [x] ~~061 - Required fields never enumerated~~ — decided: capture never blocks; missing item/quantity/location set needs_review with reasons; required_fields config exists
- [x] ~~062 - Clarify auto-confirm~~ — decided: always confirm by default (§4.1); `auto_confirm_high_confidence` opt-in saves when every field clears its threshold
- [ ] 063 - MVP auth undefined: specify Phase A operator login, device enrollment, and operator_id provenance — operator id is set via SetOperator; no authentication yet
- [ ] 064 - Wake-phrase scope conflict (§4.1 vs §16.2 vs §17 P4) — align the spec; code treats it as P4 (item 050)
- [ ] 065 - Verify modernc.org/sqlite under gomobile on iOS + Android — works on desktop/CI (WAL, durability tested); mobile verification needs the toolchain; note cgo-free rationale is moot since whisper.cpp forces cgo
- [ ] 066 - Correct Android acceleration claims in spec: ggml/whisper.cpp has Vulkan/OpenCL but no NNAPI backend (§8.5); MVP VAD is energy-based, so no onnxruntime dependency
- [x] ~~067 - Post-sync edits undefined~~ — decided: synced records are immutable on-device (backend owns them); only audio_ref clearing is allowed after purge
- [ ] 068 - Accuracy metric ambiguous: define how "≥95% after confirmation" is measured for §15 acceptance
- [x] ~~069 - Specify behavior at the ~30 s utterance cap~~ — decided: transcribe what was captured, flag the record "utterance hit the 30-second cap"; PTT buffer hard-caps at 30 s
- [ ] 070 - TTS engine unspecified: platform TTS via native shell vs a library — interacts with the §16.1 UI decision
- [x] ~~071 - Voice-confirm listening window~~ — decided: push-to-talk requires pressing again to speak "yes"/corrections; wake mode keeps listening continuously
- [ ] 072 - Define batch-review export: format and destination — JSON export exists (CLI `list`, mobile ListJSON); decide operator-facing format (CSV? share sheet?)
- [ ] 073 - Supervisor scope boundary: §3 grants backend review/approve/export but §2.2 declares downstream out of scope — say where that UI lives
- [x] ~~074 - Data-model nits~~ — decided: corrections log uses canonical field names with human-readable values; `language` stores the language actually used (not "auto"); batch review lists by status/flags rather than an undefined "session"
- [ ] 075 - Resolve remaining §16 open decisions: UI approach (Gio vs native shells — blocks 001), device-joins-grid vs gateway, audio-retention default window (code default: on, 7 days — confirm), part-alias curation strategy

## Code review findings — security & usability (2026-07-11 review pass)

- [x] ~~076 - Security: WAV decoder trusted the chunk-size header — a corrupt/malicious file could demand a multi-GB allocation (DoS)~~ — fixed: 512 MB chunk cap in `audio.DecodeWAV`
- [x] ~~077 - Security: sync client decoded backend responses unbounded — a compromised server could exhaust device memory~~ — fixed: 64 MB `io.LimitReader` on all responses
- [x] ~~078 - Security: model download accepted an empty SHA-256 (unverified ggml weights feed C++ parsing code)~~ — fixed: `EnsureModel` refuses checksum-less downloads unless `AllowUnverified` (dev only)
- [x] ~~079 - Security: `vinv mockserver` (no auth) listened on all interfaces by default~~ — fixed: default bind 127.0.0.1
- [x] ~~080 - Security/usability: bearer token passed via `-token` argv is visible in `ps`~~ — fixed: `VINV_TOKEN` env fallback + flag help warning
- [x] ~~081 - Correctness (reviewer-confirmed): push starvation — backend-rejected records at the head of the queue filled every batch and newer records behind them never synced~~ — fixed: cursor-paginated batches; every confirmed record is offered once per pass, rejects retry next pass
- [x] ~~082 - Correctness (reviewer-confirmed): RFC3339Nano trims trailing zeros, so stored timestamps weren't lexicographically ordered — `AudioToPurge`'s SQL string comparison mis-selected records with sub-second `synced_at`~~ — fixed: fixed-width timestamp format; regression test
- [x] ~~083 - Usability: `vinv list` printed `null` for an empty queue~~ — fixed: prints `[]`
- [ ] 084 - Security decision: at-rest encryption for the SQLite queue and audio clips — rely on OS full-device encryption (baseline) or add SQLCipher/encrypted-FS; transcripts and audio are business data (§12)
- [ ] 085 - Security: grid signing keys (item 048) need platform-secure storage in the shell (Android Keystore / iOS Secure Enclave); PEM helpers exist for dev only
- [ ] 086 - Privacy/usability: "wake" capture mode is currently continuous VAD with NO keyword gate — every utterance in range becomes a record attempt; keep opt-in + document until the keyword spotter (item 050) lands
- [ ] 087 - Usability: backend-rejected records only surface in the one-shot push report — batch review should badge them persistently so a supervisor notices (relates to 060/073)
- [x] ~~088 - Correctness (reviewer-confirmed): "3 hundred" parsed as two numbers (3, 100) instead of 300 — the digit branch blocked the hundred scale~~ — fixed + regression test
- [x] ~~089 - Correctness (reviewer-confirmed): "a couple hundred screws" parsed as quantity 100 at full confidence with "a couple" dumped into the description~~ — fixed: vague values now seed scale words (2×100 = 200, approximate)
- [x] ~~090 - Correctness (reviewer-confirmed): Whisper's spaced-dash rendering "bin A - 14" split the code at a false clause break → location "A", quantity 14~~ — fixed: bare ASCII dashes are dropped, not clause breaks
- [x] ~~091 - Usability/safety (reviewer-confirmed): Scratch() while idle armed the microphone as a side effect~~ — fixed: only leaving review re-arms
- [x] ~~092 - Usability/battery (reviewer-confirmed): a constant low hum above the energy threshold (HVAC/compressor) locked wake-mode VAD into endless 30 s utterances — the noise floor never adapted upward~~ — fixed: minimum-ZCR gate rejects pure tones + dual-rate adaptive floor absorbs steady ambience; regression test covers hum, and speech-after-hum
- [x] ~~093 - Concurrency (reviewer-confirmed): CorrectField/redictate mutated the shared pending record outside the session mutex while Pending() (UI thread) read it — data race under -race~~ — fixed: clone-mutate-swap; handed-out snapshots are never written again; concurrent regression test
- [x] ~~094 - Concurrency (reviewer-confirmed): review() read hasLocations/hasParts unlocked while RefreshRefData wrote them~~ — fixed: snapshot under the lock
- [x] ~~095 - Correctness (reviewer-confirmed): "several hundred bolts" parsed as an exact 100 at full confidence (would sail through auto-confirm) — the NaN-vague + scale path fell through~~ — fixed: vague + scale is consumed as a vague, flagged quantity
- [x] ~~096 - Correctness (reviewer-confirmed): a spoken unit correction ("…, no, fifty reels") was demoted away and the corrected-away word resurrected as the item~~ — fixed: override units are never demoted; article-quantity path now demotes consistently
- [x] ~~097 - Audio quality (reviewer suspicion, confirmed): per-chunk stateless resampling dropped ~0.13% of samples at non-integer ratios (44.1 kHz mics) and reset interpolation phase every chunk~~ — fixed: stateful `audio.Resampler` carries phase/last-sample across chunks; §8.3 high-pass filter (`audio.HighPassFilter`) is now actually wired into the session pipeline (config `high_pass_hz`, default 100, 0 disables)
- [x] ~~098 - Security (reviewer suspicion, confirmed): the grid capability token signed only identity claims — a tampered payload (quantity/location) passed verification~~ — fixed: token now carries a SHA-256 payload digest (CWT private claim); DecodeObservation rejects any payload that doesn't match; tamper regression test
- [x] ~~099 - Usability: `vinv transcript "text" -lang es` silently swallowed the trailing flag into the utterance (Go flag parsing stops at the first positional arg) and parsed Spanish with the English table~~ — fixed: trailing flags are rejected with a clear error
