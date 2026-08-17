package pub.dhf.grix

import android.content.Intent
import android.net.Uri
import android.provider.OpenableColumns
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel
import java.io.ByteArrayOutputStream
import java.util.UUID

class TextDocumentBridge(private val activity: MainActivity) {
    companion object {
        private const val CHANNEL = "pub.dhf.grix/text_document"
        private const val MAX_PREVIEW_BYTES = 10 * 1024 * 1024
    }

    private val handles = mutableMapOf<String, Uri>()
    private var channel: MethodChannel? = null
    private var pendingPayload: Map<String, Any?>? = null
    private var dartReady = false

    fun configure(messenger: BinaryMessenger) {
        channel = MethodChannel(messenger, CHANNEL).also { methodChannel ->
            methodChannel.setMethodCallHandler { call, result ->
                when (call.method) {
                    "getInitialDocument" -> {
                        result.success(pendingPayload)
                        pendingPayload = null
                        dartReady = true
                    }
                    "writeDocument" -> {
                        val handle = call.argument<String>("handle")
                        val bytes = call.argument<ByteArray>("bytes")
                        if (handle.isNullOrBlank() || bytes == null) {
                            result.error("invalid_args", "Missing handle or bytes", null)
                            return@setMethodCallHandler
                        }
                        write(handle, bytes, result)
                    }
                    "closeDocument" -> {
                        call.argument<String>("handle")?.let(handles::remove)
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            }
        }
    }

    fun handleIntent(intent: Intent?) {
        if (intent?.action != Intent.ACTION_VIEW) return
        // 来源确认：只处理外部应用通过 FLAG_GRANT_READ_URI_PERMISSION 显式授权的
        // "打开方式"请求；没有读授权的 VIEW intent 一律拒绝，避免任意应用伪造
        // intent 让本应用读取并展示其本无权访问的 content:// 文档。
        if (intent.flags and Intent.FLAG_GRANT_READ_URI_PERMISSION == 0) return
        val uri = intent.data ?: return
        if (uri.scheme != "content" && uri.scheme != "file") return
        try {
            val payload = readPayload(uri, intent)
            if (!dartReady || channel == null) {
                pendingPayload = payload
            } else {
                channel?.invokeMethod("documentOpened", payload)
            }
        } catch (_: Exception) {
            // Invalid, revoked, binary, and over-limit inputs are surfaced by
            // the Dart page only after a valid payload can be constructed.
        }
    }

    private fun readPayload(uri: Uri, intent: Intent): Map<String, Any?> {
        var displayName = uri.lastPathSegment ?: "document.txt"
        var size: Long? = null
        activity.contentResolver.query(
            uri,
            arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE),
            null,
            null,
            null,
        )?.use { cursor ->
            if (cursor.moveToFirst()) {
                val nameIndex = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                val sizeIndex = cursor.getColumnIndex(OpenableColumns.SIZE)
                if (nameIndex >= 0) displayName = cursor.getString(nameIndex) ?: displayName
                if (sizeIndex >= 0 && !cursor.isNull(sizeIndex)) size = cursor.getLong(sizeIndex)
            }
        }
        if (size != null && size!! > MAX_PREVIEW_BYTES) {
            throw IllegalArgumentException("text_document_too_large")
        }
        val bytes = activity.contentResolver.openInputStream(uri)?.use { input ->
            val output = ByteArrayOutputStream()
            val buffer = ByteArray(8192)
            var total = 0
            while (true) {
                val read = input.read(buffer)
                if (read < 0) break
                total += read
                if (total > MAX_PREVIEW_BYTES) {
                    throw IllegalArgumentException("text_document_too_large")
                }
                output.write(buffer, 0, read)
            }
            output.toByteArray()
        } ?: throw IllegalArgumentException("Unable to read document")

        val handle = UUID.randomUUID().toString()
        handles[handle] = uri
        val canWrite = intent.flags and Intent.FLAG_GRANT_WRITE_URI_PERMISSION != 0
        return mapOf(
            "descriptor" to mapOf(
                "handle" to handle,
                "displayName" to displayName,
                "mimeType" to (activity.contentResolver.getType(uri) ?: intent.type ?: "text/plain"),
                "canWrite" to canWrite,
                "source" to "androidContentUri",
                "byteLength" to bytes.size,
            ),
            "bytes" to bytes,
        )
    }

    private fun write(handle: String, bytes: ByteArray, result: MethodChannel.Result) {
        val uri = handles[handle]
        if (uri == null) {
            result.error("document_closed", "The document is no longer available", null)
            return
        }
        try {
            activity.contentResolver.openOutputStream(uri, "wt")?.use { output ->
                output.write(bytes)
                output.flush()
            } ?: throw IllegalStateException("Provider does not support writing")
            result.success(null)
        } catch (error: Exception) {
            result.error("save_failed", error.message, null)
        }
    }
}
