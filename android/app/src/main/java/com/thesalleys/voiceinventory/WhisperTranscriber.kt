package com.thesalleys.voiceinventory

import mobile.Transcriber

/**
 * JNI surface of libwhisper_bridge.so (see cpp/whisper_jni.c). All
 * whisper.cpp behavior lives in the desktop-verified C core.
 */
object WhisperLib {
    init {
        System.loadLibrary("whisper_bridge")
    }

    external fun init(modelPath: String): Long
    external fun free(handle: Long)
    external fun transcribeWav(handle: Long, wav: ByteArray, lang: String, nThreads: Int): String?
}

/**
 * Implements the Go core's `mobile.Transcriber`: 16 kHz mono WAV bytes in,
 * whisper.cpp-style JSON out (docs/mobile-integration.md §3).
 */
class WhisperTranscriber(modelPath: String, private val threads: Int) : Transcriber {
    private val handle: Long = WhisperLib.init(modelPath)

    init {
        check(handle != 0L) { "whisper model failed to load: $modelPath" }
    }

    @Throws(Exception::class)
    override fun transcribeWAV(wav: ByteArray?, lang: String?): String {
        val bytes = wav ?: throw IllegalArgumentException("empty audio")
        return WhisperLib.transcribeWav(handle, bytes, lang ?: "auto", threads)
            ?: throw IllegalStateException("whisper transcription failed")
    }

    fun close() {
        WhisperLib.free(handle)
    }
}
