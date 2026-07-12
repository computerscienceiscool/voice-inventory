package com.thesalleys.voiceinventory

import android.Manifest
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import com.thesalleys.voiceinventory.ui.BatchReviewScreen
import com.thesalleys.voiceinventory.ui.CaptureScreen

class MainActivity : ComponentActivity() {
    private val vm: AppViewModel by viewModels()

    private val micPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
            if (granted) vm.arm()
            // denied → manual entry stays available in batch review (§13)
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        micPermission.launch(Manifest.permission.RECORD_AUDIO)
        setContent {
            MaterialTheme {
                Surface {
                    var screen by remember { mutableStateOf(Screen.Capture) }
                    val error by vm.errors.collectAsState()
                    LaunchedEffect(error) {
                        error?.let {
                            Toast.makeText(this@MainActivity, it, Toast.LENGTH_LONG).show()
                            vm.errors.value = null
                        }
                    }
                    when (screen) {
                        Screen.Capture -> CaptureScreen(
                            vm,
                            onOpenReview = { screen = Screen.Review },
                            onOpenSettings = { screen = Screen.Settings },
                            onOpenHelp = { screen = Screen.Help },
                        )
                        Screen.Review -> BatchReviewScreen(vm) { screen = Screen.Capture }
                        Screen.Settings -> SettingsScreen(vm) { screen = Screen.Capture }
                        Screen.Help -> HelpScreen(spanish = false) { screen = Screen.Capture }
                    }
                }
            }
        }
    }
}

private enum class Screen { Capture, Review, Settings, Help }
