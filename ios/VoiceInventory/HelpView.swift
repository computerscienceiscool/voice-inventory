import SwiftUI

/// The on-device "how to speak to it" card (§5, item 035). Mirrors the
/// Android HelpScreen and docs/parsing.md; keep the three in sync.
struct HelpView: View {
    var spanish: Bool
    var onBack: () -> Void

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                Button(spanish ? "← Volver" : "← Back", action: onBack)
                Text(spanish ? "Cómo hablarle" : "How to speak to it")
                    .font(.largeTitle.weight(.black))
                ForEach(sections, id: \.0) { title, body in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(title).font(.headline)
                        Text(body)
                    }
                    .padding()
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 10))
                }
            }
            .padding()
        }
    }

    private var sections: [(String, String)] {
        spanish ? [
            ("Dilo así",
             "\"Doce cajas de conectores RJ45 en el bin A-14\"\nCantidad + unidad + artículo + ubicación. Lo demás queda como nota."),
            ("Corrige sobre la marcha", "\"…en el bin A-40, digo, A-14\" — vale el último valor."),
            ("Durante la confirmación",
             "\"Sí\" guarda. \"La ubicación es A-40\" corrige un campo. \"Borra eso\" lo descarta."),
            ("Los aproximados sirven",
             "\"Unos cuarenta\", \"una docena\" — se guardan como aproximados y marcados para revisión."),
        ] : [
            ("Say it like this",
             "\"Twelve boxes of RJ45 connectors in bin A-14\"\nQuantity + unit + item + location. Anything extra becomes a note."),
            ("Fix a slip mid-sentence", "\"…in bin A-40, no, A-14\" — the last value wins."),
            ("While it reads back",
             "\"Yes\" saves. \"Location is A-40\" fixes one field. \"Scratch that\" throws it away."),
            ("Rough counts are fine",
             "\"About forty\", \"a couple hundred\", \"a dozen\" — saved as approximate and flagged."),
        ]
    }
}
