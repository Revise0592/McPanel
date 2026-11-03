package io.github.revise0592.mcpanel.ui

import androidx.activity.compose.BackHandler
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import io.github.revise0592.mcpanel.data.PairingLink
import io.github.revise0592.mcpanel.data.PanelStore

sealed interface Screen {
    data object Panels : Screen
    data class AddPanel(val address: String = "", val code: String = "") : Screen
    data class Servers(val panelId: String) : Screen
}

private val ScreenSaver = listSaver<Screen, String>(
    save = {
        when (it) {
            Screen.Panels -> listOf("panels")
            is Screen.AddPanel -> listOf("add", it.address, it.code)
            is Screen.Servers -> listOf("servers", it.panelId)
        }
    },
    restore = {
        when (it[0]) {
            "add" -> Screen.AddPanel(it[1], it[2])
            "servers" -> Screen.Servers(it[1])
            else -> Screen.Panels
        }
    },
)

/**
 * The app's screens. [link] is a pairing link the app was opened with; it
 * opens Add panel with the address and code filled in.
 */
@Composable
fun App(store: PanelStore, link: PairingLink?, onLinkHandled: () -> Unit) {
    var screen by rememberSaveable(stateSaver = ScreenSaver) { mutableStateOf<Screen>(Screen.Panels) }
    var panels by remember { mutableStateOf(store.panels()) }

    LaunchedEffect(link) {
        if (link != null) {
            screen = Screen.AddPanel(link.address, link.code)
            onLinkHandled()
        }
    }
    BackHandler(enabled = screen != Screen.Panels) { screen = Screen.Panels }

    when (val s = screen) {
        Screen.Panels -> PanelsScreen(
            panels = panels,
            onAdd = { screen = Screen.AddPanel() },
            onOpen = { screen = Screen.Servers(it.id) },
        )
        is Screen.AddPanel -> AddPanelScreen(
            initialAddress = s.address,
            initialCode = s.code,
            store = store,
            onBack = { screen = Screen.Panels },
            onPaired = {
                panels = store.panels()
                screen = Screen.Servers(it.id)
            },
        )
        is Screen.Servers -> {
            val panel = panels.find { it.id == s.panelId }
            if (panel == null) {
                LaunchedEffect(s.panelId) { screen = Screen.Panels }
            } else {
                ServersScreen(
                    panel = panel,
                    store = store,
                    onBack = { screen = Screen.Panels },
                    onRemoved = {
                        panels = store.panels()
                        screen = Screen.Panels
                    },
                )
            }
        }
    }
}
