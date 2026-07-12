# Voice Inventory Indexer — Technical Specification

**Status:** Draft v0.2 (see Changes, §18)
**Target platforms:** Android and iOS (phones and tablets)
**Implementation language:** Go (core) + whisper.cpp (speech-to-text)
**Deployment model:** Offline-first mobile app; optional sync to a PromiseGrid backend

---

## 1. Purpose

A handheld tool that lets a person **walk the warehouse and speak** what they find —
**what it is, where it is, and how many** — plus an optional free-text description. Each
spoken observation is transcribed on-device and turned into a structured inventory record.
No clipboard, no typing, no stopping to interact with a screen for each item.

The tool exists because **Whisper runs locally**: the neural-net weights are open and cache
to the device on first use, so speech-to-text has **no per-utterance/token cost**. That is
what makes continuous, all-day inventory-by-voice economically viable and is the core
premise of this spec.

### 1.1 Primary use case

An operator carries a phone (or wears a headset paired to one), walks the aisles, and for
each thing they want to record, says something like:

> "Twelve boxes of RJ45 connectors in bin A-14."
> "Bin C-7 — about forty spools of Cat6, three of them have water damage."

The app captures audio, transcribes it locally, parses it into fields, shows the operator a
readback for confirmation, and stores the record locally. Records sync to the backend when
connectivity is available.

---

## 2. Goals and non-goals

### 2.1 Goals
- Fully **hands-free capture** while walking — one tap (or wake phrase) to start an utterance.
- **On-device** speech-to-text with **no network dependency** and no per-use cost.
- Extract structured fields — **item, location, quantity, unit, description** — from natural speech.
- **English and Spanish** support (warehouse staff speak both; Spanish part procedures exist).
- **Offline-first**: capture works with zero connectivity; sync is opportunistic.
- Low friction correction so a mis-heard entry takes seconds to fix.
- Run acceptably on **mid-range phones** (not just flagships).

### 2.2 Non-goals (explicitly out of scope for this tool)
- Reconciling consumption against sales, BOM explosion, or any "digital twin" modelling — that is a **separate downstream system**. This tool only *captures observations*; what consumes them is not specified here.
- Barcode/RFID scanning (may be added later; not required).
- Full warehouse-management-system features (picking, put-away optimization, etc.).
- Real-time multi-user conflict resolution during capture (each device captures its own stream; merge happens at the backend).

---

## 3. Users and roles

| Role | Capability |
|------|-----------|
| Operator | Capture observations, review/correct their own queue, trigger sync. |
| Supervisor | All operator abilities + review/approve/export the merged backend records. |
| Admin | Manage device enrollment, vocabulary/synonyms, locations list, model selection. |

Identity is per-device + per-operator login (see §11 security). MVP may collapse Supervisor
and Admin into one role.

---

## 4. User experience / capture flow

### 4.1 Happy path (per observation)
1. **Arm capture.** Operator taps a large push-to-talk button. (A wake phrase — configurable, default "log item" — arrives with the P4 keyword spotter, §17; until then hands-busy capture is continuous-VAD mode, §4.2.) Push-to-talk is the default and most reliable in a noisy warehouse.
2. **Speak the observation.** A voice-activity detector (VAD) determines utterance start/end; a visible level meter confirms the mic is hearing them.
3. **Transcribe.** whisper.cpp transcribes the buffered audio on-device.
4. **Parse.** The transcript is parsed into fields (§6). Unfilled required fields are flagged.
5. **Readback.** The app displays the parsed record and (optionally) speaks it back via TTS: *"12 boxes, RJ45 connectors, bin A-14. Correct?"*
6. **Confirm or correct.** Operator confirms (voice "yes"/"correct" or tap ✓) or corrects (re-speak a single field: "location is A-40", or tap a field to re-dictate/edit).
7. **Save.** Record is written to the local queue with status `confirmed`. App returns to armed/idle for the next item.

