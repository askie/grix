package im.grix.widget

import android.content.Context
import android.content.SharedPreferences

/**
 * Abstraction for storing `visitor_key` scoped by `siteKey`.
 */
interface GrixVisitorStore {
    fun getVisitorKey(siteKey: String): String?
    fun saveVisitorKey(visitorKey: String, siteKey: String)
}

/**
 * `SharedPreferences`-backed implementation of [GrixVisitorStore].
 */
class GrixSharedPreferencesStore(
    context: Context,
    prefsName: String = PREFS_NAME
) : GrixVisitorStore {
    private val prefs: SharedPreferences =
        context.applicationContext.getSharedPreferences(prefsName, Context.MODE_PRIVATE)

    override fun getVisitorKey(siteKey: String): String? {
        val key = siteKey.trim()
        if (key.isEmpty()) return null
        val stored = prefs.getString(prefKey(key), null)?.trim().orEmpty()
        return stored.ifEmpty { null }
    }

    override fun saveVisitorKey(visitorKey: String, siteKey: String) {
        val key = siteKey.trim()
        val valTrimmed = visitorKey.trim()
        if (key.isEmpty() || valTrimmed.isEmpty()) return
        prefs.edit().putString(prefKey(key), valTrimmed).apply()
    }

    private fun prefKey(siteKey: String): String = "visitor_key:$siteKey"

    companion object {
        const val PREFS_NAME = "im.grix.widget.visitor_store"
    }
}

/**
 * In-memory implementation of [GrixVisitorStore] for unit testing.
 */
class GrixMemoryVisitorStore : GrixVisitorStore {
    private val map = mutableMapOf<String, String>()

    @Synchronized
    override fun getVisitorKey(siteKey: String): String? = map[siteKey.trim()]

    @Synchronized
    override fun saveVisitorKey(visitorKey: String, siteKey: String) {
        val k = siteKey.trim()
        val v = visitorKey.trim()
        if (k.isNotEmpty() && v.isNotEmpty()) {
            map[k] = v
        }
    }
}
