package io.github.revise0592.mcpanel.ui

import android.os.Build
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import io.github.revise0592.mcpanel.data.ApiException
import io.github.revise0592.mcpanel.data.Conn
import io.github.revise0592.mcpanel.data.Headers
import io.github.revise0592.mcpanel.data.Pairing
import io.github.revise0592.mcpanel.data.Panel
import io.github.revise0592.mcpanel.data.PanelApi
import io.github.revise0592.mcpanel.data.PanelStore
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddPanelScreen(
    initialAddress: String,
    initialCode: String,
    store: PanelStore,
    onBack: () -> Unit,
    onPaired: (Panel) -> Unit,
) {
    var address by rememberSaveable(initialAddress) { mutableStateOf(initialAddress) }
    var code by rememberSaveable(initialCode) { mutableStateOf(initialCode) }
    var headersText by rememberSaveable { mutableStateOf("") }
    var showHeaders by rememberSaveable { mutableStateOf(false) }
    var headersError by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var proxyError by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val base = Pairing.normalizeAddress(address)

    fun connect() {
        if (busy) return
        if (base == null) {
            error = "Enter the panel's address, such as 192.168.1.10:8080 or https://panel.example.com."
            return
        }
        val headers = when (val h = Headers.parse(headersText)) {
            is Headers.Result.Ok -> h.headers
            is Headers.Result.Bad -> {
                headersError = h.message
                showHeaders = true
                return
            }
        }
        busy = true
        error = null
        proxyError = false
        scope.launch {
            try {
                val r = PanelApi.pair(Conn(base, null, headers), code.trim(), Build.MODEL)
                onPaired(store.add(base, r.panel, r.deviceId, r.token, headers))
            } catch (e: ApiException) {
                error = e.message
                proxyError = e.proxy
            } finally {
                busy = false
            }
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Add panel") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
            )
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .imePadding()
                .verticalScroll(rememberScrollState())
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Text(
                "In your panel, open Settings → Mobile app and press Pair a phone. Enter the address and code it shows, or paste the whole pairing link. Use the same address you use in your browser, such as https://panel.example.com or 192.168.1.10:8080.",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            OutlinedTextField(
                value = address,
                onValueChange = {
                    // A pairing link pasted whole fills in both fields.
                    val link = Pairing.parseLink(it)
                    if (link != null) {
                        address = link.address
                        if (link.code.isNotEmpty()) code = link.code
                    } else {
                        address = it
                    }
                },
                label = { Text("Panel address") },
                placeholder = { Text("https://panel.example.com") },
                singleLine = true,
                enabled = !busy,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Next),
                modifier = Modifier.fillMaxWidth(),
            )
            if (base?.startsWith("http://") == true) {
                Text(
                    "This address isn't HTTPS, so others on the network could read the app's traffic. That's fine at home; use HTTPS from outside.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            OutlinedTextField(
                value = code,
                onValueChange = { code = it },
                label = { Text("Pairing code") },
                placeholder = { Text("ABCD-EFGH") },
                singleLine = true,
                enabled = !busy,
                keyboardOptions = KeyboardOptions(
                    capitalization = KeyboardCapitalization.Characters,
                    imeAction = ImeAction.Done,
                ),
                keyboardActions = KeyboardActions(onDone = { connect() }),
                modifier = Modifier.fillMaxWidth(),
            )
            if (showHeaders) {
                HeadersField(headersText, { headersText = it; headersError = null }, headersError, enabled = !busy)
            } else {
                TextButton(onClick = { showHeaders = true }, contentPadding = PaddingValues(0.dp)) {
                    Text("Behind a login proxy? Add headers")
                }
            }
            error?.let { msg ->
                if (proxyError) ErrorCard(msg) else Text(msg, color = MaterialTheme.colorScheme.error)
            }
            Button(
                onClick = { connect() },
                enabled = !busy && address.isNotBlank() && code.isNotBlank(),
                modifier = Modifier.fillMaxWidth(),
            ) {
                if (busy) {
                    CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
                } else {
                    Text("Connect")
                }
            }
        }
    }
}
