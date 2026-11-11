package im.grix.widget

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GrixWidgetTest {

    @Test
    fun `buildAppUrl places all startup parameters in fragment and none in query`() {
        val config = GrixConfig(
            siteKey = "wk_live_abc",
            apiBase = "https://gb.grix.im/",
            locale = "zh-CN",
            themeColor = "#0f766e",
            title = "Grix 客服",
            user = GrixUser(name = "张三", email = "zhangsan@example.com")
        )
        val url = GrixUrlBuilder.buildAppUrl(
            config = config,
            hostPackageName = "com.example.shop",
            visitorKey = "vk_999"
        )

        assertFalse("URL must not contain query string", url.substringBefore("#").contains("?"))
        assertTrue(url.startsWith("https://gb.grix.im/public/widget/app.html#"))

        val fragment = url.substringAfter("#")
        assertTrue(fragment.contains("site_key=wk_live_abc"))
        assertTrue(fragment.contains("platform=android"))
        assertTrue(fragment.contains("app_id=com.example.shop"))
        assertTrue(fragment.contains("sdk_version=1.0.0"))
        assertTrue(fragment.contains("visitor_key=vk_999"))
        assertTrue(fragment.contains("locale=zh-CN"))
        assertTrue(fragment.contains("theme_color=%230f766e"))
        assertTrue(fragment.contains("visitor_email=zhangsan%40example.com"))
    }

    @Test
    fun `parse GrixBridgeEvent handles all 6 event types`() {
        val ready = GrixBridgeEvent.parse("""{"type":"ready","visitor_key":"vk_1","session_id":"sess_1"}""")
        assertEquals(GrixBridgeEvent.Ready("vk_1", "sess_1"), ready)

        val close = GrixBridgeEvent.parse("""{"type":"close"}""")
        assertEquals(GrixBridgeEvent.Close, close)

        val sessionClosed = GrixBridgeEvent.parse("""{"type":"session_closed","reason":"closed"}""")
        assertEquals(GrixBridgeEvent.SessionClosed("closed"), sessionClosed)

        val unread = GrixBridgeEvent.parse("""{"type":"unread","count":4}""")
        assertEquals(GrixBridgeEvent.Unread(4), unread)

        val openUrl = GrixBridgeEvent.parse("""{"type":"open_url","url":"https://grix.im/docs"}""")
        assertEquals(GrixBridgeEvent.OpenUrl("https://grix.im/docs"), openUrl)

        // Reject javascript: schemes
        assertNull(GrixBridgeEvent.parse("""{"type":"open_url","url":"javascript:alert(1)"}"""))

        val err = GrixBridgeEvent.parse("""{"type":"error","code":"4003","message":"origin not allowed"}""")
        assertEquals(GrixBridgeEvent.Error("4003", "origin not allowed"), err)
    }

    @Test
    fun `visitor store scopes visitor_key by siteKey`() {
        val store = GrixMemoryVisitorStore()
        store.saveVisitorKey("vk_site_a", "site_a")
        store.saveVisitorKey("vk_site_b", "site_b")

        assertEquals("vk_site_a", store.getVisitorKey("site_a"))
        assertEquals("vk_site_b", store.getVisitorKey("site_b"))
        assertNull(store.getVisitorKey("site_c"))
    }

    @Test
    fun `callbacks are invoked when bridge events arrive`() {
        var closedCalled = false
        var unreadVal = -1
        var sessionClosedReason = ""
        var errorPair = ""

        GrixWidget.onClose = { closedCalled = true }
        GrixWidget.onUnreadChange = { unreadVal = it }
        GrixWidget.onSessionClosed = { sessionClosedReason = it }
        GrixWidget.onError = { code, msg -> errorPair = "$code:$msg" }

        GrixWidget.handleBridgeEvent(null, GrixBridgeEvent.Close)
        GrixWidget.handleBridgeEvent(null, GrixBridgeEvent.Unread(5))
        GrixWidget.handleBridgeEvent(null, GrixBridgeEvent.SessionClosed("closed"))
        GrixWidget.handleBridgeEvent(null, GrixBridgeEvent.Error("4003", "origin not allowed"))

        assertTrue(closedCalled)
        assertEquals(5, unreadVal)
        assertEquals("closed", sessionClosedReason)
        assertEquals("4003:origin not allowed", errorPair)
    }
}
