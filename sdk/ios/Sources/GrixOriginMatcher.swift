import Foundation

/// Pure origin comparison used by navigation and media-capture permission checks.
enum GrixOriginMatcher {
    /// Returns true when the candidate origin has the same scheme, host, and
    /// effective port as `apiBase`.
    static func matches(
        apiBase: String,
        scheme: String?,
        host: String?,
        port: Int?
    ) -> Bool {
        guard let baseURL = URL(string: apiBase),
              let baseScheme = baseURL.scheme?.lowercased(),
              let baseHost = baseURL.host?.lowercased()
        else {
            return false
        }
        let candidateScheme = scheme?.lowercased() ?? ""
        let candidateHost = host?.lowercased() ?? ""
        return candidateScheme == baseScheme &&
            candidateHost == baseHost &&
            effectivePort(scheme: candidateScheme, port: port) ==
            effectivePort(scheme: baseScheme, port: baseURL.port)
    }

    /// WKSecurityOrigin reports port 0 for the scheme's default port, while
    /// URL.port is nil. Treat both as "use the default for the scheme".
    static func effectivePort(scheme: String, port: Int?) -> Int? {
        if let port = port, port > 0 {
            return port
        }
        switch scheme.lowercased() {
        case "https": return 443
        case "http": return 80
        default: return nil
        }
    }
}
