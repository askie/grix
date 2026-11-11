import Foundation
import XCTest
@testable import GrixWidget

final class GrixWidgetTests: XCTestCase {
    func testBuildAppURLUsesFragmentNotQuery() {
        let config = GrixConfig(
            siteKey: "site_abc123",
            apiBase: "https://gb.grix.im/",
            locale: "zh-CN",
            themeColor: "#0f766e",
            title: "Grix 客服",
            user: GrixUser(name: "Alice & Bob", email: "alice+test@example.com"),
            appIdOverride: "Com.Example.MyApp"
        )

        let url = GrixURLBuilder.buildAppURL(config: config, visitorKey: "vk_999")
        XCTAssertNotNil(url)
        XCTAssertNil(url?.query, "Startup parameters must not be placed in the query string")
        XCTAssertEqual(url?.scheme, "https")
        XCTAssertEqual(url?.host, "gb.grix.im")
        XCTAssertEqual(url?.path, "/public/widget/app.html")

        let fragment = url?.fragment ?? ""
        XCTAssertTrue(fragment.contains("site_key=site_abc123"))
        XCTAssertTrue(fragment.contains("platform=ios"))
        XCTAssertTrue(fragment.contains("app_id=com.example.myapp"))
        XCTAssertTrue(fragment.contains("sdk_version=1.0.0"))
        XCTAssertTrue(fragment.contains("visitor_key=vk_999"))
        XCTAssertTrue(fragment.contains("locale=zh-CN"))
        XCTAssertTrue(fragment.contains("theme_color=%230f766e"))
        XCTAssertTrue(fragment.contains("visitor_name=Alice%20%26%20Bob"))
        XCTAssertTrue(fragment.contains("visitor_email=alice%2Btest@example.com"))
    }

    func testOriginMatcherTreatsDefaultPortZeroAsHTTPSDefault() {
        XCTAssertTrue(GrixOriginMatcher.matches(
            apiBase: "https://grix.example.com",
            scheme: "https",
            host: "grix.example.com",
            port: 0
        ))
        XCTAssertTrue(GrixOriginMatcher.matches(
            apiBase: "https://grix.example.com",
            scheme: "https",
            host: "grix.example.com",
            port: 443
        ))
        XCTAssertFalse(GrixOriginMatcher.matches(
            apiBase: "https://grix.example.com",
            scheme: "https",
            host: "grix.example.com",
            port: 8443
        ))
    }

    func testBridgeEventParsing() {
        let readyEvent = GrixBridgeEvent.parse([
            "type": "ready",
            "visitor_key": "vk_001",
            "session_id": "sess_001",
        ])
        XCTAssertEqual(readyEvent, .ready(visitorKey: "vk_001", sessionId: "sess_001"))

        let closeEvent = GrixBridgeEvent.parse("{\"type\":\"close\"}")
        XCTAssertEqual(closeEvent, .close)

        let sessionClosedEvent = GrixBridgeEvent.parse([
            "type": "session_closed",
            "reason": "closed",
        ])
        XCTAssertEqual(sessionClosedEvent, .sessionClosed(reason: "closed"))

        let unreadEvent = GrixBridgeEvent.parse([
            "type": "unread",
            "count": 5,
        ])
        XCTAssertEqual(unreadEvent, .unread(count: 5))

        let openUrlEvent = GrixBridgeEvent.parse([
            "type": "open_url",
            "url": "https://grix.im/docs",
        ])
        XCTAssertEqual(openUrlEvent, .openURL(url: URL(string: "https://grix.im/docs")!))

        let disallowedJavascriptUrl = GrixBridgeEvent.parse([
            "type": "open_url",
            "url": "javascript:alert(1)",
        ])
        XCTAssertNil(disallowedJavascriptUrl)

        let errorEvent = GrixBridgeEvent.parse([
            "type": "error",
            "code": "app_not_allowed",
            "message": "app is not allowed for this site",
        ])
        XCTAssertEqual(errorEvent, .error(code: "app_not_allowed", message: "app is not allowed for this site"))
    }

    func testVisitorKeyPersistenceScopedBySiteKey() {
        let store = GrixMemoryVisitorStore()
        XCTAssertNil(store.visitorKey(for: "site_1"))
        XCTAssertNil(store.visitorKey(for: "site_2"))

        store.saveVisitorKey("visitor_a", for: "site_1")
        store.saveVisitorKey("visitor_b", for: "site_2")

        XCTAssertEqual(store.visitorKey(for: "site_1"), "visitor_a")
        XCTAssertEqual(store.visitorKey(for: "site_2"), "visitor_b")

        store.saveVisitorKey("visitor_a_updated", for: "site_1")
        XCTAssertEqual(store.visitorKey(for: "site_1"), "visitor_a_updated")
    }

    func testCallbacksTriggeredByBridgeEvents() {
        var didClose = false
        var unreadCount = -1
        var closedReason = ""
        var receivedError: (String, String)?

        GrixWidget.onClose = { didClose = true }
        GrixWidget.onUnreadChange = { unreadCount = $0 }
        GrixWidget.onSessionClosed = { closedReason = $0 }
        GrixWidget.onError = { receivedError = ($0, $1) }

        GrixWidget.shared.handleBridgeEvent(.close)
        GrixWidget.shared.handleBridgeEvent(.unread(count: 3))
        GrixWidget.shared.handleBridgeEvent(.sessionClosed(reason: "closed"))
        GrixWidget.shared.handleBridgeEvent(.error(code: "app_not_allowed", message: "Denied"))

        XCTAssertTrue(didClose)
        XCTAssertEqual(unreadCount, 3)
        XCTAssertEqual(closedReason, "closed")
        XCTAssertEqual(receivedError?.0, "app_not_allowed")
        XCTAssertEqual(receivedError?.1, "Denied")
    }
}
