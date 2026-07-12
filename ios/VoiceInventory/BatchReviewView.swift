import SwiftUI

struct Rec: Identifiable {
    let id: String
    let summary: String
    let status: String
    let needsReview: Bool
    let syncRejected: String
}

/// One-field editor for a queued record (§4.2 batch edit), mirroring the
/// Android RecordEditDialog.
struct RecordEditView: View {
    let rec: Rec
    var onSave: (String, String) -> Void
    var onCancel: () -> Void

    @State private var field = "location"
    @State private var value = ""

    private let fields = ["location", "quantity", "item", "unit", "description"]

    var body: some View {
        NavigationView {
            Form {
                Text(rec.summary).font(.caption)
                Picker("Field", selection: $field) {
                    ForEach(fields, id: \.self) { Text($0).tag($0) }
                }
                TextField("New \(field)", text: $value)
                    .autocorrectionDisabled()
            }
            .navigationTitle("Edit record")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel", action: onCancel)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { onSave(field, value) }
                }
            }
        }
    }
}

/// Batch review (§4.2): queue with needs-review + backend-rejected badges
/// (item 087), per-record confirm/reject, sync, and CSV share (item 072).
struct BatchReviewView: View {
    @ObservedObject var model: AppModel
    var onBack: () -> Void

    @State private var records: [Rec] = []
    @State private var shareURL: URL?
    @State private var note = ""
    @State private var editing: Rec?

    var body: some View {
        VStack(alignment: .leading) {
            HStack {
                Button("← Capture", action: onBack)
                Spacer()
                Button("Export") {
                    Task { shareURL = await model.exportCSV() }
                }
                Button("Sync") {
                    Task { note = await model.sync(); await reload() }
                }
            }
            if !note.isEmpty { Text(note).font(.caption) }
            List(records) { rec in
                VStack(alignment: .leading, spacing: 4) {
                    Text(rec.summary).bold()
                    HStack {
                        badge(rec.status)
                        if rec.needsReview { badge("review") }
                        if !rec.syncRejected.isEmpty { badge("backend: \(rec.syncRejected)") }
                    }
                    if rec.status == "draft" || rec.status == "confirmed" {
                        HStack {
                            if rec.status == "draft" {
                                Button("Confirm") {
                                    model.confirmRecord(rec.id)
                                    Task { await reload() }
                                }
                            }
                            Button("Reject") {
                                model.rejectRecord(rec.id)
                                Task { await reload() }
                            }
                            Button("Edit") { editing = rec }
                        }
                        .buttonStyle(.bordered)
                    }
                }
            }
        }
        .padding()
        .task { await reload() }
        .sheet(item: $shareURL) { url in ShareSheet(items: [url]) }
        .sheet(item: $editing) { rec in
            RecordEditView(rec: rec) { field, value in
                model.editRecord(rec.id, field: field, value: value)
                editing = nil
                Task { await reload() }
            } onCancel: { editing = nil }
        }
    }

    private func badge(_ text: String) -> some View {
        Text(text)
            .font(.caption2)
            .padding(.horizontal, 6).padding(.vertical, 2)
            .background(.thinMaterial, in: Capsule())
    }

    private func reload() async {
        let json = await model.listRecords(status: "")
        guard let data = json.data(using: .utf8),
              let root = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let arr = root["observations"] as? [[String: Any]]
        else { records = []; return }
        records = arr.map { o in
            let p = o["parsed"] as? [String: Any] ?? [:]
            let qty = p["quantity"].flatMap { "\($0)" } ?? "—"
            return Rec(
                id: o["id"] as? String ?? "",
                summary: "\(qty) \(p["unit"] as? String ?? "") \(p["item_text"] as? String ?? "") @ \(p["location_text"] as? String ?? "—")",
                status: o["status"] as? String ?? "",
                needsReview: o["needs_review"] as? Bool ?? false,
                syncRejected: o["sync_rejected_reason"] as? String ?? "",
            )
        }
    }
}

extension URL: Identifiable { public var id: String { absoluteString } }

/// UIActivityViewController wrapper for the share sheet.
struct ShareSheet: UIViewControllerRepresentable {
    let items: [Any]
    func makeUIViewController(context: Context) -> UIActivityViewController {
        UIActivityViewController(activityItems: items, applicationActivities: nil)
    }
    func updateUIViewController(_ vc: UIActivityViewController, context: Context) {}
}
