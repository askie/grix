package im.grix.widget

import android.app.Activity
import android.app.Application
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Bundle
import android.view.View
import android.view.ViewGroup
import java.lang.ref.WeakReference

/**
 * Unified entry point for the Grix Website Widget Android SDK.
 */
object GrixWidget {
    var config: GrixConfig? = null
        private set

    var visitorStore: GrixVisitorStore? = null

    /** Fired when the visitor taps the close button or when `close` event is emitted. */
    var onClose: (() -> Unit)? = null

    /** Fired when the unread message count changes. */
    var onUnreadChange: ((Int) -> Unit)? = null

    /** Fired when the session is closed by the agent/owner (`reason` e.g. `"closed"`). */
    var onSessionClosed: ((String) -> Unit)? = null

    /** Fired when an error occurs (e.g. `app_not_allowed`, `site_disabled`, `network_error`). */
    var onError: ((String, String) -> Unit)? = null

    /** Optional hook invoked when `open_url` is dispatched before opening the system browser. */
    var onOpenUrl: ((String) -> Unit)? = null

    /** Optional hook invoked when WebView requests microphone capture permission (`onPermissionRequest`). */
    var onMediaPermissionRequest: ((String) -> Unit)? = null

    private var unreadCount: Int = 0
    private var launcherVisible: Boolean = false
    private var launcherViewRef: WeakReference<GrixLauncherView>? = null
    private var currentActivityRef: WeakReference<Activity>? = null
    private var pendingPermissionViewRef: WeakReference<GrixWidgetView>? = null
    private val activeViews = mutableSetOf<WeakReference<GrixWidgetView>>()
    private var lifecycleRegistered = false

    /**
     * Configures the Grix Widget SDK.
     */
    @JvmStatic
    @JvmOverloads
    fun configure(
        siteKey: String,
        apiBase: String = GrixConfig.DEFAULT_API_BASE,
        locale: String? = null,
        themeColor: String? = null,
        title: String? = null,
        user: GrixUser? = null,
        appIdOverride: String? = null
    ) {
        config = GrixConfig(
            siteKey = siteKey,
            apiBase = apiBase,
            locale = locale,
            themeColor = themeColor,
            title = title,
            user = user,
            appIdOverride = appIdOverride
        )
        launcherViewRef?.get()?.updateThemeColor(themeColor)
    }

    /**
     * Opens the full-screen Grix chat `Activity`.
     */
    @JvmStatic
    fun present(context: Context) {
        val cfg = config
        if (cfg == null) {
            onError?.invoke("not_configured", "GrixWidget.configure(...) must be called before present().")
            return
        }
        ensureLifecycleRegistered(context)
        val intent = Intent(context, GrixWidgetActivity::class.java)
        if (context !is Activity) {
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        context.startActivity(intent)
    }

    /**
     * Returns an embeddable `View` ([GrixWidgetView]) that can be added to any host layout or `Fragment`.
     */
    @JvmStatic
    fun makeView(context: Context): GrixWidgetView {
        val cfg = config ?: GrixConfig(siteKey = "").also {
            onError?.invoke("not_configured", "GrixWidget.configure(...) must be called before makeView().")
        }
        ensureLifecycleRegistered(context)
        if (context is Activity) {
            currentActivityRef = WeakReference(context)
        }
        val store = resolveVisitorStore(context)
        return GrixWidgetView(
            context = context,
            config = cfg,
            visitorStore = store,
            onEvent = { event ->
                handleBridgeEvent(context, event)
            }
        )
    }

    /**
     * Shows or hides the floating launcher button on the active `Activity`.
     */
    @JvmStatic
    fun setLauncherVisible(visible: Boolean, activity: Activity? = currentActivityRef?.get()) {
        launcherVisible = visible
        if (!visible) {
            val existing = launcherViewRef?.get()
            (existing?.parent as? ViewGroup)?.removeView(existing)
            launcherViewRef = null
            return
        }
        val targetActivity = activity ?: currentActivityRef?.get() ?: return
        currentActivityRef = WeakReference(targetActivity)
        ensureLifecycleRegistered(targetActivity)
        attachLauncherToActivity(targetActivity)
    }

    /**
     * Helper that host Activities embedding `makeView()` can call from their `onRequestPermissionsResult`
     * to forward `RECORD_AUDIO` runtime permission results to the embedded [GrixWidgetView].
     */
    @JvmStatic
    fun onRequestPermissionsResult(
        requestCode: Int,
        grantResults: IntArray
    ) {
        if (requestCode == GrixWidgetView.REQUEST_CODE_RECORD_AUDIO) {
            val granted = grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED
            pendingPermissionViewRef?.get()?.onRecordAudioPermissionResult(granted)
            pendingPermissionViewRef = null
        }
    }

    internal fun resolveVisitorStore(context: Context): GrixVisitorStore {
        visitorStore?.let { return it }
        return GrixSharedPreferencesStore(context.applicationContext).also {
            visitorStore = it
        }
    }

    internal fun handleBridgeEvent(context: Context?, event: GrixBridgeEvent) {
        when (event) {
            is GrixBridgeEvent.Ready -> Unit
            is GrixBridgeEvent.Close -> {
                onClose?.invoke()
            }
            is GrixBridgeEvent.SessionClosed -> {
                onSessionClosed?.invoke(event.reason)
            }
            is GrixBridgeEvent.Unread -> {
                unreadCount = event.count
                launcherViewRef?.get()?.updateUnreadCount(event.count)
                onUnreadChange?.invoke(event.count)
            }
            is GrixBridgeEvent.OpenUrl -> {
                onOpenUrl?.invoke(event.url)
                if (context != null) {
                    try {
                        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(event.url)).apply {
                            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                        }
                        context.startActivity(intent)
                    } catch (_: Throwable) {
                        // Ignore if no browser is available in test environment
                    }
                }
            }
            is GrixBridgeEvent.Error -> {
                onError?.invoke(event.code, event.message)
            }
        }
    }

