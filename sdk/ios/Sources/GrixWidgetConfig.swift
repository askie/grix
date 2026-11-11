import Foundation

/// Optional visitor profile passed to `GrixWidget.configure`.
public struct GrixUser: Equatable {
    public let name: String?
    public let email: String?

    public init(name: String? = nil, email: String? = nil) {
        self.name = name
        self.email = email
    }
}

/// Configuration for the Grix Widget iOS SDK.
public struct GrixConfig: Equatable {
    public static let defaultApiBase = "https://gb.grix.im"
    public static let sdkVersion = "1.0.0"

    public let siteKey: String
    public let apiBase: String
    public let locale: String?
    public let themeColor: String?
    public let title: String?
    public let user: GrixUser?
    public let appIdOverride: String?

    public init(
        siteKey: String,
        apiBase: String = GrixConfig.defaultApiBase,
        locale: String? = nil,
        themeColor: String? = nil,
        title: String? = nil,
        user: GrixUser? = nil,
        appIdOverride: String? = nil
    ) {
        self.siteKey = siteKey.trimmingCharacters(in: .whitespacesAndNewlines)
        var trimmedBase = apiBase.trimmingCharacters(in: .whitespacesAndNewlines)
        while trimmedBase.hasSuffix("/") {
            trimmedBase.removeLast()
        }
        self.apiBase = trimmedBase.isEmpty ? GrixConfig.defaultApiBase : trimmedBase
        self.locale = locale?.trimmingCharacters(in: .whitespacesAndNewlines)
        self.themeColor = themeColor?.trimmingCharacters(in: .whitespacesAndNewlines)
        self.title = title?.trimmingCharacters(in: .whitespacesAndNewlines)
        self.user = user
        self.appIdOverride = appIdOverride?.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Resolves the normalized lowercase host bundle identifier.
    public func resolvedAppId(bundle: Bundle = .main) -> String {
        if let override = appIdOverride, !override.isEmpty {
            return override.lowercased()
        }
        return (bundle.bundleIdentifier ?? "").trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    }
}
