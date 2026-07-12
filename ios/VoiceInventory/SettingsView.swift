import SwiftUI

/// Device profile editor (§14) + operator login (§3) over the Go core's
/// ConfigJSON/SetConfigJSON. Language/mode changes apply on next launch.
struct SettingsView: View {
    @ObservedObject var model: AppModel
    var onBack: () -> Void

    @State private var operatorID = ""
    @State private var language = "en"
    @State private var endpoint = ""
    @State private var token = ""
    @State private var note = ""

    var body: some View {
        Form {
            Section {
                Button("← Back", action: onBack)
            }
            Section("Operator") {
                TextField("Operator ID", text: $operatorID)
                    .autocorrectionDisabled()
            }
            Section("Language (applies on restart)") {
                Picker("Language", selection: $language) {
                    Text("English").tag("en")
                    Text("Español").tag("es")
                    Text("Auto").tag("auto")
                }.pickerStyle(.segmented)
            }
            Section("Sync") {
                TextField("Endpoint (https://…)", text: $endpoint)
                    .autocorrectionDisabled().textInputAutocapitalization(.never)
                SecureField("Token", text: $token)
            }
            Section {
                Button("Save") { save() }
                if !note.isEmpty { Text(note).font(.caption) }
            }
        }
        .onAppear(perform: load)
    }

    private func load() {
        guard let data = model.configJSON().data(using: .utf8),
              let c = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return }
        operatorID = c["operator_id"] as? String ?? ""
        language = c["language"] as? String ?? "en"
        let sync = c["sync"] as? [String: Any]
        endpoint = sync?["endpoint"] as? String ?? ""
        token = sync?["token"] as? String ?? ""
    }

    private func save() {
        let cfg: [String: Any] = [
            "operator_id": operatorID,
            "language": language,
            "sync": ["endpoint": endpoint, "token": token],
        ]
        guard let data = try? JSONSerialization.data(withJSONObject: cfg),
              let json = String(data: data, encoding: .utf8)
        else { return }
        do {
            try model.saveConfig(json)
            note = "Saved. Language/mode changes apply on restart."
        } catch {
            note = "Error: \(error.localizedDescription)"
        }
    }
}
