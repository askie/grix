package pub.dhf.grix

import android.content.Intent
import android.net.Uri
import android.provider.OpenableColumns
import androidx.lifecycle.lifecycleScope
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.io.FileOutputStream
import java.util.UUID

class ShareBridge(private val activity: MainActivity) {
    companion object {
        private const val METHOD_CHANNEL = "grix/share_ingest"
        private const val EVENT_CHANNEL = "grix/share_ingest_events"
        private const val MAX_SHARE_FILE_BYTES = 50L * 1024L * 1024L
        const val EXTRA_SHARE_INGEST_CONSUMED = "grix.share_ingest.consumed"
    }

    private var methodChannel: MethodChannel? = null
    private var eventChannel: EventChannel? = null
    private var eventSink: EventChannel.EventSink? = null

    fun configure(messenger: BinaryMessenger) {
        methodChannel = MethodChannel(messenger, METHOD_CHANNEL).also { channel ->
            channel.setMethodCallHandler { call, result ->
                when (call.method) {
                    "consumePending" -> {
                        val manifests = ShareInboxStorage.listPendingManifests(activity)
                            .map(ShareInboxStorage::jsonObjectToMap)
                        result.success(manifests)
                    }
                    "deleteEntry" -> {
                        val id = call.argument<String>("id")?.trim() ?: ""
                        if (id.isNotEmpty()) {
                            ShareInboxStorage.deleteEntry(activity, id)
                        }
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            }
        }
        eventChannel = EventChannel(messenger, EVENT_CHANNEL).also { channel ->
            channel.setStreamHandler(
                object : EventChannel.StreamHandler {
                    override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
                        eventSink = events
                    }

                    override fun onCancel(arguments: Any?) {
                        eventSink = null
                    }
                },
            )
        }
    }

    fun handleIntent(intent: Intent?) {
        if (intent == null) return
        if (intent.getBooleanExtra(EXTRA_SHARE_INGEST_CONSUMED, false)) {
            return
        }
        when (intent.action) {
            Intent.ACTION_SEND, Intent.ACTION_SEND_MULTIPLE -> {
                activity.lifecycleScope.launch(Dispatchers.IO) {
                    try {
                        when (intent.action) {
                            Intent.ACTION_SEND -> ingestSend(intent)
                            Intent.ACTION_SEND_MULTIPLE -> ingestSendMultiple(intent)
                        }
                    } finally {
                        activity.runOnUiThread {
                            intent.putExtra(EXTRA_SHARE_INGEST_CONSUMED, true)
                            activity.setIntent(intent)
                        }
                    }
                }
            }
        }
    }

    private fun ingestSend(intent: Intent) {
        val entryId = UUID.randomUUID().toString()
        val dir = ShareInboxStorage.entryDir(activity, entryId)
        dir.mkdirs()
        val items = JSONArray()
        var skippedCount = 0
        val stream = intent.getParcelableExtra<Uri>(Intent.EXTRA_STREAM)
        if (stream != null) {
            skippedCount += copyUriIntoEntry(stream, intent.type, items, 0, dir)
        } else {
            appendTextItem(intent.getStringExtra(Intent.EXTRA_TEXT), items)
        }
        commitEntry(entryId, dir, items, skippedCount, intent.`package`)
    }

    private fun ingestSendMultiple(intent: Intent) {
        val streams = intent.getParcelableArrayListExtra<Uri>(Intent.EXTRA_STREAM)
        if (streams.isNullOrEmpty()) return
        val entryId = UUID.randomUUID().toString()
        val dir = ShareInboxStorage.entryDir(activity, entryId)
        dir.mkdirs()
        val items = JSONArray()
        var skippedCount = 0
        var index = 0
        for (uri in streams) {
            if (uri == null) continue
            skippedCount += copyUriIntoEntry(uri, intent.type, items, index, dir)
            index++
        }
        commitEntry(entryId, dir, items, skippedCount, intent.`package`)
    }

    private fun commitEntry(
        entryId: String,
        dir: File,
        items: JSONArray,
        skippedCount: Int,
        source: String?,
    ) {
        if (items.length() == 0 && skippedCount == 0) {
            dir.deleteRecursively()
            return
        }
        val manifest = JSONObject()
            .put("id", entryId)
            .put("created_at", System.currentTimeMillis())
            .put("items", items)
            .put("skipped_count", skippedCount)
        if (!source.isNullOrBlank()) {
            manifest.put("source", source.trim())
        }
        File(dir, ShareInboxStorage.MANIFEST_FILE_NAME).writeText(manifest.toString())
        notifyFlutter(entryId)
    }

    private fun appendTextItem(text: String?, items: JSONArray) {
        val normalized = text?.trim() ?: ""
        if (normalized.isEmpty()) return
        val itemType =
            if (normalized.startsWith("http://") || normalized.startsWith("https://")) {
                "url"
            } else {
                "text"
            }
        items.put(
            JSONObject()
                .put("type", itemType)
                .put("text", normalized),
        )
    }

    /**
     * @return number of files skipped (oversize or read failure).
     */
    private fun copyUriIntoEntry(
        uri: Uri,
        fallbackMime: String?,
        items: JSONArray,
        index: Int,
        dir: File,
    ): Int {
        var displayName = uri.lastPathSegment ?: "shared_file"
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
                if (nameIndex >= 0) {
                    displayName = cursor.getString(nameIndex) ?: displayName
                }
                if (sizeIndex >= 0 && !cursor.isNull(sizeIndex)) {
                    size = cursor.getLong(sizeIndex)
                }
            }
        }
        if (size != null && size!! > MAX_SHARE_FILE_BYTES) {
            return 1
        }
        val mime = activity.contentResolver.getType(uri) ?: fallbackMime
            ?: "application/octet-stream"
        val relativeName = "item_${index}_${sanitizeFileName(displayName)}"
        val outFile = File(dir, relativeName)
        try {
            activity.contentResolver.openInputStream(uri)?.use { input ->
                FileOutputStream(outFile).use { output ->
                    val buffer = ByteArray(8192)
                    var total = 0L
                    while (true) {
                        val read = input.read(buffer)
                        if (read < 0) break
                        total += read
                        if (total > MAX_SHARE_FILE_BYTES) {
                            outFile.delete()
                            return 1
                        }
                        output.write(buffer, 0, read)
                    }
                }
            } ?: return 1
        } catch (_: Exception) {
            outFile.delete()
            return 1
        }
        items.put(
            JSONObject()
                .put("type", "file")
                .put("path", relativeName)
                .put("file_name", displayName)
                .put("mime", mime)
                .put("size", outFile.length()),
        )
        return 0
    }

    private fun sanitizeFileName(name: String): String {
        val trimmed = name.trim().ifEmpty { "file" }
        return trimmed.replace(Regex("[\\\\/]+"), "_")
    }

    private fun notifyFlutter(manifestId: String) {
        val sink = eventSink
        if (sink != null) {
            activity.runOnUiThread { sink.success(manifestId) }
        }
    }
}
