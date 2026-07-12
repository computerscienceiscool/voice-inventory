# Voice Inventory Indexer — TODO

Source: [voice-inventory-indexer-spec.md](../voice-inventory-indexer-spec.md) (Draft v0.1)

Convention: one numbered item per feature/issue, numbers start at 001 and are never
reused (new items append at the next number). Mark an item done by checking it and
crossing it off: `- [x] ~~001 - example~~`

## P0 — Core capture (English, push-to-talk, Android-first)

- [ ] 001 - Go core scaffold packaged via gomobile bind (Android AAR + iOS xcframework) with thin native shell (§9, §17)
- [ ] 002 - Audio capture pipeline: mic at native rate → downsample to 16 kHz mono PCM float32 (§8.3)
- [ ] 003 - VAD utterance segmentation (energy-based MVP), silence trimming, ~30 s utterance cap (§8.3)
- [ ] 004 - Transcriber interface + whisper.cpp cgo backend returning text + token timing/confidence (§8.1)
- [ ] 005 - Model lifecycle: bundle or first-run fetch, on-device cache, missing/corrupt → re-fetch with progress, capture disabled until ready (§8.2, §13)
- [ ] 006 - Push-to-talk capture: hold or tap-start/tap-stop; armed/idle session state machine (§4.1, §4.2)
- [ ] 007 - Visible mic level meter while capturing (§4.1)
- [ ] 008 - Deterministic slot-filler parser: tokenize, anchor keywords, span→slot assignment (§5, §7)
- [ ] 009 - Number-word normalization: "forty"→40, "a dozen"→12, "a couple"→2 low-confidence (§5.2)
- [ ] 010 - Unit vocabulary, extensible; unknown units stored as raw text (§5.2)
- [ ] 011 - Leftover tokens → description field (§7)
- [ ] 012 - Per-field confidence scores; below-threshold fields flagged for confirmation (§6.1, §7)
- [ ] 013 - Observation record per §6.1: UUIDv7 id, device/operator ids, captured_at, raw_transcript always retained, corrections log, schema_version
- [ ] 014 - SQLite local store for observation queue + sync state; WAL/crash-safe so confirmed records survive force-quit (§10.1, §12)
- [ ] 015 - Readback screen: parsed fields, doubtful-field highlighting, high-contrast glanceable layout (§4.1, §4.3, §13)
- [ ] 016 - Confirm/correct interactions: tap ✓ to confirm, tap a field to edit or re-dictate (§4.1)
- [ ] 017 - Record status lifecycle draft → confirmed on save; auto-return to armed/idle (§4.1)
- [ ] 018 - Audible + haptic confirmation on save (§4.3)
- [ ] 019 - Glove-friendly large-button, one-handed UI (§4.3)
- [ ] 020 - "Scratch that" / "delete" voice command discards in-progress or last-saved record (§5.2, §13)
- [ ] 021 - Mid-utterance self-correction: last value spoken for a slot wins ("…A-40, no, A-14") (§13)
- [ ] 022 - Manual-entry fallback when mic permission denied / no mic present (§13)
- [ ] 023 - Missing quantity → save with quantity:null, flagged (§13)

## P1 — Usability + Spanish

- [ ] 024 - TTS spoken readback (optional) (§4.1, §17)
- [ ] 025 - Voice confirmation: "yes" / "correct" (§4.1)
- [ ] 026 - Voice field corrections: "location is A-40", "change … to …" (§4.1, §5.2)
- [ ] 027 - Spanish support: multilingual model config + Spanish rule/vocab tables (§2.1, §7)
- [ ] 028 - Language auto-detect mode (en|es|auto) (§6.1)
- [ ] 029 - Table-driven per-language rulesets; new vocabulary is data, not code (§7)
- [ ] 030 - Locations reference data with spoken aliases + fuzzy resolver location_text → location_id (§6.2)
- [ ] 031 - Part vocabulary + fuzzy resolver item_text → part_number; suggestive, never blocks capture (§6.2)
- [ ] 032 - Unresolved part/location flagging for later supervisor/backend resolution (§13)
- [ ] 033 - Batch review screen: session record list, bulk review/edit/delete, export + sync trigger (§4.2)
- [ ] 034 - Audio clip retention + purge policy (default: delete after sync + N days; configurable, can disable) (§6.3)
- [ ] 035 - On-device "how to speak to it" help card with recommended phrasing (§5)

## P2 — Sync (HTTPS) + iOS

- [ ] 036 - Syncer interface with HTTPS MVP implementation (§10.2, §11 Phase A)
- [ ] 037 - Opportunistic resumable sync: batch-push confirmed records → synced; idempotent retry by UUIDv7 id (§10.2)
- [ ] 038 - Reference-data pull (locations/parts/units) cached for offline matching (§10.2)
- [ ] 039 - Manual sync trigger for operators (§3)
- [ ] 040 - Operator login + per-device identity; stamp operator_id/device_id on records (§3)
- [ ] 041 - Admin device-profile config: model/quant/language, capture mode + wake phrase text, vocab tables, retention, sync endpoint + credentials, confidence thresholds (§14)
- [ ] 042 - iOS build with Metal + CoreML-converted encoder (build step produces CoreML model alongside ggml weights) (§8.5)
- [ ] 043 - Android acceleration: ARM NEON baseline, GPU backends where supported, clean CPU fallback (§8.5)
- [ ] 044 - Latency instrumentation vs §8.4 targets; auto-suggest base model when target missed
- [ ] 045 - Low-end device profile: quantized base/tiny, English-only option (§8.2)
- [ ] 046 - Optional noise suppression / high-pass filter pipeline stage (§8.3)