### 4.2 Modes
- **Push-to-talk (default):** hold or tap-to-start/tap-to-stop. Most robust with background noise.
- **Continuous / hands-busy:** for operators whose hands are full; opt-in. MVP: continuous VAD segmentation — **every** utterance in range becomes a capture attempt, so this mode is a deliberate privacy/battery trade-off until the P4 wake-phrase spotter gates it. Higher false-trigger risk.
- **Batch review:** a list screen of the session's records for bulk review, edit, delete, and export/sync.

### 4.3 Design constraints for the UI
- Buttons large enough to operate with gloves and without looking.
- Everything reachable one-handed.
- High-contrast, glanceable readback (warehouse lighting varies).
- Audible + haptic confirmation on save (operator may not be looking at the screen).

---

## 5. Utterance grammar

The operator is **not** required to speak in a rigid format, but the parser is tuned around a
recommended phrasing and a set of anchor keywords. A short on-device "how to speak to it"
card ships with the app.

### 5.1 Recommended phrasing
```
[quantity] [unit] [of] <item> [in|at] <location> [, <description>]
<location> , [quantity] [unit] <item> [, <description>]
```

### 5.2 Anchor keywords (configurable, per language)
- **Location anchors:** `in`, `at`, `bin`, `location`, `shelf`, `rack`, `aisle`
- **Quantity:** spoken number words → digits ("forty" → 40; "a dozen" → 12; "a couple" → 2 flagged low-confidence)
- **Unit vocabulary:** `box`, `boxes`, `spool`, `spools`, `reel`, `bag`, `each`, `piece`, `foot`, `feet`, `meter`, … (extensible list; unknown units stored as raw text)
- **Correction verbs:** `location is`, `quantity is`, `change … to …`, `scratch that`, `delete`

### 5.3 Examples → parsed
| Utterance | quantity | unit | item | location | description |
|-----------|----------|------|------|----------|-------------|
| "Twelve boxes of RJ45 connectors in bin A-14" | 12 | boxes | RJ45 connectors | A-14 | — |
| "Bin C-7, forty spools of Cat6, three have water damage" | 40 | spools | Cat6 | C-7 | three have water damage |
| "About a dozen assorted brackets, aisle five shelf two" | 12 (low-conf) | — | assorted brackets | aisle 5 / shelf 2 | — |

---

## 6. Data model

### 6.1 `Observation` record
```jsonc
{
  "id": "uuid-v7",                 // time-ordered, generated on device
  "device_id": "string",
  "operator_id": "string",
  "captured_at": "RFC3339 timestamp",
  "language": "en|es|auto",

  "raw_transcript": "string",      // exact Whisper output, always retained
  "audio_ref": "string|null",      // local path/hash of the retained clip (optional, see §6.3)

  "parsed": {
    "item_text": "string",         // as spoken
    "part_number": "string|null",  // resolved match, if matched (§6.2)
    "quantity": "number|null",
    "unit": "string|null",
    "location_text": "string",     // as spoken
    "location_id": "string|null",  // resolved to known location, if matched
    "description": "string|null"
  },

  "confidence": {
    "asr": 0.0,                    // Whisper avg logprob → normalized 0–1
    "quantity": 0.0,
    "location": 0.0,
    "item": 0.0
  },

  "status": "draft|confirmed|synced|rejected",
  "corrections": [ { "field": "location_id", "from": "A-40", "to": "A-14", "at": "..." } ],
  "schema_version": 1
}
```

### 6.2 Reference data (synced down to the device)
- **Locations list** — known bins/shelves/aisles with IDs and spoken aliases ("bin A-14", "A fourteen"). Used to resolve `location_text` → `location_id` by fuzzy match.
- **Part vocabulary** — known part numbers + human-spoken names/synonyms, used to resolve `item_text` → `part_number`. Matching is **suggestive, not blocking**: an unmatched item is still saved as free text so capture never stalls on an unknown part.
- **Unit vocabulary** — recognized units + synonyms.

### 6.3 Audio retention
The short audio clip per utterance is retained by default so a human can re-verify a
low-confidence record later, then purged on a policy (default: delete after successful sync +
N days). Retention is configurable and can be disabled for storage-constrained devices.

---

## 7. Parsing strategy

Deterministic first, model-assisted later (mirrors the "deterministic MVP, LLM afterwards"
principle used elsewhere in the project).

