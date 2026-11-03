package io.github.revise0592.mcpanel.data

import java.net.URI
import java.net.URLDecoder

/** What a pairing QR code or link holds: mcpanel://pair?url=…&code=… */
data class PairingLink(val address: String, val code: String)

object Pairing {
    /** Reads a pairing link, or returns null if it isn't one. */
    fun parseLink(link: String): PairingLink? {
        val uri = try {
            URI(link.trim())
        } catch (e: Exception) {
            return null
        }
        if (!uri.scheme.equals("mcpanel", ignoreCase = true) || !uri.host.equals("pair", ignoreCase = true)) {
            return null
        }
        val params = uri.rawQuery.orEmpty().split('&').mapNotNull { part ->
            val i = part.indexOf('=')
            if (i <= 0) null else decode(part.substring(0, i)) to decode(part.substring(i + 1))
        }.toMap()
        val address = params["url"]?.takeIf { it.isNotBlank() } ?: return null
        return PairingLink(address, params["code"].orEmpty())
    }

    /**
     * Turns what the user typed into the panel's base address. Without a
     * scheme, a domain name such as "panel.example.com" gets https://
     * (it's most likely behind a reverse proxy), while an IP address, a
     * name with a port or a home network name gets http://, as in
     * "192.168.1.10:8080". Returns null if it can't be an address.
     */
    fun normalizeAddress(input: String): String? {
        var s = input.trim()
        if (s.isEmpty()) return null
        if (!s.contains("://")) {
            val probe = try {
                URI("http://$s")
            } catch (e: Exception) {
                return null
            }
            s = (if (looksLocal(probe)) "http://" else "https://") + s
        }
        val uri = try {
            URI(s)
        } catch (e: Exception) {
            return null
        }
        val scheme = uri.scheme?.lowercase()
        if (scheme != "http" && scheme != "https") return null
        if (uri.host.isNullOrEmpty()) return null
        return "$scheme://${uri.rawAuthority}"
    }

    private fun looksLocal(uri: URI): Boolean {
        val host = uri.host?.lowercase() ?: return true
        return uri.port != -1 ||
            host.startsWith("[") ||
            host.all { it.isDigit() || it == '.' } ||
            !host.contains('.') ||
            host.endsWith(".local") || host.endsWith(".lan") || host.endsWith(".home.arpa")
    }

    private fun decode(s: String): String = URLDecoder.decode(s, "UTF-8")
}
