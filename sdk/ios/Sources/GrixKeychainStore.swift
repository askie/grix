import Foundation
import Security

/// Abstraction for persisting `visitor_key` scoped by `siteKey`.
public protocol GrixVisitorStore {
    func visitorKey(for siteKey: String) -> String?
    func saveVisitorKey(_ visitorKey: String, for siteKey: String)
}

/// Keychain-backed store for persisting `visitor_key` per `siteKey`.
public final class GrixKeychainStore: GrixVisitorStore {
    private let service: String

    public init(service: String = "im.grix.widget.visitor") {
        self.service = service
    }

    public func visitorKey(for siteKey: String) -> String? {
        let account = accountKey(for: siteKey)
        guard !account.isEmpty else { return nil }

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]

        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        guard status == errSecSuccess,
              let data = item as? Data,
              let value = String(data: data, encoding: .utf8),
              !value.isEmpty
        else {
            return nil
        }
        return value
    }

    public func saveVisitorKey(_ visitorKey: String, for siteKey: String) {
        let account = accountKey(for: siteKey)
        let trimmedKey = visitorKey.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !account.isEmpty, !trimmedKey.isEmpty, let data = trimmedKey.data(using: .utf8) else {
            return
        }

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]

        let attributes: [String: Any] = [
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlock,
        ]

        let updateStatus = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
        if updateStatus == errSecItemNotFound {
            var addQuery = query
            addQuery[kSecValueData as String] = data
            addQuery[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
            SecItemAdd(addQuery as CFDictionary, nil)
        }
    }

    private func accountKey(for siteKey: String) -> String {
        let trimmed = siteKey.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return "" }
        return "grix_visitor_\(trimmed)"
    }
}

/// In-memory store for unit testing.
public final class GrixMemoryVisitorStore: GrixVisitorStore {
    private var storage: [String: String] = [:]

    public init() {}

    public func visitorKey(for siteKey: String) -> String? {
        storage[siteKey.trimmingCharacters(in: .whitespacesAndNewlines)]
    }

    public func saveVisitorKey(_ visitorKey: String, for siteKey: String) {
        let key = siteKey.trimmingCharacters(in: .whitespacesAndNewlines)
        let val = visitorKey.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !key.isEmpty, !val.isEmpty else { return }
        storage[key] = val
    }
}
