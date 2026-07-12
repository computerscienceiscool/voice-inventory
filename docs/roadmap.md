# Roadmap — what needs to be done

The Go core (spec §9.1) is complete, reviewed, and tested: 79 of the 112
tracked items in [TODO/TODO.md](../TODO/TODO.md) are done. The 33 open
items all fall into four buckets. Numbers reference the todo.

## 1. Native app work — needs Android/iOS toolchains

The Go side of each item is finished and documented in
[mobile-integration.md](mobile-integration.md); what's left is shell code.

| Item | Work | Notes |
|------|------|-------|
| 001 | `gomobile bind` AAR/xcframework + thin Compose shell | Android-first (§17 P0); blocked on decision 075 (Gio vs native) |
| 007, 015, 016 | Level meter, readback screen, tap confirm/edit | Core emits levels, readback text + doubtful fields, and Confirm/CorrectField APIs |
| 018, 019 | Audible+haptic save cue, glove-friendly layout | Hook is `OnSaved`; §4.3 constraints |
| 024 | TTS spoken readback | Blocked on decision 070 (platform TTS vs library) |
| 033, 035 | Batch review screen, "how to speak" help card | List/edit/confirm/reject APIs done; card content lives in [parsing.md](parsing.md) |
| 042, 043 | whisper.cpp device builds: Metal/CoreML (iOS), NEON/GPU (Android) | §8.5; note 066 — no NNAPI backend exists |
| 085 | Grid signing keys into Keystore / Secure Enclave | PEM helpers are dev-only |

## 2. Real-world validation — needs hardware and warehouse time

| Item | Work |
|------|------|
| 054 | Record the golden-audio clips (en+es, quiet+noisy). The CI harness (`asr.TestGoldenAudio`) is built and waiting for WAVs |
| 055 | Device matrix: low/mid/flagship per OS, latency vs §8.4 targets, thermal |
| 056 | 8-hour intermittent-capture battery test |
| 057 | Field trial: ≥50 consecutive items by voice, ≥95% qty+location accuracy — the MVP acceptance gate (§15) |
| 065 | Confirm modernc.org/sqlite builds under gomobile on both OSes (proven on desktop only) |

## 3. Decisions — for the project owner / mentor

| Item | Decision |
|------|----------|
| 060 | Does audio upload for low-confidence records, or is re-verification device-only? Clips currently never leave the device |
| 063 | Phase-A operator authentication + device enrollment (operator id is currently just set, not authenticated) |
| 064, 066, 068, 072, 073 | Spec edits: wake-phrase phasing, drop the NNAPI claim, define how the 95% metric is measured, batch-review export format, where the supervisor UI lives |
| 070 | TTS engine (interacts with 075) |
| 075 | §16 set: Gio vs native shells (blocks 001), device-joins-grid vs gateway, retention default (code ships on/7 days), part-alias curation |
| 084 | At-rest encryption: rely on OS device encryption, or add SQLCipher |
| 112 | Sync protocol needs a void/tombstone: a record rejected locally after upload diverges silently from the backend |

## 4. Later-phase features

| Item | Work |
|------|------|
| 049 | PromiseGrid agent transport — message format + capability tokens are done in `grid`; blocked on the upstream agent protocol |
| 050 | Wake-phrase keyword spotter — until it lands, "wake" mode is continuous listening (privacy note 086) |
| 052 | Optional on-device LLM parse assist (P4; deterministic path stays the fallback) |
| 086, 087 | Privacy note for continuous mode; persistent badge for backend-rejected records in batch review |

## Suggested order

1. Decide 075 (UI approach) → build the thin Android shell (001) — this
   unblocks every UI item at once.
2. Wire a real device build (043, 065) → record golden audio (054) → run
   the device matrix (055, 056).
3. Field trial (057) gates the MVP; decisions 060/063 should land before
   pilot data flows.
