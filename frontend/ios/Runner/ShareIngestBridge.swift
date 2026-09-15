import Flutter
import Foundation

final class ShareIngestBridge: NSObject {
  static let shared = ShareIngestBridge()

  private let methodChannelName = "grix/share_ingest"
  private let eventChannelName = "grix/share_ingest_events"
  private var methodChannel: FlutterMethodChannel?
  private var eventChannel: FlutterEventChannel?
  private var eventSink: FlutterEventSink?

  private override init() {}

  func configure(messenger: FlutterBinaryMessenger) {
    let methodChannel = FlutterMethodChannel(
      name: methodChannelName,
      binaryMessenger: messenger
    )
    self.methodChannel = methodChannel
    methodChannel.setMethodCallHandler { [weak self] call, result in
      guard let self else {
        result(FlutterError(code: "bridge_unavailable", message: nil, details: nil))
        return
      }
      switch call.method {
      case "consumePending":
        result(self.loadPendingManifestMaps())
      case "deleteEntry":
        if let args = call.arguments as? [String: Any],
           let id = args["id"] as? String {
          self.deleteEntry(id: id.trimmingCharacters(in: .whitespacesAndNewlines))
        }
        result(nil)
      default:
        result(FlutterMethodNotImplemented)
      }
    }

    let eventChannel = FlutterEventChannel(
      name: eventChannelName,
      binaryMessenger: messenger
    )
    self.eventChannel = eventChannel
    eventChannel.setStreamHandler(self)
  }

  func notifySharePending() {
    guard let sink = eventSink else { return }
    sink("pending")
  }

  private func loadPendingManifestMaps() -> [[String: Any]] {
    guard let inbox = ShareInboxConstants.inboxRootURL() else { return [] }
    let fm = FileManager.default
    guard let entries = try? fm.contentsOfDirectory(
      at: inbox,
      includingPropertiesForKeys: [.isDirectoryKey],
      options: [.skipsHiddenFiles]
    ) else {
      return []
    }

    var manifests: [[String: Any]] = []
    for dir in entries {
      let values = try? dir.resourceValues(forKeys: [.isDirectoryKey])
      guard values?.isDirectory == true else { continue }
      let manifestURL = dir.appendingPathComponent(ShareInboxConstants.manifestFileName)
      guard fm.fileExists(atPath: manifestURL.path),
            let data = try? Data(contentsOf: manifestURL),
            let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
      else {
        continue
      }
      manifests.append(enrichManifest(json, entryDir: dir))
    }
    manifests.sort {
      let left = ($0["created_at"] as? NSNumber)?.int64Value ?? 0
      let right = ($1["created_at"] as? NSNumber)?.int64Value ?? 0
      return left > right
    }
    return manifests
  }

  private func enrichManifest(_ manifest: [String: Any], entryDir: URL) -> [String: Any] {
    var copy = manifest
    guard let items = manifest["items"] as? [[String: Any]] else {
      return copy
    }
    let enrichedItems = items.map { item -> [String: Any] in
      var itemCopy = item
      if let relative = item["path"] as? String, !relative.isEmpty {
        itemCopy["absolute_path"] = entryDir.appendingPathComponent(relative).path
      }
      return itemCopy
    }
    copy["items"] = enrichedItems
    return copy
  }

  private func deleteEntry(id: String) {
    guard !id.isEmpty, let inbox = ShareInboxConstants.inboxRootURL() else { return }
    let dir = inbox.appendingPathComponent(id, isDirectory: true)
    try? FileManager.default.removeItem(at: dir)
  }
}

extension ShareIngestBridge: FlutterStreamHandler {
  func onListen(withArguments arguments: Any?, eventSink events: @escaping FlutterEventSink) -> FlutterError? {
    eventSink = events
    return nil
  }

  func onCancel(withArguments arguments: Any?) -> FlutterError? {
    eventSink = nil
    return nil
  }
}