    internal fun notifyMediaPermissionRequested(origin: String) {
        onMediaPermissionRequest?.invoke(origin)
    }

    internal fun registerPendingViewForPermission(view: GrixWidgetView) {
        pendingPermissionViewRef = WeakReference(view)
    }

    internal fun registerActiveView(view: GrixWidgetView) {
        activeViews.removeAll { it.get() == null || it.get() === view }
        activeViews.add(WeakReference(view))
    }

    internal fun unregisterActiveView(view: GrixWidgetView) {
        activeViews.removeAll { it.get() == null || it.get() === view }
    }

    private fun attachLauncherToActivity(activity: Activity) {
        val contentRoot = activity.findViewById<ViewGroup>(android.R.id.content) ?: return
        val existing = launcherViewRef?.get()
        if (existing != null && existing.parent === contentRoot) {
            existing.updateThemeColor(config?.themeColor)
            existing.updateUnreadCount(unreadCount)
            existing.visibility = View.VISIBLE
            return
        }
        (existing?.parent as? ViewGroup)?.removeView(existing)
        val button = GrixLauncherView(activity).apply {
            updateThemeColor(config?.themeColor)
            updateUnreadCount(unreadCount)
            setOnClickListener {
                present(activity)
            }
        }
        contentRoot.addView(button)
        launcherViewRef = WeakReference(button)
    }

    private fun ensureLifecycleRegistered(context: Context) {
        if (lifecycleRegistered) return
        val app = context.applicationContext as? Application ?: return
        lifecycleRegistered = true
        app.registerActivityLifecycleCallbacks(object : Application.ActivityLifecycleCallbacks {
            override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) {}
            override fun onActivityStarted(activity: Activity) {}
            override fun onActivityResumed(activity: Activity) {
                if (activity !is GrixWidgetActivity) {
                    currentActivityRef = WeakReference(activity)
                    if (launcherVisible) {
                        attachLauncherToActivity(activity)
                    }
                }
                // App returned to foreground: notify any visible GrixWidgetView
                activeViews.mapNotNull { it.get() }.forEach { view ->
                    if (view.isAttachedToWindow && view.visibility == View.VISIBLE) {
                        view.notifyWebVisibility(true)
                    }
                }
            }

            override fun onActivityPaused(activity: Activity) {}
            override fun onActivityStopped(activity: Activity) {}
            override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) {}
            override fun onActivityDestroyed(activity: Activity) {
                if (currentActivityRef?.get() === activity) {
                    currentActivityRef = null
                }
            }
        })
    }
}
