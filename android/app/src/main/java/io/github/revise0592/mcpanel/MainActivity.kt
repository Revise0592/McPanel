package io.github.revise0592.mcpanel

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import io.github.revise0592.mcpanel.data.LogoCache
import io.github.revise0592.mcpanel.data.Pairing
import io.github.revise0592.mcpanel.data.PairingLink
import io.github.revise0592.mcpanel.data.PanelStore
import io.github.revise0592.mcpanel.ui.App

class MainActivity : ComponentActivity() {
    /** A mcpanel://pair link the app was opened with, until it's been shown. */
    private var link by mutableStateOf<PairingLink?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        if (savedInstanceState == null) link = linkFrom(intent)
        val store = PanelStore(this)
        val logos = LogoCache(this)
        setContent {
            App(store = store, logos = logos, link = link, onLinkHandled = { link = null })
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        linkFrom(intent)?.let { link = it }
    }

    private fun linkFrom(intent: Intent?): PairingLink? =
        intent?.takeIf { it.action == Intent.ACTION_VIEW }?.dataString?.let(Pairing::parseLink)
}
