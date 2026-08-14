package pub.dhf.grix.push

import android.os.Bundle
import com.huawei.hms.push.HmsMessageService

/**
 * 接收 HMS Push 异步下发的 token。
 *
 * `HmsInstanceId.getToken` 首次调用常返回空串，真正的 token 稍后经此回调送达，
 * 故需在此暂存，供 [HuaweiPushTokenSource] 取用。
 *
 * 两个重载都覆写：
 *   - 双参版本由 SDK **无条件**调用，是可靠的那条路径；
 *   - 单参版本仅在 SDK 内部缓存标记命中时才被调用（且已被华为标记废弃），
 *     只覆写它等于依赖无文档的内部实现细节。
 *
 * [HuaweiPushTokenHolder] 对重复写入是安全的，两个都覆写没有副作用。
 */
class HuaweiPushService : HmsMessageService() {

    override fun onNewToken(token: String?) {
        HuaweiPushTokenHolder.publish(token)
    }

    override fun onNewToken(token: String?, bundle: Bundle?) {
        HuaweiPushTokenHolder.publish(token)
    }
}