- **MVP — deterministic slot filler.** Tokenize the transcript, normalize number words to
  digits, locate anchor keywords, and assign spans to slots. Fuzzy-match location/item spans
  against reference data (§6.2). Everything left over becomes `description`. Every field
  carries a confidence score; anything below threshold is flagged for confirmation.
- **Phase 2 — optional on-device small LLM** to structure free-form or unusual phrasings the
  slot filler misses. Runs locally to preserve the zero-cost property. Strictly additive; the
  deterministic path remains the fallback.

The parser is a standalone Go package with a table-driven, language-specific ruleset so
English and Spanish rules live side by side and new vocabulary is data, not code.

---

## 8. Speech-to-text (Whisper) subsystem

### 8.1 Engine
- **whisper.cpp** (ggml/gguf inference) invoked from Go via its cgo bindings. Chosen because
  it is the portable, mobile-friendly Whisper runtime with quantization and hardware
  acceleration on both target OSes.
- Wrapped behind a Go `Transcriber` interface so the engine can be swapped (e.g. a future
  faster-whisper server for desktop, or a native platform recognizer) without touching
  callers.

```go
type Transcriber interface {
    // Transcribe returns text + token-level timing/confidence for a mono 16 kHz PCM buffer.
    Transcribe(ctx context.Context, pcm []float32, lang Lang) (Result, error)
    LoadModel(path string, opts ModelOpts) error
    Close() error
}
```

### 8.2 Models
- **Default:** quantized `small` (multilingual, for en+es) — best accuracy/latency balance for
  warehouse vocabulary and mixed-language staff. Quantization `q5_1` or `q8_0`.
- **Low-end fallback:** quantized `base`/`tiny` for older devices, English-only where staff
  are English-only, selectable per device.
- Weights are bundled with the app or fetched once on first run and **cached on device**
  (the local-caching behavior is the whole cost premise). No weights are downloaded per use.
- Model choice, language, and quantization are admin-configurable per device profile.

