# Mobile integration guide

The `mobile` package is the gomobile bind surface (spec §9.2). Build it:

```sh
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
gomobile bind -target=android -androidapi 24 -o VoiceInventory.aar ./mobile
gomobile bind -target=ios -o VoiceInventory.xcframework ./mobile
```

Everything crosses the boundary as strings, numbers, `[]byte`, and two
small interfaces the shell implements. Structured data is JSON.

Status note: the Go bind surface is tested in this repo, while the native
Android and iOS shells under `android/` and `ios/` are scaffolded first-pass
integrations that still need their first real SDK/Xcode build.

## 1. Boot

```kotlin
val app = Mobile.newApp(context.filesDir.path, "", eventsImpl)
app.setTranscriber(whisperBridge)   // or setExecTranscriber(bin, model, threads) on desktop
app.setOperator("op-7")             // after operator login
```

`NewApp(dataDir, configJSON, events)` opens `dataDir/observations.db`,
loads `dataDir/config.json` (see `config.Config`; the JSON argument
overrides it), and keeps audio clips in `dataDir/audio/`.

## 2. Implement `Events` (UI callbacks)

| Callback | Drive this UI |
|----------|---------------|
| `OnState(state)` | idle / armed / reviewing indicator |
| `OnLevel(rms)` | mic level meter (§4.1 step 2) |
| `OnSpeechStart()` | "listening" cue in wake mode |
| `OnReadback(json)` | readback screen: `{observation, doubtful[], text, auto_confirmed}` — highlight `doubtful` fields, speak `text` via platform TTS |
| `OnSaved(id, status)` | **play the audible + haptic save cue (§4.3)** |
| `OnDiscarded(id)` | brief "scratched" feedback |
| `OnError(msg)` | non-fatal toast ("no speech detected", …) |
| `OnSuggestion(msg)` | e.g. "switch to the base model" (§8.4) |

Callbacks arrive on whichever thread called into the core — marshal to the
main thread yourself.

## 3. Implement `Transcriber` (whisper.cpp bridge)

```java
String transcribeWAV(byte[] wav, String lang) throws Exception
```

Input is a complete 16 kHz mono 16-bit WAV of one utterance; `lang` is
`en`, `es`, or `auto`. Run whisper.cpp natively and return its `-oj` JSON
output as a string (the core parses text, language, and token confidences
from it). Model weights: bundle them or fetch-once with
`asr.EnsureModel`-equivalent behavior — never per use (§1, §8.2).

## 4. Capture loop

Push-to-talk (default, §4.2):

```kotlin
app.arm()
// button down:
app.beginUtterance()
// AudioRecord thread, any rate/channels — the core converts:
app.feedPCM16(chunkBytes, sampleRate, channelCount)
// button up:
app.endUtterance()   // → OnReadback fires (or OnError "no speech detected")
```

Wake/continuous mode (`capture_mode: "wake"`): just `arm()` and keep
feeding PCM; the VAD segments utterances. Note: until the keyword spotter
ships this is *continuous* listening — opt-in only.

## 5. Review actions

```kotlin
app.confirm()                        // tap ✓  → OnSaved
app.correctField("location", "A-40") // tap-to-edit a field
app.scratch()                        // discard pending (or last saved)
```

Voice does the same automatically: while reviewing, "yes/correct" confirms,
"location is A-40" / "no, A-14" corrects, "scratch that" discards, and any
other utterance is treated as a full re-dictation of the pending record.

## 6. Batch review, sync, maintenance

```kotlin
app.listJSON("draft", 50)        // {"observations":[…]}, newest first
app.listSyncRejectedJSON(50)     // records the backend refused — badge these
app.editRecord(id, "quantity", "15")
app.confirmRecord(id); app.rejectRecord(id)
app.addManual(parsedJSON, true)  // mic-permission-denied fallback (§13)
app.exportCSV("")                // CSV string → temp file → share sheet (072)

app.syncPush(); app.syncPull()   // returns report JSON; call opportunistically
app.purgeAudio()                 // retention policy (§6.3), e.g. daily
app.statsJSON()                  // latency + queue counts
app.configJSON(); app.setConfigJSON(json)  // read/merge device profile (§14)
```

Records carry `sync_rejected_reason`/`sync_rejected_at` after the backend
refuses them on a push; the badge clears automatically when a later push
succeeds. Rejecting a record whose upload is already in flight is safe:
the core voids it on the backend automatically (see
[backend-protocol.md](backend-protocol.md)).

**Privacy note — continuous mode.** Until the wake-phrase spotter ships,
`capture_mode: "wake"` means the VAD treats *every* utterance in mic range
as a capture attempt. Keep it opt-in per §4.2, show a persistent
"listening" indicator, and prefer push-to-talk on shared floors.

`syncPull` hot-reloads the location/part resolvers; no restart needed.

## 7. Shell responsibilities checklist

- Mic permission + AudioRecord/AVAudioEngine capture, fed via `FeedPCM16`.
- Big glove-friendly PTT button, one-handed layout, high-contrast readback
  (§4.3); TTS for `OnReadback.text`; sound+vibration on `OnSaved`.
- whisper.cpp native build (NEON baseline; Metal/CoreML on iOS — §8.5).
- Operator login UI → `SetOperator`.
- Store grid signing keys (Phase B) in Keystore/Secure Enclave.
