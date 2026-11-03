@file:OptIn(ExperimentalMaterial3Api::class)

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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import io.github.revise0592.mcpanel.data.ApiException
import io.github.revise0592.mcpanel.data.Panel
import io.github.revise0592.mcpanel.data.PanelApi
import io.github.revise0592.mcpanel.data.PanelStore
import io.github.revise0592.mcpanel.data.Players
import io.github.revise0592.mcpanel.data.Server
import io.github.revise0592.mcpanel.data.durationText
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** The lists a player can be added to, with the API's action for each. */
private enum class PlayerList(val title: String, val add: String) {
    Whitelist("Whitelist", "allow"),
    Ops("Operators", "op"),
    Bans("Banned", "ban"),
}

/** What a done action says, as the web UI's Players page does. */
private fun doneMessage(action: String, name: String): String? = when (action) {
    "kick" -> "$name was kicked."
    "whitelist-on" -> "Only players on the list can join now."
    "whitelist-off" -> "Anyone can join now."
    "allow" -> "$name can join now."
    "unallow" -> "$name was removed from the list."
    "op" -> "$name is now an operator."
    "deop" -> "$name is no longer an operator."
    "ban" -> "$name was banned."
    "unban" -> "$name was unbanned."
    else -> null
}

@Composable
fun PlayersTab(panel: Panel, server: Server, store: PanelStore) {
    var players by remember(server.id) { mutableStateOf<Players?>(null) }
    var error by remember(server.id) { mutableStateOf<String?>(null) }
    var message by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var confirmBan by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val lifecycle = LocalLifecycleOwner.current.lifecycle

    // Who's online changes often; the lists less so.
    LaunchedEffect(server.id) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                val conn = store.conn(panel) ?: break
                if (!busy) {
                    try {
                        players = PanelApi.players(conn, server.id)
                        error = null
                    } catch (e: ApiException) {
                        error = e.message
                        if (e.unauthorized) break
                    }
                }
                delay(10_000)
            }
        }
    }

    fun act(action: String, name: String = "") {
        if (busy) return
        val conn = store.conn(panel) ?: return
        busy = true
        message = null
        error = null
        scope.launch {
            try {
                players = PanelApi.playersAction(conn, server.id, action, name)
                message = doneMessage(action, name)
            } catch (e: ApiException) {
                error = e.message
            } finally {
                busy = false
            }
        }
    }

    val p = players
    LazyColumn(
        Modifier.fillMaxSize(),
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        error?.let { item(key = "error") { ErrorCard(it) } }
        message?.let { item(key = "message") { Text(it, color = MaterialTheme.colorScheme.primary) } }
        if (p == null) {
            if (error == null) {
                item(key = "loading") {
                    Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                }
            }
            return@LazyColumn
        }

        item(key = "online") {
            Section("Online (${p.online.size})") {
                when {
                    p.online.isNotEmpty() -> p.online.forEach { name ->
                        PlayerRow(name, null, enabled = !busy, menu = buildList<Pair<String, () -> Unit>> {
                            add("Kick" to { act("kick", name) })
                            if (name in p.ops) add("Remove operator" to { act("deop", name) }) else add("Make operator" to { act("op", name) })
                            if (p.whitelistOn && name !in p.whitelist) add("Add to whitelist" to { act("allow", name) })
                            add("Ban" to { confirmBan = name })
                        })
                    }
                    server.state == "running" -> Muted("Nobody is playing right now.")
                    else -> Muted("The server isn't running.")
                }
            }
        }

        if (p.rejected.isNotEmpty()) {
            item(key = "rejected") {
                Section("Turned away by the whitelist") {
                    p.rejected.forEach { name ->
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(name, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                            TextButton(onClick = { act("dismiss", name) }, enabled = !busy) { Text("Dismiss") }
                            Button(onClick = { act("allow", name) }, enabled = !busy) { Text("Allow") }
                        }
                    }
                }
            }
        }

        item(key = "add") { AddPlayer(enabled = !busy, bedrock = p.bedrock) { list, name -> act(list.add, name) } }

        item(key = "whitelist") {
            Section(if (p.bedrock) "Allowlist" else "Whitelist") {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("Only players on the list can join")
                        Text(
                            if (p.whitelistOn) "On" else "Off: anyone can join",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Switch(
                        checked = p.whitelistOn,
                        onCheckedChange = { act(if (it) "whitelist-on" else "whitelist-off") },
                        enabled = !busy,
                    )
                }
                if (p.whitelist.isNotEmpty()) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                NameList(p.whitelist, "Nobody is on the list yet.", "Remove", !busy) { act("unallow", it) }
            }
        }

        if (!p.bedrock || p.ops.isNotEmpty()) {
            item(key = "ops") {
                Section("Operators") { NameList(p.ops, "No operators.", "Remove", !busy) { act("deop", it) } }
            }
        }
        if (!p.bedrock || p.bans.isNotEmpty()) {
            item(key = "bans") {
                Section("Banned") { NameList(p.bans, "Nobody is banned.", "Unban", !busy) { act("unban", it) } }
            }
        }

        if (p.recent.isNotEmpty()) {
            item(key = "recent") {
                Section("Recently played") {
                    p.recent.forEach { r ->
                        val seen = r.seen?.let { DateUtils.getRelativeTimeSpanString(it.toEpochMilli()).toString() }
                        val played = if (r.playedMins > 0) "played ${durationText(r.playedMins.toLong())}" else null
                        PlayerRow(r.name, listOfNotNull(seen, played).joinToString(" · "), enabled = !busy, menu = buildList<Pair<String, () -> Unit>> {
                            if (r.name !in p.whitelist) add("Add to whitelist" to { act("allow", r.name) })
                            if (r.name !in p.ops) add("Make operator" to { act("op", r.name) })
                            if (r.name !in p.bans) add("Ban" to { confirmBan = r.name })
                        })
                    }
                }
            }
        }
    }

    confirmBan?.let { name ->
        ConfirmDialog(
            title = "Ban $name?",
            text = "$name is removed from the game and can't join again until they're unbanned.",
            confirm = "Ban",
            onConfirm = { act("ban", name) },
            onDismiss = { confirmBan = null },
        )
    }
}