### 8.3 Audio pipeline
1. Capture mic at device-native rate, **downsample to 16 kHz mono PCM float32** (Whisper's expected input).
2. **VAD** segments utterances. MVP: an energy + zero-crossing detector with an adaptive noise floor (no ML dependency); Silero VAD (ONNX) is a possible later upgrade at the cost of an onnxruntime native dependency. VAD trims silence so only speech is fed to Whisper, cutting latency and battery.
3. Optional light noise suppression / high-pass filter for warehouse ambient noise.
4. Buffer per utterance (cap length, e.g. 30 s) → `Transcribe`.

### 8.4 Performance targets (per utterance, ~5 s of speech)
| Device class | Model | Target transcribe latency |
|--------------|-------|---------------------------|
| Recent flagship | small q5 | ≤ 1.5 s |
| Mid-range (2–3 yr) | small q5 | ≤ 3 s |
| Low-end | base/tiny q5 | ≤ 3 s |

Latency is measured from utterance-end to readback shown. If a device can't hit target on
`small`, it auto-suggests `base`.

### 8.5 Hardware acceleration
- **iOS:** Metal + optional CoreML encoder (whisper.cpp supports a CoreML-converted encoder for a large speedup); build step produces the CoreML model alongside the ggml weights.
- **Android:** CPU with ARM NEON as the baseline; Vulkan or OpenCL acceleration where the device supports it (ggml/whisper.cpp has **no NNAPI backend** — earlier drafts were wrong). Fall back to CPU cleanly.

---

## 9. Architecture

### 9.1 Layers
```
┌──────────────────────────────────────────────────────────┐
│ Native UI shell (thin)         iOS: SwiftUI | Android: Compose │  ← option A
│   OR  Gio (pure-Go cross-platform UI)                          │  ← option B
├──────────────────────────────────────────────────────────┤
│ Go core  (packaged for mobile via gomobile bind)              │
│   • capture orchestration / session state                     │
│   • parser (slot filler + resolvers)                          │
│   • data model + validation                                   │
│   • local store (SQLite)                                       │
│   • sync client                                               │
│   • Transcriber interface                                     │
├──────────────────────────────────────────────────────────┤
│ whisper.cpp (C++/cgo)     │   Audio capture + VAD             │
│   ggml/gguf weights       │   (miniaudio / native mic)        │
└──────────────────────────────────────────────────────────┘
```

### 9.2 Go core packaging
- Built as a mobile library with **gomobile bind** → an Android **AAR** and an iOS
  **xcframework**. The native shell calls into it. (Option B: **Gio** for a single pure-Go
  UI across both platforms; chosen if we want to minimize per-platform native code. Decision
  in §16.)

### 9.3 Key dependencies (candidate)
| Concern | Library |
|---------|---------|
| Mobile binding | `golang.org/x/mobile/cmd/gomobile` |
| Whisper | whisper.cpp Go bindings (cgo) |
| Audio capture | `malgo` (miniaudio; Android/iOS support) or native mic via shell |
| VAD | Silero VAD (ONNX) or energy-based (MVP) |
| Local DB | `modernc.org/sqlite` (pure-Go, cgo-free — simplest for gomobile) |
| UUIDv7 | `github.com/google/uuid` or equivalent |
| CBOR (grid msgs) | `github.com/fxamacker/cbor/v2` |
| Fuzzy match | Levenshtein/Jaro-Winkler (small local impl) |

---

## 10. Storage and offline-first sync

### 10.1 Local store
- SQLite on device holds the observation queue, reference data (locations/parts/units), and
  sync state. All capture reads/writes are local and never block on the network.

### 10.2 Sync
- **Opportunistic, resumable, one-way-up for observations, one-way-down for reference data.**
- On connectivity, unsynced `confirmed` records are pushed in batches; on success they become
  `synced`. Reference data (locations, parts, units) is pulled and cached for offline matching.
- **Idempotent** by record `id` (UUIDv7); safe to retry. Conflict handling is trivial because
  each device owns its own records — the backend merges, it does not overwrite device records.
- Sync is a defined Go `Syncer` interface so the transport can be plain HTTPS in the MVP and
  a PromiseGrid transport later (§11) without changing capture code.

---

## 11. Backend / PromiseGrid integration (phased)

The mentor's standing requirement is that persistent, shared logic runs as **PromiseGrid grid
app(s)**, not laptop/VM-bound scripts. To respect that without blocking a shippable MVP:

- **Phase A (MVP):** device syncs to a single backend endpoint over HTTPS; records land in a
  store that a grid app can later own. The mobile app is standalone and useful immediately.
- **Phase B (grid-native):** the sync target becomes a PromiseGrid agent. Observations are
  encoded as **CBOR** grid messages consistent with the project's existing message
  conventions (payload as a proper structure, protocol referenced by piece — not embedded in
  the payload; see the ex1 order-flow conventions). Capture and edit tokens use the project's
  capability-token scheme (CWT/COSE, ECDSA) so device identity and operator authority travel
  with each record.

The mobile app never needs to become a grid node itself in Phase A; it speaks to one. Whether
it later participates directly in the grid is an open decision (§16).

---

## 12. Non-functional requirements

- **Accuracy:** target ≥ 95% correct on quantity and location for in-vocabulary items under
  normal warehouse noise, after confirmation. (ASR + parse + human confirm compound.)
- **Latency:** utterance-end → readback within the §8.4 targets.
- **Offline:** 100% of capture functionality works with no network.
- **Battery/thermal:** an 8-hour shift of intermittent capture should not exhaust a typical
  phone; VAD and per-utterance (not continuous) inference keep the model idle between items.
- **Storage:** model + app + a full shift of audio clips fits comfortably in a few GB;
  audio retention policy bounds growth.
- **Robustness:** a crash or force-quit never loses confirmed records (write-ahead to SQLite).
- **Privacy/security:** see §11 tokens; audio and transcripts are business data and stay on
  device until synced to an authorized backend.

---

## 13. Error handling and edge cases
- **Low ASR confidence** → force the readback/confirm step; highlight the doubtful field.
- **Unknown part/location** → save as free text, flag `unresolved`, let the backend or a
  supervisor resolve later. Never block capture.
- **No quantity spoken** → save with `quantity: null`, flagged; operator can add it or leave it.
- **"Scratch that" / "delete"** → discard the in-progress or last-saved record on command.
- **Mid-utterance correction** ("…in bin A-40, no, A-14") → parser keeps the last value for a slot.
- **Multiple items in one breath** → MVP records one observation per utterance; a "…and…" splitter is a Phase-2 enhancement.
- **Mic permission denied / no mic** → clear, non-fatal error; app still allows manual entry.
- **Model missing/corrupt** → re-fetch/re-cache with clear progress; capture disabled until ready.

---

## 14. Configuration (admin, per device profile)
- Whisper model + quantization + language(s).
- Capture mode default (push-to-talk vs wake phrase), wake phrase text.
- Anchor keyword / unit / synonym tables per language.
- Audio retention policy.
- Sync endpoint / grid agent + credentials.
- Confidence thresholds for forced confirmation.

---

## 15. Testing and acceptance

- **Unit:** parser (table-driven cases per language), number-word normalization, fuzzy resolvers, DB layer.
- **Golden-audio suite:** a fixed set of recorded warehouse utterances (en + es, quiet + noisy) with expected parsed output; run in CI to catch ASR/parse regressions across model changes.
- **Device matrix:** at least one low-end, one mid, one flagship, per OS; verify latency targets and thermal behavior.
- **Field trial (acceptance):** an operator inventories a real section by voice; measure correction rate and time-per-item vs the current paper method.

**MVP acceptance criteria:**
1. Operator captures ≥ 50 consecutive items by voice, walking, without typing.
2. ≥ 95% quantity+location accuracy after confirmation on in-vocabulary items.
3. Full offline operation; records survive force-quit and sync cleanly on reconnect.
4. Runs on the designated mid-range test device within latency targets.

---

## 16. Open decisions
1. **UI:** Gio (single pure-Go codebase) vs native shells (SwiftUI/Compose) over the gomobile core. Native gives the best mic/permission/UX integration; Gio minimizes code. *Recommend starting native-thin on Android, evaluate Gio.*
2. ~~**Wake-phrase engine** for hands-busy mode — include in MVP or defer?~~ **Resolved: deferred to P4** (§17); MVP hands-busy mode is continuous VAD, opt-in, documented as a privacy/battery trade-off (§4.2).
3. **Does the device join the grid directly** in a later phase, or always talk to a grid agent gateway? (§11 Phase B.)
4. **Audio retention default window** and whether it's on by default given storage/privacy.
5. **Part-number resolution:** ship a curated spoken-alias list, or auto-generate aliases from the part master?

---

## 17. Suggested build phases
1. **P0 — Core capture (English, push-to-talk):** gomobile core + whisper.cpp on one Android device, deterministic parser, SQLite queue, on-screen readback/confirm. No sync.
2. **P1 — Usability + Spanish:** TTS readback, voice corrections, Spanish model+rules, location/part resolvers, batch review screen.
3. **P2 — Sync (HTTPS):** offline queue → backend, reference-data pull. iOS build.
4. **P3 — Grid-native:** CBOR grid messages + capability tokens; sync target becomes a PromiseGrid agent.
5. **P4 — Enhancements:** wake-phrase mode, multi-item utterances, optional on-device LLM parse assist.

---

## 18. Changes

**v0.2 (2026-07-12)** — corrections from implementation review (repo TODO items 064, 066, 086):
- §8.5: removed the NNAPI claim — ggml/whisper.cpp offers CPU/NEON, Vulkan, and OpenCL on Android, no NNAPI backend.
- §4.1/§16.2: wake-phrase scoping aligned with §17 — deferred to P4; resolved open decision 2.
- §4.2/§8.3: MVP hands-busy mode is continuous VAD (energy + zero-crossing, adaptive noise floor, no ONNX dependency) and is documented as an opt-in privacy/battery trade-off until the keyword spotter lands.

The reference implementation of the core lives in this repository; behavioral decisions that resolved v0.1 ambiguities are catalogued in README "Spec decisions made in code" and TODO/TODO.md items 058–075.
