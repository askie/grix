package pub.dhf.grix.push

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PushChannelResolverTest {

    @Test
    fun `maps each vendor rom to its own channel`() {
        val cases = mapOf(
            ("HUAWEI" to "HUAWEI") to PushChannel.HUAWEI,
            ("Xiaomi" to "Redmi") to PushChannel.XIAOMI,
            ("Xiaomi" to "POCO") to PushChannel.XIAOMI,
            ("OPPO" to "OPPO") to PushChannel.OPPO,
            ("OnePlus" to "OnePlus") to PushChannel.OPPO,
            ("realme" to "realme") to PushChannel.OPPO,
            ("vivo" to "vivo") to PushChannel.VIVO,
            ("vivo" to "iQOO") to PushChannel.VIVO,
        )
        for ((device, expected) in cases) {
            val (manufacturer, brand) = device
            assertEquals(
                "manufacturer=$manufacturer brand=$brand",
                expected,
                PushChannelResolver.vendorChannelFor(manufacturer, brand),
            )
        }
    }

    /** 分家前的荣耀机 MANUFACTURER 仍是 HUAWEI，必须按 BRAND 判成荣耀通道。 */
    @Test
    fun `legacy honor device with huawei manufacturer resolves to honor`() {
        assertEquals(
            PushChannel.HONOR,
            PushChannelResolver.vendorChannelFor("HUAWEI", "HONOR"),
        )
    }

    @Test
    fun `standalone honor device resolves to honor`() {
        assertEquals(
            PushChannel.HONOR,
            PushChannelResolver.vendorChannelFor("HONOR", "HONOR"),
        )
    }

    @Test
    fun `non vendor rom has no vendor channel`() {
        assertNull(PushChannelResolver.vendorChannelFor("Google", "google"))
        assertNull(PushChannelResolver.vendorChannelFor("samsung", "samsung"))
        assertNull(PushChannelResolver.vendorChannelFor("meizu", "meizu"))
        assertNull(PushChannelResolver.vendorChannelFor(null, null))
        assertNull(PushChannelResolver.vendorChannelFor("", "  "))
    }

    /** 国产机：先厂商通道，无 GMS 时不试 FCM，最后极光兜底。 */
    @Test
    fun `vendor rom without gms tries vendor then jpush`() {
        assertEquals(
            listOf(PushChannel.HUAWEI, PushChannel.JPUSH),
            PushChannelResolver.channelOrder("HUAWEI", "HUAWEI", googlePlayServicesAvailable = false),
        )
    }

    /** 国产机装了 GMS：厂商通道仍然优先，FCM 作为第一降级。 */
    @Test
    fun `vendor rom with gms keeps vendor first`() {
        assertEquals(
            listOf(PushChannel.XIAOMI, PushChannel.FCM, PushChannel.JPUSH),
            PushChannelResolver.channelOrder("Xiaomi", "Redmi", googlePlayServicesAvailable = true),
        )
    }

    /** 海外机行为与改动前一致：FCM 优先、极光兜底。 */
    @Test
    fun `non vendor rom with gms falls back to fcm then jpush`() {
        assertEquals(
            listOf(PushChannel.FCM, PushChannel.JPUSH),
            PushChannelResolver.channelOrder("Google", "google", googlePlayServicesAvailable = true),
        )
    }

    @Test
    fun `non vendor rom without gms uses jpush only`() {
        assertEquals(
            listOf(PushChannel.JPUSH),
            PushChannelResolver.channelOrder("meizu", "meizu", googlePlayServicesAvailable = false),
        )
    }

    /** 后端拒绝某条通道后，该通道从降级链上摘掉，设备落到下一条。 */
    @Test
    fun `excluded channel is dropped from the order`() {
        assertEquals(
            listOf(PushChannel.JPUSH),
            PushChannelResolver.channelOrder(
                "Google",
                "google",
                googlePlayServicesAvailable = true,
                excludedPlatforms = setOf("android_fcm"),
            ),
        )
    }

    @Test
    fun `excluding every channel yields an empty order`() {
        assertEquals(
            emptyList<PushChannel>(),
            PushChannelResolver.channelOrder(
                "Google",
                "google",
                googlePlayServicesAvailable = true,
                excludedPlatforms = setOf("android_fcm", "android_jpush"),
            ),
        )
    }

    /** platform 串是与后端的契约，改动即破坏已绑定设备的路由。 */
    @Test
    fun `channel platform identifiers match backend contract`() {
        assertEquals("android_huawei", PushChannel.HUAWEI.platform)
        assertEquals("android_honor", PushChannel.HONOR.platform)
        assertEquals("android_xiaomi", PushChannel.XIAOMI.platform)
        assertEquals("android_oppo", PushChannel.OPPO.platform)
        assertEquals("android_vivo", PushChannel.VIVO.platform)
        assertEquals("android_fcm", PushChannel.FCM.platform)
        assertEquals("android_jpush", PushChannel.JPUSH.platform)
    }
}
