package io.github.revise0592.mcpanel

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import io.github.revise0592.mcpanel.ui.HomeScreen
import io.github.revise0592.mcpanel.ui.theme.McPanelTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            McPanelTheme {
                HomeScreen()
            }
        }
    }
}
