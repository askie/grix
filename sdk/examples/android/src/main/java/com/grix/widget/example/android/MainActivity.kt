package com.grix.widget.example.android

import android.app.Activity
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.InputType
import android.util.TypedValue
import android.view.Gravity
import android.view.ViewGroup
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import im.grix.widget.GrixUser
import im.grix.widget.GrixWidget
import im.grix.widget.GrixWidgetView
import java.io.OutputStreamWriter
import java.net.HttpURLConnection
import java.net.URL
import kotlin.concurrent.thread

class MainActivity : Activity() {
    private lateinit var siteKeyInput: EditText
    private lateinit var apiBaseInput: EditText
    private lateinit var appIdOverrideInput: EditText
    private lateinit var statusLabel: TextView
    private lateinit var embedContainer: FrameLayout
    private var embeddedView: GrixWidgetView? = null
    private var launcherVisible = false
    private val mainHandler = Handler(Looper.getMainLooper())
    private var testCallbackUrl: String = ""

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val initialSiteKey = intent?.getStringExtra("GRIX_SITE_KEY") ?: "wk_test_sdk_site"
        val initialApiBase = intent?.getStringExtra("GRIX_API_BASE") ?: "http://127.0.0.1:27180"
        val initialAppIdOverride = intent?.getStringExtra("GRIX_APP_ID_OVERRIDE") ?: ""
        val autoAction = intent?.getStringExtra("GRIX_AUTO_ACTION") ?: ""
        testCallbackUrl = intent?.getStringExtra("GRIX_TEST_CALLBACK_URL") ?: ""

        setupUI(initialSiteKey, initialApiBase, initialAppIdOverride)
        applyConfiguration()

