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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.graphics.Color
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
import io.github.revise0592.mcpanel.data.usageLine
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** How often the list refreshes while it's on screen. */
private const val REFRESH_MS = 10_000L

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServersScreen(panel: Panel, store: PanelStore, onBack: () -> Unit, onRemoved: () -> Unit) {
    var servers by remember(panel.id) { mutableStateOf<List<Server>?>(null) }
    var error by remember(panel.id) { mutableStateOf<String?>(null) }
    var unauthorized by remember(panel.id) { mutableStateOf(false) }
    var refreshes by remember { mutableIntStateOf(0) }
    var menuOpen by remember { mutableStateOf(false) }
    var confirmRemove by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val lifecycle = LocalLifecycleOwner.current.lifecycle

    // Refresh every few seconds, but only while the app is on screen.
    LaunchedEffect(panel.id, refreshes) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (!unauthorized) {
                val token = store.token(panel.id)
                if (token == null) {
                    error = "This phone's sign-in for the panel can't be read any more. Remove the panel and add it again."
                    unauthorized = true
                    break
                }
                try {
                    servers = PanelApi.servers(panel.address, token)
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
            store.token(panel.id)?.let { token ->
                try {
                    PanelApi.unpair(panel.address, token)
                } catch (e: ApiException) {
                    // Already removed there, or offline.
                }
            }
            store.remove(panel.id)
            onRemoved()
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Text(panel.name, maxLines = 1, overflow = TextOverflow.Ellipsis)
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
                    Card(
                        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.errorContainer),
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            Text(msg, color = MaterialTheme.colorScheme.onErrorContainer)
                            if (unauthorized) {
                                Button(onClick = { remove() }) { Text("Remove panel") }
                            }
                        }
                    }
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
                list != null -> items(list, key = { it.id }) { ServerCard(it) }
            }
        }
    }

    if (confirmRemove) {
        AlertDialog(
            onDismissRequest = { confirmRemove = false },
            title = { Text("Remove ${panel.name}?") },
            text = { Text("This phone is unpaired from the panel. You can add it again later with a new pairing code.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmRemove = false
                    remove()
                }) { Text("Remove") }
            },
            dismissButton = {
                TextButton(onClick = { confirmRemove = false }) { Text("Cancel") }
            },
        )
    }
}

@Composable
private fun ServerCard(s: Server) {
    Card(modifier = Modifier.fillMaxWidth()) {
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

private val Green = Color(0xFF16A34A)
private val Amber = Color(0xFFD97706)

@Composable
private fun StateBadge(s: Server) {
    val (bg, fg) = when (s.state) {
        "running" -> Green.copy(alpha = 0.16f) to Green
        "starting", "stopping" -> Amber.copy(alpha = 0.16f) to Amber
        "crashed" -> MaterialTheme.colorScheme.errorContainer to MaterialTheme.colorScheme.onErrorContainer
        else -> MaterialTheme.colorScheme.surfaceVariant to MaterialTheme.colorScheme.onSurfaceVariant
    }
    Surface(color = bg, contentColor = fg, shape = RoundedCornerShape(50)) {
        Text(
            s.label,
            style = MaterialTheme.typography.labelMedium,
            maxLines = 1,
            modifier = Modifier.padding(horizontal = 10.dp, vertical = 4.dp),
        )
    }
}
