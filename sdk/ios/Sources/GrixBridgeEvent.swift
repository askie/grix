import Foundation

/// Events emitted by `/public/widget/app.html` over `webkit.messageHandlers.grix`.
public enum GrixBridgeEvent: Equatable {
    case ready(visitorKey: String, sessionId: String)
    case close
    case sessionClosed(reason: String)
    case unread(count: Int)
    case openURL(url: URL)
    case error(code: String, message: String)

    /// Parses a message payload received from `WKScriptMessage.body` (either `[String: Any]` or a JSON `String`).
    public static func parse(_ body: Any) -> GrixBridgeEvent? {
        let dict: [String: Any]
        if let d = body as? [String: Any] {
            dict = d
        } else if let str = body as? String,
                  let data = str.data(using: .utf8),
                  let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        {
            dict = obj
        } else {
            return nil
        }

        guard let type = dict["type"] as? String else {
            return nil
        }

        switch type {
        case "ready":
            let visitorKey = (dict["visitor_key"] as? String) ?? ""
            let sessionId = (dict["session_id"] as? String) ?? ""
            return .ready(visitorKey: visitorKey, sessionId: sessionId)

        case "close":
            return .close

        case "session_closed":
            let reason = (dict["reason"] as? String) ?? "closed"
            return .sessionClosed(reason: reason)

        case "unread":
            let count: Int
            if let intVal = dict["count"] as? Int {
                count = max(0, intVal)
            } else if let doubleVal = dict["count"] as? Double {
                count = max(0, Int(doubleVal))
            } else if let numVal = dict["count"] as? NSNumber {
                count = max(0, numVal.intValue)
            } else {
                count = 0
            }
            return .unread(count: count)

        case "open_url":
            guard let rawURL = (dict["url"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines),
                  let url = URL(string: rawURL),
                  let scheme = url.scheme?.lowercased(),
                  scheme == "http" || scheme == "https"
            else {
                return nil
            }
            return .openURL(url: url)

        case "error":
            let code = (dict["code"] as? String) ?? "unknown_error"
            let message = (dict["message"] as? String) ?? ""
            return .error(code: code, message: message)

        default:
            return nil
        }
    }
}
