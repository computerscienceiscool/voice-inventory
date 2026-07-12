# vinv — CLI reference

`vinv` drives the full core on a desktop: parsing, capture from WAV files
via whisper.cpp, queue management, sync, and a mock backend. Build with
`go build -o vinv ./cmd/vinv`. Every command that touches records takes
`-db <path>` (created on first use).

## Capture

```sh
# parser only (no store); add -db to resolve against cached reference data
vinv parse [-lang en|es] [-multi] [-db inv.db] "twelve boxes of RJ45 in bin A-14"

# text-path capture: parse → draft (+ -confirm to save immediately)
vinv transcript -db inv.db [-lang en] [-confirm] "Bin C-7, forty spools of Cat6"

# full pipeline from a recording: decode → VAD trim → whisper.cpp → parse → store
vinv capture -db inv.db -wav clip.wav -whisper ./whisper-cli \
     -model ggml-small-q5_1.bin [-lang auto] [-threads 4] [-confirm] [-audio-dir clips/]

# typed entry (mic-denied fallback, §13)
vinv add -db inv.db -item "hex nuts" -qty 7 -unit box -loc B-2 [-desc "…"] [-confirm]
```

## Queue management (batch review, §4.2)

```sh
vinv list -db inv.db [-status draft|confirmed|synced|rejected] [-limit 20] [-needs-review]
vinv confirm -db inv.db <id> [<id>…]
vinv reject  -db inv.db <id> [<id>…]          # "scratch" — auditable, not a delete
vinv edit    -db inv.db -id <id> -field location -value "A-40"
vinv stats   -db inv.db                        # counts by status
vinv export  -db inv.db [-status confirmed] [-o out.csv]   # CSV (stdout default)
```

`export` writes RFC 4180 CSV with a stable column header (item 072); every
field is quoted, so transcripts with commas/quotes/newlines are safe.

`edit` fields: `location` (re-resolved), `quantity` (number words OK),
`item` (re-matched against parts), `unit`, `description`.

## Sync

```sh
export VINV_TOKEN=…    # preferred over -token (argv is visible in ps)
vinv sync -db inv.db -endpoint https://backend.example.com \
     [-device dev-42] [-mode push|pull|all] [-insecure]
```

`push` uploads confirmed records (idempotent, batched); `pull` refreshes
locations/parts/units with ETag caching. `-insecure` permits `http://` for
development only.

## Reference data & maintenance

```sh
# import local JSON (arrays of {id,name,aliases} / {part_number,name,aliases} / {name,language,aliases})
vinv refdata -db inv.db -locations locs.json -parts parts.json -units units.json

# retention: delete clips for records synced more than N days ago (§6.3)
vinv purge-audio -db inv.db -audio-dir clips/ -keep-days 7
```

## Mock backend

```sh
vinv mockserver [-addr 127.0.0.1:8873] [-refdata refdata.json]
```

Implements the [backend protocol](backend-protocol.md) in memory with
sample warehouse reference data: accepts batches idempotently, serves
refdata with an ETag, and exposes `GET /v1/records` to inspect what
arrived. Loopback-only by default (it has no auth).

## End-to-end smoke test

```sh
vinv mockserver &                                  # terminal 1
vinv transcript -db demo.db -confirm "twelve boxes of RJ45 connectors in bin A-14"
vinv sync -db demo.db -endpoint http://127.0.0.1:8873 -insecure -mode all
vinv transcript -db demo.db -confirm "forty spools of Cat6, bin C-7"   # now resolves via pulled refdata
vinv stats -db demo.db
```
