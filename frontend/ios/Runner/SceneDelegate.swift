import Flutter
import Foundation
import UIKit

final class TextDocumentBridge {
  static let shared = TextDocumentBridge()

  private let channelName = "pub.dhf.grix/text_document"
  private let maxPreviewBytes = 10 * 1024 * 1024
  private var channel: FlutterMethodChannel?
  private var pendingPayload: [String: Any]?
  private var handles: [String: URL] = [:]
  private var dartReady = false

  private init() {}

  func configure(messenger: FlutterBinaryMessenger) {
    let methodChannel = FlutterMethodChannel(
      name: channelName,
      binaryMessenger: messenger
    )
    channel = methodChannel
    methodChannel.setMethodCallHandler { [weak self] call, result in
      guard let self else {
        result(FlutterError(code: "bridge_unavailable", message: nil, details: nil))
        return
      }
      switch call.method {
      case "getInitialDocument":
        result(self.pendingPayload)
        self.pendingPayload = nil
        self.dartReady = true
      case "writeDocument":
        self.writeDocument(call: call, result: result)
      case "closeDocument":
        if let args = call.arguments as? [String: Any],
           let handle = args["handle"] as? String {
          self.handles.removeValue(forKey: handle)
        }
        result(nil)
      default:
        result(FlutterMethodNotImplemented)
      }
    }
  }

  func handle(url: URL) {
    guard url.isFileURL else { return }
    do {
      let payload = try makePayload(url: url)
      if dartReady, let channel {
        channel.invokeMethod("documentOpened", arguments: payload)
      } else {
        pendingPayload = payload
      }
    } catch {
      NSLog("[TextDocument] unable to open %@: %@", url.path, error.localizedDescription)
    }
  }

  private func makePayload(url: URL) throws -> [String: Any] {
    let accessed = url.startAccessingSecurityScopedResource()
    defer {
      if accessed { url.stopAccessingSecurityScopedResource() }
    }
    let values = try url.resourceValues(forKeys: [
      .fileSizeKey,
      .contentModificationDateKey,
    ])
    if let size = values.fileSize, size > maxPreviewBytes {
      throw TextDocumentError.tooLarge
    }
    let data = try Data(contentsOf: url, options: [.mappedIfSafe])
    if data.count > maxPreviewBytes {
      throw TextDocumentError.tooLarge
    }
    let handle = UUID().uuidString
    handles[handle] = url
    var descriptor: [String: Any] = [
      "handle": handle,
      "displayName": url.lastPathComponent.isEmpty ? "document.txt" : url.lastPathComponent,
      "mimeType": mimeType(for: url),
      "canWrite": FileManager.default.isWritableFile(atPath: url.path),
      "source": "iosSecurityScopedUrl",
      "byteLength": data.count,
    ]
    if let modifiedAt = values.contentModificationDate {
      descriptor["modifiedAt"] = Int(modifiedAt.timeIntervalSince1970 * 1000)
    }
    return [
      "descriptor": descriptor,
      "bytes": FlutterStandardTypedData(bytes: data),
    ]
  }

  private func mimeType(for url: URL) -> String {
    switch url.pathExtension.lowercased() {
    case "md", "markdown", "mdx":
      return "text/markdown"
    case "json", "jsonl":
      return "application/json"
    case "xml":
      return "application/xml"
    case "js", "jsx":
      return "application/javascript"
    case "yaml", "yml":
      return "application/x-yaml"
    default:
      return "text/plain"
    }
  }

  private func writeDocument(
    call: FlutterMethodCall,
    result: @escaping FlutterResult
  ) {
    guard
      let args = call.arguments as? [String: Any],
      let handle = args["handle"] as? String,
      let typedData = args["bytes"] as? FlutterStandardTypedData,
      let url = handles[handle]
    else {
      result(
        FlutterError(
          code: "invalid_args",
          message: "Missing handle or bytes",
          details: nil
        )
      )
      return
    }

    let accessed = url.startAccessingSecurityScopedResource()
    defer {
      if accessed { url.stopAccessingSecurityScopedResource() }
    }
    let coordinator = NSFileCoordinator()
    var coordinationError: NSError?
    var writeError: Error?
    coordinator.coordinate(
      writingItemAt: url,
      options: .forReplacing,
      error: &coordinationError
    ) { coordinatedURL in
      do {
        try typedData.data.write(to: coordinatedURL, options: .atomic)
      } catch {
        writeError = error
      }
    }
    if let error = coordinationError ?? writeError as NSError? {
      result(
        FlutterError(
          code: "save_failed",
          message: error.localizedDescription,
          details: nil
        )
      )
      return
    }
    result(nil)
  }

  private enum TextDocumentError: Error {
    case tooLarge
  }
}

class SceneDelegate: FlutterSceneDelegate {
  override func scene(
    _ scene: UIScene,
    willConnectTo session: UISceneSession,
    options connectionOptions: UIScene.ConnectionOptions
  ) {
    super.scene(
      scene,
      willConnectTo: session,
      options: connectionOptions
    )
    for context in connectionOptions.urlContexts where context.url.isFileURL {
      TextDocumentBridge.shared.handle(url: context.url)
    }
  }

  override func scene(
    _ scene: UIScene,
    openURLContexts URLContexts: Set<UIOpenURLContext>
  ) {
    super.scene(scene, openURLContexts: URLContexts)
    for context in URLContexts where context.url.isFileURL {
      TextDocumentBridge.shared.handle(url: context.url)
    }
  }
}
