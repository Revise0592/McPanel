package io.github.revise0592.mcpanel.data

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID

/**
 * Keeps the paired panels. Tokens and extra headers are encrypted by
 * [TokenCipher], since both can let someone in.
 */
class PanelStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("panels", Context.MODE_PRIVATE)

    private data class Entry(val panel: Panel, val encryptedToken: String, val encryptedHeaders: String)

    @Synchronized
    fun panels(): List<Panel> = load().map { it.panel }

    /** Saves a newly paired panel, replacing any earlier pairing with the same address. */
    @Synchronized
    fun add(address: String, info: PanelInfo, deviceId: String, token: String, headers: Map<String, String>): Panel {
        val panel = Panel(
            id = UUID.randomUUID().toString(),
            address = address,
            name = info.name,
            deviceId = deviceId,
            accent = info.accent,
            accentText = info.accentText,
            theme = info.theme,
            logo = info.logo,
        )
        val list = load().filter { it.panel.address != address } + Entry(panel, TokenCipher.encrypt(token), encryptHeaders(headers))
        save(list)
        return panel
    }

    /** Takes in the panel's latest name and look. Returns the updated panel, or null if nothing changed. */
    @Synchronized
    fun updateInfo(id: String, info: PanelInfo): Panel? {
        var changed: Panel? = null
        val list = load().map { e ->
            if (e.panel.id != id) return@map e
            val p = e.panel.copy(
                name = info.name,
                accent = info.accent,
                accentText = info.accentText,
                theme = info.theme,
                logo = info.logo,
            )
            if (p != e.panel) changed = p
            e.copy(panel = p)
        }
        if (changed != null) save(list)
        return changed
    }

    @Synchronized
    fun token(id: String): String? = load().find { it.panel.id == id }?.encryptedToken?.let(TokenCipher::decrypt)

    /** The panel's extra headers; empty if there are none or they can't be read. */
    @Synchronized
    fun headers(id: String): Map<String, String> {
        val enc = load().find { it.panel.id == id }?.encryptedHeaders.orEmpty()
        if (enc.isEmpty()) return emptyMap()
        val o = TokenCipher.decrypt(enc)?.let {
            try {
                JSONObject(it)
            } catch (e: org.json.JSONException) {
                null
            }
        } ?: return emptyMap()
        return o.keys().asSequence().associateWith { o.getString(it) }
    }

    @Synchronized
    fun setHeaders(id: String, headers: Map<String, String>) =
        save(load().map { if (it.panel.id == id) it.copy(encryptedHeaders = encryptHeaders(headers)) else it })

    @Synchronized
    fun remove(id: String) = save(load().filter { it.panel.id != id })

    private fun encryptHeaders(h: Map<String, String>): String =
        if (h.isEmpty()) "" else TokenCipher.encrypt(JSONObject(h).toString())

    private fun load(): List<Entry> {
        val arr = try {
            JSONArray(prefs.getString(KEY, "[]"))
        } catch (e: org.json.JSONException) {
            JSONArray()
        }
        return (0 until arr.length()).map {
            val o = arr.getJSONObject(it)
            Entry(
                Panel(
                    id = o.getString("id"),
                    address = o.getString("address"),
                    name = o.getString("name"),
                    deviceId = o.getString("deviceId"),
                    accent = o.optString("accent").ifEmpty { DEFAULT_ACCENT },
                    accentText = o.optString("accentText").ifEmpty { "#ffffff" },
                    theme = o.optString("theme").ifEmpty { "system" },
                    logo = o.optString("logo"),
                ),
                o.getString("token"),
                o.optString("headers"),
            )
        }
    }

    private fun save(list: List<Entry>) {
        val arr = JSONArray()
        list.forEach {
            arr.put(
                JSONObject()
                    .put("id", it.panel.id)
                    .put("address", it.panel.address)
                    .put("name", it.panel.name)
                    .put("deviceId", it.panel.deviceId)
                    .put("accent", it.panel.accent)
                    .put("accentText", it.panel.accentText)
                    .put("theme", it.panel.theme)
                    .put("logo", it.panel.logo)
                    .put("token", it.encryptedToken)
                    .put("headers", it.encryptedHeaders),
            )
        }
        prefs.edit().putString(KEY, arr.toString()).apply()
    }

    private companion object {
        const val KEY = "panels"
    }
}
