package im.grix.widget

import java.net.URLEncoder

/**
 * Builds the `/public/widget/app.html#<fragment>` URL according to the Grix Widget App Channel contract.
 * Startup parameters are strictly placed in the URL fragment (`#`) and never in the query string.
 */
object GrixUrlBuilder {
    fun buildAppUrl(
        config: GrixConfig,
        hostPackageName: String,
        visitorKey: String?
    ): String {
        val baseUrl = "${config.normalizedApiBase}/public/widget/app.html"
        val pairs = mutableListOf(
            "site_key" to config.normalizedSiteKey,
            "platform" to "android",
            "app_id" to config.resolvedAppId(hostPackageName),
            "sdk_version" to GrixConfig.SDK_VERSION
        )

        val trimmedVisitorKey = visitorKey?.trim().orEmpty()
        if (trimmedVisitorKey.isNotEmpty()) {
            pairs.add("visitor_key" to trimmedVisitorKey)
        }

        val trimmedLocale = config.locale?.trim().orEmpty()
        if (trimmedLocale.isNotEmpty()) {
            pairs.add("locale" to trimmedLocale)
        }

        val trimmedTheme = config.themeColor?.trim().orEmpty()
        if (trimmedTheme.isNotEmpty()) {
            pairs.add("theme_color" to trimmedTheme)
        }

        val trimmedTitle = config.title?.trim().orEmpty()
        if (trimmedTitle.isNotEmpty()) {
            pairs.add("title" to trimmedTitle)
        }

        val trimmedName = config.user?.name?.trim().orEmpty()
        if (trimmedName.isNotEmpty()) {
            pairs.add("visitor_name" to trimmedName)
        }

        val trimmedEmail = config.user?.email?.trim().orEmpty()
        if (trimmedEmail.isNotEmpty()) {
            pairs.add("visitor_email" to trimmedEmail)
        }

        val fragment = pairs.joinToString("&") { (k, v) ->
            "${percentEncode(k)}=${percentEncode(v)}"
        }
        return "$baseUrl#$fragment"
    }

    private fun percentEncode(value: String): String {
        return URLEncoder.encode(value, "UTF-8")
            .replace("+", "%20")
            .replace("*", "%2A")
            .replace("%7E", "~")
    }
}
