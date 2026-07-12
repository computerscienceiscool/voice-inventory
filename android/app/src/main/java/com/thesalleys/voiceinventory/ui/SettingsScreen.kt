package com.thesalleys.voiceinventory.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
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

/**
 * Device profile editor (§14) over ConfigJSON/SetConfigJSON, plus the
 * operator login field (§3; real authentication is TODO 063). Language and
 * capture-mode changes apply on next app start.
 */
@Composable
fun SettingsScreen(vm: AppViewModel, onBack: () -> Unit) {
    var operator by remember { mutableStateOf("") }
    var endpoint by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    var language by remember { mutableStateOf("en") }
    var note by remember { mutableStateOf("") }
    var loaded by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) {
        vm.loadConfig { json ->
            val c = JSONObject(json)
            operator = c.optString("operator_id")
            language = c.optString("language", "en")
            val sync = c.optJSONObject("sync")
            endpoint = sync?.optString("endpoint") ?: ""
            token = sync?.optString("token") ?: ""
            loaded = true
        }
    }

    Column(
        Modifier.fillMaxSize().padding(20.dp).verticalScroll(rememberScrollState()),
    ) {
        OutlinedButton(onClick = onBack) { Text("← Back") }
        Spacer(Modifier.height(12.dp))
        Text("Settings", style = MaterialTheme.typography.headlineMedium,
            fontWeight = FontWeight.Black)
        Spacer(Modifier.height(16.dp))

        OutlinedTextField(
            value = operator, onValueChange = { operator = it },
            label = { Text("Operator ID") }, singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(8.dp))
        Text("Language (applies on restart)")
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            listOf("en", "es", "auto").forEach { code ->
                FilterChip(
                    selected = language == code,
                    onClick = { language = code },
                    label = { Text(code) },
                )
            }
        }
        Spacer(Modifier.height(8.dp))
        OutlinedTextField(
            value = endpoint, onValueChange = { endpoint = it },
            label = { Text("Sync endpoint (https://…)") }, singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(8.dp))
        OutlinedTextField(
            value = token, onValueChange = { token = it },
            label = { Text("Sync token") }, singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(16.dp))
        Button(
            enabled = loaded,
            onClick = {
                val json = JSONObject()
                    .put("operator_id", operator)
                    .put("language", language)
                    .put("sync", JSONObject().put("endpoint", endpoint).put("token", token))
                    .toString()
                vm.saveConfig(json) { note = "Saved. Language/mode changes apply on restart." }
            },
            modifier = Modifier.fillMaxWidth().height(56.dp),
        ) { Text("Save") }
        if (note.isNotEmpty()) {
            Spacer(Modifier.height(8.dp))
            Text(note, style = MaterialTheme.typography.bodySmall)
        }
    }
}
