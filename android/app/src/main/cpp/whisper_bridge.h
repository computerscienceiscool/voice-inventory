// whisper_bridge: JNI-free C core of the Android whisper.cpp bridge.
//
// Everything that touches the whisper.cpp API lives here so it can be
// compiled and run against a desktop whisper.cpp checkout for testing
// (see android/README.md); whisper_jni.c is only thin JNI glue.
//
// The JSON produced matches whisper.cpp's own -oj output closely enough
// for the Go core's asr.ParseWhisperJSON: result.language, and per
// segment text/offsets plus token text/p/offsets in milliseconds.
#ifndef VOICE_INVENTORY_WHISPER_BRIDGE_H
#define VOICE_INVENTORY_WHISPER_BRIDGE_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// vi_bridge_init loads ggml weights and returns an opaque context handle
// (0 on failure). Thread-safe for distinct handles.
int64_t vi_bridge_init(const char *model_path);

// vi_bridge_free releases a context handle.
void vi_bridge_free(int64_t handle);

// vi_bridge_transcribe_wav transcribes a canonical WAV produced by the Go
// core's audio.EncodeWAV16 (44-byte header, PCM16 mono 16 kHz) and returns
// a malloc'd JSON string the caller must free with vi_bridge_free_string.
// lang is "en", "es", or "auto". Returns NULL on failure.
char *vi_bridge_transcribe_wav(int64_t handle, const uint8_t *wav, size_t wav_len,
                               const char *lang, int n_threads);

// vi_bridge_free_string releases a string returned by this library.
void vi_bridge_free_string(char *s);

#ifdef __cplusplus
}
#endif

#endif
