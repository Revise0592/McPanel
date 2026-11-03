package io.github.revise0592.mcpanel.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
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
import io.github.revise0592.mcpanel.data.Conn
import io.github.revise0592.mcpanel.data.LogoCache
import io.github.revise0592.mcpanel.data.Panel
import io.github.revise0592.mcpanel.data.PanelApi
import io.github.revise0592.mcpanel.data.PanelStore
import io.github.revise0592.mcpanel.data.Server
import io.github.revise0592.mcpanel.data.usageLine
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** How often the list refreshes while it's on screen. */
private const val REFRESH_MS = 10_000L

/** The connection details for [panel], or null if its token can't be read. */
fun PanelStore.conn(panel: Panel): Conn? = token(panel.id)?.let { Conn(panel.address, it, headers(panel.id)) }

const val LOST_TOKEN = "This phone's sign-in for the panel can't be read any more. Remove the panel and add it again."

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServersScreen(
    panel: Panel,
    store: PanelStore,
    logos: LogoCache,
    onBack: () -> Unit,
    onOpen: (Server) -> Unit,
    onPanelChanged: (Panel) -> Unit,
    onRemoved: () -> Unit,
) {
    var servers by remember(panel.id) { mutableStateOf<List<Server>?>(null) }
    var error by remember(panel.id) { mutableStateOf<String?>(null) }
    var unauthorized by remember(panel.id) { mutableStateOf(false) }
    var refreshes by remember { mutableIntStateOf(0) }
    var menuOpen by remember { mutableStateOf(false) }
    var confirmRemove by remember { mutableStateOf(false) }
    var editHeaders by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val lifecycle = LocalLifecycleOwner.current.lifecycle

    // Pick up the panel's latest name, accent and logo once per visit.
    LaunchedEffect(panel.id) {
        val conn = store.conn(panel) ?: return@LaunchedEffect
        try {
            store.updateInfo(panel.id, PanelApi.me(conn))?.let(onPanelChanged)
        } catch (e: ApiException) {
            // The list below shows the problem.
        }
    }

    // Refresh every few seconds, but only while the app is on screen.
    LaunchedEffect(panel.id, refreshes) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (!unauthorized) {
                val conn = store.conn(panel)
                if (conn == null) {
                    error = LOST_TOKEN
                    unauthorized = true
                    break
                }
                try {
                    servers = PanelApi.servers(conn)
                    error = null
                } catch (e: ApiException) {
                    error = e.message
                    if (e.unauthorized) unauthorized = true
                }
                delay(REFRESH_MS)
            }
        }
    }

    fun remove() {
        scope.launch {
            // Tell the panel too, if it can be reached; it doesn't matter if not.
            store.conn(panel)?.let { conn ->
                try {
                    PanelApi.unpair(conn)
                } catch (e: ApiException) {
                    // Already removed there, or offline.
                }
            }
            store.remove(panel.id)
            logos.forget(panel.id)
            onRemoved()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        PanelLogo(panel, logos, { store.conn(panel) })
                        Text(panel.name, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
                actions = {
                    IconButton(onClick = { refreshes++ }, enabled = !unauthorized) {
                        Icon(Icons.Default.Refresh, contentDescription = "Refresh")
                    }
                    Box {
                        IconButton(onClick = { menuOpen = true }) {
                            Icon(Icons.Default.MoreVert, contentDescription = "More")
                        }
                        DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                            DropdownMenuItem(
                                text = { Text("Extra headers") },
                                onClick = {
                                    menuOpen = false
                                    editHeaders = true
                                },
                            )
                            DropdownMenuItem(
                                text = { Text("Remove panel") },
                                onClick = {
                                    menuOpen = false
                                    confirmRemove = true
                                },
                            )
                        }
                    }
                },
            )
        },
    ) { padding ->
        val list = servers
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            error?.let { msg ->
                item(key = "error") {
                    if (unauthorized) ErrorCard(msg, "Remove panel") { remove() } else ErrorCard(msg)
                }
            }
            when {
                list == null && error == null -> item(key = "loading") {
                    Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator()
                    }
                }
                list != null && list.isEmpty() -> item(key = "empty") {
                    Text(
                        "There are no servers on this panel yet.",
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                list != null -> items(list, key = { it.id }) { ServerCard(it) { onOpen(it) } }
            }
        }
    }

    if (confirmRemove) {
        ConfirmDialog(
            title = "Remove ${panel.name}?",
            text = "This phone is unpaired from the panel. You can add it again later with a new pairing code.",
            confirm = "Remove",
            onConfirm = { remove() },
            onDismiss = { confirmRemove = false },
        )
    }
    if (editHeaders) {
        HeadersDialog(
            initial = remember { store.headers(panel.id) },
            onSave = {
                store.setHeaders(panel.id, it)
                unauthorized = false
                refreshes++
            },
            onDismiss = { editHeaders = false },
        )
    }
}

@Composable
private fun ServerCard(s: Server, onClick: () -> Unit) {
    Card(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    s.name,
                    style = MaterialTheme.typography.titleMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                StateBadge(s)
            }
            Text(
                "${s.typeLabel} ${s.version}",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (s.address.isNotEmpty()) {
                Text(s.address, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
            }
            when {
                s.players.isNotEmpty() -> Text(
                    "${s.players.size} online: ${s.players.joinToString()}",
                    style = MaterialTheme.typography.bodyMedium,
                )
                s.online -> Text(
                    "Nobody online",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            s.usage?.let { u ->
                Text(usageLine(u, s.memoryLimitMB), style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}
