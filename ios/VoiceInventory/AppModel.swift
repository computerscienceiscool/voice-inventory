import AVFoundation
import Foundation
import Mobile
import UIKit

struct ReadbackField: Identifiable {
    let id = UUID()
    let label: String
    let value: String
    let doubtful: Bool
}

/// Owns the bound Go core, the AVAudioEngine capture loop, TTS readback
/// (§4.1) and the haptic+audible save cue (§4.3). Mirror of the Android
/// AppViewModel; see docs/mobile-integration.md.
@MainActor
final class AppModel: NSObject, ObservableObject {
    @Published var state = "idle"
    @Published var level: Float = 0
    @Published var readbackText: String?
    @Published var fields: [ReadbackField] = []
    @Published var lastError: String?
    @Published var modelReady = false

    private var app: MobileApp!
    private let engine = AVAudioEngine()
    private let synthesizer = AVSpeechSynthesizer()
    private let haptic = UINotificationFeedbackGenerator()

    override init() {
        super.init()
        let dataDir = FileManager.default.urls(
            for: .documentDirectory, in: .userDomainMask)[0].path
        var err: NSError?
        app = MobileNewApp(dataDir, "", EventsBridge(model: self), &err)
        if let err { lastError = err.localizedDescription }
        loadModel(URL(fileURLWithPath: dataDir)
            .appendingPathComponent("models/ggml-small-q5_1.bin").path)
    }

    private func loadModel(_ path: String) {
        Task.detached { [app] in
            do {
                let t = try WhisperTranscriber(modelPath: path)
                app?.setTranscriber(t)
                await MainActor.run { self.modelReady = true }
            } catch {
                await MainActor.run {
                    self.lastError = "model: \(error.localizedDescription) — capture disabled (§13)"
                }
            }
        }
    }

    // --- capture (push-to-talk §4.2) ---------------------------------------

    func arm() { off { $0.arm() } }
    func disarm() { stopMic(); off { $0.disarm() } }

    func pttDown() {
        guard modelReady else { return }
        off { try? $0.beginUtterance() }
        startMic()
    }

    func pttUp() {
        stopMic()
        off { $0.endUtterance() }
    }

    private func startMic() {
        let session = AVAudioSession.sharedInstance()
        try? session.setCategory(.record, mode: .measurement)
        try? session.setActive(true)
        let input = engine.inputNode
        let inFormat = input.outputFormat(forBus: 0)
        // The Go core resamples; feed PCM16 at the hardware rate.
        let outFormat = AVAudioFormat(
            commonFormat: .pcmFormatInt16,
            sampleRate: inFormat.sampleRate, channels: 1, interleaved: true)!
        let converter = AVAudioConverter(from: inFormat, to: outFormat)!
        input.installTap(onBus: 0, bufferSize: 1600, format: inFormat) { [weak self] buf, _ in
            guard let self else { return }
            let frames = AVAudioFrameCount(buf.frameLength)
            guard let out = AVAudioPCMBuffer(pcmFormat: outFormat, frameCapacity: frames)
            else { return }
            var consumed = false
            converter.convert(to: out, error: nil) { _, status in
                if consumed {
                    status.pointee = .noDataNow
                    return nil
                }
                consumed = true
                status.pointee = .haveData
                return buf
            }
            guard let ch = out.int16ChannelData else { return }
            let data = Data(bytes: ch[0], count: Int(out.frameLength) * 2)
            self.app.feedPCM16(data, sampleRate: Int(inFormat.sampleRate), channels: 1)
        }
        try? engine.start()
    }

    private func stopMic() {
        engine.inputNode.removeTap(onBus: 0)
        engine.stop()
    }

    // --- review --------------------------------------------------------------

    func confirm() { off { try? $0.confirm() } }
    func scratch() { off { $0.scratch() } }
    func correct(field: String, value: String) {
        off { try? $0.correctField(field, value: value) }
    }

    // --- batch review, sync, export, settings (§4.2, §14) -------------------

    func listRecords(status: String) async -> String {
        (try? app.listJSON(status, limit: 200)) ?? #"{"observations":[]}"#
    }

    func confirmRecord(_ id: String) { off { try? $0.confirmRecord(id) } }
    func rejectRecord(_ id: String) { off { try? $0.rejectRecord(id) } }
    func editRecord(_ id: String, field: String, value: String) {
        off { try? $0.editRecord(id, field: field, value: value) }
    }

    func sync() async -> String {
        let app = self.app!
        return await Task.detached {
            (try? app.syncPush()).map { "push \($0)" } ?? "sync failed"
        }.value
    }

    /// CSV to a temp file for the iOS share sheet (item 072).
    func exportCSV() async -> URL? {
        guard let csv = try? app.exportCSV("") else { return nil }
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("inventory-export.csv")
        try? csv.write(to: url, atomically: true, encoding: .utf8)
        return url
    }

    func configJSON() -> String { (try? app.configJSON()) ?? "{}" }
    func saveConfig(_ json: String) throws { try app.setConfigJSON(json) }

    // --- events (called from core threads via EventsBridge) ------------------

    func handle(state s: String) { state = s }
    func handle(level rms: Double) { level = Float(rms) }

    func handle(readbackJSON json: String) {
        guard let data = json.data(using: .utf8),
              let root = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let obs = root["observation"] as? [String: Any],
              let parsed = obs["parsed"] as? [String: Any]
        else { return }
        let doubtful = Set((root["doubtful"] as? [String]) ?? [])
        func field(_ label: String, _ key: String) -> ReadbackField {
            let v = parsed[key].flatMap { "\($0)" } ?? "—"
            return ReadbackField(label: label, value: v == "<null>" ? "—" : v,
                                 doubtful: doubtful.contains(label.lowercased()))
        }
        fields = [
            field("Quantity", "quantity"),
            field("Item", "item_text"),
            field("Location", "location_text"),
        ]
        let text = (root["text"] as? String) ?? ""
        readbackText = text
        let utterance = AVSpeechUtterance(string: text) // TTS readback (§4.1)
        synthesizer.speak(utterance)
    }

    func handleSaved() {
        readbackText = nil
        fields = []
        haptic.notificationOccurred(.success) // §4.3 save cue
        AudioServicesPlaySystemSound(1057)
    }

    func handle(error message: String) { lastError = message }

    private func off(_ block: @escaping (MobileApp) -> Void) {
        let app = self.app!
        Task.detached { block(app) }
    }
}

/// Marshals Go-core events onto the main actor.
private final class EventsBridge: NSObject, MobileEventsProtocol {
    weak var model: AppModel?
    init(model: AppModel) { self.model = model }

    func onState(_ s: String?) { post { $0.handle(state: s ?? "idle") } }
    func onLevel(_ rms: Double) { post { $0.handle(level: rms) } }
    func onSpeechStart() {}
    func onReadback(_ json: String?) {
        guard let json else { return }
        post { $0.handle(readbackJSON: json) }
    }
    func onSaved(_ id: String?, status: String?) { post { $0.handleSaved() } }
    func onDiscarded(_ id: String?) { post { $0.handleSaved() } }
    func onError(_ message: String?) {
        guard let message else { return }
        post { $0.handle(error: message) }
    }
    func onSuggestion(_ message: String?) {
        guard let message else { return }
        post { $0.handle(error: message) }
    }

    private func post(_ update: @escaping (AppModel) -> Void) {
        Task { @MainActor [weak model] in
            if let model { update(model) }
        }
    }
}
