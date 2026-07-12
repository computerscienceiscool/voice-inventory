# Voice Inventory Indexer — Go core

Walk the warehouse, speak what you see — *"Twelve boxes of RJ45 connectors
in bin A-14"* — and get structured, offline-first inventory records.
This repository implements the **Go core** of the
[voice-inventory spec](voice-inventory-indexer-spec.md): everything below
the native UI shell in §9.1, plus a desktop CLI and a mock backend.

Speech-to-text runs locally through [whisper.cpp](https://github.com/ggml-org/whisper.cpp)
— weights cache on the device once, so capture has **no per-utterance cost**,
which is the whole premise (§1).

## What's here

| Package | Spec | Purpose |
|---------|------|---------|
| `parser` | §5, §7 | Deterministic slot filler: transcript → quantity/unit/item/location/description; mid-utterance corrections ("…A-40, no, A-14"); voice commands; multi-item splitting |
| `lang` | §5.2, §7 | Table-driven English + Spanish vocabularies, number-word engine ("forty" → 40, "dos mil quinientos" → 2500) |
| `refdata` | §6.2 | Locations/parts/units reference data + fuzzy resolvers (suggestive, never blocking) |
| `observation` | §6.1 | The record schema, status lifecycle (draft→confirmed→synced; →rejected), validation, readback text |
| `store` | §10.1 | SQLite (WAL, synchronous FULL) offline queue, corrections log, reference cache, sync state |
| `vad` | §8.3 | Energy + zero-crossing voice-activity detector: utterance segmentation, pre-roll, 30 s cap |
| `audio` | §8.3 | Resample→16 kHz mono float32, WAV codec, high-pass filter |
| `asr` | §8.1, §8.2 | `Transcriber` interface; whisper.cpp CLI runner (desktop/CI); model fetch-once + SHA-256 cache; scriptable mock |
| `session` | §4 | Capture state machine: arm → VAD → transcribe → parse → readback → confirm/correct/scratch → save; audio retention + purge; latency instrumentation |
| `syncer` | §10.2, §11-A | Offline-first HTTPS sync: idempotent batched push, ETag reference pull, bounded retries |
| `grid` | §11-B | CBOR grid messages (protocol by reference) + CWT/COSE capability tokens (ES256) |
| `config` | §14 | Per-device profile: model, capture mode, thresholds, retention, sync endpoint |
| `mobile` | §9.2 | gomobile-bind facade (JSON-string API + Events/Transcriber bridge interfaces) |
| `cmd/vinv` | — | Desktop driver + in-memory mock backend |

## Documentation

- [Architecture](docs/architecture.md) — data flow, package map, design decisions, concurrency contract
- [How to speak to it / parser internals](docs/parsing.md) — operator phrasing guide + slot-filler reference
- [CLI reference](docs/cli.md) — every `vinv` command
- [Backend sync protocol](docs/backend-protocol.md) — the contract a real backend implements
- [Mobile integration](docs/mobile-integration.md) — gomobile bind, Events/Transcriber bridges, shell checklist
- [Operations & field trial](docs/operations.md) — stand up a pilot, enroll devices, run the §15 acceptance trial
- [Roadmap](docs/roadmap.md) — what remains: native shells, device validation, open decisions
- [Decision proposals](docs/proposals.md) — a recommendation for each open decision, ready to approve
- [CHANGELOG.md](CHANGELOG.md) — what's in 0.1.0 and the hardening history
- [TODO/TODO.md](TODO/TODO.md) — numbered feature/issue tracker (spec review + code review findings)

## Quick start (desktop)

```sh
go build -o vinv ./cmd/vinv

# parse only
./vinv parse "Twelve boxes of RJ45 connectors in bin A-14"

# full text-path capture into a local queue
./vinv transcript -db inv.db -confirm "Bin C-7, forty spools of Cat6, three have water damage"

# capture from a recording with whisper.cpp
./vinv capture -db inv.db -wav clip.wav -whisper ./whisper-cli -model ggml-small-q5_1.bin -confirm

# run a mock backend and sync against it
./vinv mockserver -addr 127.0.0.1:8873 &
./vinv sync -db inv.db -endpoint http://127.0.0.1:8873 -insecure -mode all

# review queue
./vinv list -db inv.db -needs-review
./vinv edit -db inv.db -id <uuid> -field location -value "A-40"
./vinv stats -db inv.db
```

## Testing

```sh
go test ./...          # unit + golden-transcript suites (en+es)
go test -race ./...
```

The golden-audio suite (§15) runs end-to-end ASR→parse when whisper.cpp is
available:

```sh
VINV_WHISPER_BIN=…/whisper-cli VINV_WHISPER_MODEL=…/ggml-small-q5_1.bin \
  go test ./asr -run TestGoldenAudio -v
```

Recordings live in `asr/testdata/golden_audio/` next to `cases.json`
(`{"wav": "...", "lang": "en", "quantity": 12, "item": "...", "location": "..."}`).

## Mobile packaging (§9.2)

The `mobile` package is the bind surface:

```sh
gomobile bind -target=android -o VoiceInventory.aar ./mobile
gomobile bind -target=ios     -o VoiceInventory.xcframework ./mobile
```

The native shell:

1. implements `mobile.Events` (state, level meter, readback, saved/haptic cue, errors);
2. implements `mobile.Transcriber` around whisper.cpp (JNI / ObjC), returning
   whisper.cpp `-oj` JSON — or calls `SetExecTranscriber` on desktop;
3. feeds mic PCM via `FeedPCM16` between `BeginUtterance`/`EndUtterance`
   (push-to-talk) or continuously in wake mode;
4. drives `Confirm` / `Scratch` / `CorrectField` from taps, and
   `SyncPush` / `SyncPull` opportunistically.

## Spec decisions made in code

Ambiguities found during the spec review (see `TODO/TODO.md`, items 058+)
were resolved conservatively and are documented where they land:

- **Drafts** are written to SQLite the moment they parse (crash safety);
  only **confirmed** records sync. Batch review lists drafts.
- **"Scratch that" marks records `rejected`** rather than deleting them, so
  the action is auditable — this also gives the spec's orphaned `rejected`
  status its transition (item 059).
- **Synced records are immutable on-device** (item 067); the one sanctioned
  change is clearing `audio_ref` after a retention purge.
- **No auto-confirm by default** (§4.1 always confirms); an admin can opt in
  via `auto_confirm_high_confidence` (item 062).
- **Required fields never block capture** (§13): missing item/quantity/
  location flag the record `needs_review` instead (item 061).
- **The record's `language` stores the language actually used**, not
  "auto" (item 074).
- Latency is measured **utterance-end → readback** (§8.4) and a persistent
  miss suggests the smaller model.

## Not in this repository

The native shells (SwiftUI / Jetpack Compose), the whisper.cpp JNI/ObjC
glue, on-device GPU acceleration flags (§8.5), TTS readback voices, the
wake-phrase keyword spotter, and the production PromiseGrid agent transport
(§11 Phase B — the message format and tokens are here in `grid`; the agent
protocol is upstream) are platform work tracked in `TODO/TODO.md`.
