import UIKit
import UniformTypeIdentifiers

final class ShareViewController: UIViewController {
  override func viewDidLoad() {
    super.viewDidLoad()
    view.backgroundColor = .systemBackground
    ingestSharedContent()
  }

  private func ingestSharedContent() {
    guard let context = extensionContext else {
      return
    }
    let entryId = UUID().uuidString
    guard let inbox = ShareInboxConstants.inboxRootURL() else {
      finishExtension(context: context)
      return
    }
    let entryDir = inbox.appendingPathComponent(entryId, isDirectory: true)
    try? FileManager.default.createDirectory(at: entryDir, withIntermediateDirectories: true)

    var items: [[String: Any]] = []
    var skippedCount = 0
    let group = DispatchGroup()
    var fileIndex = 0
    let lock = NSLock()

    for case let item as NSExtensionItem in context.inputItems {
      for provider in item.attachments ?? [] {
        if provider.hasItemConformingToTypeIdentifier(UTType.plainText.identifier) {
          group.enter()
          provider.loadItem(forTypeIdentifier: UTType.plainText.identifier, options: nil) { value, _ in
            defer { group.leave() }
            let text = (value as? String)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
            if text.isEmpty { return }
            let type = text.hasPrefix("http://") || text.hasPrefix("https://") ? "url" : "text"
            lock.lock()
            items.append(["type": type, "text": text])
            lock.unlock()
          }
          continue
        }
        if provider.hasItemConformingToTypeIdentifier(UTType.url.identifier) {
          group.enter()
          provider.loadItem(forTypeIdentifier: UTType.url.identifier, options: nil) { value, _ in
            defer { group.leave() }
            let url = (value as? URL)?.absoluteString ?? ""
            if url.isEmpty { return }
            lock.lock()
            items.append(["type": "url", "text": url])
            lock.unlock()
          }
          continue
        }
        let index = fileIndex
        fileIndex += 1
        group.enter()
        copyAttachment(provider: provider, entryDir: entryDir, index: index) { fileItem, skipped in
          lock.lock()
          if skipped {
            skippedCount += 1
          } else if let fileItem {
            items.append(fileItem)
          } else {
            skippedCount += 1
          }
          lock.unlock()
          group.leave()
        }
      }
    }

    group.notify(queue: .main) {
      if items.isEmpty && skippedCount == 0 {
        try? FileManager.default.removeItem(at: entryDir)
        self.finishExtension(context: context)
        return
      }
      let manifest: [String: Any] = [
        "id": entryId,
        "created_at": Int(Date().timeIntervalSince1970 * 1000),
        "items": items,
        "skipped_count": skippedCount,
        "source": "ios_share_extension",
      ]
      let manifestURL = entryDir.appendingPathComponent(ShareInboxConstants.manifestFileName)
      if let data = try? JSONSerialization.data(withJSONObject: manifest, options: []) {
        try? data.write(to: manifestURL, options: .atomic)
      }
      self.openHostApp(context: context)
    }
  }

  private func copyAttachment(
    provider: NSItemProvider,
    entryDir: URL,
    index: Int,
    completion: @escaping ([String: Any]?, Bool) -> Void
  ) {
    let typeIdentifiers = [
      UTType.image.identifier,
      UTType.movie.identifier,
      UTType.pdf.identifier,
      UTType.data.identifier,
      UTType.fileURL.identifier,
    ]
    guard let typeId = typeIdentifiers.first(where: { provider.hasItemConformingToTypeIdentifier($0) }) else {
      completion(nil, true)
      return
    }
    provider.loadFileRepresentation(forTypeIdentifier: typeId) { url, _ in
      guard let sourceURL = url else {
        completion(nil, true)
        return
      }
      let displayName = sourceURL.lastPathComponent.isEmpty ? "shared_file" : sourceURL.lastPathComponent
      let safeName = displayName.replacingOccurrences(of: "/", with: "_")
      let relative = "item_\(index)_\(safeName)"
      let destURL = entryDir.appendingPathComponent(relative)
      do {
        if FileManager.default.fileExists(atPath: destURL.path) {
          try FileManager.default.removeItem(at: destURL)
        }
        try FileManager.default.copyItem(at: sourceURL, to: destURL)
        let values = try destURL.resourceValues(forKeys: [.fileSizeKey])
        let size = values.fileSize ?? 0
        if Int64(size) > ShareInboxConstants.maxShareFileBytes {
          try? FileManager.default.removeItem(at: destURL)
          completion(nil, true)
          return
        }
        let mime = self.mimeType(for: destURL)
        completion([
          "type": "file",
          "path": relative,
          "file_name": displayName,
          "mime": mime,
          "size": size,
        ], false)
      } catch {
        try? FileManager.default.removeItem(at: destURL)
        completion(nil, true)
      }
    }
  }

  private func mimeType(for url: URL) -> String {
    if let type = UTType(filenameExtension: url.pathExtension),
       let mime = type.preferredMIMEType {
      return mime
    }
    return "application/octet-stream"
  }

  private func openHostApp(context: NSExtensionContext) {
    guard let url = URL(string: "grix://share") else {
      finishExtension(context: context)
      return
    }
    context.open(url, completionHandler: { _ in
      self.finishExtension(context: context)
    })
  }

  private func finishExtension(context: NSExtensionContext) {
    context.completeRequest(returningItems: nil, completionHandler: nil)
  }
}
