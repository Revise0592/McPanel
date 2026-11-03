package io.github.revise0592.mcpanel.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PairingTest {
    @Test
    fun readsTheLinkFromThePanelsQrCode() {
        // As the panel builds it with url.Values.Encode().
        val link = Pairing.parseLink("mcpanel://pair?code=JHYRVNWP&url=https%3A%2F%2Fpanel.example.com%3A8443")
        assertEquals(PairingLink("https://panel.example.com:8443", "JHYRVNWP"), link)
    }

    @Test
    fun ignoresOtherLinks() {
        assertNull(Pairing.parseLink("https://pair?url=x&code=y"))
        assertNull(Pairing.parseLink("mcpanel://other?url=x&code=y"))
        assertNull(Pairing.parseLink("mcpanel://pair?code=ABC"))
        assertNull(Pairing.parseLink("not a link at all"))
    }

    @Test
    fun normalizesTypedAddresses() {
        mapOf(
            "192.168.1.10:8080" to "http://192.168.1.10:8080",
            "  http://192.168.1.10:8080/  " to "http://192.168.1.10:8080",
            "HTTPS://Panel.example.com/settings/mobile" to "https://Panel.example.com",
            "http://[::1]:8080" to "http://[::1]:8080",
            // Domain names are most likely behind a reverse proxy with HTTPS.
            "panel.mcraid.uk" to "https://panel.mcraid.uk",
            "panel.example.com/settings" to "https://panel.example.com",
            "panel.example.com:8080" to "http://panel.example.com:8080",
            "minecraft-box" to "http://minecraft-box",
            "mc.local" to "http://mc.local",
        ).forEach { (typed, want) -> assertEquals(typed, want, Pairing.normalizeAddress(typed)) }
        listOf("", "   ", "ftp://example.com", "http://", "exa mple.com").forEach {
            assertNull(it, Pairing.normalizeAddress(it))
        }
    }
}
