package pub.dhf.grix

import android.content.Context
import android.system.Os
import io.sentry.Sentry
import io.sentry.SentryEvent
import io.sentry.SentryOptions
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.io.FileOutputStream
import java.security.MessageDigest

/** Persistent beforeSend filter for Android Java, NDK crash, and ANR events. */
object NativeSentryEventDeduplicator {
    private const val FILE_NAME = "sentry-event-dedup-v2.json"
    private const val WINDOW_MS = 24 * 60 * 60 * 1000L
    private const val PENDING_WINDOW_MS = 5 * 60 * 1000L
    private const val MAX_ENTRIES = 512
    private const val MAX_BYTES = 256 * 1024L
    @Volatile private var installed = false

    @Synchronized
    fun install(context: Context) {
        if (installed || !Sentry.isEnabled()) return
        val options = Sentry.getCurrentScopes().options
        val previous = options.beforeSend
        val store = Store(context.filesDir)
        options.beforeSend = SentryOptions.BeforeSendCallback { event, hint ->
            val prepared = if (previous == null) event else previous.execute(event, hint)
            if (prepared != null && store.shouldSend(prepared)) prepared else null
        }
        installed = true
    }

    private class Store(directory: File) {
        private val stateFile = File(directory, FILE_NAME)
        private val lockFile = File(directory, "$FILE_NAME.lock")

        @Synchronized
        fun shouldSend(event: SentryEvent): Boolean {
            val fingerprint = fingerprint(event) ?: return true
            return try {
                lockFile.parentFile?.mkdirs()
                FileOutputStream(lockFile, true).channel.use { channel ->
                    channel.lock().use {
                        val now = System.currentTimeMillis()
                        val state = readState()
                        prune(state, now)
                        val duplicate = state.sent.has(fingerprint) || state.pending.has(fingerprint)
                        if (!duplicate) state.sent.put(fingerprint, now)
                        trim(state)
                        writeState(state)
                        !duplicate
                    }
                }
            } catch (_: Throwable) {
                // Observability must never crash the app. A broken cache fails open.
                true
            }
        }

        private fun readState(): State {
            if (!stateFile.isFile || stateFile.length() > MAX_BYTES) return State()
            return try {
                val root = JSONObject(stateFile.readText(Charsets.UTF_8))
                if (root.optInt("version") != 2) return State()
                State(
                    sent = root.optJSONObject("sent") ?: JSONObject(),
                    pending = root.optJSONObject("pending") ?: JSONObject(),
                )
            } catch (_: Throwable) {
                State()
            }
        }

        private fun prune(state: State, now: Long) {
            state.sent.keys().asSequence().toList().forEach { key ->
                val timestamp = state.sent.optLong(key, Long.MIN_VALUE)
                if (!isFingerprint(key) || timestamp == Long.MIN_VALUE ||
                    timestamp > now || now - timestamp >= WINDOW_MS
                ) {
                    state.sent.remove(key)
                }
            }
            state.pending.keys().asSequence().toList().forEach { key ->
                val value = state.pending.optJSONObject(key)
                val timestamp = value?.optLong("timestamp", Long.MIN_VALUE) ?: Long.MIN_VALUE
                if (!isFingerprint(key) || timestamp == Long.MIN_VALUE ||
                    timestamp > now || now - timestamp >= PENDING_WINDOW_MS
                ) {
                    state.pending.remove(key)
                }
            }
        }

        private fun trim(state: State) {
            data class Stored(val key: String, val timestamp: Long, val pending: Boolean)
            val entries = mutableListOf<Stored>()
            state.sent.keys().asSequence().forEach { key ->
                entries += Stored(key, state.sent.optLong(key), false)
            }
            state.pending.keys().asSequence().forEach { key ->
                entries += Stored(
                    key,
                    state.pending.optJSONObject(key)?.optLong("timestamp") ?: 0L,
                    true,
                )
            }
            if (entries.size <= MAX_ENTRIES) return
            entries.sortedBy { it.timestamp }.take(entries.size - MAX_ENTRIES).forEach { entry ->
                if (entry.pending) state.pending.remove(entry.key) else state.sent.remove(entry.key)
            }
        }

