package pub.dhf.grix.push

/**
 * 推送通道标识。platform 与后端 device.platform 枚举一一对应，
 * 由 /devices/bind 上报，决定后端用哪条通道下发。
 */
enum class PushChannel(val platform: String) {
    HUAWEI("android_huawei"),
    HONOR("android_honor"),
    XIAOMI("android_xiaomi"),
    OPPO("android_oppo"),
    VIVO("android_vivo"),
    FCM("android_fcm"),
    JPUSH("android_jpush"),
}

/**
 * 按设备 ROM 决定推送通道的优先级。
 *
 * 国产 ROM 优先走各自的系统级通道：通知由系统代收代展示，应用进程被杀后仍可送达。
 * 非国产 ROM 走 FCM；两者都拿不到 token 时由极光自有通道兜底。
 */
object PushChannelResolver {

    // 各厂商通道认领的 Build.MANUFACTURER / Build.BRAND 取值（均按小写比较）。
    // 荣耀须排在华为之前：分家前的荣耀机 MANUFACTURER 仍是 HUAWEI，只有 BRAND 是 HONOR。
    private val VENDOR_BRANDS: List<Pair<PushChannel, Set<String>>> = listOf(
        PushChannel.HONOR to setOf("honor"),
        PushChannel.HUAWEI to setOf("huawei"),
        PushChannel.XIAOMI to setOf("xiaomi", "redmi", "poco"),
        PushChannel.OPPO to setOf("oppo", "oneplus", "realme"),
        PushChannel.VIVO to setOf("vivo", "iqoo"),
    )

    /**
     * 解析该设备所属的厂商通道；非国产 ROM 返回 null。
     */
    fun vendorChannelFor(manufacturer: String?, brand: String?): PushChannel? {
        val candidates = listOfNotNull(
            brand?.trim()?.lowercase()?.takeIf { it.isNotEmpty() },
            manufacturer?.trim()?.lowercase()?.takeIf { it.isNotEmpty() },
        )
        if (candidates.isEmpty()) return null

        for ((channel, brands) in VENDOR_BRANDS) {
            if (candidates.any { it in brands }) return channel
        }
        return null
    }

    /**
     * 返回该设备上应依次尝试的通道。前一条拿不到 token 时降级到下一条，
     * 保证任何设备都不会因为单一通道不可用而彻底收不到推送。
     *
     * 每台设备至多命中一个厂商通道，厂商 SDK 也只在对应 ROM 上初始化——
     * 避免非目标机型上多起一条无谓的长连接。
     */
    fun channelOrder(
        manufacturer: String?,
        brand: String?,
        googlePlayServicesAvailable: Boolean,
    ): List<PushChannel> {
        val order = mutableListOf<PushChannel>()
        vendorChannelFor(manufacturer, brand)?.let { order.add(it) }
        if (googlePlayServicesAvailable) order.add(PushChannel.FCM)
        order.add(PushChannel.JPUSH)
        return order
    }
}
