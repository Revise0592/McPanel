package io.github.revise0592.mcpanel.data

import org.json.JSONObject
import java.util.Locale

/** A panel this phone is paired with. Its token is kept by [PanelStore]. */
data class Panel(
    val id: String,
    val address: String,
    val name: String,
    val deviceId: String,
)

data class Usage(
    /** Percent of the machine; -1 until the panel knows. */
    val cpu: Double,
    val memoryMB: Int,
    /** 0 when the server doesn't report it. */
    val tps: Double,
)

data class Server(
    val id: String,
    val name: String,
    val typeLabel: String,
    val version: String,
    val installed: Boolean,
    /** stopped, starting, running, stopping, crashed or sleeping. */
    val state: String,
    /** The panel's own wording, such as "Online" or "Installing". */
    val label: String,
    val players: List<String>,
    val address: String,
    val memoryLimitMB: Int,
    val usage: Usage?,
) {
    val online: Boolean get() = state == "running" || state == "starting"
}

/** Reads the body of GET /api/v1/servers. */
fun parseServers(body: String): List<Server> {
    val arr = JSONObject(body).getJSONArray("servers")
    return (0 until arr.length()).map { parseServer(arr.getJSONObject(it)) }
}

fun parseServer(o: JSONObject): Server {
    val players = o.optJSONArray("players")
    val usage = o.optJSONObject("usage")
    return Server(
        id = o.getString("id"),
        name = o.optString("name"),
        typeLabel = o.optString("typeLabel"),
        version = o.optString("version"),
        installed = o.optBoolean("installed"),
        state = o.optString("state", "stopped"),
        label = o.optString("label"),
        players = if (players == null) emptyList() else (0 until players.length()).map { players.getString(it) },
        address = o.optString("address"),
        memoryLimitMB = o.optInt("memoryLimitMB"),
        usage = usage?.let { Usage(it.optDouble("cpu", -1.0), it.optInt("memoryMB"), it.optDouble("tps", 0.0)) },
    )
}

/** A short line such as "CPU 12% · Memory 2.0 GB of 4.0 GB · TPS 19.9". */
fun usageLine(u: Usage, limitMB: Int, locale: Locale = Locale.getDefault()): String = buildList {
    if (u.cpu >= 0) add(String.format(locale, "CPU %.0f%%", u.cpu))
    add(if (limitMB > 0) "Memory ${size(u.memoryMB, locale)} of ${size(limitMB, locale)}" else "Memory ${size(u.memoryMB, locale)}")
    if (u.tps > 0) add(String.format(locale, "TPS %.1f", u.tps))
}.joinToString(" · ")

private fun size(mb: Int, locale: Locale): String =
    if (mb >= 1024) String.format(locale, "%.1f GB", mb / 1024.0) else "$mb MB"
