package im.grix.widget

/**
 * Optional visitor identity passed to [GrixWidget.configure].
 */
data class GrixUser(
    val name: String? = null,
    val email: String? = null
)

/**
 * Configuration for the Grix Website Widget Android SDK.
 */
data class GrixConfig(
    val siteKey: String,
    val apiBase: String = DEFAULT_API_BASE,
    val locale: String? = null,
    val themeColor: String? = null,
    val title: String? = null,
    val user: GrixUser? = null,
    val appIdOverride: String? = null
) {
    val normalizedSiteKey: String = siteKey.trim()
    val normalizedApiBase: String = apiBase.trim().trimEnd('/').ifEmpty { DEFAULT_API_BASE }

    fun resolvedAppId(hostPackageName: String): String {
        val override = appIdOverride?.trim().orEmpty()
        if (override.isNotEmpty()) {
            return override
        }
        return hostPackageName.trim()
    }

    companion object {
        const val DEFAULT_API_BASE = "https://gb.grix.im"
        const val SDK_VERSION = "1.0.0"
    }
}
