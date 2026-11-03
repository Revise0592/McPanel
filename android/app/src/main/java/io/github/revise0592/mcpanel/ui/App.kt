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
import io.github.revise0592.mcpanel.data.LogoCache
import io.github.revise0592.mcpanel.data.Panel
import io.github.revise0592.mcpanel.data.PairingLink
import io.github.revise0592.mcpanel.data.PanelStore
import io.github.revise0592.mcpanel.ui.theme.McPanelTheme

sealed interface Screen {
    data object Panels : Screen
    data class AddPanel(val address: String = "", val code: String = "") : Screen
    data class Servers(val panelId: String) : Screen
    data class Server(val panelId: String, val serverId: String) : Screen
}

private val ScreenSaver = listSaver<Screen, String>(
    save = {
        when (it) {
            Screen.Panels -> listOf("panels")
            is Screen.AddPanel -> listOf("add", it.address, it.code)
            is Screen.Servers -> listOf("servers", it.panelId)
            is Screen.Server -> listOf("server", it.panelId, it.serverId)
        }
    },
    restore = {
        when (it[0]) {
            "add" -> Screen.AddPanel(it[1], it[2])
            "servers" -> Screen.Servers(it[1])
            "server" -> Screen.Server(it[1], it[2])
            else -> Screen.Panels
        }
    },
)

/**
 * The app's screens. [link] is a pairing link the app was opened with; it
 * opens Add panel with the address and code filled in.
 *
 * A panel's own screens take on its accent colour and light or dark
 * setting; the panel list and Add panel use the phone's colours.
 */
@Composable
fun App(store: PanelStore, logos: LogoCache, link: PairingLink?, onLinkHandled: () -> Unit) {
    var screen by rememberSaveable(stateSaver = ScreenSaver) { mutableStateOf<Screen>(Screen.Panels) }
    var panels by remember { mutableStateOf(store.panels()) }

    LaunchedEffect(link) {
        if (link != null) {
            screen = Screen.AddPanel(link.address, link.code)
            onLinkHandled()
        }
    }
    BackHandler(enabled = screen != Screen.Panels) {
        screen = when (val s = screen) {
            is Screen.Server -> Screen.Servers(s.panelId)
            else -> Screen.Panels
        }
    }

    fun panelOrHome(id: String): Panel? = panels.find { it.id == id }

    when (val s = screen) {
        Screen.Panels -> McPanelTheme {
            PanelsScreen(
                panels = panels,
                store = store,
                logos = logos,
                onAdd = { screen = Screen.AddPanel() },
                onOpen = { screen = Screen.Servers(it.id) },
            )
        }
        is Screen.AddPanel -> McPanelTheme {
            AddPanelScreen(
                initialAddress = s.address,
                initialCode = s.code,
                store = store,
                onBack = { screen = Screen.Panels },
                onPaired = {
                    panels = store.panels()
                    screen = Screen.Servers(it.id)
                },
            )
        }
        is Screen.Servers -> {
            val panel = panelOrHome(s.panelId)
            if (panel == null) {
                LaunchedEffect(s.panelId) { screen = Screen.Panels }
            } else {
                Themed(panel) {
                    ServersScreen(
                        panel = panel,
                        store = store,
                        logos = logos,
                        onBack = { screen = Screen.Panels },
                        onOpen = { screen = Screen.Server(panel.id, it.id) },
                        onPanelChanged = { panels = store.panels() },
                        onRemoved = {
                            panels = store.panels()
                            screen = Screen.Panels
                        },
                    )
                }
            }
        }
        is Screen.Server -> {
            val panel = panelOrHome(s.panelId)
            if (panel == null) {
                LaunchedEffect(s.panelId) { screen = Screen.Panels }
            } else {
                Themed(panel) {
                    ServerScreen(
                        panel = panel,
                        serverId = s.serverId,
                        store = store,
                        onBack = { screen = Screen.Servers(panel.id) },
                    )
                }
            }
        }
    }
}

@Composable
private fun Themed(panel: Panel, content: @Composable () -> Unit) =
    McPanelTheme(accent = panel.accent, accentText = panel.accentText, theme = panel.theme, content = content)