        when (autoAction) {
            "present" -> mainHandler.postDelayed({ handlePresentTapped() }, 400)
            "embed" -> mainHandler.postDelayed({ handleEmbedTapped() }, 400)
            "embed_full_test" -> mainHandler.postDelayed({ runAutomatedAcceptanceSequence() }, 400)
        }
    }

    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        GrixWidget.onRequestPermissionsResult(requestCode, grantResults)
    }

    private fun setupUI(siteKey: String, apiBase: String, appIdOverride: String) {
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(Color.parseColor("#F3F4F6"))
            val pad = dp(16f)
            setPadding(pad, dp(20f), pad, pad)
        }

        val header = TextView(this).apply {
            text = "Grix Widget Android Demo"
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 18f)
            setTypeface(typeface, Typeface.BOLD)
            setTextColor(Color.parseColor("#111827"))
            gravity = Gravity.CENTER_HORIZONTAL
            setPadding(0, 0, 0, dp(10f))
        }
        root.addView(header)

        siteKeyInput = createEditText("Site Key", siteKey)
        apiBaseInput = createEditText("API Base URL", apiBase)
        appIdOverrideInput = createEditText("App ID Override (blank = Package Name)", appIdOverride)
        root.addView(siteKeyInput, createMarginLayoutParams(bottomDp = 8f))
        root.addView(apiBaseInput, createMarginLayoutParams(bottomDp = 8f))
        root.addView(appIdOverrideInput, createMarginLayoutParams(bottomDp = 10f))

        val btnRow = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            weightSum = 3f
        }
        val presentBtn = createButton("present()", "#0F766E") { handlePresentTapped() }
        val embedBtn = createButton("makeView()", "#2563EB") { handleEmbedTapped() }
        val launcherBtn = createButton("Launcher", "#4F46E5") { handleLauncherTapped() }

        btnRow.addView(presentBtn, LinearLayout.LayoutParams(0, dp(40f), 1f).apply { rightMargin = dp(6f) })
        btnRow.addView(embedBtn, LinearLayout.LayoutParams(0, dp(40f), 1f).apply { rightMargin = dp(6f) })
        btnRow.addView(launcherBtn, LinearLayout.LayoutParams(0, dp(40f), 1f))
        root.addView(btnRow, createMarginLayoutParams(bottomDp = 8f))

        statusLabel = TextView(this).apply {
            text = "Status: Ready (Package: $packageName)"
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 12f)
            setTextColor(Color.parseColor("#4B5563"))
            typeface = Typeface.MONOSPACE
        }
        root.addView(statusLabel, createMarginLayoutParams(bottomDp = 10f))

        embedContainer = FrameLayout(this).apply {
            background = GradientDrawable().apply {
                setColor(Color.WHITE)
                cornerRadius = dp(12f).toFloat()
            }
            clipToOutline = true
        }
        root.addView(
            embedContainer,
            LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                0,
                1f
            )
        )

        setContentView(root)
    }

    private fun createEditText(hintText: String, initialValue: String): EditText {
        return EditText(this).apply {
            hint = hintText
            setText(initialValue)
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 13f)
            setTextColor(Color.parseColor("#1F2937"))
            setSingleLine(true)
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
            val hPad = dp(10f)
            val vPad = dp(8f)
            setPadding(hPad, vPad, hPad, vPad)
            background = GradientDrawable().apply {
                setColor(Color.WHITE)
                cornerRadius = dp(8f).toFloat()
                setStroke(dp(1f), Color.parseColor("#D1D5DB"))
            }
        }
    }

    private fun createButton(title: String, hexColor: String, onClick: () -> Unit): Button {
        return Button(this).apply {
            text = title
            isAllCaps = false
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 13f)
            setTextColor(Color.WHITE)
            background = GradientDrawable().apply {
                setColor(Color.parseColor(hexColor))
                cornerRadius = dp(8f).toFloat()
            }
            setOnClickListener { onClick() }
        }
    }

    private fun createMarginLayoutParams(bottomDp: Float): LinearLayout.LayoutParams {
        return LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.WRAP_CONTENT
        ).apply {
            bottomMargin = dp(bottomDp)
        }
    }

    private fun applyConfiguration() {
        val overrideText = appIdOverrideInput.text?.toString()?.trim().orEmpty()
        GrixWidget.configure(
            siteKey = siteKeyInput.text?.toString()?.trim().orEmpty(),
            apiBase = apiBaseInput.text?.toString()?.trim()?.ifEmpty { "http://127.0.0.1:27180" }
                ?: "http://127.0.0.1:27180",
            locale = "zh-CN",
            themeColor = "#0f766e",
            title = "Grix Android 客服",
            user = GrixUser(name = "Android 访客", email = "android@example.com"),
            appIdOverride = overrideText.ifEmpty { null }
        )

        GrixWidget.onClose = {
            appendLog("onClose triggered")
        }
        GrixWidget.onUnreadChange = { count ->
            appendLog("onUnreadChange($count)")
        }
        GrixWidget.onSessionClosed = { reason ->
            appendLog("onSessionClosed($reason)")
        }
        GrixWidget.onError = { code, message ->
            appendLog("onError($code): $message")
        }
        GrixWidget.onOpenUrl = { url ->
            appendLog("open_url($url)")
        }
        GrixWidget.onMediaPermissionRequest = { _ ->
            appendLog("onPermissionRequest(AUDIO_CAPTURE) triggered")
        }
    }

    private fun appendLog(text: String) {
        val siteKey = siteKeyInput.text?.toString()?.trim().orEmpty()
        val visitorKey = GrixWidget.visitorStore?.getVisitorKey(siteKey).orEmpty()
        if (testCallbackUrl.isNotEmpty()) {
            val escapedEvent = text.replace("\\", "\\\\").replace("\"", "\\\"")
            val escapedKey = visitorKey.replace("\\", "\\\\").replace("\"", "\\\"")
            val payload = """{"event":"$escapedEvent","visitor_key":"$escapedKey"}"""
            thread {
                try {
                    val conn = (URL(testCallbackUrl).openConnection() as HttpURLConnection).apply {
                        requestMethod = "POST"
                        doOutput = true
                        setRequestProperty("Content-Type", "application/json")
                        connectTimeout = 2000
                        readTimeout = 2000
                    }
                    OutputStreamWriter(conn.outputStream, Charsets.UTF_8).use { it.write(payload) }
                    conn.responseCode
                    conn.disconnect()
                } catch (_: Throwable) {
                }
            }
        }
        mainHandler.post {
            statusLabel.text = "Last Event: $text"
        }
    }

    private fun handlePresentTapped() {
        applyConfiguration()
        GrixWidget.present(this)
    }

    private fun handleEmbedTapped() {
        applyConfiguration()
        embedContainer.removeAllViews()
        val v = GrixWidget.makeView(this)
        embedContainer.addView(
            v,
            FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT
            )
        )
        embeddedView = v
        appendLog("Embedded makeView() loaded")
    }

    private fun handleLauncherTapped() {
        applyConfiguration()
        launcherVisible = !launcherVisible
        GrixWidget.setLauncherVisible(launcherVisible, this)
        appendLog("setLauncherVisible($launcherVisible)")
    }

    private fun runAutomatedAcceptanceSequence() {
        handleEmbedTapped()
        val grixView = embeddedView ?: return

        // 1. After 2.5s, send a visitor message via the hosted page composer
        mainHandler.postDelayed({
            appendLog("auto: sending visitor message")
            grixView.evaluateJavaScript(
                """
                (function() {
                    var input = document.getElementById('input');
                    var send = document.getElementById('send');
                    if (input && send) {
                        input.value = '你好，我是来自 Android App 的测试访客！';
                        send.click();
                    }
                })();
                """.trimIndent()
            )
        }, 2500)

        // 2. After 4.5s, setVisible(false) so subsequent owner message increments unread badge
        mainHandler.postDelayed({
            appendLog("auto: setVisible(false) + show launcher")
            launcherVisible = true
            GrixWidget.setLauncherVisible(true, this)
            grixView.notifyWebVisibility(false)
        }, 4500)

        // 3. After 7.5s, setVisible(true) so unread badge clears to 0
        mainHandler.postDelayed({
            appendLog("auto: setVisible(true) to clear unread")
            grixView.notifyWebVisibility(true)
        }, 7500)

        // 4. After 9.5s, click the markdown link inside the peer message to trigger open_url
        mainHandler.postDelayed({
            appendLog("auto: clicking link in message")
            grixView.evaluateJavaScript(
                """
                (function() {
                    var link = document.querySelector('#msgs a[href]');
                    if (link) { link.click(); }
                })();
                """.trimIndent()
            )
        }, 9500)

        // 5. After 11.5s, click the voice call button 📞 to trigger microphone permission request
        mainHandler.postDelayed({
            appendLog("auto: clicking callBtn for voice call")
            grixView.evaluateJavaScript(
                """
                (function() {
                    var btn = document.getElementById('callBtn');
                    if (btn) { btn.click(); }
                })();
                """.trimIndent()
            )
        }, 11500)

        // 6. After 14.5s, invoke requestClose() to trigger onClose
        mainHandler.postDelayed({
            appendLog("auto: invoking requestClose()")
            grixView.requestClose()
        }, 14500)
    }

    private fun dp(value: Float): Int {
        return TypedValue.applyDimension(
            TypedValue.COMPLEX_UNIT_DIP,
            value,
            resources.displayMetrics
        ).toInt()
    }
}
