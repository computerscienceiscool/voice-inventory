# Architecture

This repository is the Go core of the voice-inventory indexer
([spec](../voice-inventory-indexer-spec.md) §9.1) — everything between the
native UI shell above and whisper.cpp/the microphone below.

## Data flow (one observation)

```
mic PCM (any rate/channels, from the shell)
  │  session.FeedPCM
  ▼
audio: downmix → stateful resample to 16 kHz mono → high-pass (100 Hz)
  │
  ▼
vad: energy + zero-crossing detector
  • push-to-talk: buffer between BeginUtterance/EndUtterance, then Trim
  • wake mode: continuous segmentation, pre-roll, 30 s cap
  │  one utterance (float32, 16 kHz)
  ▼
asr.Transcriber                whisper.cpp (native binding on device,
  │  text + token confidence   CLI subprocess on desktop, mock in tests)
  ▼
parser: deterministic slot filler (per-language tables from lang)
  • numbers: "forty"→40, "a couple hundred"→~200
  • location anchors: in/at/bin/aisle/shelf… → "A-14", "aisle 5 / shelf 2"
  • mid-utterance corrections: "…A-40, no, A-14" (last value wins)
  • leftover → description
  • refdata resolvers: location_text→location_id, item_text→part_number
  │  observation.Parsed + certainties
  ▼
session: draft saved to store immediately (crash safety) → readback text
  → operator confirms/corrects/scratches (voice or taps) → confirmed
  │
  ▼
store (SQLite, WAL + synchronous FULL): the offline queue
  │  opportunistic, when connectivity exists
  ▼
syncer: batched idempotent push (confirmed → synced), ETag refdata pull
  └─ Phase B: grid.EncodeObservation → CBOR message + CWT/COSE token
```

## Package dependency shape

`lang`, `fuzzy`, `audio`, `vad`, `asr`, `observation` are leaves.
`refdata` uses `fuzzy`+`lang`. `parser` uses `lang`+`refdata`+`observation`.
`store` persists `observation`+`refdata`. `session` orchestrates all of the
above. `syncer` uses `store`. `grid` wraps `observation` for Phase B.
`mobile` and `cmd/vinv` are the two entry points; nothing imports them.

## Key design decisions

- **Deterministic first (§7).** The parser is table-driven rules, no ML.
  Language tables live in `lang/tables_en.go` / `tables_es.go`; adding
  vocabulary is data, not code. A future on-device LLM assist slots in
  behind the same `parser.Result` shape (P4).
- **The Transcriber seam (§8.1).** Everything upstream depends only on
  `asr.Transcriber`. Desktop/CI run whisper.cpp as a subprocess
  (`asr.ExecTranscriber`); devices bind whisper.cpp natively and hand back
  its `-oj` JSON through `mobile.Transcriber`; tests script `asr.Mock`.
- **Drafts persist at parse time.** A record is in SQLite before the
  operator ever confirms it, so a crash or force-quit loses nothing (§12).
  Status lifecycle: `draft → confirmed → synced`, with `rejected` reachable
  from draft/confirmed ("scratch that" is an audit event, not a delete).
- **Synced records are immutable on-device.** The backend owns them after
  upload; the one sanctioned change is clearing `audio_ref` after a
  retention purge (§6.3).
- **No goroutines inside the library.** All session methods are
  synchronous and mutex-guarded; the shell chooses threading. Snapshots
  handed out (Pending, readbacks) are deep copies and never mutated again.
- **Confidence = parse certainty × ASR confidence.** Every field carries a
  score; below-threshold fields are "doubtful" (highlighted in readback)
  and set `needs_review` with human-readable reasons.

## Concurrency contract

`session.Session`, `store.Store`, and `mobile.App` are safe for concurrent
use. The session mutates records by clone-and-swap so a UI thread reading
`Pending()` never races a capture thread applying corrections. SQLite runs
with a single pooled connection (one writer).

## What lives outside this repo

Native shells (SwiftUI/Compose), whisper.cpp JNI/ObjC glue, GPU
acceleration flags (§8.5), TTS voices, the wake-phrase keyword spotter, and
the production PromiseGrid agent transport. `TODO/TODO.md` tracks all of it.
