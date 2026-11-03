package io.github.revise0592.mcpanel.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Locale

class ModelsTest {
    @Test
    fun readsTheServerList() {
        // Shaped like the panel's answer; see docs/mobile-api.md.
        val body = """
            {"servers":[
              {"id":"survival-3fa2c1","name":"Survival","type":"paper","typeLabel":"Paper","version":"1.21.4",
               "installed":true,"state":"running","label":"Online","progress":0,"startedAt":"2026-11-03T07:00:00Z",
               "players":["Steve","Alex"],"address":"mc.example.com:25565","memoryLimitMB":4096,
               "usage":{"cpu":12.5,"memoryMB":2048,"tps":19.98},"autoStart":true,"restartOnCrash":true,"sleepAfterMins":0},
              {"id":"creative-1","name":"Creative","type":"vanilla","typeLabel":"Vanilla","version":"1.21.4",
               "installed":false,"state":"stopped","label":"Offline","progress":0,"players":[],
               "autoStart":false,"restartOnCrash":false,"sleepAfterMins":0}
            ]}
        """.trimIndent()
        val servers = parseServers(body)
        assertEquals(2, servers.size)
        val s = servers[0]
        assertEquals("Survival", s.name)
        assertEquals(listOf("Steve", "Alex"), s.players)
        assertEquals(Usage(12.5, 2048, 19.98), s.usage)
        assertTrue(s.online)
        val c = servers[1]
        assertFalse(c.online)
        assertNull(c.usage)
        assertEquals("", c.address)
    }

    @Test
    fun describesUsage() {
        assertEquals(
            "CPU 13% · Memory 2.0 GB of 4.0 GB · TPS 20.0",
            usageLine(Usage(12.5, 2048, 19.98), 4096, Locale.ROOT),
        )
        // CPU unknown yet, no TPS, no memory limit.
        assertEquals("Memory 512 MB", usageLine(Usage(-1.0, 512, 0.0), 0, Locale.ROOT))
    }
}
