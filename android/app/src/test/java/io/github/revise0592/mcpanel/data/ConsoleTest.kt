package io.github.revise0592.mcpanel.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ConsoleTest {
    @Test
    fun sortsLinesLikeTheWebConsole() {
        val c = LineClassifier()
        val kinds = listOf(
            "[McPanel] > say hi",
            "[McPanel] Server started",
            "[10:00:00 INFO]: <Steve> hello",
            "[10:00:01 WARN]: Can't keep up!",
            "[10:00:02 ERROR]: Could not pass event",
            "\tat com.example.Plugin.run(Plugin.java:12)",
            "Caused by: java.io.IOException",
            "[10:00:03 INFO]: Done",
            "\tindented but not after a problem",
        ).map(c::classify)
        assertEquals(
            listOf(
                LineKind.Command, LineKind.Panel, LineKind.Chat, LineKind.Warning, LineKind.Error,
                LineKind.Error, LineKind.Error, LineKind.Plain, LineKind.Plain,
            ),
            kinds,
        )
    }

    @Test
    fun filters() {
        assertFalse(ConsoleFilter.NoChat.shows(LineKind.Chat))
        assertTrue(ConsoleFilter.NoChat.shows(LineKind.Plain))
        assertTrue(ConsoleFilter.Problems.shows(LineKind.Warning))
        assertFalse(ConsoleFilter.Problems.shows(LineKind.Command))
    }
}