        private fun writeState(state: State) {
            val root = JSONObject()
                .put("version", 2)
                .put("sent", state.sent)
                .put("pending", state.pending)
            val temporary = File(
                stateFile.parentFile,
                "${stateFile.name}.${android.os.Process.myPid()}.${System.nanoTime()}.tmp",
            )
            try {
                FileOutputStream(temporary).use { output ->
                    output.write(root.toString().toByteArray(Charsets.UTF_8))
                    output.fd.sync()
                }
                Os.chmod(temporary.path, 0x180) // 0600
                Os.rename(temporary.path, stateFile.path)
                try {
                    FileOutputStream(stateFile.parentFile, true).fd.sync()
                } catch (_: Throwable) {
                    // Directory fsync support varies by Android version/filesystem.
                }
            } finally {
                if (temporary.exists()) temporary.delete()
            }
        }
    }

    private data class State(
        val sent: JSONObject = JSONObject(),
        val pending: JSONObject = JSONObject(),
    )

    private fun fingerprint(event: SentryEvent): String? {
        val identity = JSONObject()
        val exceptions = event.exceptions
        when {
            !exceptions.isNullOrEmpty() -> {
                val values = JSONArray()
                exceptions.forEach { exception ->
                    val frames = exception.stacktrace?.frames.orEmpty()
                    val frameValues = JSONArray()
                    frames.takeLast(8).forEach { frame ->
                        frameValues.put(
                            JSONObject()
                                .put("file", frame.filename ?: "")
                                .put("function", frame.function ?: "")
                                .put("line", frame.lineno ?: 0)
                                .put("column", frame.colno ?: 0),
                        )
                    }
                    values.put(
                        JSONObject()
                            .put("type", exception.type ?: "")
                            .put("value", normalize(exception.value ?: ""))
                            .put("frames", frameValues),
                    )
                }
                identity.put("exceptions", values)
            }
            event.message != null -> {
                val message = event.message?.formatted ?: event.message?.message ?: ""
                identity.put("message", normalize(message))
            }
            event.throwable != null -> {
                identity
                    .put("throwableType", event.throwable?.javaClass?.name ?: "")
                    .put("throwable", normalize(event.throwable.toString()))
            }
            event.fingerprints.isNullOrEmpty() -> return null
        }

        val source = JSONObject()
            .put("release", event.release ?: "")
            .put("platform", event.platform ?: "")
            .put("logger", event.logger ?: "")
            .put("level", event.level?.name?.lowercase() ?: "")
            .put(
                "fingerprint",
                JSONArray(event.fingerprints.orEmpty().map(::normalize)),
            )
            .put("identity", identity)
        val canonicalSource = canonicalJson(source)
        return MessageDigest.getInstance("SHA-256")
            .digest(canonicalSource.toByteArray(Charsets.UTF_8))
            .joinToString("") { (it.toInt() and 0xff).toString(16).padStart(2, '0') }
    }

    private fun canonicalJson(value: Any?): String = when (value) {
        null, JSONObject.NULL -> "null"
        is JSONObject -> value.keys().asSequence().toList().sorted().joinToString(
            prefix = "{",
            postfix = "}",
        ) { key -> "${JSONObject.quote(key)}:${canonicalJson(value.opt(key))}" }
        is JSONArray -> (0 until value.length()).joinToString(
            prefix = "[",
            postfix = "]",
        ) { index -> canonicalJson(value.opt(index)) }
        is String -> JSONObject.quote(value)
        is Number, is Boolean -> value.toString()
        else -> JSONObject.quote(value.toString())
    }

    private fun normalize(value: String): String {
        return value
            .replace(
                Regex(
                    "\\b(session(?:_id)?|sid|message_id|mid|trace(?:_id)?|request(?:_id)?|rid|event(?:_id)?)\\s*[=:]\\s*[^\\s,;]+",
                    RegexOption.IGNORE_CASE,
                ),
                "\$1=<id>",
            )
            .replace(
                Regex(
                    "\\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\\b",
                    RegexOption.IGNORE_CASE,
                ),
                "<uuid>",
            )
            .replace(
                Regex(
                    "\\b\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}(?:\\.\\d+)?(?:Z|[+-]\\d{2}:?\\d{2})\\b",
                    RegexOption.IGNORE_CASE,
                ),
                "<timestamp>",
            )
            .replace(Regex("\\b0x[0-9a-f]{12,}\\b", RegexOption.IGNORE_CASE), "<address>")
            .replace(Regex("\\b[0-9a-f]{16,}\\b", RegexOption.IGNORE_CASE), "<id>")
            .replace(Regex("\\b\\d{13}\\b"), "<timestamp>")
    }

    private fun isFingerprint(value: String): Boolean = value.matches(Regex("^[0-9a-f]{64}$"))
}
