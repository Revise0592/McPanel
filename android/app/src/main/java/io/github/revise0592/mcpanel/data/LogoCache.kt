package io.github.revise0592.mcpanel.data

import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File

/**
 * Keeps each panel's logo on the phone, so it shows straight away and is
 * only downloaded again when the panel's logo changes.
 */
class LogoCache(context: Context) {
    private val dir = File(context.applicationContext.filesDir, "logos")

    /** The logo's PNG bytes, or null if the panel has none or it can't be loaded. */
    suspend fun get(panel: Panel, conn: Conn): ByteArray? = withContext(Dispatchers.IO) {
        val img = File(dir, "${panel.id}.png")
        val ver = File(dir, "${panel.id}.v")
        if (panel.logo.isEmpty()) {
            img.delete()
            ver.delete()
            return@withContext null
        }
        if (img.exists() && ver.exists() && ver.readText() == panel.logo) return@withContext img.readBytes()
        try {
            val b = PanelApi.logo(conn, panel.logo)
            dir.mkdirs()
            img.writeBytes(b)
            ver.writeText(panel.logo)
            b
        } catch (e: ApiException) {
            // Offline: an older logo is better than none.
            if (img.exists()) img.readBytes() else null
        }
    }

    fun forget(panelId: String) {
        File(dir, "$panelId.png").delete()
        File(dir, "$panelId.v").delete()
    }
}
