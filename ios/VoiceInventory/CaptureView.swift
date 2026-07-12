import SwiftUI

/// One-handed capture screen (§4.3): state banner, level meter, readback
/// with doubtful-field highlighting, hold-to-talk. Mirror of the Android
/// CaptureScreen.
struct CaptureView: View {
    @ObservedObject var model: AppModel
    var onOpenReview: () -> Void
    var onOpenSettings: () -> Void
    var onOpenHelp: () -> Void

    var body: some View {
        VStack(spacing: 16) {
            HStack {
                Text(banner)
                    .font(.title2.weight(.black))
                Spacer()
                Button("?", action: onOpenHelp)
                Button("⚙", action: onOpenSettings)
                Button("Review", action: onOpenReview)
            }

            ProgressView(value: min(1.0, Double(model.level) * 6)) // level meter (§4.1)
                .progressViewStyle(.linear)

            if !model.fields.isEmpty {
                VStack(spacing: 8) {
                    ForEach(model.fields) { f in
                        HStack {
                            Text(f.label).bold()
                            Spacer()
                            Text(f.value)
                        }
                        .padding(8)
                        .background(f.doubtful ? Color.red.opacity(0.2) : Color.clear)
                        .clipShape(RoundedRectangle(cornerRadius: 6))
                        .font(.title3)
                    }
                    HStack(spacing: 12) {
                        Button(action: model.confirm) {
                            Text("✓ Save").font(.title3.bold())
                                .frame(maxWidth: .infinity, minHeight: 56)
                        }
                        .buttonStyle(.borderedProminent)
                        Button(action: model.scratch) {
                            Text("Scratch").font(.title3)
                                .frame(maxWidth: .infinity, minHeight: 56)
                        }
                        .buttonStyle(.bordered)
                    }
                }
                .padding()
                .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 12))
            }

            Spacer()

            // Hold-to-talk (§4.2): press begins, release ends the utterance.
            Circle()
                .fill(model.state == "idle" ? Color.gray : Color.accentColor)
                .frame(width: 200, height: 200)
                .overlay(
                    Text("HOLD\nTO TALK")
                        .multilineTextAlignment(.center)
                        .font(.title2.weight(.black))
                        .foregroundStyle(.white)
                )
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { _ in if !pressing { pressing = true; model.pttDown() } }
                        .onEnded { _ in pressing = false; model.pttUp() }
                )

            HStack(spacing: 12) {
                Button("Arm", action: model.arm).buttonStyle(.borderedProminent)
                Button("Stop", action: model.disarm).buttonStyle(.bordered)
            }
        }
        .padding(20)
    }

    @State private var pressing = false

    private var banner: String {
        if !model.modelReady { return "MODEL NOT READY" }
        switch model.state {
        case "reviewing": return "CONFIRM?"
        case "armed": return "LISTENING"
        default: return "IDLE"
        }
    }
}
