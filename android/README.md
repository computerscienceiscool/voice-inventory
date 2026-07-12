# Android shell (scaffold)

The thin native shell over the Go core (spec §9, option A): Compose UI,
AudioRecord → `FeedPCM16`, platform TTS/haptics, and whisper.cpp via JNI.

## Verification status — read this first

This scaffold was written on a machine **without an Android SDK**, so the
Kotlin/Gradle layer is **unbuilt**. The risky part, however, is verified:

- `cpp/whisper_bridge.c` — every whisper.cpp API call — was **compiled and
  executed on desktop Linux** against whisper.cpp v1.6.2 with real weights
  and real speech; its JSON output round-trips through the Go core's
  `asr.ParseWhisperJSON` (language, confidence, and token timings intact).
- The Go facade's bind surface was verified with `gobind`: the full Java
  API (26 `App` methods, 8 `Events` callbacks) generates with no skipped
  symbols.
- `cpp/whisper_jni.c` (~40 lines of standard JNI marshaling) and the
  Kotlin/Gradle code are conventional but **untested** — expect first-build
  fixes of the usual kind (version bumps, small API drift).

To re-run the desktop bridge verification:

```sh
git clone --depth 1 --branch v1.6.2 https://github.com/ggerganov/whisper.cpp /tmp/w
(cd /tmp/w && make -j main)   # produces whisper.o, ggml*.o
gcc -c -O2 -I/tmp/w app/src/main/cpp/whisper_bridge.c -o /tmp/bridge.o
gcc -c -O2 -Iapp/src/main/cpp app/src/main/cpp/bridge_test_main.c -o /tmp/main.o
g++ /tmp/main.o /tmp/bridge.o /tmp/w/whisper.o /tmp/w/ggml*.o -lm -lpthread -o /tmp/bridge_test
/tmp/bridge_test ggml-tiny.en-q5_1.bin sample16k.wav en   # prints the JSON
```

## Build steps (on a machine with Android Studio / SDK + NDK)

1. **Build the Go core AAR** (from the repo root):

   ```sh
   go install golang.org/x/mobile/cmd/gomobile@latest
   gomobile init
   gomobile bind -target=android -androidapi 26 \
     -o android/app/libs/voiceinventory.aar ./mobile
   ```

2. **Vendor whisper.cpp** (pinned to the verified version):

   ```sh
   git clone --depth 1 --branch v1.6.2 \
     https://github.com/ggerganov/whisper.cpp android/app/src/main/cpp/whisper.cpp
   ```

3. **Open `android/` in Android Studio** (or `./gradlew assembleDebug`
   after generating the wrapper with `gradle wrapper`).

4. **Push model weights** to the device (§8.2 — bundle or fetch-once;
   the scaffold looks in the app's files dir):

   ```sh
   adb shell mkdir -p /data/user/0/com.thesalleys.voiceinventory/files/models
   adb push ggml-small-q5_1.bin \
     /data/user/0/com.thesalleys.voiceinventory/files/models/
   ```

5. Grant the mic permission on first launch, **Arm**, hold the button,
   speak: *"twelve boxes of RJ45 connectors in bin A-14"*.

## What's wired

| Spec | Where |
|------|-------|
| Push-to-talk hold button, one-handed, glove-sized (§4.2, §4.3) | `ui/CaptureScreen.kt` |
| Mic level meter (§4.1 step 2) | `CaptureScreen` ← `Events.onLevel` |
| Readback card with doubtful-field highlighting (§4.1, §13) | `CaptureScreen` ← `Events.onReadback` |
| TTS spoken readback (§4.1 step 5) | `AppViewModel.speak` |
| Audible + haptic save cue (§4.3) | `AppViewModel.saveCue` ← `Events.onSaved` |
| Batch review with needs-review + backend-rejected badges (§4.2, TODO 087) | `ui/BatchReviewScreen.kt` |
| Sync trigger (§3) | `BatchReviewScreen` → `syncPush`/`syncPull` |
| Mic-denied fallback (§13) | permission denial leaves review/manual paths usable |
| whisper.cpp on-device (§8.1) | `WhisperTranscriber.kt` → `cpp/whisper_bridge.c` |

Not in the scaffold yet: operator login screen (TODO 063 decision),
settings/config UI (§14), the help card (035 — content in
`docs/parsing.md`), wake-phrase mode (P4), CoreML/iOS (042).
