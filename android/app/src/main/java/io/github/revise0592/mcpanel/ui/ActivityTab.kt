package io.github.revise0592.mcpanel.ui

import android.text.format.DateUtils
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import io.github.revise0592.mcpanel.data.ApiException
import io.github.revise0592.mcpanel.data.Event
import io.github.revise0592.mcpanel.data.Panel
import io.github.revise0592.mcpanel.data.PanelApi
import io.github.revise0592.mcpanel.data.PanelStore
import io.github.revise0592.mcpanel.data.Server
import io.github.revise0592.mcpanel.ui.theme.LocalPanelColors
import kotlinx.coroutines.delay

/** The server's history, newest first. */
@Composable
fun ActivityTab(panel: Panel, server: Server, store: PanelStore) {
    var events by remember(server.id) { mutableStateOf<List<Event>?>(null) }
    var error by remember(server.id) { mutableStateOf<String?>(null) }
    val lifecycle = LocalLifecycleOwner.current.lifecycle

    LaunchedEffect(server.id) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                val conn = store.conn(panel) ?: break
                try {
                    events = PanelApi.activity(conn, server.id, 100)
                    error = null
                } catch (e: ApiException) {
                    error = e.message
                    if (e.unauthorized) break
                }
                delay(15_000)
            }
        }
    }

    val list = events
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp)) {
        error?.let { item(key = "error") { ErrorCard(it) } }
        when {
            list == null && error == null -> item(key = "loading") {
                Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
            }
            list != null && list.isEmpty() -> item(key = "empty") {
                Text("Nothing has happened on this server yet.", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            list != null -> items(list.size) { i ->
                EventRow(list[i])
                if (i < list.size - 1) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            }
        }
    }
}

@Composable
private fun EventRow(e: Event) {
    val pc = LocalPanelColors.current
    val dot = when (e.kind) {
        "crash", "failed" -> MaterialTheme.colorScheme.error
        "update" -> MaterialTheme.colorScheme.primary
        "power" -> pc.ok
        else -> pc.idle
    }
    Row(
        Modifier.fillMaxWidth().padding(vertical = 10.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Surface(color = dot, shape = CircleShape, modifier = Modifier.padding(top = 6.dp).size(8.dp)) {}
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(if (e.repeats > 0) "${e.text} (×${e.repeats + 1})" else e.text, style = MaterialTheme.typography.bodyMedium)
            Text(
                DateUtils.getRelativeTimeSpanString(e.time.toEpochMilli()).toString(),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
