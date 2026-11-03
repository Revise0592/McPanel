package io.github.revise0592.mcpanel.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HeadersTest {
    @Test
    fun readsOnePerLine() {
        val r = Headers.parse("CF-Access-Client-Id: abc.access\n\n  CF-Access-Client-Secret:s3cr:et  \n")
        assertEquals(
            Headers.Result.Ok(mapOf("CF-Access-Client-Id" to "abc.access", "CF-Access-Client-Secret" to "s3cr:et")),
            r,
        )
        assertEquals(Headers.Result.Ok(emptyMap()), Headers.parse("  \n"))
    }

    @Test
    fun refusesBadLines() {
        assertTrue(Headers.parse("no colon here") is Headers.Result.Bad)
        assertTrue(Headers.parse("Bad Name: x") is Headers.Result.Bad)
        // The app's own token must not be replaced.
        assertTrue(Headers.parse("Authorization: Basic xyz") is Headers.Result.Bad)
    }

    @Test
    fun formatsBack() {
        val h = mapOf("A" to "1", "B" to "2")
        assertEquals(Headers.Result.Ok(h), Headers.parse(Headers.format(h)))
    }
}
