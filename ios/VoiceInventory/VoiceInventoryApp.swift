import SwiftUI

@main
struct VoiceInventoryApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            CaptureView(model: model)
                .alert("Error", isPresented: .constant(model.lastError != nil)) {
                    Button("OK") { model.lastError = nil }
                } message: {
                    Text(model.lastError ?? "")
                }
        }
    }
}
