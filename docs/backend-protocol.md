# Backend sync protocol (Phase A, HTTPS)

The device is offline-first: capture never touches the network. When
connectivity exists, the syncer pushes confirmed observations up and pulls
reference data down (spec §10.2). This document is the contract a real
backend must implement; `vinv mockserver` is a working in-memory reference.

Transport requirements: HTTPS (the client refuses `http://` unless built
with `AllowInsecure` for development), bearer-token auth
(`Authorization: Bearer <token>`), JSON bodies.

## POST /v1/observations:batch

Uploads a batch of confirmed records (default batch size 50, oldest first).

Request:

```jsonc
{
  "device_id": "dev-42",
  "observations": [
    {
      "id": "019f5451-9a77-7a6b-b5f3-6f173cbbcc83",   // UUIDv7, time-ordered
      "device_id": "dev-42",
      "operator_id": "op-7",
      "captured_at": "2026-07-11T20:14:09.123456789Z",
      "language": "en",                                // language actually used
      "raw_transcript": "forty spools of Cat6 in bin C-7",
      "audio_ref": "019f5451….wav",                    // device-local; audio does NOT upload
      "parsed": {
        "item_text": "Cat6",
        "part_number": "PN-2002",                      // null when unresolved
        "quantity": 40,                                // null when unspoken
        "unit": "spools",
        "location_text": "C-7",
        "location_id": "LOC-C7",
        "description": null
      },
      "confidence": { "asr": 0.93, "quantity": 0.93, "location": 0.91, "item": 0.9 },
      "status": "confirmed",
      "corrections": [
        { "field": "location", "from": "C-1", "to": "C-7", "at": "2026-07-11T20:14:15Z" }
      ],
      "needs_review": false,
      "schema_version": 1
    }
  ]
}
```

Response (200):

```json
{
  "accepted": ["019f5451-9a77-7a6b-b5f3-6f173cbbcc83"],
  "rejected": [ { "id": "…", "reason": "schema_version unsupported" } ]
}
```

Semantics the backend MUST honor:

- **Idempotent by `id`.** Re-uploads of an already-stored id are accepted
  again (overwrite or no-op — device records are immutable after sync, so
  both are equivalent). The device retries batches on failure; duplicates
  are normal.
- **Merge, never overwrite across devices.** Each device owns its ids
  (UUIDv7); there is nothing to conflict on.
- **`accepted` drives the device state machine.** Only accepted ids become
  `synced` locally. Rejected ids stay `confirmed` and are retried on the
  next push pass (they never block records queued behind them).
- Status codes: 5xx and 429 are retried with backoff; other 4xx abort the
  pass and surface to the operator (bad token, bad request).

## GET /v1/refdata

Returns the full reference-data set with an `ETag`. The device sends
`If-None-Match`; reply `304 Not Modified` when unchanged.

```json
{
  "locations": [ { "id": "LOC-A14", "name": "Bin A-14", "aliases": ["A-14", "A fourteen"] } ],
  "parts":     [ { "part_number": "PN-1001", "name": "RJ45 connector", "aliases": ["RJ45 connectors"] } ],
  "units":     [ { "name": "skid", "language": "en", "aliases": ["skids"] } ]
}
```

The device replaces its cache wholesale (small data set, no diffing) and
hot-reloads its fuzzy resolvers. `language` on a unit is `en`, `es`, or
empty for all languages.

## Phase B — PromiseGrid (spec §11)

The `grid` package defines the grid-native message: a CBOR envelope
`{1: protocol, 2: payload, 3: token}` where `protocol` is the reference
string `voice-inventory/observation/v1` (protocol by piece, not embedded),
`payload` is the CBOR-encoded observation, and `token` is a COSE_Sign1 CWT
(ES256) whose claims carry device id (iss), operator id (sub), record id
(cti), issue/expiry times, and a SHA-256 digest of the payload bytes
(private claim −65537) so payload tampering is detectable. The transport to
an actual grid agent plugs in behind the `syncer.Syncer` interface once the
agent protocol is available upstream.
