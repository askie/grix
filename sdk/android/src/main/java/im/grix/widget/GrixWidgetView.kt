package im.grix.widget

import android.Manifest
import android.annotation.SuppressLint
import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.webkit.JavascriptInterface
import android.webkit.PermissionRequest
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout

/**
 * JavascriptInterface bound to `window.GrixBridge`.
 */
class GrixJavaScriptBridge(
    private val mainHandler: Handler,
    private val onRawMessage: (String?) -> Unit
) {
    @JavascriptInterface
    fun postMessage(rawJson: String?) {
        mainHandler.post {
            onRawMessage(rawJson)
        }
    }
}

/**
 * Embeddable Android `FrameLayout` hosting the Grix Widget `WebView`.
 */
@SuppressLint("SetJavaScriptEnabled", "ViewConstructor")
class GrixWidgetView(
    context: Context,
    private val config: GrixConfig,
    private val visitorStore: GrixVisitorStore,
    private val onEvent: (GrixBridgeEvent) -> Unit
) : FrameLayout(context) {

    val webView: WebView = WebView(context)
    private val mainHandler = Handler(Looper.getMainLooper())
    private var appUrl: String? = null
    private var pendingWebPermissionRequest: PermissionRequest? = null
    private var isViewVisible = false

    init {
        setBackgroundColor(Color.WHITE)
        setupWebView()
        addView(webView, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
        loadWidget()
    }

    private fun setupWebView() {
        val settings: WebSettings = webView.settings
        settings.javaScriptEnabled = true
        settings.domStorageEnabled = true
        settings.mediaPlaybackRequiresUserGesture = false
        settings.loadsImagesAutomatically = true
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.LOLLIPOP) {
            settings.mixedContentMode = WebSettings.MIXED_CONTENT_COMPATIBILITY_MODE
        }

        webView.addJavascriptInterface(
            GrixJavaScriptBridge(mainHandler) { rawJson ->
                val event = GrixBridgeEvent.parse(rawJson) ?: return@GrixJavaScriptBridge
                if (event is GrixBridgeEvent.Ready && event.visitorKey.isNotEmpty()) {
                    visitorStore.saveVisitorKey(event.visitorKey, config.normalizedSiteKey)
                }
                onEvent(event)
            },
            BRIDGE_CHANNEL_NAME
        )

        webView.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest?): Boolean {
                val targetUri = request?.url ?: return false
                return handleNavigationUri(targetUri)
            }

            @Deprecated("Deprecated in Java")
            override fun shouldOverrideUrlLoading(view: WebView?, url: String?): Boolean {
                val parsed = url?.let { Uri.parse(it) } ?: return false
                return handleNavigationUri(parsed)
            }

            override fun onPageFinished(view: WebView?, url: String?) {
                super.onPageFinished(view, url)
                if (isAttachedToWindow && visibility == VISIBLE) {
                    notifyWebVisibility(true)
                }
            }
        }

        webView.webChromeClient = object : WebChromeClient() {
            override fun onPermissionRequest(request: PermissionRequest?) {
                if (request == null) return
                val resources = request.resources ?: emptyArray()
                val wantsAudio = resources.contains(PermissionRequest.RESOURCE_AUDIO_CAPTURE)
                if (!wantsAudio) {
                    request.deny()
                    return
                }
                GrixWidget.notifyMediaPermissionRequested(request.origin?.toString().orEmpty())
                handleAudioPermissionRequest(request)
            }
        }
    }

    private fun handleNavigationUri(targetUri: Uri): Boolean {
        val currentAppUri = appUrl?.let { Uri.parse(it) }
        if (currentAppUri != null &&
            targetUri.scheme == currentAppUri.scheme &&
            targetUri.host == currentAppUri.host &&
            targetUri.port == currentAppUri.port &&
            targetUri.path == currentAppUri.path
        ) {
            return false
        }
        val scheme = targetUri.scheme?.lowercase().orEmpty()
        if (scheme == "http" || scheme == "https") {
            openExternalUrl(targetUri.toString())
            return true
        }
        return false
    }

    private fun handleAudioPermissionRequest(request: PermissionRequest) {
        val hasAudioPermission = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            context.checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED
        } else {
            true
        }

        if (hasAudioPermission) {
            request.grant(arrayOf(PermissionRequest.RESOURCE_AUDIO_CAPTURE))
            return
        }

        val activity = findHostActivity(context)
        if (activity != null && Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            pendingWebPermissionRequest = request
            GrixWidget.registerPendingViewForPermission(this)
            activity.requestPermissions(
                arrayOf(Manifest.permission.RECORD_AUDIO),
                REQUEST_CODE_RECORD_AUDIO
            )
        } else {
            // Graceful fallback when no Activity is available to request runtime permission
            request.deny()
        }
    }

    /**
     * Resolves a pending WebView microphone permission request after the host Activity's
     * `onRequestPermissionsResult` completes. If the user denied `RECORD_AUDIO`, the WebView
     * request is denied gracefully so the chat page stays usable.
     */
    fun onRecordAudioPermissionResult(granted: Boolean) {
        val req = pendingWebPermissionRequest ?: return
        pendingWebPermissionRequest = null
        try {
            if (granted) {
                req.grant(arrayOf(PermissionRequest.RESOURCE_AUDIO_CAPTURE))
            } else {
                req.deny()
            }
        } catch (_: Throwable) {
            // Ignore if the WebView PermissionRequest was already settled/canceled
        }
    }

    fun loadWidget() {
        val storedVisitorKey = visitorStore.getVisitorKey(config.normalizedSiteKey)
        val url = GrixUrlBuilder.buildAppUrl(
            config = config,
            hostPackageName = context.packageName,
            visitorKey = storedVisitorKey
        )
        appUrl = url
        webView.loadUrl(url)
    }

    /**
     * Invokes `window.grixWidget.setVisible(bool)` inside the hosted page.
     */
    fun notifyWebVisibility(visible: Boolean) {
        isViewVisible = visible
        val jsLiteral = if (visible) "true" else "false"
        val script = """
            (function(v) {
                if (window.grixWidget && typeof window.grixWidget.setVisible === 'function') {
                    window.grixWidget.setVisible(v);
                } else {
                    setTimeout(function() {
                        if (window.grixWidget && typeof window.grixWidget.setVisible === 'function') {
                            window.grixWidget.setVisible(v);
                        }
                    }, 150);
                }
            })($jsLiteral);
        """.trimIndent()
        webView.evaluateJavascript(script, null)
    }

    /**
     * Invokes `window.grixWidget.close()` inside the hosted page.
     */
    fun requestClose() {
        val script = "window.grixWidget && typeof window.grixWidget.close === 'function' && window.grixWidget.close();"
        webView.evaluateJavascript(script, null)
    }

    fun evaluateJavaScript(script: String) {
        webView.evaluateJavascript(script, null)
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        GrixWidget.registerActiveView(this)
        notifyWebVisibility(visibility == VISIBLE)
    }

    override fun onDetachedFromWindow() {
        notifyWebVisibility(false)
        GrixWidget.unregisterActiveView(this)
        super.onDetachedFromWindow()
    }

    override fun onWindowVisibilityChanged(visibility: Int) {
        super.onWindowVisibilityChanged(visibility)
        val visibleNow = (visibility == VISIBLE && this.visibility == VISIBLE)
        notifyWebVisibility(visibleNow)
    }

    private fun openExternalUrl(url: String) {
        try {
            val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url)).apply {
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            context.startActivity(intent)
        } catch (_: Throwable) {
            // No browser installed or invalid URI
        }
    }

    private fun findHostActivity(ctx: Context?): Activity? {
        var cur = ctx
        while (cur is ContextWrapper) {
            if (cur is Activity) return cur
            cur = cur.baseContext
        }
        return null
    }

    companion object {
        const val BRIDGE_CHANNEL_NAME = "GrixBridge"
        const val REQUEST_CODE_RECORD_AUDIO = 27181
    }
}
