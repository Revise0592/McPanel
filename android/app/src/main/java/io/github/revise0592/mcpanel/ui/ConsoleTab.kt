@file:OptIn(ExperimentalMaterial3Api::class)

package io.github.revise0592.mcpanel.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
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
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material3.AssistChip
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import io.github.revise0592.mcpanel.data.ApiException
import io.github.revise0592.mcpanel.data.ConsoleFilter
import io.github.revise0592.mcpanel.data.ConsoleLine
import io.github.revise0592.mcpanel.data.LineClassifier
import io.github.revise0592.mcpanel.data.LineKind
import io.github.revise0592.mcpanel.data.Panel
import io.github.revise0592.mcpanel.data.PanelApi
import io.github.revise0592.mcpanel.data.PanelStore
import io.github.revise0592.mcpanel.data.Server
import io.github.revise0592.mcpanel.data.shows
import io.github.revise0592.mcpanel.ui.theme.LocalPanelColors
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** As many lines as the phone keeps; the web UI keeps 2000. */
private const val MAX_LINES = 1000

/** Common commands, one tap away. */
private val quickCommands = listOf("list", "save-all", "time set day", "weather clear")

@Composable
fun ConsoleTab(panel: Panel, server: Server, store: PanelStore) {
    val lines = remember(server.id) { mutableStateListOf<ConsoleLine>() }
    var status by remember(server.id) { mutableStateOf<String?>("Connecting…") }
    var filter by rememberSaveable { mutableStateOf(ConsoleFilter.All) }
    var command by rememberSaveable { mutableStateOf("") }
    var sendError by remember { mutableStateOf<String?>(null) }
    var sending by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val listState = rememberLazyListState()
    val pc = LocalPanelColors.current

    // Stream while on screen; reconnect when the connection drops.
    LaunchedEffect(server.id) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                val conn = store.conn(panel) ?: run {
                    status = LOST_TOKEN
                    return@repeatOnLifecycle
                }
                // The panel sends the backlog again on each connection.
                val classifier = LineClassifier()
                var next = 0L
                var first = true
                try {
                    PanelApi.console(conn, server.id).collect { text ->
                        if (first) {
                            lines.clear()
                            status = null
                            first = false
                        }
                        lines.add(ConsoleLine(next++, text, classifier.classify(text)))
                        if (lines.size > MAX_LINES) lines.removeRange(0, lines.size - MAX_LINES)
                    }
                } catch (e: ApiException) {
                    if (e.unauthorized) {
                        status = e.message
                        return@repeatOnLifecycle
                    }
                    status = "${e.message} Reconnecting…"
                }
                if (first) status = status ?: "Reconnecting…"
                delay(3_000)
            }
        }
    }

    val shown by remember(server.id) { derivedStateOf { lines.filter { filter.shows(it.kind) } } }
    val atBottom by remember {
        derivedStateOf {
            val info = listState.layoutInfo
            val last = info.visibleItemsInfo.lastOrNull()
            last == null || last.index >= info.totalItemsCount - 2
        }
    }
    // Follow new lines, unless the user has scrolled up to read.
    LaunchedEffect(shown.size, filter) {
        if (shown.isNotEmpty() && atBottom) listState.scrollToItem(shown.size - 1)
    }

    fun send(cmd: String) {
        val text = cmd.trim()
        if (text.isEmpty() || sending) return
        val conn = store.conn(panel) ?: return
        sending = true
        sendError = null
        scope.launch {
            try {
                PanelApi.command(conn, server.id, text)
                if (text == command.trim()) command = ""
                listState.scrollToItem(maxOf(shown.size - 1, 0))
            } catch (e: ApiException) {
                sendError = e.message
            } finally {
                sending = false
            }
        }
    }

    val running = server.state == "running"
    Column(Modifier.fillMaxSize().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            ConsoleFilter.entries.forEach { f ->
                FilterChip(selected = filter == f, onClick = { filter = f }, label = { Text(f.title) })
            }
        }
        Box(
            Modifier
                .weight(1f)
                .fillMaxWidth()
                .clip(RoundedCornerShape(8.dp))
                .background(pc.consoleBg),
        ) {
            SelectionContainer {
                LazyColumn(
                    state = listState,
                    contentPadding = PaddingValues(10.dp),
                    modifier = Modifier.fillMaxSize(),
                ) {
                    items(shown, key = { it.id }) { line ->
                        Text(
                            line.text,
                            color = when (line.kind) {
                                LineKind.Command -> pc.consoleCmd
                                LineKind.Panel -> pc.consolePanel
                                LineKind.Error -> pc.consoleErr
                                LineKind.Warning -> pc.consoleWarn
                                else -> pc.consoleText
                            },
                            fontFamily = FontFamily.Monospace,
                            fontSize = 12.sp,
                            lineHeight = 16.sp,
                        )
                    }
                }
            }
            status?.let {
                Text(
                    it,
                    color = pc.consoleText.copy(alpha = 0.7f),
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.align(Alignment.BottomStart).padding(10.dp),
                )
            }
        }
        if (running) {
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                quickCommands.forEach { c ->
                    AssistChip(onClick = { send(c) }, label = { Text(c, fontFamily = FontFamily.Monospace) }, enabled = !sending)
                }
            }
        }
        sendError?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
        OutlinedTextField(
            value = command,
            onValueChange = { command = it },
            placeholder = { Text(if (running) "Type a command" else "Start the server to send commands") },
            enabled = running,
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            keyboardOptions = KeyboardOptions(
                capitalization = KeyboardCapitalization.None,
                autoCorrectEnabled = false,
                imeAction = ImeAction.Send,
            ),
            keyboardActions = KeyboardActions(onSend = { send(command) }),
            trailingIcon = {
                IconButton(onClick = { send(command) }, enabled = running && command.isNotBlank() && !sending) {
                    Icon(Icons.AutoMirrored.Filled.Send, contentDescription = "Send")
                }
            },
            modifier = Modifier.fillMaxWidth(),
        )
    }
}