@Composable
private fun Section(title: String, content: @Composable () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(title, style = MaterialTheme.typography.titleSmall)
            content()
        }
    }
}

@Composable
private fun Muted(text: String) {
    Text(text, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
}

@Composable
private fun PlayerRow(name: String, detail: String?, enabled: Boolean, menu: List<Pair<String, () -> Unit>>) {
    var open by remember { mutableStateOf(false) }
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(name, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!detail.isNullOrEmpty()) {
                Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (menu.isNotEmpty()) {
            Box {
                IconButton(onClick = { open = true }, enabled = enabled) {
                    Icon(Icons.Default.MoreVert, contentDescription = "Actions for $name")
                }
                DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
                    menu.forEach { (label, action) ->
                        DropdownMenuItem(text = { Text(label) }, onClick = {
                            open = false
                            action()
                        })
                    }
                }
            }
        }
    }
}

@Composable
private fun NameList(names: List<String>, empty: String, remove: String, enabled: Boolean, onRemove: (String) -> Unit) {
    if (names.isEmpty()) {
        Muted(empty)
        return
    }
    names.forEach { name ->
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(name, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
            TextButton(onClick = { onRemove(name) }, enabled = enabled) { Text(remove) }
        }
    }
}

@Composable
private fun AddPlayer(enabled: Boolean, bedrock: Boolean, onAdd: (PlayerList, String) -> Unit) {
    var name by rememberSaveable { mutableStateOf("") }
    var list by rememberSaveable { mutableStateOf(PlayerList.Whitelist) }
    val lists = if (bedrock) listOf(PlayerList.Whitelist) else PlayerList.entries
    fun add() {
        val n = name.trim()
        if (n.isEmpty()) return
        onAdd(list, n)
        name = ""
    }
    Section("Add a player") {
        if (lists.size > 1) {
            SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                lists.forEachIndexed { i, l ->
                    SegmentedButton(
                        selected = list == l,
                        onClick = { list = l },
                        shape = SegmentedButtonDefaults.itemShape(i, lists.size),
                    ) { Text(l.title, maxLines = 1) }
                }
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedTextField(
                value = name,
                onValueChange = { name = it },
                label = { Text("Player name") },
                singleLine = true,
                enabled = enabled,
                keyboardOptions = KeyboardOptions(
                    capitalization = KeyboardCapitalization.None,
                    autoCorrectEnabled = false,
                    imeAction = ImeAction.Done,
                ),
                keyboardActions = KeyboardActions(onDone = { add() }),
                modifier = Modifier.weight(1f),
            )
            Button(onClick = { add() }, enabled = enabled && name.isNotBlank()) { Text("Add") }
        }
    }
}
