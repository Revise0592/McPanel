package io.github.revise0592.mcpanel.data

/** How a console line is coloured and filtered, as in the web UI's console. */
enum class LineKind { Plain, Command, Panel, Error, Warning, Chat }

data class ConsoleLine(val id: Long, val text: String, val kind: LineKind)

/** Which lines the console shows. */
enum class ConsoleFilter(val title: String) { All("All"), NoChat("No chat"), Problems("Only problems") }

fun ConsoleFilter.shows(kind: LineKind): Boolean = when (this) {
    ConsoleFilter.All -> true
    ConsoleFilter.NoChat -> kind != LineKind.Chat
    ConsoleFilter.Problems -> kind == LineKind.Error || kind == LineKind.Warning
}

/** Sorts console lines the way internal/web/static/app.js does. */
class LineClassifier {
    private var last = LineKind.Plain

    private val followOn = Regex("""^(\s|Caused by:|\.\.\. \d+ more)""")
    private val error = Regex("""\b(ERROR|SEVERE|Exception)\b""")
    private val warning = Regex("""\bWARN(ING)?\b""")
    private val chat = Regex("""\]: (\[Not Secure] )?<[^>\s]+> """)

    fun classify(line: String): LineKind {
        val kind = when {
            // Stack traces take the kind of the line they belong to, so
            // "Only problems" shows them whole.
            (last == LineKind.Error || last == LineKind.Warning) && followOn.containsMatchIn(line) -> last
            line.startsWith("[McPanel] > ") -> LineKind.Command
            line.startsWith("[McPanel]") -> LineKind.Panel
            error.containsMatchIn(line) -> LineKind.Error
            warning.containsMatchIn(line) -> LineKind.Warning
            chat.containsMatchIn(line) -> LineKind.Chat
            else -> LineKind.Plain
        }
        last = kind
        return kind
    }
}
