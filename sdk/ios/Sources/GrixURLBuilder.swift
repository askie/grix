import Foundation

/// Builds the `/public/widget/app.html#<fragment>` URL according to the Grix Widget App Channel contract.
/// Startup parameters are strictly placed in the URL fragment (`#`) and never in the query string.
public enum GrixURLBuilder {
    public static func buildAppURL(
        config: GrixConfig,
        visitorKey: String?,
        bundle: Bundle = .main
    ) -> URL? {
        let baseString = "\(config.apiBase)/public/widget/app.html"
        guard URL(string: baseString) != nil else {
            return nil
        }

        var pairs: [(String, String)] = [
            ("site_key", config.siteKey),
            ("platform", "ios"),
            ("app_id", config.resolvedAppId(bundle: bundle)),
            ("sdk_version", GrixConfig.sdkVersion),
        ]

        if let visitorKey = visitorKey?.trimmingCharacters(in: .whitespacesAndNewlines), !visitorKey.isEmpty {
            pairs.append(("visitor_key", visitorKey))
        }
        if let locale = config.locale, !locale.isEmpty {
            pairs.append(("locale", locale))
        }
        if let themeColor = config.themeColor, !themeColor.isEmpty {
            pairs.append(("theme_color", themeColor))
        }
        if let title = config.title, !title.isEmpty {
            pairs.append(("title", title))
        }
        if let name = config.user?.name?.trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty {
            pairs.append(("visitor_name", name))
        }
        if let email = config.user?.email?.trimmingCharacters(in: .whitespacesAndNewlines), !email.isEmpty {
            pairs.append(("visitor_email", email))
        }

        let fragment = pairs.map { key, value in
            "\(percentEncode(key))=\(percentEncode(value))"
        }.joined(separator: "&")

        return URL(string: "\(baseString)#\(fragment)")
    }

    private static let allowedCharacters: CharacterSet = {
        var cs = CharacterSet.urlQueryAllowed
        cs.remove(charactersIn: "&=+#%?")
        return cs
    }()

    private static func percentEncode(_ value: String) -> String {
        value.addingPercentEncoding(withAllowedCharacters: allowedCharacters) ?? value
    }
}
