import SwiftUI

private struct Rec: Identifiable {
    let id: String
    let summary: String
    let status: String
    let needsReview: Bool
    let syncRejected: String
}

/// Batch review (§4.2): queue with needs-review + backend-rejected badges
/// (item 087), per-record confirm/reject, sync, and CSV share (item 072).
struct BatchReviewView: View {
    @ObservedObject var model: AppModel
    var onBack: () -> Void

    @State private var records: [Rec] = []
    @State private var shareURL: URL?
    @State private var note = ""

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
                        }
                        .buttonStyle(.bordered)
                    }
                }
            }
        }
        .padding()
        .task { await reload() }
        .sheet(item: $shareURL) { url in ShareSheet(items: [url]) }
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
