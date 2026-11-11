package im.grix.widget

/**
 * Outbound events sent by `/public/widget/app.html` to the host app via `window.GrixBridge.postMessage(json)`.
 */
sealed class GrixBridgeEvent {
    data class Ready(val visitorKey: String, val sessionId: String) : GrixBridgeEvent()
    data object Close : GrixBridgeEvent()
    data class SessionClosed(val reason: String) : GrixBridgeEvent()
    data class Unread(val count: Int) : GrixBridgeEvent()
    data class OpenUrl(val url: String) : GrixBridgeEvent()
    data class Error(val code: String, val message: String) : GrixBridgeEvent()

    companion object {
        /**
         * Parses a JSON object string emitted by `window.GrixBridge.postMessage(JSON.stringify(evt))`
         * without relying on Android platform `org.json.JSONObject` stubs in local JVM unit tests.
         */
        fun parse(rawJson: String?): GrixBridgeEvent? {
            val text = rawJson?.trim().orEmpty()
            if (!text.startsWith("{") || !text.endsWith("}")) {
                return null
            }
            val fields = parseFlatJsonObject(text)
            return when (fields["type"]) {
                "ready" -> Ready(
                    visitorKey = fields["visitor_key"].orEmpty(),
                    sessionId = fields["session_id"].orEmpty()
                )
                "close" -> Close
                "session_closed" -> {
                    val reason = fields["reason"]?.ifEmpty { "closed" } ?: "closed"
                    SessionClosed(reason)
                }
                "unread" -> {
                    val rawCount = fields["count"]?.toDoubleOrNull()?.toInt() ?: 0
                    Unread(maxOf(0, rawCount))
                }
                "open_url" -> {
                    val url = fields["url"]?.trim().orEmpty()
                    if (url.startsWith("http://", ignoreCase = true) ||
                        url.startsWith("https://", ignoreCase = true)
                    ) {
                        OpenUrl(url)
                    } else {
                        null
                    }
                }
                "error" -> Error(
                    code = fields["code"]?.ifEmpty { "unknown" } ?: "unknown",
                    message = fields["message"]?.ifEmpty { "Unknown error" } ?: "Unknown error"
                )
                else -> null
            }
        }

        internal fun parseFlatJsonObject(json: String): Map<String, String> {
            val result = mutableMapOf<String, String>()
            var i = 1
            val n = json.length - 1

            fun skipWhitespace() {
                while (i < n && json[i].isWhitespace()) i++
            }

            fun readString(): String? {
                if (i >= n || json[i] != '"') return null
                i++
                val sb = StringBuilder()
                while (i < n) {
                    val c = json[i++]
                    if (c == '"') return sb.toString()
                    if (c == '\\' && i < n) {
                        when (val esc = json[i++]) {
                            '"', '\\', '/' -> sb.append(esc)
                            'b' -> sb.append('\b')
                            'f' -> sb.append('\u000C')
                            'n' -> sb.append('\n')
                            'r' -> sb.append('\r')
                            't' -> sb.append('\t')
                            'u' -> {
                                if (i + 4 <= n) {
                                    val hex = json.substring(i, i + 4)
                                    hex.toIntOrNull(16)?.let { sb.append(it.toChar()) }
                                    i += 4
                                }
                            }
                            else -> sb.append(esc)
                        }
                    } else {
                        sb.append(c)
                    }
                }
                return null
            }

            fun readPrimitive(): String {
                val start = i
                while (i < n && json[i] != ',' && json[i] != '}') {
                    i++
                }
                return json.substring(start, i).trim()
            }

            while (i < n) {
                skipWhitespace()
                if (i >= n || json[i] == '}') break
                if (json[i] == ',') {
                    i++
                    continue
                }
                val key = readString() ?: break
                skipWhitespace()
                if (i >= n || json[i] != ':') break
                i++ // skip ':'
                skipWhitespace()
                if (i >= n) break
                val value = if (json[i] == '"') {
                    readString().orEmpty()
                } else {
                    val prim = readPrimitive()
                    if (prim == "null") "" else prim
                }
                result[key] = value
                skipWhitespace()
                if (i < n && json[i] == ',') i++
            }
            return result
        }
    }
}
