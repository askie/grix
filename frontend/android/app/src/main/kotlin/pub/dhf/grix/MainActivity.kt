package pub.dhf.grix

import android.content.BroadcastReceiver
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.media.MediaScannerConnection
import android.os.Build
import android.os.Handler
import android.os.Environment
import android.os.Looper
import android.provider.MediaStore
import androidx.credentials.CustomCredential
import androidx.credentials.CredentialManager
import androidx.credentials.GetCredentialRequest
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.credentials.exceptions.GetCredentialException
import androidx.lifecycle.lifecycleScope
import com.google.android.gms.common.ConnectionResult
import com.google.android.gms.common.GoogleApiAvailability
import com.google.firebase.FirebaseApp
import com.google.firebase.FirebaseOptions
import com.google.firebase.messaging.FirebaseMessaging
import com.google.android.libraries.identity.googleid.GetGoogleIdOption
import com.google.android.libraries.identity.googleid.GoogleIdTokenCredential
import com.google.android.libraries.identity.googleid.GoogleIdTokenParsingException
import cn.jpush.android.api.JPushInterface
import pub.dhf.grix.push.HuaweiPushTokenSource
import pub.dhf.grix.push.PushChannel
import pub.dhf.grix.push.PushChannelResolver
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.embedding.android.FlutterActivity
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.io.FileOutputStream
import kotlinx.coroutines.launch

class MainActivity : FlutterActivity() {
    private val textDocumentBridge by lazy { TextDocumentBridge(this) }
    private var pushTapChannel: MethodChannel? = null
    private var pendingTapSessionId: String? = null
    private var pendingTapRecipientId: String? = null
    companion object {
        private const val MERMAID_CHANNEL = "pub.dhf.grix/mermaid_image_saver"
        private const val PUSH_CHANNEL = "pub.dhf.grix/push_registration"
        private const val GOOGLE_SIGN_IN_CHANNEL = "pub.dhf.grix/google_sign_in"
        private const val PUSH_TAP_CHANNEL = "pub.dhf.grix/push_tap"
        private const val CALL_CHANNEL = "com.aibot/android_call"
        private const val SENTRY_DEDUP_CHANNEL = "pub.dhf.grix/sentry_event_dedup"
        private const val JPUSH_POLL_INTERVAL_MS = 500L
        private const val JPUSH_MAX_POLLS = 20
        // 会话 ID（UUID）等短标识符字符集白名单。
        private val PUSH_ID_PATTERN = Regex("^[A-Za-z0-9_-]{8,64}$")
        private val NUMERIC_ID_PATTERN = Regex("^[0-9]{1,20}$")
    }

