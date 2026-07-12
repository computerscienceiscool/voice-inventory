import SwiftUI

@main
struct VoiceInventoryApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            RootView(model: model)
        }
    }
}

private enum Screen { case capture, review, settings, help }

struct RootView: View {
    @ObservedObject var model: AppModel
    @State private var screen: Screen = .capture

    var body: some View {
        content
            .alert("Error", isPresented: .constant(model.lastError != nil)) {
                Button("OK") { model.lastError = nil }
            } message: {
                Text(model.lastError ?? "")
            }
    }

    @ViewBuilder private var content: some View {
        switch screen {
        case .capture:
            CaptureView(
                model: model,
                onOpenReview: { screen = .review },
                onOpenSettings: { screen = .settings },
                onOpenHelp: { screen = .help }
            )
        case .review:
            BatchReviewView(model: model) { screen = .capture }
        case .settings:
            SettingsView(model: model) { screen = .capture }
        case .help:
            HelpView(spanish: false) { screen = .capture }
        }
    }
}
