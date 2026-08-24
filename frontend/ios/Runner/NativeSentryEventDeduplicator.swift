import CryptoKit
import Darwin
import Foundation
@_spi(Private) import Sentry

/// Persistent processor for native iOS crashes and app hangs. It is registered
/// before Sentry starts so cached crash reports from the previous launch pass
/// through the same 24-hour filter.
enum NativeSentryEventDeduplicator {
  private static let fileName = "sentry-event-dedup-v2.json"
  private static let windowMs: Int64 = 24 * 60 * 60 * 1000
  private static let pendingWindowMs: Int64 = 5 * 60 * 1000
  private static let maxEntries = 512
  private static let maxBytes = 256 * 1024
  private static let processLock = NSLock()
  private static var installed = false

  static func install() {
    guard !installed else { return }
    installed = true
    SentryDependencyContainer.sharedInstance().globalEventProcessor.add { event in
      shouldSend(event) ? event : nil
    }
  }

  private static func shouldSend(_ event: Event) -> Bool {
    guard let signature = fingerprint(event) else { return true }
    processLock.lock()
    defer { processLock.unlock() }

    guard
      let directory = FileManager.default.urls(
        for: .applicationSupportDirectory,
        in: .userDomainMask
      ).first
    else { return true }

    do {
      try FileManager.default.createDirectory(
        at: directory,
        withIntermediateDirectories: true,
        attributes: [.posixPermissions: 0o700]
      )
      let stateURL = directory.appendingPathComponent(fileName)
      let lockURL = directory.appendingPathComponent("\(fileName).lock")
      let fd = Darwin.open(lockURL.path, O_CREAT | O_RDWR, S_IRUSR | S_IWUSR)
      guard fd >= 0 else { return true }
      defer { Darwin.close(fd) }

      var fileLock = flock(
        l_start: 0,
        l_len: 0,
        l_pid: 0,
        l_type: Int16(F_WRLCK),
        l_whence: Int16(SEEK_SET)
      )
      guard fcntl(fd, F_SETLKW, &fileLock) != -1 else { return true }
      defer {
        fileLock.l_type = Int16(F_UNLCK)
        _ = fcntl(fd, F_SETLK, &fileLock)
      }

      let now = Int64(Date().timeIntervalSince1970 * 1000)
      var state = readState(stateURL)
      prune(&state, now: now)
      let duplicate = state.sent[signature] != nil || state.pending[signature] != nil
      if !duplicate { state.sent[signature] = now }
      trim(&state)
      try writeState(state, to: stateURL)
      return !duplicate
    } catch {
      // Observability must never make application startup fail.
      return true
    }
  }

  private struct State {
    var sent: [String: Int64] = [:]
    var pending: [String: [String: Any]] = [:]
  }

  private static func readState(_ url: URL) -> State {
    guard
      let attributes = try? FileManager.default.attributesOfItem(atPath: url.path),
      let size = attributes[.size] as? NSNumber,
      size.intValue <= maxBytes,
      let data = try? Data(contentsOf: url),
      let root = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
      root["version"] as? Int == 2
    else { return State() }

    var state = State()
    if let sent = root["sent"] as? [String: Any] {
      for (key, value) in sent where isFingerprint(key) {
        if let number = value as? NSNumber { state.sent[key] = number.int64Value }
      }
    }
    if let pending = root["pending"] as? [String: Any] {
      for (key, value) in pending where isFingerprint(key) {
        guard
          let entry = value as? [String: Any],
          let token = entry["token"] as? String,
          token.count <= 128,
          entry["timestamp"] is NSNumber
        else { continue }
        state.pending[key] = entry
      }
    }
    return state
  }

  private static func prune(_ state: inout State, now: Int64) {
    state.sent = state.sent.filter { _, timestamp in
      timestamp <= now && now - timestamp < windowMs
    }
    state.pending = state.pending.filter { _, entry in
      guard let timestamp = (entry["timestamp"] as? NSNumber)?.int64Value else { return false }
      return timestamp <= now && now - timestamp < pendingWindowMs
    }
  }

