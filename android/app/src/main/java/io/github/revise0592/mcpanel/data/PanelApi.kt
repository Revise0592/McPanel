package io.github.revise0592.mcpanel.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.net.ConnectException
import java.net.HttpURLConnection
import java.net.SocketTimeoutException
import java.net.URI
import java.net.URL
import java.net.UnknownHostException
import javax.net.ssl.SSLException
import kotlin.concurrent.thread

/** A problem talking to a panel, with a message fit to show the user. */
class ApiException(
    message: String,
    val status: Int = 0,
    /** A login proxy in front of the panel answered, not the panel. */
    val proxy: Boolean = false,
) : Exception(message) {
    /** The panel doesn't know this phone (any more). */
    val unauthorized: Boolean get() = status == 401 && !proxy
}

data class PairResult(val token: String, val deviceId: String, val panel: PanelInfo)

/** Where and how to reach one panel. */
data class Conn(val address: String, val token: String?, val headers: Map<String, String> = emptyMap())

/** Calls a panel's /api/v1. See docs/mobile-api.md in the repo. */
object PanelApi {
    private const val TIMEOUT_MS = 10_000

    /** The console sends a ping every 25 seconds; give it some slack. */
    private const val STREAM_READ_TIMEOUT_MS = 60_000

    private const val NOT_MCPANEL =
        "That doesn't look like a McPanel with the mobile app API. Check the address, and update McPanel if it's older."

    suspend fun pair(conn: Conn, code: String, deviceName: String): PairResult = withContext(Dispatchers.IO) {
        val body = JSONObject().put("code", code).put("name", deviceName).toString()
        val text = request(conn, "POST", "/api/v1/pair", body)
        json {
            val o = JSONObject(text)
            PairResult(
                token = o.getString("token"),
                deviceId = o.getJSONObject("device").getString("id"),
                panel = parsePanelInfo(o.getJSONObject("panel")),
            )
        }
    }

    suspend fun me(conn: Conn): PanelInfo = get(conn, "/api/v1/me") { parsePanelInfo(JSONObject(it).getJSONObject("panel")) }

    suspend fun unpair(conn: Conn) {
        withContext(Dispatchers.IO) { request(conn, "POST", "/api/v1/unpair", null) }
    }

    suspend fun servers(conn: Conn): List<Server> = get(conn, "/api/v1/servers", ::parseServers)

    suspend fun server(conn: Conn, id: String): Server =
        get(conn, "/api/v1/servers/${path(id)}") { parseServer(JSONObject(it)) }

    /** [action] is start, stop, restart or kill. */
    suspend fun power(conn: Conn, id: String, action: String): Server = withContext(Dispatchers.IO) {
        val text = request(conn, "POST", "/api/v1/servers/${path(id)}/power/$action", null)
        json { parseServer(JSONObject(text)) }
    }

    suspend fun command(conn: Conn, id: String, command: String) {
        withContext(Dispatchers.IO) {
            request(conn, "POST", "/api/v1/servers/${path(id)}/command", JSONObject().put("command", command).toString())
        }
    }

    suspend fun activity(conn: Conn, id: String, limit: Int = 50): List<Event> =
        get(conn, "/api/v1/servers/${path(id)}/activity?limit=$limit", ::parseEvents)

    suspend fun players(conn: Conn, id: String): Players = get(conn, "/api/v1/servers/${path(id)}/players", ::parsePlayers)

    /** Does one of the Players page's actions and returns the lists afterwards. */
    suspend fun playersAction(conn: Conn, id: String, action: String, name: String): Players = withContext(Dispatchers.IO) {
        val body = JSONObject().put("action", action).put("name", name).toString()
        val text = request(conn, "POST", "/api/v1/servers/${path(id)}/players", body)
        json { parsePlayers(text) }
    }

    /** The panel's logo as PNG bytes. [logo] is the path from [PanelInfo.logo]. */
    suspend fun logo(conn: Conn, logo: String): ByteArray = withContext(Dispatchers.IO) {
        val c = open(conn, "GET", logo, TIMEOUT_MS)
        try {
            val status = c.responseCode
            if (status != 200) throw ApiException("Couldn't load the logo (error $status).", status)
            c.inputStream.use { it.readBytes() }
        } catch (e: IOException) {
            throw friendly(e)
        } finally {
            c.disconnect()
        }
    }

    /**
     * The live console: the recent backlog, then each new line. The flow
     * ends with an [ApiException] when the connection drops.
     */
    fun console(conn: Conn, id: String): Flow<String> = callbackFlow {
        val c = open(conn, "GET", "/api/v1/servers/${path(id)}/console", STREAM_READ_TIMEOUT_MS)
        c.setRequestProperty("Accept", "text/event-stream")
        // A blocking read can't be interrupted, so read on a thread of its
        // own; disconnecting ends it.
        val reader = thread(name = "console-$id", isDaemon = true) {
            try {
                ensureOk(conn, c)
                c.inputStream.bufferedReader().useLines { lines ->
                    for (line in lines) {
                        // One "data:" line per event; ": ping" keeps it open.
                        if (line.startsWith("data:")) trySend(line.removePrefix("data:").removePrefix(" "))
                    }
                }
                close(ApiException("The panel closed the console."))
            } catch (e: ApiException) {
                close(e)
            } catch (e: IOException) {
                close(friendly(e))
            }
        }
        awaitClose {
            c.disconnect()
            reader.interrupt()
        }
    }

    private suspend fun <T> get(conn: Conn, path: String, parse: (String) -> T): T = withContext(Dispatchers.IO) {
        val text = request(conn, "GET", path, null)
        json { parse(text) }
    }

