package io.github.revise0592.mcpanel.data

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject
import java.util.UUID

/** Keeps the paired panels, with their tokens encrypted by [TokenCipher]. */
class PanelStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences("panels", Context.MODE_PRIVATE)

    private class Entry(val panel: Panel, val encryptedToken: String)

    @Synchronized
    fun panels(): List<Panel> = load().map { it.panel }

    /** Saves a newly paired panel, replacing any earlier pairing with the same address. */
    @Synchronized
    fun add(address: String, name: String, deviceId: String, token: String): Panel {
        val panel = Panel(UUID.randomUUID().toString(), address, name, deviceId)
        val list = load().filter { it.panel.address != address } + Entry(panel, TokenCipher.encrypt(token))
        save(list)
        return panel
    }

    @Synchronized
    fun token(id: String): String? = load().find { it.panel.id == id }?.encryptedToken?.let(TokenCipher::decrypt)

    @Synchronized
    fun remove(id: String) = save(load().filter { it.panel.id != id })

    private fun load(): List<Entry> {
        val arr = try {
            JSONArray(prefs.getString(KEY, "[]"))
        } catch (e: org.json.JSONException) {
            JSONArray()
        }
        return (0 until arr.length()).map {
            val o = arr.getJSONObject(it)
            Entry(
                Panel(o.getString("id"), o.getString("address"), o.getString("name"), o.getString("deviceId")),
                o.getString("token"),
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
                    .put("token", it.encryptedToken),
            )
        }
        prefs.edit().putString(KEY, arr.toString()).apply()
    }

    private companion object {
        const val KEY = "panels"
    }
}