  private static func trim(_ state: inout State) {
    struct Stored {
      let key: String
      let timestamp: Int64
      let pending: Bool
    }
    var entries = state.sent.map { Stored(key: $0.key, timestamp: $0.value, pending: false) }
    entries += state.pending.map {
      Stored(
        key: $0.key,
        timestamp: ($0.value["timestamp"] as? NSNumber)?.int64Value ?? 0,
        pending: true
      )
    }
    let overflow = entries.count - maxEntries
    guard overflow > 0 else { return }
    for entry in entries.sorted(by: { $0.timestamp < $1.timestamp }).prefix(overflow) {
      if entry.pending { state.pending.removeValue(forKey: entry.key) }
      else { state.sent.removeValue(forKey: entry.key) }
    }
  }

  private static func writeState(_ state: State, to url: URL) throws {
    let root: [String: Any] = [
      "version": 2,
      "sent": state.sent,
      "pending": state.pending,
    ]
    let data = try JSONSerialization.data(withJSONObject: root, options: [.sortedKeys])
    let temporary = url.deletingLastPathComponent().appendingPathComponent(
      "\(url.lastPathComponent).\(getpid()).\(DispatchTime.now().uptimeNanoseconds).tmp"
    )
    FileManager.default.createFile(
      atPath: temporary.path,
      contents: nil,
      attributes: [.posixPermissions: 0o600]
    )
    defer { try? FileManager.default.removeItem(at: temporary) }
    let handle = try FileHandle(forWritingTo: temporary)
    try handle.write(contentsOf: data)
    try handle.synchronize()
    try handle.close()
    guard Darwin.rename(temporary.path, url.path) == 0 else {
      throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
    }
    let directoryFD = Darwin.open(url.deletingLastPathComponent().path, O_RDONLY)
    if directoryFD >= 0 {
      _ = fsync(directoryFD)
      Darwin.close(directoryFD)
    }
  }

  private static func fingerprint(_ event: Event) -> String? {
    var identity: [String: Any]?
    if let exceptions = event.exceptions, !exceptions.isEmpty {
      identity = [
        "exceptions": exceptions.map { exception -> [String: Any] in
          let frames = Array((exception.stacktrace?.frames ?? []).suffix(8))
          return [
            "type": exception.type,
            "value": normalize(exception.value),
            "frames": frames.map { frame in
              [
                "file": frame.fileName ?? "",
                "function": frame.function ?? "",
                "line": frame.lineNumber?.intValue ?? 0,
                "column": frame.columnNumber?.intValue ?? 0,
              ]
            },
          ]
        }
      ]
    } else if let message = event.message {
      identity = ["message": normalize(message.formatted)]
    } else if let error = event.error {
      identity = [
        "throwableType": String(reflecting: type(of: error)),
        "throwable": normalize(error.localizedDescription),
      ]
    } else if event.fingerprint?.isEmpty != false {
      return nil
    }

    let source: [String: Any] = [
      "release": event.releaseName ?? "",
      "platform": event.platform,
      "logger": event.logger ?? "",
      "level": String(describing: event.level),
      "fingerprint": (event.fingerprint ?? []).map(normalize),
      "identity": identity ?? NSNull(),
    ]
    guard let data = try? JSONSerialization.data(withJSONObject: source, options: [.sortedKeys]) else {
      return nil
    }
    return SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
  }

  private static func normalize(_ value: String) -> String {
    let replacements: [(String, String)] = [
      (#"\b(session(?:_id)?|sid|message_id|mid|trace(?:_id)?|request(?:_id)?|rid|event(?:_id)?)\s*[=:]\s*[^\s,;]+"#, "$1=<id>"),
      (#"\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b"#, "<uuid>"),
      (#"\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})\b"#, "<timestamp>"),
      (#"\b0x[0-9a-f]{12,}\b"#, "<address>"),
      (#"\b[0-9a-f]{16,}\b"#, "<id>"),
      (#"\b\d{13}\b"#, "<timestamp>"),
    ]
    return replacements.reduce(value) { result, replacement in
      guard let expression = try? NSRegularExpression(
        pattern: replacement.0,
        options: [.caseInsensitive]
      ) else { return result }
      return expression.stringByReplacingMatches(
        in: result,
        range: NSRange(result.startIndex..., in: result),
        withTemplate: replacement.1
      )
    }
  }

  private static func isFingerprint(_ value: String) -> Bool {
    value.range(of: #"^[0-9a-f]{64}$"#, options: .regularExpression) != nil
  }
}
