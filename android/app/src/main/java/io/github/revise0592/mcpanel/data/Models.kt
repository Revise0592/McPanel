package io.github.revise0592.mcpanel.data

import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant
import java.util.Locale

/** A panel this phone is paired with. Its token and headers are kept by [PanelStore]. */
data class Panel(
    val id: String,
    val address: String,
    val name: String,
    val deviceId: String,
    /** The panel's accent colour, such as "#3b82f6". */
    val accent: String = DEFAULT_ACCENT,
    /** Text colour on accent-filled buttons, as the web UI picks it. */
    val accentText: String = "#ffffff",
    /** system, light or dark, from the panel's Appearance settings. */
    val theme: String = "system",
    /** API path of the panel's logo with its ?v= version, or "" if none. */
    val logo: String = "",
)

const val DEFAULT_ACCENT = "#3b82f6"

/** What /api/v1/me and /api/v1/pair say about the panel. */
data class PanelInfo(
    val name: String,
    val accent: String,
    val accentText: String,
    val theme: String,
    val logo: String,
    val version: String,
)

fun parsePanelInfo(o: JSONObject) = PanelInfo(
    name = o.optString("name", "McPanel"),
    accent = o.optString("accent").ifEmpty { DEFAULT_ACCENT },
    accentText = o.optString("accentText").ifEmpty { "#ffffff" },
    theme = o.optString("theme").ifEmpty { "system" },
    logo = o.optString("logo"),
    version = o.optString("version"),
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
    val type: String = "",
    val typeLabel: String,
    val version: String,
    val installed: Boolean,
    /** stopped, starting, running, stopping, crashed or sleeping. */
    val state: String,
    /** The panel's own wording, such as "Online" or "Installing". */
    val label: String,
    val task: String = "",
    /** Percent done of [task]; -1 when unknown. */
    val progress: Int = 0,
    val startedAt: Instant? = null,
    val players: List<String>,
    val address: String,
    val bedrockAddress: String = "",
    val memoryLimitMB: Int,
    val usage: Usage?,
) {
    val online: Boolean get() = state == "running" || state == "starting"

    /** Velocity has no player lists of its own. */
    val isProxy: Boolean get() = type == "velocity"
}

/** Reads the body of GET /api/v1/servers. */
fun parseServers(body: String): List<Server> {
    val arr = JSONObject(body).getJSONArray("servers")
    return (0 until arr.length()).map { parseServer(arr.getJSONObject(it)) }
}

fun parseServer(o: JSONObject): Server {
    val usage = o.optJSONObject("usage")
    return Server(
        id = o.getString("id"),
        name = o.optString("name"),
        type = o.optString("type"),
        typeLabel = o.optString("typeLabel"),
        version = o.optString("version"),
        installed = o.optBoolean("installed"),
        state = o.optString("state", "stopped"),
        label = o.optString("label"),
        task = o.optString("task"),
        progress = o.optInt("progress"),
        startedAt = o.optString("startedAt").takeIf { it.isNotEmpty() }?.let(::parseTime),
        players = strings(o.optJSONArray("players")),
        address = o.optString("address"),
        bedrockAddress = o.optString("bedrockAddress"),
        memoryLimitMB = o.optInt("memoryLimitMB"),
        usage = usage?.let { Usage(it.optDouble("cpu", -1.0), it.optInt("memoryMB"), it.optDouble("tps", 0.0)) },
    )
}

data class Event(
    val time: Instant,
    /** crash, failed, update, power, or "" for plain events. */
    val kind: String,
    val text: String,
    val repeats: Int,
)

/** Reads the body of GET /api/v1/servers/{id}/activity. */
fun parseEvents(body: String): List<Event> {
    val arr = JSONObject(body).getJSONArray("events")
    return (0 until arr.length()).mapNotNull {
        val o = arr.getJSONObject(it)
        val t = parseTime(o.optString("time")) ?: return@mapNotNull null
        Event(t, o.optString("kind"), o.optString("text"), o.optInt("repeats"))
    }
}

data class RecentPlayer(val name: String, val seen: Instant?, val playedMins: Int)

data class Players(
    val online: List<String>,
    val rejected: List<String>,
    val whitelistOn: Boolean,
    val whitelist: List<String>,
    val ops: List<String>,
    val bans: List<String>,
    val recent: List<RecentPlayer>,
    val bedrock: Boolean,
)

/** Reads the body of GET /api/v1/servers/{id}/players. */
fun parsePlayers(body: String): Players {
    val o = JSONObject(body)
    val recent = o.optJSONArray("recent") ?: JSONArray()
    return Players(
        online = strings(o.optJSONArray("online")),
        rejected = strings(o.optJSONArray("rejected")),
        whitelistOn = o.optBoolean("whitelistOn"),
        whitelist = strings(o.optJSONArray("whitelist")),
        ops = strings(o.optJSONArray("ops")),
        bans = strings(o.optJSONArray("bans")),
        recent = (0 until recent.length()).map {
            val r = recent.getJSONObject(it)
            RecentPlayer(r.optString("name"), parseTime(r.optString("seen")), r.optInt("playedMins"))
        },
        bedrock = o.optBoolean("bedrock"),
    )
}

private fun strings(arr: JSONArray?): List<String> =
    if (arr == null) emptyList() else (0 until arr.length()).map { arr.getString(it) }

private fun parseTime(s: String): Instant? = try {
    Instant.parse(s)
} catch (e: Exception) {
    null
}

/** A short line such as "CPU 12% · Memory 2.0 GB of 4.0 GB · TPS 19.9". */
fun usageLine(u: Usage, limitMB: Int, locale: Locale = Locale.getDefault()): String = buildList {
    if (u.cpu >= 0) add(String.format(locale, "CPU %.0f%%", u.cpu))
    add(if (limitMB > 0) "Memory ${size(u.memoryMB, locale)} of ${size(limitMB, locale)}" else "Memory ${size(u.memoryMB, locale)}")
    if (u.tps > 0) add(String.format(locale, "TPS %.1f", u.tps))
}.joinToString(" · ")

private fun size(mb: Int, locale: Locale): String =
    if (mb >= 1024) String.format(locale, "%.1f GB", mb / 1024.0) else "$mb MB"

/** "3h 12m", "45m" or "under a minute": how long something has run. */
fun durationText(mins: Long): String = when {
    mins < 1 -> "under a minute"
    mins < 60 -> "${mins}m"
    mins < 24 * 60 -> "${mins / 60}h ${mins % 60}m"
    else -> "${mins / (24 * 60)}d ${mins / 60 % 24}h"
}
