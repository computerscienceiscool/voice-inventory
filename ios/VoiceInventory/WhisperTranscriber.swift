import Foundation
import Mobile

/// Implements the Go core's `MobileTranscriberProtocol` over the same
/// desktop-verified C bridge the Android app uses (cpp/whisper_bridge.c —
/// Swift calls it directly through the bridging header; no JNI layer).
final class WhisperTranscriber: NSObject, MobileTranscriberProtocol {
    private var handle: Int64 = 0

    init(modelPath: String) throws {
        handle = vi_bridge_init(modelPath)
        guard handle != 0 else {
            throw NSError(domain: "whisper", code: 1, userInfo: [
                NSLocalizedDescriptionKey: "model failed to load: \(modelPath)",
            ])
        }
    }

    func transcribeWAV(_ wav: Data?, lang: String?) throws -> String {
        guard let wav else {
            throw NSError(domain: "whisper", code: 2, userInfo: [
                NSLocalizedDescriptionKey: "empty audio",
            ])
        }
        let json: UnsafeMutablePointer<CChar>? = wav.withUnsafeBytes { buf in
            vi_bridge_transcribe_wav(
                handle,
                buf.bindMemory(to: UInt8.self).baseAddress,
                wav.count,
                lang ?? "auto",
                4
            )
        }
        guard let json else {
            throw NSError(domain: "whisper", code: 3, userInfo: [
                NSLocalizedDescriptionKey: "transcription failed",
            ])
        }
        defer { vi_bridge_free_string(json) }
        return String(cString: json)
    }

    deinit {
        if handle != 0 { vi_bridge_free(handle) }
    }
}
