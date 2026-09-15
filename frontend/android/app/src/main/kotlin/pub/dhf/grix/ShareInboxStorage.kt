package pub.dhf.grix

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.UUID

object ShareInboxStorage {
    const val INBOX_DIR_NAME = "share_inbox"
    const val MANIFEST_FILE_NAME = "manifest.json"

    fun inboxRoot(context: Context): File =
        File(context.filesDir, INBOX_DIR_NAME).also { it.mkdirs() }

    fun entryDir(context: Context, id: String): File =
        File(inboxRoot(context), id)

    fun listPendingManifests(context: Context): List<JSONObject> {
        val root = inboxRoot(context)
        val entries = root.listFiles()?.filter { it.isDirectory } ?: emptyList()
        val manifests = mutableListOf<JSONObject>()
        for (dir in entries) {
            val manifestFile = File(dir, MANIFEST_FILE_NAME)
            if (!manifestFile.isFile) continue
            try {
                val json = JSONObject(manifestFile.readText())
                val enriched = enrichWithAbsolutePaths(dir, json)
                manifests.add(enriched)
            } catch (_: Exception) {
                // Skip corrupt inbox entries.
            }
        }
        manifests.sortByDescending { it.optLong("created_at", 0L) }
        return manifests
    }

    fun deleteEntry(context: Context, id: String) {
        val dir = entryDir(context, id.trim())
        if (dir.exists()) {
            dir.deleteRecursively()
        }
    }

    fun writeNewEntry(
        context: Context,
        items: JSONArray,
        source: String?,
    ): String {
        val id = UUID.randomUUID().toString()
        val dir = entryDir(context, id)
        dir.mkdirs()
        val manifest = JSONObject()
            .put("id", id)
            .put("created_at", System.currentTimeMillis())
            .put("items", items)
        if (!source.isNullOrBlank()) {
            manifest.put("source", source.trim())
        }
        File(dir, MANIFEST_FILE_NAME).writeText(manifest.toString())
        return id
    }

    private fun enrichWithAbsolutePaths(dir: File, manifest: JSONObject): JSONObject {
        val items = manifest.optJSONArray("items") ?: JSONArray()
        val enrichedItems = JSONArray()
        for (i in 0 until items.length()) {
            val item = items.optJSONObject(i) ?: continue
            val copy = JSONObject(item.toString())
            val relative = copy.optString("path", "")
            if (relative.isNotBlank()) {
                copy.put("absolute_path", File(dir, relative).absolutePath)
            }
            enrichedItems.put(copy)
        }
        val result = JSONObject(manifest.toString())
        result.put("items", enrichedItems)
        return result
    }

    fun jsonObjectToMap(json: JSONObject): Map<String, Any?> {
        val map = mutableMapOf<String, Any?>()
        for (key in json.keys()) {
            map[key] = jsonValueToKotlin(json.get(key))
        }
        return map
    }

    private fun jsonValueToKotlin(value: Any?): Any? = when (value) {
        is JSONObject -> jsonObjectToMap(value)
        is JSONArray -> {
            val list = mutableListOf<Any?>()
            for (i in 0 until value.length()) {
                list.add(jsonValueToKotlin(value.get(i)))
            }
            list
        }
        JSONObject.NULL -> null
        else -> value
    }
}
