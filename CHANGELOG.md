# Changelog

## 0.1.0 — 2026-07-11 (unreleased)

Initial implementation of the voice-inventory Go core
([spec](voice-inventory-indexer-spec.md) §9.1).

### Added

- Deterministic EN+ES slot-filler parser with number-word engine,
  location-anchor grammar, mid-utterance corrections, voice commands,
  multi-item splitting, and golden-transcript suites (the spec §5.3
  examples are tests).
- Reference-data resolvers (locations → ids, items → part numbers;
  suggestive, never blocking) with backend-extensible unit vocabulary.
- SQLite offline queue (WAL + synchronous FULL): drafts persist at parse
  time; status lifecycle draft → confirmed → synced with auditable
  rejects; corrections log; retention-aware audio references.
- Energy + zero-crossing VAD with pre-roll, hangover, adaptive dual-rate
  noise floor, hum rejection, and a 30 s utterance cap.
- Streaming audio pipeline: phase-continuous resampling to 16 kHz mono,
  stateful high-pass filter, WAV codec.
- `asr.Transcriber` seam: whisper.cpp CLI runner (desktop/CI), native
  bridge interface for mobile, scriptable mock, and verified fetch-once
  model caching (checksum before install, serialized concurrent fetches).
- Capture session state machine: push-to-talk and continuous modes,
  readback with doubtful-field highlighting, voice confirm/correct/
  scratch, re-dictation, manual entry, audio retention + purge, latency
  instrumentation, out-of-band status reconciliation.
- Offline-first HTTPS sync: cursor-paginated idempotent push, ETag
  reference pull, bounded retries, TLS enforced by default.
- Phase-B grid format: CBOR messages (protocol by reference) with
  ES256 COSE/CWT capability tokens binding identity AND payload digest.
- gomobile bind facade (`mobile`), `vinv` CLI, and an in-memory mock
  backend implementing the sync protocol.
- Docs: architecture, backend protocol, mobile integration, parsing
  guide, CLI reference, roadmap. CI: gofmt + vet + race tests.

### Hardening (four review rounds, 37 bugs fixed — TODO items 076–111)

- Two independent adversarial code reviews, staticcheck, and four fuzz
  targets (parser, WAV decoder, grid decoder, session state machine;
  ~14M+ executions) — every confirmed finding fixed with a regression
  test. Highlights: push starvation, timestamp-ordering purge bug,
  pending-record data races, VAD hum lock, payload-unbound grid tokens,
  session wedging on batch-review races, model-install verification
  order, and silent CLI failure modes.

### Rounds 6–8 additions (2026-07-12)

- **Shells**: Android brought to P0/P1 feature-completeness (tap-to-edit
  fields, batch-review edit, en+es help card, settings over a new config
  facade, CSV export via share sheet); iOS SwiftUI shell to parity
  (capture, review with edit, settings, help) reusing the same
  desktop-verified whisper C bridge.
- **Config facade**: `mobile.ConfigJSON`/`SetConfigJSON` (merge, validate,
  persist) — shells can now read and edit the device profile.
- **CSV export** (`export` package): stable, injection-safe columns via
  `vinv export`, `mobile.ExportCSV`, and both share sheets.
- **Void protocol**: `POST /v1/observations:void` closes the
  in-flight-reject divergence; persistent backend-rejection badges
  (schema v2 migration + filters + surfaces).
- **Integration package**: the full capture→confirm→sync→export chain and
  an offline-then-reconnect test — the end-to-end regression guard.
- **Performance**: benchmarks for the on-device work around whisper.cpp;
  a resolver hot-path optimization (precomputed key forms) halving
  allocations and time, proven behavior-preserving by an independent
  oracle test.
- **Docs**: operations/field-trial guide, decision proposals, spec v0.2.

### Round 5 additions (2026-07-12)

- Sync protocol: `POST /v1/observations:void` tombstones records discarded
  while their upload was in flight (persistent retry; mock server support).
- Persistent backend-rejection badges on records (schema v2 migration,
  store filter, `vinv list -sync-rejected`, `mobile.ListSyncRejectedJSON`).
- Spec bumped to Draft v0.2: NNAPI claim corrected, wake-phrase scoping
  aligned to P4, continuous-mode privacy trade-off documented.
- Verified: real whisper.cpp inference through the full pipeline (0.56 s
  wall on desktop), and the mobile facade generates complete Java bindings
  via gobind (26 methods, 8 callbacks, no skips).

### Not in 0.1.0

Native UI shells, on-device whisper.cpp builds, the wake-phrase spotter,
and the PromiseGrid agent transport — see [docs/roadmap.md](docs/roadmap.md).
