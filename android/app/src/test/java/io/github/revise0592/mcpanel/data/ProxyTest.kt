package io.github.revise0592.mcpanel.data

import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ProxyTest {
    private val panel = "https://panel.mcraid.uk"

    @Test
    fun recognisesPangolin() {
        // What Pangolin sends a browser that isn't signed in.
        val m = LoginProxy.message(
            panel,
            302,
            "https://pangolin.mcraid.uk/auth/resource/40876256-5e5c?redirect=https%3A%2F%2Fpanel.mcraid.uk%2F",
        )
        assertTrue(m, m!!.startsWith("Pangolin's login"))
        // And what it sends the app: a bare 401 with no redirect.
        assertTrue(LoginProxy.message(panel, 401, "")!!.contains("Pangolin"))
    }

    @Test
    fun recognisesCloudflareAccess() {
        val m = LoginProxy.message(panel, 302, "https://team.cloudflareaccess.com/cdn-cgi/access/login/panel.mcraid.uk")
        assertTrue(m!!.startsWith("Cloudflare Access"))
    }

    @Test
    fun leavesThePanelsOwnRedirectsAlone() {
        // An old panel without the API sends the app to its sign-in page.
        assertNull(LoginProxy.message(panel, 303, "/login"))
        // Plain HTTP sent to HTTPS on the same host.
        assertNull(LoginProxy.message("http://panel.mcraid.uk", 301, "https://panel.mcraid.uk/api/v1/pair"))
    }

    @Test
    fun anyOtherSiteIsALoginProxy() {
        assertTrue(LoginProxy.message(panel, 302, "https://sso.example.com/login")!!.startsWith("A login proxy"))
    }
}
