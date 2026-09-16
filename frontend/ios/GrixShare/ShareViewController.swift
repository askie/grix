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
    var offeredTypes = Set<String>()
    let lock = NSLock()

    for case let item as NSExtensionItem in context.inputItems {
      for provider in item.attachments ?? [] {
        offeredTypes.formUnion(provider.registeredTypeIdentifiers)

        let appendFileResult: ([String: Any]?, Bool) -> Void = { fileItem, skipped in
          lock.lock()
          if let fileItem, !skipped {
            items.append(fileItem)
          } else {
            skippedCount += 1
          }
          lock.unlock()
          group.leave()
        }

        // A file URL handed over through NSItemProvider comes with system-granted
        // read access, so it must be copied out before any text/url branch: those
        // only yield the path as a string and throw the real bytes away.
        if provider.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) {
          let index = fileIndex
          fileIndex += 1
          group.enter()
          copyFileURLAttachment(
            provider: provider,
            entryDir: entryDir,
            index: index,
            completion: appendFileResult
          )
          continue
        }

        if provider.hasItemConformingToTypeIdentifier(UTType.image.identifier)
          || provider.hasItemConformingToTypeIdentifier(UTType.movie.identifier)
          || provider.hasItemConformingToTypeIdentifier(UTType.pdf.identifier)
          || provider.hasItemConformingToTypeIdentifier(UTType.zip.identifier) {
          let index = fileIndex
          fileIndex += 1
          group.enter()
          copyAttachment(
            provider: provider,
            entryDir: entryDir,
            index: index,
            completion: appendFileResult
          )
          continue
        }

        // Only a non-file URL is a link worth keeping as text.
        if provider.hasItemConformingToTypeIdentifier(UTType.url.identifier) {
          group.enter()
          provider.loadItem(forTypeIdentifier: UTType.url.identifier, options: nil) { value, _ in
            defer { group.leave() }
            guard let url = Self.resolveURL(from: value) else { return }
            lock.lock()
            items.append(["type": "url", "text": url.absoluteString])
            lock.unlock()
          }
          continue
        }

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

        let index = fileIndex
        fileIndex += 1
        group.enter()
        copyAttachment(
          provider: provider,
          entryDir: entryDir,
          index: index,
          completion: appendFileResult
        )
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
        "offered_types": offeredTypes.sorted(),
      ]
      let manifestURL = entryDir.appendingPathComponent(ShareInboxConstants.manifestFileName)
      if let data = try? JSONSerialization.data(withJSONObject: manifest, options: []) {
        try? data.write(to: manifestURL, options: .atomic)
      }
      self.openHostApp(context: context)
    }
  }

  /// Resolves the several shapes an item provider may hand a URL back in.
  private static func resolveURL(from value: Any?) -> URL? {
    if let url = value as? URL { return url }
    if let url = (value as? NSURL) as URL? { return url }
    if let data = value as? Data { return URL(dataRepresentation: data, relativeTo: nil) }
    if let string = value as? String { return URL(string: string) }
    return nil
  }

  /// Copies a `public.file-url` attachment. The source file lives in the sending
  /// app's container, but the item provider hands over system-granted access, so
  /// it must be read inside the security scope and copied into our own inbox.
  private func copyFileURLAttachment(
    provider: NSItemProvider,
    entryDir: URL,
    index: Int,
    completion: @escaping ([String: Any]?, Bool) -> Void
  ) {
    provider.loadItem(forTypeIdentifier: UTType.fileURL.identifier, options: nil) { value, _ in
      guard let sourceURL = Self.resolveURL(from: value), sourceURL.isFileURL else {
        completion(nil, true)
        return
      }

      let scoped = sourceURL.startAccessingSecurityScopedResource()
      defer { if scoped { sourceURL.stopAccessingSecurityScopedResource() } }

      let displayName = sourceURL.lastPathComponent.isEmpty
        ? "shared_file"
        : sourceURL.lastPathComponent.removingPercentEncoding ?? sourceURL.lastPathComponent
      let relative = "item_\(index)_\(displayName.replacingOccurrences(of: "/", with: "_"))"
      let destURL = entryDir.appendingPathComponent(relative)

      do {
        if FileManager.default.fileExists(atPath: destURL.path) {
          try FileManager.default.removeItem(at: destURL)
        }
        try FileManager.default.copyItem(at: sourceURL, to: destURL)
        let size = (try destURL.resourceValues(forKeys: [.fileSizeKey])).fileSize ?? 0
        if Int64(size) > ShareInboxConstants.maxShareFileBytes {
          try? FileManager.default.removeItem(at: destURL)
          completion(nil, true)
          return
        }
        completion([
          "type": "file",
          "path": relative,
          "file_name": displayName,
          "mime": self.mimeType(for: destURL),
          "size": size,
        ], false)
      } catch {
        try? FileManager.default.removeItem(at: destURL)
        completion(nil, true)
      }
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
      UTType.zip.identifier,
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
