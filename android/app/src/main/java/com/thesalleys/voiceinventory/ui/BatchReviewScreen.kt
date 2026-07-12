package com.thesalleys.voiceinventory.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.thesalleys.voiceinventory.AppViewModel
import org.json.JSONObject

private data class Rec(
    val id: String,
    val summary: String,
    val status: String,
    val needsReview: Boolean,
    val syncRejected: String,
)

/**
 * Batch review (§4.2): the queue with needs-review and backend-rejected
 * badges (TODO 087), per-record confirm/reject, and a sync trigger.
 */
@Composable
fun BatchReviewScreen(vm: AppViewModel, onBack: () -> Unit) {
    var records by remember { mutableStateOf(listOf<Rec>()) }
    var syncNote by remember { mutableStateOf("") }
    var reloadKey by remember { mutableStateOf(0) }

    LaunchedEffect(reloadKey) {
        vm.listRecords("") { json ->
            val arr = JSONObject(json).getJSONArray("observations")
            records = (0 until arr.length()).map { i ->
                val o = arr.getJSONObject(i)
                val p = o.getJSONObject("parsed")
                val qty = p.opt("quantity")?.takeIf { it != JSONObject.NULL } ?: "—"
                Rec(
                    id = o.getString("id"),
                    summary = "$qty ${p.optString("unit", "")} ${p.optString("item_text")} @ ${p.optString("location_text", "—")}",
                    status = o.getString("status"),
                    needsReview = o.optBoolean("needs_review"),
                    syncRejected = o.optString("sync_rejected_reason", ""),
                )
            }
        }
    }

    Column(Modifier.fillMaxSize().padding(16.dp)) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            OutlinedButton(onClick = onBack) { Text("← Capture") }
            Button(onClick = { vm.sync { syncNote = it; reloadKey++ } }) { Text("Sync") }
        }
        if (syncNote.isNotEmpty()) {
            Text(syncNote, style = MaterialTheme.typography.bodySmall)
        }
        Spacer(Modifier.height(8.dp))
        LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(records, key = { it.id }) { rec ->
                Card(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(12.dp)) {
                        Text(rec.summary, fontWeight = FontWeight.Bold)
                        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                            AssistChip(onClick = {}, label = { Text(rec.status) })
                            if (rec.needsReview) {
                                AssistChip(onClick = {}, label = { Text("review") })
                            }
                            if (rec.syncRejected.isNotEmpty()) {
                                AssistChip(onClick = {}, label = { Text("backend: ${rec.syncRejected}") })
                            }
                        }
                        if (rec.status == "draft" || rec.status == "confirmed") {
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                if (rec.status == "draft") {
                                    Button(onClick = {
                                        vm.confirmRecord(rec.id); reloadKey++
                                    }) { Text("Confirm") }
                                }
                                OutlinedButton(onClick = {
                                    vm.rejectRecord(rec.id); reloadKey++
                                }) { Text("Reject") }
                            }
                        }
                    }
                }
            }
        }
    }
}
