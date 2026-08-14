package pub.dhf.grix.push

import android.content.Context
import android.os.Handler
import android.os.Looper
import android.util.Log
import com.huawei.hms.aaid.HmsInstanceId
import java.util.concurrent.atomic.AtomicReference
import kotlin.concurrent.thread

/**
 * 暂存 HMS 异步下发的 token。
 *
 * [HuaweiPushService.onNewToken] 可能早于、也可能晚于 [HuaweiPushTokenSource.resolve] 的等待窗口，
 * 因此用一个进程内的槽位承接，两侧各自读写。
 */
object HuaweiPushTokenHolder {
    private val token = AtomicReference<String?>(null)

    /** 只接受非空 token：HMS 会以空串表示"尚未就绪"，写入会把已有的有效值覆盖掉。 */
    fun publish(value: String?) {
        val trimmed = value?.trim()
        if (!trimmed.isNullOrEmpty()) token.set(trimmed)
    }

    fun peek(): String? = token.get()

    /** 仅供测试复位。 */
    internal fun reset() = token.set(null)
}

/**
 * 取华为 Push Kit 的设备 token。
 *
 * HMS 的 token 有两条到达路径：`getToken` 同步返回，或稍后经 `onNewToken` 异步下发。
 * 两条都要接住，否则首次安装大概率拿不到 token。
 */
object HuaweiPushTokenSource {

    private const val TAG = "HuaweiPushTokenSource"

    /** HMS 规定的推送 token 作用域。 */
    private const val TOKEN_SCOPE = "HCM"

    private const val POLL_INTERVAL_MS = 500L

    /** getToken 同步返回空串时的等待预算：token 正在路上，值得等满 10 秒。 */
    private const val MAX_POLLS = 20

    /**
     * getToken 抛异常时的等待预算（3 秒）。
     *
     * 抛异常多半是本机没有 HMS（海外设备的常态），此时久等无益；但也可能是
     * HMS Core 尚未就绪的瞬时抖动，而 onNewToken 仍会把 token 送达。给一个短窗口，
     * 免得华为机因一次抖动在本次启动内永久掉到极光通道。
     */
    private const val MAX_POLLS_AFTER_ERROR = 6

    /**
     * App ID 未注入即视为未配置。海外包不设该环境变量，据此完全不触碰 HMS。
     */
    fun isConfigured(appId: String): Boolean = appId.isNotBlank()

    /**
     * 异步取 token。成功回调 [onToken]；本机不支持 HMS、未配置 App ID 或超时则回调 [onUnavailable]，
     * 并带上原因（`not_configured` / `get_token_error` / `timeout`），供上层埋点排查降级原因。
     * 两个回调恰好触发其一，且都在主线程。
     */
    fun resolve(
        context: Context,
        appId: String,
        onToken: (String) -> Unit,
        onUnavailable: (String) -> Unit,
    ) {
        if (!isConfigured(appId)) {
            onUnavailable("not_configured")
            return
        }

        // getToken 是阻塞的网络调用，禁止在主线程执行。
        thread(name = "huawei-push-token") {
            val direct = try {
                HmsInstanceId.getInstance(context.applicationContext)
                    .getToken(appId, TOKEN_SCOPE)
                    .orEmpty()
                    .trim()
            } catch (error: Throwable) {
                // 接 Throwable 而非 Exception：本线程背着"恰好回调一次"的契约，
                // Error 逃逸会让 Flutter 侧的 result 永远等不到回调。
                // 抛异常不等于没有 token：onNewToken 可能已送达或仍在路上，
                // 故仍进短窗口等待，而不是直接判死。
                Log.i(TAG, "HMS getToken failed: ${error.message}")
                Handler(Looper.getMainLooper()).post {
                    awaitAsyncToken(0, MAX_POLLS_AFTER_ERROR, "get_token_error", onToken, onUnavailable)
                }
                return@thread
            }

            if (direct.isNotEmpty()) {
                HuaweiPushTokenHolder.publish(direct)
                Handler(Looper.getMainLooper()).post { onToken(direct) }
                return@thread
            }

            // 同步为空：token 会经 onNewToken 异步送达，轮询等待。
            Handler(Looper.getMainLooper()).post {
                awaitAsyncToken(0, MAX_POLLS, "timeout", onToken, onUnavailable)
            }
        }
    }

    private fun awaitAsyncToken(
        attempt: Int,
        maxPolls: Int,
        timeoutReason: String,
        onToken: (String) -> Unit,
        onUnavailable: (String) -> Unit,
    ) {
        HuaweiPushTokenHolder.peek()?.let {
            onToken(it)
            return
        }
        if (attempt >= maxPolls) {
            Log.i(TAG, "timed out waiting for HMS token, reason=$timeoutReason")
            onUnavailable(timeoutReason)
            return
        }
        Handler(Looper.getMainLooper()).postDelayed(
            { awaitAsyncToken(attempt + 1, maxPolls, timeoutReason, onToken, onUnavailable) },
            POLL_INTERVAL_MS,
        )
    }
}
