package com.thesalleys.voiceinventory

import android.annotation.SuppressLint
import android.app.Application
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import android.media.ToneGenerator
import android.os.Build
import android.os.VibrationEffect
import android.os.Vibrator
import android.os.VibratorManager
import android.speech.tts.TextToSpeech
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import mobile.App
import mobile.Events
import mobile.Mobile
import org.json.JSONObject
import java.io.File
import java.util.Locale
import kotlin.concurrent.thread

/** One field of the pending readback, with its doubt flag (§4.1 step 5). */
data class ReadbackField(val label: String, val value: String, val doubtful: Boolean)

data class ReadbackUi(
    val text: String,
    val fields: List<ReadbackField>,
    val autoConfirmed: Boolean,
)

/**
 * Owns the bound Go core (`mobile.App`), the microphone loop, and the
 * platform feedback (TTS readback §4.1, haptic+audible save cue §4.3).
 * Threading: the Go core is called off the main thread; Events callbacks
 * arrive on core threads and are marshaled into StateFlows.
 */
class AppViewModel(application: Application) : AndroidViewModel(application) {
    val state = MutableStateFlow("idle")
    val level = MutableStateFlow(0f)
    val readback = MutableStateFlow<ReadbackUi?>(null)
    val errors = MutableStateFlow<String?>(null)
    val modelReady = MutableStateFlow(false)

    private lateinit var app: App
    private var recorder: AudioRecord? = null
    private var recording = false
    private var tts: TextToSpeech? = null
    private val tone = ToneGenerator(ToneGenerator.TONE_DTMF_1, 80)

    private val events = object : Events {
        override fun onState(s: String?) {
            state.value = s ?: "idle"
        }

        override fun onLevel(rms: Double) {
            level.value = rms.toFloat()
        }

        override fun onSpeechStart() {}

        override fun onReadback(json: String?) {
            json ?: return
            val root = JSONObject(json)
            val obs = root.getJSONObject("observation")
            val parsed = obs.getJSONObject("parsed")
            val doubtful = buildSet {
                val arr = root.optJSONArray("doubtful") ?: return@buildSet
                for (i in 0 until arr.length()) add(arr.getString(i))
            }
            fun field(label: String, key: String, unit: String? = null): ReadbackField {
                var v = parsed.opt(key)?.takeIf { it != JSONObject.NULL }?.toString() ?: "—"
                if (unit != null && parsed.opt("unit") != JSONObject.NULL) {
                    v = "$v ${parsed.optString("unit")}"
                }
                return ReadbackField(label, v, doubtful.contains(label.lowercase()))
            }
            readback.value = ReadbackUi(
                text = root.optString("text"),
                fields = listOf(
                    field("Quantity", "quantity", unit = ""),
                    field("Item", "item_text"),
                    field("Location", "location_text"),
                ),
                autoConfirmed = root.optBoolean("auto_confirmed"),
            )
            speak(root.optString("text"))
        }

        override fun onSaved(id: String?, status: String?) {
            readback.value = null
            saveCue()
        }

        override fun onDiscarded(id: String?) {
            readback.value = null
        }

        override fun onError(message: String?) {
            errors.value = message
        }

        override fun onSuggestion(message: String?) {
            errors.value = message
        }
    }

    init {
        val dataDir = application.filesDir.absolutePath
        app = Mobile.newApp(dataDir, "", events)
        tts = TextToSpeech(application) { status ->
            if (status == TextToSpeech.SUCCESS) tts?.language = Locale.US
        }
        loadModel(File(application.filesDir, "models/ggml-small-q5_1.bin"))
    }

    /** Weights arrive via adb push / a download screen; §8.2 fetch-once. */
    private fun loadModel(model: File) {
        viewModelScope.launch(Dispatchers.IO) {
            if (!model.exists()) {
                errors.value = "model missing: ${model.path} — push weights, capture disabled (§13)"
                return@launch
            }
            try {
                app.setTranscriber(WhisperTranscriber(model.absolutePath, threads = 4))
                modelReady.value = true
            } catch (e: Exception) {
                errors.value = "model load failed: ${e.message}"
            }
        }
    }