## P3 — Grid-native

- [ ] 047 - CBOR grid-message encoding for observations (protocol referenced by piece, ex1 conventions) (§11 Phase B)
- [ ] 048 - Capability tokens (CWT/COSE, ECDSA) carrying device identity + operator authority (§11)
- [ ] 049 - PromiseGrid agent as sync target behind the Syncer interface (§11)

## P4 — Enhancements

- [ ] 050 - Wake-phrase capture mode: keyword spotter, configurable phrase (default "log item"), opt-in (§4.2, §17)
- [ ] 051 - Multi-item utterance splitter ("…and…") (§13)
- [ ] 052 - Optional on-device small-LLM parse assist; deterministic path remains fallback (§7)

## Testing / acceptance

- [ ] 053 - Unit suites: table-driven parser per language, number normalization, fuzzy resolvers, DB layer (§15)
- [ ] 054 - Golden-audio CI suite: recorded en+es utterances, quiet + noisy, expected parsed output (§15)
- [ ] 055 - Device-matrix verification: low/mid/flagship per OS, latency + thermal (§15)
- [ ] 056 - Battery test: 8-hour intermittent-capture shift on a typical phone (§12)
- [ ] 057 - Field trial: ≥50 consecutive voice captures while walking, ≥95% qty+location accuracy, offline + force-quit resilience, time-per-item vs paper (§15)

## Spec issues found in review (fix the spec / decide, then cross off)

- [ ] 058 - Draft records never sync: §10.2 pushes only confirmed; define draft visibility in batch review + end-of-shift handling so unconfirmed captures aren't silently stranded
- [ ] 059 - `rejected` status is orphaned: appears in the §6.1 enum but no flow ever sets it; define its transitions (e.g. is "scratch that" a hard delete or a reject?)
- [ ] 060 - Audio re-verification contradiction: clips are kept so a human can re-verify low-confidence records (§6.3), but audio never syncs (§10.2) and is purged after sync — a backend supervisor can never hear it; decide audio upload for low-confidence records vs device-only review
- [ ] 061 - Required fields never enumerated: §4.1 step 4 flags "unfilled required fields" but no list exists; schema implies location is required yet §13 has no "no location spoken" case — conflicts with "never block capture"
- [ ] 062 - Clarify auto-confirm: §13 "force the readback/confirm step" implies a high-confidence fast path that §4.1 (always confirm) doesn't describe — which is it?
- [ ] 063 - MVP auth undefined: §3 defers identity to §11, but §11 tokens are Phase B only; specify Phase A operator login, device enrollment, and operator_id provenance
- [ ] 064 - Wake-phrase scope conflict: §4.1 offers it at capture, §16.2 asks "MVP or defer?", §17 schedules it P4 — align the three
- [ ] 065 - Verify modernc.org/sqlite builds/runs under gomobile on iOS and Android before committing; note the "cgo-free" rationale (§9.3) is moot since whisper.cpp already forces cgo
- [ ] 066 - Verify Android acceleration claims: ggml/whisper.cpp offers Vulkan/OpenCL but has no NNAPI backend (§8.5); Silero-ONNX VAD drags in an onnxruntime native dep (§9.3) — energy VAD first
- [ ] 067 - Post-sync edits undefined: can operators edit/delete a `synced` record in batch review, and how does that change re-sync? (§4.2, §10.2)
- [ ] 068 - Accuracy metric ambiguous: define how "≥95% after confirmation" is measured — what counts as an error once the operator has confirmed? (§12, §15)
- [ ] 069 - Specify behavior at the ~30 s utterance cap: truncate, split, or warn the operator (§8.3; missing from §13)
- [ ] 070 - TTS engine unspecified: platform TTS via native shell vs a library — interacts with the §16.1 UI decision (§4.1)
- [ ] 071 - Specify the voice-confirm listening window: after readback, does the mic auto re-arm for "yes" in push-to-talk mode, or must the operator press again? (§4.1)
- [ ] 072 - Define batch-review export: format and destination (CSV? share sheet?) (§4.2)
- [ ] 073 - Supervisor scope boundary: §3 grants review/approve/export of merged backend records, but §2.2 declares downstream out of scope — say where that UI lives
- [ ] 074 - Data-model nits: corrections[] example uses field "location_id" with spoken-text values (§6.1); `language` should store the resolved language, not "auto"; "session" (§4.2) is never defined
- [ ] 075 - Resolve remaining §16 open decisions: UI approach (Gio vs native shells — blocks 001), device-joins-grid vs gateway, audio-retention default window, part-alias curation strategy
