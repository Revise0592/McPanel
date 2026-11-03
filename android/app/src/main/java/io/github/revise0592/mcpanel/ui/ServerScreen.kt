@file:OptIn(ExperimentalMaterial3Api::class)

package io.github.revise0592.mcpanel.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import io.github.revise0592.mcpanel.data.ApiException
import io.github.revise0592.mcpanel.data.Panel
import io.github.revise0592.mcpanel.data.PanelApi
import io.github.revise0592.mcpanel.data.PanelStore
import io.github.revise0592.mcpanel.data.Server
import io.github.revise0592.mcpanel.data.durationText
import io.github.revise0592.mcpanel.data.usageLine
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.Duration
import java.time.Instant

private enum class ServerTab(val title: String) { Console("Console"), Players("Players"), Activity("Activity") }

/** One server: its state, power buttons, and console, players and activity tabs. */
@Composable
fun ServerScreen(panel: Panel, serverId: String, store: PanelStore, onBack: () -> Unit) {
    var server by remember(serverId) { mutableStateOf<Server?>(null) }
    var error by remember(serverId) { mutableStateOf<String?>(null) }
    var unauthorized by remember(serverId) { mutableStateOf(false) }
    var busy by remember { mutableStateOf<String?>(null) }
    var actionError by remember { mutableStateOf<String?>(null) }
    var confirm by remember { mutableStateOf<String?>(null) }
    var menuOpen by remember { mutableStateOf(false) }
    var tab by rememberSaveable { mutableStateOf(ServerTab.Console) }
    val scope = rememberCoroutineScope()
    val lifecycle = LocalLifecycleOwner.current.lifecycle

    // Refresh often while the state is changing, as the web UI does.
    LaunchedEffect(serverId) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (!unauthorized) {
                val conn = store.conn(panel)
                if (conn == null) {
                    error = LOST_TOKEN
                    unauthorized = true
                    break
                }
                try {
                    server = PanelApi.server(conn, serverId)
                    error = null
                } catch (e: ApiException) {
                    error = e.message
                    if (e.unauthorized) unauthorized = true
                }
                val s = server
                delay(if (s != null && (s.state == "starting" || s.state == "stopping" || s.task.isNotEmpty())) 2_000L else 5_000L)
            }
        }
    }

    fun power(action: String) {
        val conn = store.conn(panel) ?: return
        busy = action
        actionError = null
        scope.launch {
            try {
                server = PanelApi.power(conn, serverId, action)
            } catch (e: ApiException) {
                actionError = e.message
            } finally {
                busy = null
            }
        }
    }

    /** Stopping with people on asks first; so does killing, always. */
    fun ask(action: String) {
        val s = server ?: return
        if (action == "kill" || s.players.isNotEmpty()) confirm = action else power(action)
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(server?.name ?: "Server", maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(
                            panel.name,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                        )
                    }
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
                actions = {
                    val s = server
                    if (s != null && s.state in setOf("running", "starting", "stopping")) {
                        Box {
                            IconButton(onClick = { menuOpen = true }) {
                                Icon(Icons.Default.MoreVert, contentDescription = "More")
                            }
                            DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                                DropdownMenuItem(
                                    text = { Text("Kill") },
                                    onClick = {
                                        menuOpen = false
                                        ask("kill")
                                    },
                                )
                            }
                        }
                    }
                },
            )
        },
    ) { padding ->
        Column(
            Modifier
                .fillMaxSize()
                .padding(padding)
                .imePadding(),
        ) {
            val s = server
            Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                error?.let { ErrorCard(it) }
                when {
                    s == null && error == null -> Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator()
                    }
                    s != null -> {
                        ServerHeader(s)
                        PowerButtons(s, busy, onPower = ::ask)
                        actionError?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium) }
                    }
                }
            }
            if (s != null) {
                val tabs = if (s.isProxy) listOf(ServerTab.Console, ServerTab.Activity) else ServerTab.entries
                val current = if (tab in tabs) tab else ServerTab.Console
                PrimaryTabRow(selectedTabIndex = tabs.indexOf(current), modifier = Modifier.padding(top = 8.dp)) {
                    tabs.forEach { t ->
                        Tab(selected = t == current, onClick = { tab = t }, text = { Text(t.title) })
                    }
                }
                Box(Modifier.weight(1f).fillMaxWidth()) {
                    when (current) {
                        ServerTab.Console -> ConsoleTab(panel, s, store)
                        ServerTab.Players -> PlayersTab(panel, s, store)
                        ServerTab.Activity -> ActivityTab(panel, s, store)
                    }
                }
            }
        }
    }

    confirm?.let { action ->
        val s = server
        val who = s?.players?.size ?: 0
        val people = if (who == 1) "1 player is" else "$who players are"
        ConfirmDialog(
            title = when (action) {
                "kill" -> "Kill ${s?.name}?"
                "restart" -> "Restart ${s?.name}?"
                else -> "Stop ${s?.name}?"
            },
            text = when (action) {
                "kill" -> "The server is ended at once, without saving. Use it only when it won't stop on its own; recent changes to the world can be lost."
                else -> "$people online and will be disconnected."
            },
            confirm = action.replaceFirstChar { it.uppercase() },
            onConfirm = { power(action) },
            onDismiss = { confirm = null },
        )
    }
}

@Composable
private fun ServerHeader(s: Server) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    "${s.typeLabel} ${s.version}",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.weight(1f),
                )
                StateBadge(s)
            }
            if (s.task.isNotEmpty()) {
                if (s.progress in 0..100) {
                    LinearProgressIndicator(progress = { s.progress / 100f }, modifier = Modifier.fillMaxWidth())
                } else {
                    LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
                }
            }
            SelectionContainer {
                Column {
                    if (s.address.isNotEmpty()) {
                        Text(s.address, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
                    }
                    if (s.bedrockAddress.isNotEmpty()) {
                        Text(
                            "Bedrock: ${s.bedrockAddress}",
                            style = MaterialTheme.typography.bodySmall,
                            fontFamily = FontFamily.Monospace,
                        )
                    }
                }
            }
            val facts = buildList {
                s.startedAt?.let { add("Up ${durationText(Duration.between(it, Instant.now()).toMinutes())}") }
                if (s.online) add(if (s.players.isEmpty()) "Nobody online" else "${s.players.size} online")
            }
            if (facts.isNotEmpty()) Text(facts.joinToString(" · "), style = MaterialTheme.typography.bodyMedium)
            s.usage?.let { Text(usageLine(it, s.memoryLimitMB), style = MaterialTheme.typography.bodySmall) }
        }
    }
}

@Composable
private fun PowerButtons(s: Server, busy: String?, onPower: (String) -> Unit) {
    val canStart = s.installed && s.state in setOf("stopped", "crashed", "sleeping") && s.task.isEmpty()
    val canStop = s.state == "running" || s.state == "starting"
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
        if (!canStop) {
            Button(onClick = { onPower("start") }, enabled = canStart && busy == null, modifier = Modifier.weight(1f)) {
                PowerLabel(busy == "start", if (s.state == "sleeping") "Wake" else "Start")
            }
        } else {
            Button(onClick = { onPower("stop") }, enabled = busy == null, modifier = Modifier.weight(1f)) {
                PowerLabel(busy == "stop", "Stop")
            }
            OutlinedButton(onClick = { onPower("restart") }, enabled = busy == null, modifier = Modifier.weight(1f)) {
                PowerLabel(busy == "restart", "Restart")
            }
        }
    }
}

@Composable
private fun PowerLabel(busy: Boolean, text: String) {
    if (busy) CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp) else Text(text)
}