    // --- capture (push-to-talk §4.2) ---------------------------------------

    fun arm() = offMain { app.arm() }

    fun disarm() {
        stopMic()
        offMain { app.disarm() }
    }

    @SuppressLint("MissingPermission") // RECORD_AUDIO checked by MainActivity
    fun pttDown() {
        if (!modelReady.value) return
        offMain { app.beginUtterance() }
        val minBuf = AudioRecord.getMinBufferSize(
            16000, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT,
        )
        val rec = AudioRecord(
            MediaRecorder.AudioSource.VOICE_RECOGNITION,
            16000, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT,
            maxOf(minBuf, 3200 * 4),
        )
        recorder = rec
        recording = true
        rec.startRecording()
        thread(name = "ptt-mic") {
            val buf = ByteArray(3200) // 100 ms at 16 kHz PCM16
            while (recording) {
                val n = rec.read(buf, 0, buf.size)
                if (n > 0) app.feedPCM16(buf.copyOf(n), 16000, 1)
            }
        }
    }

    fun pttUp() {
        stopMic()
        offMain { app.endUtterance() } // → OnReadback / OnError
    }

    private fun stopMic() {
        recording = false
        recorder?.run { stop(); release() }
        recorder = null
    }

    // --- review (§4.1 step 6-7) --------------------------------------------

    fun confirm() = offMain { runCatching { app.confirm() }.onFailure { errors.value = it.message } }
    fun scratch() = offMain { app.scratch() }
    fun correctField(field: String, value: String) = offMain {
        runCatching { app.correctField(field, value) }.onFailure { errors.value = it.message }
    }

    // --- batch review + sync (§4.2, §10.2) ----------------------------------

    fun listRecords(status: String, onResult: (String) -> Unit) = offMain {
        runCatching { app.listJSON(status, 200) }
            .onSuccess(onResult).onFailure { errors.value = it.message }
    }

    fun confirmRecord(id: String) = offMain { runCatching { app.confirmRecord(id) } }
    fun rejectRecord(id: String) = offMain { runCatching { app.rejectRecord(id) } }

    fun editRecord(id: String, field: String, value: String, onDone: () -> Unit) = offMain {
        runCatching { app.editRecord(id, field, value) }
            .onSuccess { onDone() }
            .onFailure { errors.value = it.message }
    }

    // --- settings (§14) ------------------------------------------------------

    fun loadConfig(onResult: (String) -> Unit) = offMain {
        runCatching { app.configJSON() }
            .onSuccess(onResult).onFailure { errors.value = it.message }
    }

    /** Merge-and-persist; capture-affecting fields apply on next start. */
    fun saveConfig(json: String, onDone: () -> Unit) = offMain {
        runCatching { app.setConfigJSON(json) }
            .onSuccess { onDone() }
            .onFailure { errors.value = it.message }
    }

    fun sync(onDone: (String) -> Unit) = offMain {
        runCatching {
            val push = app.syncPush()
            val pull = app.syncPull()
            onDone("push $push · pull $pull")
        }.onFailure { errors.value = it.message }
    }

    // --- platform feedback ---------------------------------------------------

    private fun speak(text: String) {
        tts?.speak(text, TextToSpeech.QUEUE_FLUSH, null, "readback")
    }

    /** Audible + haptic confirmation on save (§4.3). */
    private fun saveCue() {
        tone.startTone(ToneGenerator.TONE_PROP_ACK, 120)
        vibrator()?.vibrate(VibrationEffect.createOneShot(60, VibrationEffect.DEFAULT_AMPLITUDE))
    }

    private fun vibrator(): Vibrator? {
        val ctx = getApplication<Application>()
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            (ctx.getSystemService(VibratorManager::class.java))?.defaultVibrator
        } else {
            @Suppress("DEPRECATION")
            ctx.getSystemService(Vibrator::class.java)
        }
    }

    private fun offMain(block: () -> Unit) {
        viewModelScope.launch(Dispatchers.IO) { block() }
    }

    override fun onCleared() {
        stopMic()
        tts?.shutdown()
        offMain { app.close() }
    }
}
