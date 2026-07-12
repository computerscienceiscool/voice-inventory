# iOS shell (scaffold)

SwiftUI capture shell over the Go core — a leaner mirror of `../android/`
(capture + readback + save cue; batch review/settings/help screens follow
the Android versions when ported). Written without Xcode: **unbuilt**; the
whisper.cpp C core it calls is the same desktop-verified
`whisper_bridge.c` the Android app uses (see `../android/README.md`).

## Assembly steps (on a Mac with Xcode)

1. **Go core framework** (repo root):

   ```sh
   gomobile bind -target=ios -o ios/Mobile.xcframework ./mobile
   ```

2. **Create the Xcode project** (App template, SwiftUI, iOS 16+), then add:
   - the Swift files in `VoiceInventory/`
   - `Mobile.xcframework`
   - `VoiceInventory-Bridging-Header.h` (set as the target's bridging header)
   - `../android/app/src/main/cpp/whisper_bridge.{h,c}` (shared source)
   - whisper.cpp v1.6.2 sources (`whisper.cpp`, `ggml*.c/.h`) — or the
     `whisper.xcframework` produced by whisper.cpp's
     `build-xcframework.sh`. Enable Metal per §8.5 (WHISPER_METAL); the
     CoreML encoder (item 042) is a follow-up.

3. **Info.plist**: `NSMicrophoneUsageDescription` ("Voice inventory
   capture"), and ship or download model weights to
   `Documents/models/ggml-small-q5_1.bin` (§8.2).

4. Run on device, Arm, hold the button, speak:
   *"twelve boxes of RJ45 connectors in bin A-14"*.

## Status

| Piece | State |
|-------|-------|
| whisper.cpp C bridge | **verified on desktop** (compiled + executed, JSON round-trips the Go parser) |
| Go facade bind surface | **verified** via gobind (obj-c naming: `MobileApp`, `MobileEventsProtocol`, `MobileTranscriberProtocol`) |
| Swift capture UI, AVAudioEngine loop, TTS, haptics | conventional but **unbuilt** |
| Batch review / settings / help screens | port from Android (`../android/.../ui/`) |
| Metal / CoreML acceleration (item 042) | follow-up after first build |
