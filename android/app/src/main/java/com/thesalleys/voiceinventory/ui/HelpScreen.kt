package com.thesalleys.voiceinventory.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp

/**
 * The on-device "how to speak to it" card (§5, item 035). Content mirrors
 * docs/parsing.md; keep the two in sync.
 */
@Composable
fun HelpScreen(spanish: Boolean, onBack: () -> Unit) {
    Column(
        Modifier.fillMaxSize().padding(20.dp).verticalScroll(rememberScrollState()),
    ) {
        OutlinedButton(onClick = onBack) { Text(if (spanish) "← Volver" else "← Back") }
        Spacer(Modifier.height(12.dp))
        Text(
            if (spanish) "Cómo hablarle" else "How to speak to it",
            style = MaterialTheme.typography.headlineMedium,
            fontWeight = FontWeight.Black,
        )
        Spacer(Modifier.height(12.dp))
        val sections = if (spanish) esHelp else enHelp
        sections.forEach { (title, body) ->
            Card(Modifier.padding(vertical = 6.dp)) {
                Column(Modifier.padding(14.dp)) {
                    Text(title, fontWeight = FontWeight.Bold,
                        style = MaterialTheme.typography.titleMedium)
                    Spacer(Modifier.height(6.dp))
                    Text(body, style = MaterialTheme.typography.bodyLarge)
                }
            }
        }
    }
}

private val enHelp = listOf(
    "Say it like this" to
        "\"Twelve boxes of RJ45 connectors in bin A-14\"\n" +
        "\"Bin C-7, forty spools of Cat6, three have water damage\"\n" +
        "Quantity + unit + item + location. Anything extra becomes a note.",
    "Fix a slip mid-sentence" to
        "\"…in bin A-40, no, A-14\" — the last value wins.",
    "While it reads back" to
        "\"Yes\" or \"correct\" saves.\n" +
        "\"Location is A-40\" or just \"no, A-14\" fixes one field.\n" +
        "\"Scratch that\" throws it away.\n" +
        "Or say the whole thing again to replace it.",
    "After saving" to
        "\"Scratch that\" also undoes the record you just saved.",
    "Rough counts are fine" to
        "\"About forty\", \"a couple hundred\", \"a dozen\" — saved as " +
        "approximate and flagged for review. \"Several\" saves with no " +
        "number at all.",
)

private val esHelp = listOf(
    "Dilo así" to
        "\"Doce cajas de conectores RJ45 en el bin A-14\"\n" +
        "\"Bin C-7, cuarenta carretes de Cat6, tres tienen daño\"\n" +
        "Cantidad + unidad + artículo + ubicación. Lo demás queda como nota.",
    "Corrige sobre la marcha" to
        "\"…en el bin A-40, digo, A-14\" — vale el último valor.",
    "Durante la confirmación" to
        "\"Sí\" o \"correcto\" guarda.\n" +
        "\"La ubicación es A-40\" o solo \"no, A-14\" corrige un campo.\n" +
        "\"Borra eso\" lo descarta.\n" +
        "O repite todo para reemplazarlo.",
    "Después de guardar" to
        "\"Borra eso\" también deshace el último registro guardado.",
    "Los aproximados sirven" to
        "\"Unos cuarenta\", \"una docena\", \"un par\" — se guardan como " +
        "aproximados y quedan marcados para revisión.",
)
