# Operations & field-trial guide

How to stand up a pilot, enroll devices, load reference data, and run the
MVP acceptance field trial (spec §15). Assumes the Go core plus one of the
shells is built (see `android/README.md` / `ios/README.md`).

## 1. Backend

Phase A needs an HTTPS endpoint implementing the
[sync protocol](backend-protocol.md): `POST /v1/observations:batch`,
`POST /v1/observations:void`, `GET /v1/refdata`. For a pilot you can run
the reference mock (no auth, loopback) and point devices at a laptop on
the warehouse Wi-Fi:

```sh
go build -o vinv ./cmd/vinv
./vinv mockserver -addr 0.0.0.0:8873 -refdata pilot-refdata.json
```

`pilot-refdata.json` is a `RefDataResponse`: `{locations, parts, units}`
(see the sample the mock ships with). For a real backend, this is where
your WMS exports its bin list and part master.

## 2. Reference data

Resolution quality depends on spoken aliases. For each location give the
codes operators actually say ("A-14", "A fourteen", "bin A 14"); for parts,
the top spoken names ("RJ45", "cat six", "ethernet connector"). Start
curated — export the part master's most-used ~200 names — and grow the
list from field-trial misses (proposal 075). Load locally for testing:

```sh
./vinv refdata -db pilot.db -locations locs.json -parts parts.json -units units.json
```

## 3. Device configuration (§14)

Each device's `config.json` (or the Settings screen) sets: `device_id`
(unique per phone), `operator_id`, `language` (`en`/`es`/`auto`),
`sync.endpoint`, `sync.token`, model name, confidence thresholds,
retention. Push a starter profile:

```sh
adb push device-config.json /data/user/0/com.thesalleys.voiceinventory/files/config.json
```

Weights (§8.2) go in `files/models/`; capture stays disabled with a clear
message until they're present (§13).

## 4. Field-trial protocol (MVP acceptance, §15)

Acceptance criteria and how to measure them:

1. **≥50 consecutive items by voice, walking, no typing.** One operator,
   one real section. Count corrections (voice or tap) — they're allowed and
   feed the time-per-item metric, not the accuracy metric.
2. **≥95% quantity+location accuracy after confirmation** (proposal 068
   measurement): a second person records ground truth per item. A record
   counts correct iff its **final synced quantity AND location_id both
   match** ground truth, over in-vocabulary items under normal noise.
   Scratched records are excluded; `needs_review` records are graded like
   any other. Compute correct ÷ captured.
3. **Full offline operation; records survive force-quit; sync cleanly on
   reconnect.** Put the phone in airplane mode for the run, force-quit it
   mid-session once, then re-enable Wi-Fi and confirm every record reaches
   the backend (`GET /v1/records` count == captured − scratched).
4. **Latency within §8.4 on the mid-range test device.** The app self-
   reports; `StatsJSON` / `vinv stats` gives the median. If it exceeds the
   target it suggests the `base` model automatically.

Export the run for analysis:

```sh
./vinv export -db pilot.db -o trial-run.csv      # or the Export button in-app
```

Compare `trial-run.csv` against the ground-truth sheet on `quantity` and
`location_id` to compute criterion 2, and time-per-item vs the current
paper method (criterion in §15's "field trial" bullet).

## 5. Reading the results

- **High correction rate on one field** → grow that field's aliases
  (§2) or lower its confidence threshold so it's flagged less aggressively.
- **Records stuck `confirmed` with a backend badge** → the backend
  rejected them; `vinv list -sync-rejected` shows why.
- **Continuous ("wake") mode false-triggers** → expected until the P4
  keyword spotter; keep push-to-talk for the trial (privacy note, §4.2).
