package io.github.revise0592.mcpanel.data

/**
 * Extra HTTP headers sent with every request to a panel, for login proxies
 * that take a token in a header (such as Cloudflare Access service tokens
 * or a Pangolin access token). Typed one per line as "Name: value".
 */
object Headers {
    /** Headers the app sets itself, which can't be overridden. */
    private val reserved = setOf("authorization", "content-type", "accept", "content-length", "host")
    private val nameChars = Regex("^[A-Za-z0-9!#$%&'*+.^_`|~-]+$")

    sealed interface Result {
        data class Ok(val headers: Map<String, String>) : Result
        data class Bad(val message: String) : Result
    }

    fun parse(text: String): Result {
        val out = linkedMapOf<String, String>()
        text.lines().forEachIndexed { i, raw ->
            val line = raw.trim()
            if (line.isEmpty()) return@forEachIndexed
            val colon = line.indexOf(':')
            if (colon <= 0) return Result.Bad("Line ${i + 1} should look like Name: value.")
            val name = line.substring(0, colon).trim()
            val value = line.substring(colon + 1).trim()
            if (!nameChars.matches(name)) return Result.Bad("“$name” on line ${i + 1} isn't a valid header name.")
            if (name.lowercase() in reserved) return Result.Bad("The app sets $name itself, so it can't be added.")
            if (value.any { it < ' ' && it != '\t' }) return Result.Bad("The value on line ${i + 1} has characters a header can't hold.")
            out[name] = value
        }
        return Result.Ok(out)
    }

    fun format(headers: Map<String, String>): String =
        headers.entries.joinToString("\n") { "${it.key}: ${it.value}" }
}
