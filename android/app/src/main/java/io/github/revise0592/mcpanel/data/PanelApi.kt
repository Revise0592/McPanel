package io.github.revise0592.mcpanel.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONException
import org.json.JSONObject
import java.io.IOException
import java.net.ConnectException
import java.net.HttpURLConnection
import java.net.SocketTimeoutException
import java.net.URL
import java.net.UnknownHostException
import javax.net.ssl.SSLException

/** A problem talking to a panel, with a message fit to show the user. */
class ApiException(message: String, val status: Int = 0) : Exception(message) {
    /** The panel doesn't know this phone (any more). */
    val unauthorized: Boolean get() = status == 401
}

data class PairResult(val token: String, val deviceId: String, val panelName: String)

/** Calls a panel's /api/v1. See docs/mobile-api.md in the repo. */
object PanelApi {
    private const val TIMEOUT_MS = 10_000
    private const val NOT_MCPANEL =
        "That doesn't look like a McPanel with the mobile app API. Check the address, and update McPanel if it's older."

    suspend fun pair(address: String, code: String, deviceName: String): PairResult = withContext(Dispatchers.IO) {
        val body = JSONObject().put("code", code).put("name", deviceName).toString()
        val text = request("POST", "$address/api/v1/pair", null, body)
        json {
            val o = JSONObject(text)
            PairResult(
                token = o.getString("token"),
                deviceId = o.getJSONObject("device").getString("id"),
                panelName = o.getJSONObject("panel").optString("name", "McPanel"),
            )
        }
    }

    suspend fun servers(address: String, token: String): List<Server> = withContext(Dispatchers.IO) {
        val text = request("GET", "$address/api/v1/servers", token, null)
        json { parseServers(text) }
    }

    suspend fun unpair(address: String, token: String) {
        withContext(Dispatchers.IO) { request("POST", "$address/api/v1/unpair", token, null) }
    }

    private inline fun <T> json(parse: () -> T): T = try {
        parse()
    } catch (e: JSONException) {
        throw ApiException(NOT_MCPANEL)
    }

    private fun request(method: String, url: String, token: String?, body: String?): String {
        val conn = try {
            URL(url).openConnection() as HttpURLConnection
        } catch (e: Exception) {
            throw ApiException("That isn't a valid address.")
        }
        try {
            conn.requestMethod = method
            conn.connectTimeout = TIMEOUT_MS
            conn.readTimeout = TIMEOUT_MS
            // An old panel sends the app to its sign-in page; say so instead.
            conn.instanceFollowRedirects = false
            conn.setRequestProperty("Accept", "application/json")
            if (token != null) conn.setRequestProperty("Authorization", "Bearer $token")
            if (body != null) {
                conn.doOutput = true
                conn.setRequestProperty("Content-Type", "application/json")
                conn.outputStream.use { it.write(body.toByteArray()) }
            }
            val status = conn.responseCode
            if (status in 300..399) {
                val to = conn.getHeaderField("Location").orEmpty()
                if (to.startsWith("https://") && url.startsWith("http://")) {
                    throw ApiException("The panel wants a secure connection. Use its https:// address instead.")
                }
                throw ApiException(NOT_MCPANEL)
            }
            val stream = if (status >= 400) conn.errorStream else conn.inputStream
            val text = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
            if (status >= 400) throw ApiException(errorMessage(status, text), status)
            return text
        } catch (e: ApiException) {
            throw e
        } catch (e: UnknownHostException) {
            throw ApiException("Couldn't find that address. Check it, and that your phone is on a network that can reach the panel.")
        } catch (e: ConnectException) {
            throw ApiException("Couldn't connect. Check that the panel is running and that this phone can reach it.")
        } catch (e: SocketTimeoutException) {
            throw ApiException("The panel took too long to answer.")
        } catch (e: SSLException) {
            throw ApiException("Couldn't make a secure connection: ${e.message}")
        } catch (e: IOException) {
            throw ApiException("Couldn't reach the panel: ${e.message}")
        } finally {
            conn.disconnect()
        }
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
