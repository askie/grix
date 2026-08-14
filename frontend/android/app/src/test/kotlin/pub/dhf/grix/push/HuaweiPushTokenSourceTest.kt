package pub.dhf.grix.push

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class HuaweiPushTokenSourceTest {

    @Before
    fun setUp() = HuaweiPushTokenHolder.reset()

    /** 海外包不注入 App ID，必须据此完全不触碰 HMS，直接降级到下一条通道。 */
    @Test
    fun `blank app id counts as unconfigured`() {
        assertFalse(HuaweiPushTokenSource.isConfigured(""))
        assertFalse(HuaweiPushTokenSource.isConfigured("   "))
    }

    @Test
    fun `non blank app id counts as configured`() {
        assertTrue(HuaweiPushTokenSource.isConfigured("117472457"))
    }

    @Test
    fun `holder starts empty`() {
        assertNull(HuaweiPushTokenHolder.peek())
    }

    @Test
    fun `holder stores trimmed token`() {
        HuaweiPushTokenHolder.publish("  hms-token  ")
        assertEquals("hms-token", HuaweiPushTokenHolder.peek())
    }

    /** HMS 用空串表示"token 尚未就绪"，写入会把已拿到的有效 token 抹掉。 */
    @Test
    fun `holder ignores null and empty tokens`() {
        HuaweiPushTokenHolder.publish("hms-token")
        HuaweiPushTokenHolder.publish(null)
        HuaweiPushTokenHolder.publish("")
        HuaweiPushTokenHolder.publish("   ")
        assertEquals("hms-token", HuaweiPushTokenHolder.peek())
    }

    @Test
    fun `holder overwrites with a newer token`() {
        HuaweiPushTokenHolder.publish("old-token")
        HuaweiPushTokenHolder.publish("new-token")
        assertEquals("new-token", HuaweiPushTokenHolder.peek())
    }
}