    private fun path(segment: String): String = java.net.URLEncoder.encode(segment, "UTF-8").replace("+", "%20")

    private inline fun <T> json(parse: () -> T): T = try {
        parse()
    } catch (e: JSONException) {
        throw ApiException(NOT_MCPANEL)
    }

    private fun open(conn: Conn, method: String, path: String, readTimeout: Int): HttpURLConnection {
        val c = try {
            URL(conn.address + path).openConnection() as HttpURLConnection
        } catch (e: Exception) {
            throw ApiException("That isn't a valid address.")
        }
        c.requestMethod = method
        c.connectTimeout = TIMEOUT_MS
        c.readTimeout = readTimeout
        // An old panel, or a login proxy, would send the app to a sign-in
        // page; say so instead.
        c.instanceFollowRedirects = false
        conn.headers.forEach { (k, v) -> c.setRequestProperty(k, v) }
        c.setRequestProperty("Accept", "application/json")
        conn.token?.let { c.setRequestProperty("Authorization", "Bearer $it") }
        return c
    }

    /** Throws if the answer isn't a success from the panel itself. */
    private fun ensureOk(conn: Conn, c: HttpURLConnection) {
        val status = c.responseCode
        val type = c.contentType.orEmpty()
        val location = c.getHeaderField("Location").orEmpty()
        if (status in 300..399 || ((status == 401 || status == 403) && !type.startsWith("application/json"))) {
            throw ApiException(
                LoginProxy.message(conn.address, status, location)
                    ?: if (location.startsWith("https://") && conn.address.startsWith("http://")) {
                        "The panel wants a secure connection. Use its https:// address instead."
                    } else {
                        NOT_MCPANEL
                    },
                status,
                proxy = status in 300..399 || status == 401 || status == 403,
            )
        }
        if (status >= 400) {
            val text = c.errorStream?.bufferedReader()?.use { it.readText() }.orEmpty()
            throw ApiException(errorMessage(status, text), status)
        }
    }

    private fun request(conn: Conn, method: String, path: String, body: String?): String {
        val c = open(conn, method, path, TIMEOUT_MS)
        try {
            if (body != null) {
                c.doOutput = true
                c.setRequestProperty("Content-Type", "application/json")
                c.outputStream.use { it.write(body.toByteArray()) }
            }
            ensureOk(conn, c)
            return c.inputStream.bufferedReader().use { it.readText() }
        } catch (e: IOException) {
            throw friendly(e)
        } finally {
            c.disconnect()
        }
    }

    private fun friendly(e: IOException): ApiException = when (e) {
        is UnknownHostException ->
            ApiException("Couldn't find that address. Check it, and that your phone is on a network that can reach the panel.")
        is ConnectException ->
            ApiException("Couldn't connect. Check that the panel is running and that this phone can reach it.")
        is SocketTimeoutException -> ApiException("The panel took too long to answer.")
        is SSLException -> ApiException("Couldn't make a secure connection: ${e.message}")
        else -> ApiException("Couldn't reach the panel: ${e.message}")
    }

    private fun errorMessage(status: Int, text: String): String {
        val msg = try {
            JSONObject(text).optString("error")
        } catch (e: JSONException) {
            ""
        }
        return when {
            msg.isNotEmpty() -> msg
            status == 404 || status == 405 -> NOT_MCPANEL
            else -> "The panel answered with error $status."
        }
    }
}

/**
 * Recognises login proxies (Pangolin, Cloudflare Access, Authelia and the
 * like) that answer before the panel does, and says how to let the app
 * through.
 */
object LoginProxy {
    private const val GENERIC =
        "A login proxy in front of the panel answered instead of McPanel. If it's Pangolin, open this resource's " +
            "Rules, turn rules on, and add a rule that bypasses auth for the path /api/v1/*. With other proxies, " +
            "let /api/v1/* through the same way, or add the header the proxy wants under Extra headers. The " +
            "panel's own pairing still protects the API."

    /** A message for this answer, or null if it doesn't look like a login proxy. */
    fun message(address: String, status: Int, location: String): String? {
        val host = hostOf(address)
        val target = if (location.isEmpty()) null else URI(address + "/").resolveOrNull(location)
        val toHost = target?.host.orEmpty().lowercase()
        val toPath = target?.path.orEmpty()
        return when {
            toHost.endsWith("cloudflareaccess.com") || toPath.startsWith("/cdn-cgi/access/") ->
                "Cloudflare Access is in front of the panel. Add a Bypass policy for /api/v1/*, or make a service " +
                    "token and add its CF-Access-Client-Id and CF-Access-Client-Secret under Extra headers."
            toPath.startsWith("/auth/resource/") || "pangolin" in toHost ->
                "Pangolin's login is in front of the panel. In Pangolin, open this resource's Rules, turn rules on, " +
                    "and add a rule that bypasses auth for the path /api/v1/*. The panel's own pairing still " +
                    "protects it."
            "authelia" in toHost || "authentik" in toHost ->
                "A login portal is in front of the panel. Add a bypass rule for /api/v1/* on this address. The " +
                    "panel's own pairing still protects it."
            target != null && toHost.isNotEmpty() && toHost != host -> GENERIC
            // Pangolin answers requests without a browser with a bare
            // "Unauthorized"; the panel always answers in JSON.
            status == 401 || status == 403 -> GENERIC
            else -> null
        }
    }

    private fun hostOf(address: String): String = try {
        URI(address).host.orEmpty().lowercase()
    } catch (e: Exception) {
        ""
    }

    private fun URI.resolveOrNull(s: String): URI? = try {
        resolve(s)
    } catch (e: Exception) {
        null
    }
}
