package com.thesalleys.voiceinventory.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.waitForUpOrCancellation
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.thesalleys.voiceinventory.AppViewModel
import com.thesalleys.voiceinventory.ReadbackField
import kotlin.math.min

/** Text-entry fallback for one readback field ("tap to edit", §4.1). */
@Composable
fun FieldEditDialog(field: ReadbackField, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    var value by remember { mutableStateOf(if (field.value == "—") "" else field.value) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Edit ${field.label}") },
        text = {
            OutlinedTextField(
                value = value,
                onValueChange = { value = it },
                singleLine = true,
            )
        },
        confirmButton = { Button(onClick = { onSave(value) }) { Text("Save") } },
        dismissButton = { OutlinedButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/**
 * The one-handed capture screen (§4.3): state banner, level meter, readback
 * card with doubtful-field highlighting, and a glove-sized hold-to-talk
 * button. Everything reachable with a thumb.
 */
@Composable
fun CaptureScreen(
    vm: AppViewModel,
    onOpenReview: () -> Unit,
    onOpenSettings: () -> Unit,
    onOpenHelp: () -> Unit,
) {
    val state by vm.state.collectAsState()
    val level by vm.level.collectAsState()
    val readback by vm.readback.collectAsState()
    val modelReady by vm.modelReady.collectAsState()

    Column(
        modifier = Modifier.fillMaxSize().padding(20.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = when {
                    !modelReady -> "MODEL NOT READY"
                    state == "reviewing" -> "CONFIRM?"
                    state == "armed" -> "LISTENING"
                    else -> "IDLE"
                },
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.Black,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = onOpenHelp) { Text("?") }
                OutlinedButton(onClick = onOpenSettings) { Text("⚙") }
                OutlinedButton(onClick = onOpenReview) { Text("Review") }
            }
        }

        Spacer(Modifier.height(12.dp))
        LinearProgressIndicator(
            progress = { min(1f, level * 6f) }, // mic level meter (§4.1 step 2)
            modifier = Modifier.fillMaxWidth().height(10.dp),
        )

        Spacer(Modifier.height(16.dp))
        var editing by remember { mutableStateOf<ReadbackField?>(null) }
        editing?.let { f ->
            FieldEditDialog(
                field = f,
                onDismiss = { editing = null },
                onSave = { value ->
                    vm.correctField(f.label.lowercase(), value) // §4.1 tap-to-edit
                    editing = null
                },
            )
        }
        readback?.let { rb ->
            Card(
                modifier = Modifier.fillMaxWidth(),
                colors = CardDefaults.cardColors(),
            ) {
                Column(Modifier.padding(16.dp)) {
                    rb.fields.forEach { f ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(vertical = 6.dp)
                                .background(
                                    if (f.doubtful) MaterialTheme.colorScheme.errorContainer
                                    else MaterialTheme.colorScheme.surface,
                                    RoundedCornerShape(6.dp),
                                )
                                .clickable { editing = f } // tap a field to edit (§4.1 step 6)
                                .padding(6.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                        ) {
                            Text(f.label, fontWeight = FontWeight.Bold, fontSize = 20.sp)
                            Text(f.value, fontSize = 20.sp)
                        }
                    }
                    Spacer(Modifier.height(12.dp))
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Button(
                            onClick = vm::confirm,
                            modifier = Modifier.weight(1f).height(64.dp),
                        ) { Text("✓ Save", fontSize = 22.sp) }
                        OutlinedButton(
                            onClick = vm::scratch,
                            modifier = Modifier.weight(1f).height(64.dp),
                        ) { Text("Scratch", fontSize = 22.sp) }
                    }
                }
            }
        }

        Spacer(Modifier.weight(1f))

        // Hold-to-talk: press begins the utterance, release ends it (§4.2).
        Box(
            modifier = Modifier
                .size(200.dp)
                .background(
                    if (state == "armed" || state == "reviewing")
                        MaterialTheme.colorScheme.primary
                    else MaterialTheme.colorScheme.surfaceVariant,
                    CircleShape,
                )
                .pointerInput(modelReady) {
                    if (!modelReady) return@pointerInput
                    awaitEachGesture {
                        awaitFirstDown()
                        vm.pttDown()
                        waitForUpOrCancellation()
                        vm.pttUp()
                    }
                },
            contentAlignment = Alignment.Center,
        ) {
            Text(
                "HOLD\nTO TALK",
                color = MaterialTheme.colorScheme.onPrimary,
                fontSize = 26.sp,
                fontWeight = FontWeight.Black,
            )
        }
        Spacer(Modifier.height(24.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Button(onClick = vm::arm) { Text("Arm") }
            OutlinedButton(onClick = vm::disarm) { Text("Stop") }
        }
    }
}
