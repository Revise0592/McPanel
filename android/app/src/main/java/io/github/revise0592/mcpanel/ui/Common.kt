package io.github.revise0592.mcpanel.ui

import android.graphics.BitmapFactory
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import io.github.revise0592.mcpanel.data.Conn
import io.github.revise0592.mcpanel.data.Headers
import io.github.revise0592.mcpanel.data.LogoCache
import io.github.revise0592.mcpanel.data.Panel
import io.github.revise0592.mcpanel.data.Server
import io.github.revise0592.mcpanel.ui.theme.LocalPanelColors
import io.github.revise0592.mcpanel.ui.theme.parseHex

/** The panel's logo, or a square in its accent colour, as the web UI shows it. */
@Composable
fun PanelLogo(panel: Panel, logos: LogoCache?, conn: () -> Conn?, logoSize: Dp = 28.dp) {
    val bitmap by produceState<ImageBitmap?>(null, panel.id, panel.logo) {
        val c = conn() ?: return@produceState
        value = logos?.get(panel, c)?.let { b ->
            BitmapFactory.decodeByteArray(b, 0, b.size)?.asImageBitmap()
        }
    }
    val shape = RoundedCornerShape(logoSize / 4)
    val img = bitmap
    if (img != null) {
        Image(img, contentDescription = null, contentScale = ContentScale.Fit, modifier = Modifier.size(logoSize).clip(shape))
    } else {
        val accent = parseHex(panel.accent) ?: MaterialTheme.colorScheme.primary
        val onAccent = parseHex(panel.accentText) ?: MaterialTheme.colorScheme.onPrimary
        Box(Modifier.size(logoSize).clip(shape).background(accent), contentAlignment = Alignment.Center) {
            Text(
                panel.name.trim().take(1).uppercase(),
                color = onAccent,
                fontWeight = FontWeight.SemiBold,
                style = if (logoSize >= 36.dp) MaterialTheme.typography.titleMedium else MaterialTheme.typography.labelLarge,
            )
        }
    }
}

@Composable
fun StateBadge(s: Server) {
    val pc = LocalPanelColors.current
    val (bg, fg) = when (s.state) {
        "running" -> pc.ok.copy(alpha = 0.16f) to pc.ok
        "starting", "stopping" -> pc.warn.copy(alpha = 0.16f) to pc.warn
        "crashed" -> MaterialTheme.colorScheme.errorContainer to MaterialTheme.colorScheme.onErrorContainer
        else -> MaterialTheme.colorScheme.surfaceVariant to MaterialTheme.colorScheme.onSurfaceVariant
    }
    val text = if (s.task.isNotEmpty() && s.progress > 0) "${s.label} ${s.progress}%" else s.label
    Surface(color = bg, contentColor = fg, shape = RoundedCornerShape(50)) {
        Text(
            text,
            style = MaterialTheme.typography.labelMedium,
            maxLines = 1,
            modifier = Modifier.padding(horizontal = 10.dp, vertical = 4.dp),
        )
    }
}

/** A problem shown at the top of a screen, with an optional button. */
@Composable
fun ErrorCard(message: String, action: String? = null, onAction: () -> Unit = {}) {
    Card(
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.errorContainer),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(message, color = MaterialTheme.colorScheme.onErrorContainer)
            if (action != null) Button(onClick = onAction) { Text(action) }
        }
    }
}

/** A yes/no question before doing something that can't be undone. */
@Composable
fun ConfirmDialog(title: String, text: String, confirm: String, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { Text(text) },
        confirmButton = {
            TextButton(onClick = {
                onDismiss()
                onConfirm()
            }) { Text(confirm) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

/** The text field for extra headers, with its explanation. */
@Composable
fun HeadersField(value: String, onValueChange: (String) -> Unit, error: String?, enabled: Boolean = true) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            label = { Text("Extra headers") },
            placeholder = { Text("CF-Access-Client-Id: …") },
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            isError = error != null,
            enabled = enabled,
            minLines = 2,
            modifier = Modifier.fillMaxWidth().heightIn(min = 96.dp),
        )
        Text(
            error ?: "Only needed when a login proxy in front of the panel wants a token in a header. One per line, as Name: value. They're kept encrypted on this phone.",
            style = MaterialTheme.typography.bodySmall,
            color = if (error != null) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

/** A dialog to change a paired panel's extra headers. */
@Composable
fun HeadersDialog(initial: Map<String, String>, onSave: (Map<String, String>) -> Unit, onDismiss: () -> Unit) {
    var text by rememberSaveable { mutableStateOf(Headers.format(initial)) }
    var error by remember { mutableStateOf<String?>(null) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Extra headers") },
        text = { HeadersField(text, { text = it; error = null }, error) },
        confirmButton = {
            TextButton(onClick = {
                when (val r = Headers.parse(text)) {
                    is Headers.Result.Ok -> {
                        onSave(r.headers)
                        onDismiss()
                    }
                    is Headers.Result.Bad -> error = r.message
                }
            }) { Text("Save") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}
