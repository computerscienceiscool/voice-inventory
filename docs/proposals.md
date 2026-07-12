# Proposed answers to the open decisions

Every remaining decision item from [the roadmap](roadmap.md), each with a
concrete recommendation ready to approve, amend, or overrule. Approving one
means: cross the todo item off with the decision noted, and (where marked)
a small code/spec change follows.

## 075 — UI approach: native shells vs Gio ⭐ blocks item 001

**Recommend: native-thin shells** (Compose now, SwiftUI later), as the spec
itself leans (§16.1). Rationale: mic permissions, AudioRecord/AVAudioEngine,
TTS, haptics, and background audio all have first-class native APIs and
awkward Gio equivalents; the Go core already carries all logic, so the
shells stay thin (~600 lines each — both are scaffolded). Cost: two UI
codebases; acceptable because they're deliberately dumb.
*Also in 075:* device-joins-grid vs gateway → **gateway** (Phase B talks to
a grid agent; the phone never becomes a node — revisit only if offline
peer-sync becomes a requirement). Audio retention default → **on, 7 days**
(what the code ships). Part aliases → **start curated** (export the part
master's top ~200 spoken names, let the field trial grow the list;
auto-generation can seed suggestions later).

## 070 — TTS engine

**Recommend: platform TTS** (`android.speech.tts`, `AVSpeechSynthesizer`) —
zero added binary size, offline voices for en+es ship with both OSes, and
readback is a single short sentence where voice quality barely matters.
Both scaffolds already wire it. Revisit only if field-trial operators can't
hear the platform voices over warehouse noise.

## 063 — Phase-A operator auth + device enrollment

**Recommend the minimal honest scheme:** the backend issues each device a
long-lived **device token** at enrollment (admin pastes/scans it into
Settings → stored in the config); operators are a **local pick-list
synced down with reference data** (a fourth refdata array), selected at
shift start — no per-operator password on-device in Phase A. Each record
already carries device_id + operator_id; the backend attributes trust to
the device token. Real per-operator credentials arrive with Phase B
capability tokens (§11), which already encode operator authority.
*Code follow-up:* add `operators` to the refdata pull + a picker screen.

## 060 — Audio for low-confidence records

**Recommend: opt-in upload, low-confidence only.** Default stays
device-only (privacy, bandwidth). Add a config flag
`upload_low_confidence_audio`; when set, push attaches clips only for
records flagged `needs_review`, as a separate multipart endpoint so the
JSON batch path stays simple. Supervisors then re-verify remotely (§6.3's
stated purpose); everyone else loses nothing.
*Code follow-up:* one endpoint + config flag; audio purge already handles
the lifecycle.

## 084 — At-rest encryption

**Recommend: rely on OS full-device encryption in Phase A** (default-on
for Android 10+ and all supported iPhones) and require a device passcode
in the deployment checklist. SQLCipher adds a cgo dependency and key
management for data that is business-sensitive but not regulated. Revisit
if a customer contract demands app-layer encryption.

## 068 — How the ≥95% accuracy metric is measured (§12, §15)

**Recommend this protocol:** during the field trial, a checker
independently records ground truth for each item the operator captures.
A record **counts as correct iff its final synced quantity AND location_id
both match ground truth**; the metric is correct ÷ captured, measured on
in-vocabulary items under normal noise. Corrections made during capture
(voice or tap) count toward the time-per-item metric, not against
accuracy — the spec's "after confirmation" phrase means post-confirm state
is what's graded. Records the operator scratched are excluded; records
`needs_review` are graded like any other (flagging is a workflow aid, not
an excuse).

## 072 — Batch-review export

**Recommend: CSV via the platform share sheet** (operator-facing), with
the JSON already available for integrations (`vinv list`, `ListJSON`).
Columns: captured_at, operator, quantity, unit, item, part_number,
location, location_id, status, needs_review, description.
*Code follow-up:* a small CSV writer in the core + a share intent in the
shell.

## 073 — Where the supervisor review UI lives

**Recommend: on the backend, out of this repo** — exactly the §2.2
boundary. The device's batch review is for the capturing operator; the
supervisor works on merged, multi-device data, which is downstream by
definition. The mock server's `/v1/records` shows the contract a minimal
supervisor web view needs. Amend §3's supervisor row to say "via the
backend review UI (downstream system)".