    private var callMethodChannel: MethodChannel? = null
    private val callInviteReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            val callId = intent.getStringExtra("call_id") ?: return
            val callerName = intent.getStringExtra("caller_name") ?: ""
            callMethodChannel?.invokeMethod("onIncomingCall", mapOf(
                "call_id" to callId,
                "caller_name" to callerName,
            ))
        }
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        textDocumentBridge.configure(flutterEngine.dartExecutor.binaryMessenger)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, SENTRY_DEDUP_CHANNEL)
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "installNative" -> {
                        NativeSentryEventDeduplicator.install(applicationContext)
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            }
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, MERMAID_CHANNEL)
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "saveImageToGallery" -> {
                        val bytes = call.argument<ByteArray>("bytes")
                        val fileName = call.argument<String>("fileName")
                        if (bytes == null || fileName.isNullOrBlank()) {
                            result.error("invalid_args", "Missing bytes or fileName", null)
                            return@setMethodCallHandler
                        }
                        try {
                            val savedLocation = saveImageToGallery(bytes, fileName)
                            result.success(savedLocation)
                        } catch (error: Exception) {
                            result.error("save_failed", error.message, null)
                        }
                    }
                    "saveVideoToGallery" -> {
                        val filePath = call.argument<String>("filePath")
                        val fileName = call.argument<String>("fileName")
                        val mimeType = call.argument<String>("mimeType") ?: "video/mp4"
                        if (filePath.isNullOrBlank() || fileName.isNullOrBlank()) {
                            result.error("invalid_args", "Missing filePath or fileName", null)
                            return@setMethodCallHandler
                        }
                        try {
                            val savedLocation = saveVideoToGallery(filePath, fileName, mimeType)
                            result.success(savedLocation)
                        } catch (error: Exception) {
                            result.error("save_failed", error.message, null)
                        }
                    }
                    else -> result.notImplemented()
                }
            }

        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, PUSH_CHANNEL)
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "resolveAndroidPushInfo" -> resolveAndroidPushInfo(result)
                    else -> result.notImplemented()
                }
            }

        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, GOOGLE_SIGN_IN_CHANNEL)
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "signInWithGoogle" -> signInWithGoogle(result)
                    else -> result.notImplemented()
                }
            }

        val tapChannel = MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger, PUSH_TAP_CHANNEL
        )
        tapChannel.setMethodCallHandler { _, result -> result.notImplemented() }
        pushTapChannel = tapChannel

        // 语音通话来电 channel（Phase 1）
        callMethodChannel = MethodChannel(
            flutterEngine.dartExecutor.binaryMessenger, CALL_CHANNEL
        )

        // If a pending tap was cached before the engine was ready, deliver it now.
        pendingTapSessionId?.let { sid ->
            val rid = pendingTapRecipientId
            pendingTapSessionId = null
            pendingTapRecipientId = null
            Handler(Looper.getMainLooper()).postDelayed({ notifyPushTap(sid, rid) }, 500)
        }

        // Check if the app was launched from a notification tap (cold start).
        intent?.let { checkPushTapIntent(it) }
        textDocumentBridge.handleIntent(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        checkPushTapIntent(intent)
        textDocumentBridge.handleIntent(intent)
    }

    override fun onResume() {
        super.onResume()
        val filter = IntentFilter("pub.dhf.grix.CALL_INVITE")
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(callInviteReceiver, filter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            // 低版本没有 RECEIVER_NOT_EXPORTED 标志位；改用应用内签名级权限
            // 收敛导出面，外部应用无法向该接收器发广播。
            registerReceiver(
                callInviteReceiver,
                filter,
                "${packageName}.permission.INTERNAL",
                null,
            )
        }
    }

    override fun onPause() {
        super.onPause()
        try { unregisterReceiver(callInviteReceiver) } catch (_: Exception) {}
    }

    private fun checkPushTapIntent(intent: Intent) {
        // 来源校验：只接受约定的通知点击入口（本应用 PUSH_TAP_ACTION、厂商通道
        // 的 grixpush:// scheme、FCM 后台点击走 launcher MAIN）；其余 action
        // （如 https deeplink VIEW）不允许注入会话跳转。
        if (!isPushTapEntry(intent)) return
        // 格式校验：session_id 只接受短标识符字符集，recipient_id 只接受数字 ID，
        // 防止伪造 intent 注入超长/异常字符串驱动会话跳转。
        val sessionId = extractSessionId(intent)?.takeIf { PUSH_ID_PATTERN.matches(it) } ?: return
        val recipientId = extractExtra(intent, "recipient_id")?.takeIf { NUMERIC_ID_PATTERN.matches(it) }
        notifyPushTap(sessionId, recipientId)
    }

    private fun isPushTapEntry(intent: Intent): Boolean = when (intent.action) {
        "PUSH_TAP_ACTION" -> true
        Intent.ACTION_MAIN -> true
        Intent.ACTION_VIEW -> {
            val data = intent.data
            data != null && data.scheme == "grixpush" && data.host == "pub.dhf.grix"
        }
        else -> false
    }

    private fun extractSessionId(intent: Intent): String? = extractExtra(intent, "session_id")

    private fun extractExtra(intent: Intent, key: String): String? {
        // FCM: data payload is delivered as intent extras.
        intent.getStringExtra(key)?.trim()?.let { if (it.isNotEmpty()) return it }
        // JPush: extras are nested inside the notification extras bundle.
        val jpushExtras = intent.getBundleExtra("cn.jpush.android.EXTRA")
        jpushExtras?.getString(key)?.trim()?.let { if (it.isNotEmpty()) return it }
        return null
    }

    private fun notifyPushTap(sessionId: String, recipientId: String? = null) {
        val channel = pushTapChannel
        if (channel == null) {
            pendingTapSessionId = sessionId
            pendingTapRecipientId = recipientId
            return
        }
        Handler(Looper.getMainLooper()).post {
            channel.invokeMethod(
                "onPushTapped",
                mapOf("session_id" to sessionId, "recipient_id" to (recipientId ?: "")),
            )
        }
    }

    private fun saveImageToGallery(bytes: ByteArray, fileName: String): String {
        val safeFileName = if (fileName.endsWith(".png")) fileName else "$fileName.png"
        val resolver = applicationContext.contentResolver

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            val contentValues = ContentValues().apply {
                put(MediaStore.Images.Media.DISPLAY_NAME, safeFileName)
                put(MediaStore.Images.Media.MIME_TYPE, "image/png")
                put(
                    MediaStore.Images.Media.RELATIVE_PATH,
                    "${Environment.DIRECTORY_PICTURES}/Grix"
                )
                put(MediaStore.Images.Media.IS_PENDING, 1)
            }

            val uri = resolver.insert(
                MediaStore.Images.Media.EXTERNAL_CONTENT_URI,
                contentValues
            ) ?: error("Failed to create media store record")

            resolver.openOutputStream(uri)?.use { outputStream ->
                outputStream.write(bytes)
                outputStream.flush()
            } ?: error("Failed to open output stream")

            contentValues.clear()
            contentValues.put(MediaStore.Images.Media.IS_PENDING, 0)
            resolver.update(uri, contentValues, null, null)
            return uri.toString()
        }

        val picturesDir = Environment.getExternalStoragePublicDirectory(
            Environment.DIRECTORY_PICTURES
        )
        val outputDir = File(picturesDir, "Grix")
        if (!outputDir.exists() && !outputDir.mkdirs()) {
            error("Failed to create output directory")
        }
        val outputFile = File(outputDir, safeFileName)
        FileOutputStream(outputFile).use { stream ->
            stream.write(bytes)
            stream.flush()
        }
        MediaScannerConnection.scanFile(
            this,
            arrayOf(outputFile.absolutePath),
            arrayOf("image/png"),
            null
        )
        return outputFile.absolutePath
    }

    private fun saveVideoToGallery(
        filePath: String,
        fileName: String,
        mimeType: String
    ): String {
        val sourceFile = File(filePath)
        if (!sourceFile.exists()) {
            error("Source video file not found: $filePath")
        }
        val resolver = applicationContext.contentResolver

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            val contentValues = ContentValues().apply {
                put(MediaStore.Video.Media.DISPLAY_NAME, fileName)
                put(MediaStore.Video.Media.MIME_TYPE, mimeType)
                put(
                    MediaStore.Video.Media.RELATIVE_PATH,
                    "${Environment.DIRECTORY_MOVIES}/Grix"
                )
                put(MediaStore.Video.Media.IS_PENDING, 1)
            }

            val uri = resolver.insert(
                MediaStore.Video.Media.EXTERNAL_CONTENT_URI,
                contentValues
            ) ?: error("Failed to create media store record")

            try {
                resolver.openOutputStream(uri)?.use { outputStream ->
                    sourceFile.inputStream().use { input ->
                        input.copyTo(outputStream)
                    }
                } ?: error("Failed to open output stream")
            } catch (error: Exception) {
                // 写流失败时回滚，避免相册残留 IS_PENDING=1 的孤儿记录。
                resolver.delete(uri, null, null)
                throw error
            }

            contentValues.clear()
            contentValues.put(MediaStore.Video.Media.IS_PENDING, 0)
            resolver.update(uri, contentValues, null, null)
            return uri.toString()
        }

        val moviesDir = Environment.getExternalStoragePublicDirectory(
            Environment.DIRECTORY_MOVIES
        )
        val outputDir = File(moviesDir, "Grix")
        if (!outputDir.exists() && !outputDir.mkdirs()) {
            error("Failed to create output directory")
        }
        val outputFile = File(outputDir, fileName)
        sourceFile.inputStream().use { input ->
            FileOutputStream(outputFile).use { stream ->
                input.copyTo(stream)
            }
        }
        MediaScannerConnection.scanFile(
            this,
            arrayOf(outputFile.absolutePath),
            arrayOf(mimeType),
            null
        )
        return outputFile.absolutePath
    }

    private fun resolveAndroidPushInfo(result: MethodChannel.Result) {
        val order = PushChannelResolver.channelOrder(
            Build.MANUFACTURER,
            Build.BRAND,
            isGooglePlayServicesAvailable(),
        )
        resolvePushInfoInOrder(order, 0, mutableListOf(), result)
    }

    /**
     * 沿通道优先级依次尝试取 token，任一条成功即上报；全部失败才报错。
     * 每个分支要么成功一次，要么恰好调用一次 next，绝不重复回调 result。
     *
     * [trail] 记录被跳过/失败的通道及原因（如 "android_huawei:timeout"），随最终结果
     * 一并上报给后端，方便排查"本该走厂商通道却兜底到极光"这类问题，不需要额外拉取设备日志。
     */
    private fun resolvePushInfoInOrder(
        order: List<PushChannel>,
        index: Int,
        trail: MutableList<String>,
        result: MethodChannel.Result,
    ) {
        if (index >= order.size) {
            result.error(
                "push_channel_unavailable",
                "No push channel could provide a device token on this device.",
                trail.joinToString(",")
            )
            return
        }

        fun next(reason: String) {
            trail.add("${order[index].platform}:$reason")
            resolvePushInfoInOrder(order, index + 1, trail, result)
        }
        when (order[index]) {
            PushChannel.HUAWEI -> resolveHuaweiPushInfo(result, trail) { reason -> next(reason) }
            PushChannel.FCM -> resolveFcmPushInfo(result, trail) { next("unavailable") }
            PushChannel.JPUSH -> resolveJPushInfo(result, trail) { next("unavailable") }
            // 荣耀 / 小米 / OPPO / vivo 的 SDK 待各家凭据到位后接入。
            // 未接入前该 ROM 直接降级到下一条通道。
            else -> next("not_implemented")
        }
    }

    private fun resolveHuaweiPushInfo(
        result: MethodChannel.Result,
        trail: MutableList<String>,
        onUnavailable: (String) -> Unit,
    ) {
        HuaweiPushTokenSource.resolve(
            context = applicationContext,
            appId = BuildConfig.AIBOT_HUAWEI_APP_ID.trim(),
            onToken = { token ->
                result.success(
                    mapOf(
                        "platform" to PushChannel.HUAWEI.platform,
                        "pushEnv" to "default",
                        "deviceToken" to token,
                        "channelTrail" to trail.joinToString(","),
                    )
                )
            },
            onUnavailable = onUnavailable,
        )
    }

    private fun signInWithGoogle(result: MethodChannel.Result) {
        if (!isGooglePlayServicesAvailable()) {
            result.error(
                "unsupported_platform",
                "Google Play Services is unavailable on this device.",
                null
            )
            return
        }

        val serverClientId = BuildConfig.AIBOT_GOOGLE_WEB_CLIENT_ID.trim()
        if (serverClientId.isBlank()) {
            result.error(
                "google_config_missing",
                "Missing AIBOT_ANDROID_GOOGLE_WEB_CLIENT_ID for Google sign-in.",
                null
            )
            return
        }

        val googleIdOption = GetGoogleIdOption.Builder()
            .setServerClientId(serverClientId)
            .setFilterByAuthorizedAccounts(false)
            .setAutoSelectEnabled(false)
            .build()
        val request = GetCredentialRequest.Builder()
            .addCredentialOption(googleIdOption)
            .build()
        val credentialManager = CredentialManager.create(applicationContext)

        lifecycleScope.launch {
            try {
                val response = credentialManager.getCredential(this@MainActivity, request)
                val credential = response.credential
                if (credential !is CustomCredential ||
                    credential.type != GoogleIdTokenCredential.TYPE_GOOGLE_ID_TOKEN_CREDENTIAL
                ) {
                    result.error(
                        "sign_in_failed",
                        "Unsupported Google credential response.",
                        null
                    )
                    return@launch
                }

                val googleCredential = GoogleIdTokenCredential.createFrom(credential.data)
                val idToken = googleCredential.idToken.orEmpty().trim()
                if (idToken.isBlank()) {
                    result.error(
                        "sign_in_failed",
                        "Google ID token is empty.",
                        null
                    )
                    return@launch
                }

                result.success(
                    mapOf(
                        "idToken" to idToken,
                    )
                )
            } catch (_: GetCredentialCancellationException) {
                result.error(
                    "sign_in_cancelled",
                    "Google sign-in was cancelled.",
                    null
                )
            } catch (error: GoogleIdTokenParsingException) {
                result.error(
                    "sign_in_failed",
                    error.message ?: "Failed to parse Google sign-in result.",
                    null
                )
            } catch (error: GetCredentialException) {
                result.error(
                    "sign_in_failed",
                    error.message ?: "Google sign-in failed.",
                    null
                )
            } catch (error: Exception) {
                result.error(
                    "sign_in_failed",
                    error.message ?: "Google sign-in failed.",
                    null
                )
            }
        }
    }

    private fun resolveFcmPushInfo(
        result: MethodChannel.Result,
        trail: MutableList<String>,
        onUnavailable: () -> Unit,
    ) {
        val app = ensureFirebaseApp()
        if (app == null) {
            onUnavailable()
            return
        }

        FirebaseMessaging.getInstance().token
            .addOnCompleteListener { task ->
                // task 失败时读取 result 会抛异常，务必先判成功再取值，
                // 否则异常逃逸出回调、Flutter 侧的 result 永远等不到回调。
                if (!task.isSuccessful) {
                    onUnavailable()
                    return@addOnCompleteListener
                }
                val token = task.result.orEmpty().trim()
                if (token.isBlank()) {
                    onUnavailable()
                    return@addOnCompleteListener
                }

                result.success(
                    mapOf(
                        "platform" to PushChannel.FCM.platform,
                        "pushEnv" to "default",
                        "deviceToken" to token,
                        "channelTrail" to trail.joinToString(","),
                    )
                )
            }
    }

    private fun resolveJPushInfo(
        result: MethodChannel.Result,
        trail: MutableList<String>,
        onUnavailable: () -> Unit,
    ) {
        if (BuildConfig.AIBOT_JPUSH_APP_KEY.isBlank()) {
            onUnavailable()
            return
        }

        JPushInterface.setDebugMode(BuildConfig.DEBUG)
        JPushInterface.init(applicationContext)
        waitForJPushRegistrationId(result, 0, trail, onUnavailable)
    }

    private fun waitForJPushRegistrationId(
        result: MethodChannel.Result,
        attempt: Int,
        trail: MutableList<String>,
        onUnavailable: () -> Unit,
    ) {
        val registrationId = JPushInterface.getRegistrationID(applicationContext).orEmpty().trim()
        if (registrationId.isNotEmpty()) {
            result.success(
                mapOf(
                    "platform" to PushChannel.JPUSH.platform,
                    "pushEnv" to "default",
                    "deviceToken" to registrationId,
                    "channelTrail" to trail.joinToString(","),
                )
            )
            return
        }

        if (attempt >= JPUSH_MAX_POLLS) {
            onUnavailable()
            return
        }

        Handler(Looper.getMainLooper()).postDelayed(
            { waitForJPushRegistrationId(result, attempt + 1, trail, onUnavailable) },
            JPUSH_POLL_INTERVAL_MS
        )
    }

    private fun isGooglePlayServicesAvailable(): Boolean {
        return GoogleApiAvailability.getInstance()
            .isGooglePlayServicesAvailable(this) == ConnectionResult.SUCCESS
    }

    private fun ensureFirebaseApp(): FirebaseApp? {
        FirebaseApp.getApps(this).firstOrNull()?.let { return it }

        val apiKey = BuildConfig.AIBOT_FIREBASE_API_KEY
        val appId = BuildConfig.AIBOT_FIREBASE_APP_ID
        val projectId = BuildConfig.AIBOT_FIREBASE_PROJECT_ID
        val senderId = BuildConfig.AIBOT_FIREBASE_SENDER_ID
        if (apiKey.isBlank() || appId.isBlank() || projectId.isBlank() || senderId.isBlank()) {
            return null
        }

        val options = FirebaseOptions.Builder()
            .setApiKey(apiKey)
            .setApplicationId(appId)
            .setProjectId(projectId)
            .setGcmSenderId(senderId)
            .build()
        return FirebaseApp.initializeApp(this, options)
    }
}
