import UserNotifications
import UIKit

class NotificationService: UNNotificationServiceExtension {
  var contentHandler: ((UNNotificationContent) -> Void)?
  var bestAttemptContent: UNMutableNotificationContent?

  override func didReceive(
    _ request: UNNotificationRequest,
    withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void
  ) {
    self.contentHandler = contentHandler
    let content = request.content.mutableCopy() as! UNMutableNotificationContent
    bestAttemptContent = content

    guard let avatarURL = content.userInfo["sender_avatar_url"] as? String,
          !avatarURL.isEmpty
    else {
      // No avatar URL — try generating a text-based avatar from the initial.
      if let initial = content.userInfo["sender_initial"] as? String, !initial.isEmpty {
        attachTextAvatar(initial: initial)
      }
      contentHandler(content)
      return
    }

    downloadAndAttachAvatar(urlString: avatarURL, content: content)
  }

  override func serviceExtensionTimeWillExpire() {
    if let contentHandler, let content = bestAttemptContent {
      contentHandler(content)
    }
  }

  private func downloadAndAttachAvatar(
    urlString: String,
    content: UNMutableNotificationContent
  ) {
    guard let url = URL(string: urlString) else {
      deliver(content: content)
      return
    }

    let task = URLSession.shared.downloadTask(with: url) { [weak self] tmpURL, _, error in
      guard let self, let tmpURL else {
        self?.deliver(content: content)
        return
      }
      do {
        let ext = url.pathExtension.isEmpty ? "png" : url.pathExtension
        let fileURL = URL(fileURLWithPath: NSTemporaryDirectory())
          .appendingPathComponent("avatar_\(UUID().uuidString).\(ext)")
        try FileManager.default.moveItem(at: tmpURL, to: fileURL)

        if let attachment = try? UNNotificationAttachment(
          identifier: "sender_avatar",
          url: fileURL,
          options: [UNNotificationAttachmentOptionsTypeHintKey: ext]
        ) {
          content.attachments = [attachment]
        }
      } catch {
        // Download or file move failed — deliver without avatar.
      }
      self.deliver(content: content)
    }
    task.resume()
  }

  private func attachTextAvatar(initial: String) {
    guard let content = bestAttemptContent else { return }

    let size: CGFloat = 80
    let renderer = UIGraphicsImageRenderer(size: CGSize(width: size, height: size))
    let image = renderer.image { ctx in
      // Background
      UIColor.systemBlue.setFill()
      ctx.fill(CGRect(x: 0, y: 0, width: size, height: size))

      // Text
      let paragraphStyle = NSMutableParagraphStyle()
      paragraphStyle.alignment = .center
      let attrs: [NSAttributedString.Key: Any] = [
        .font: UIFont.systemFont(ofSize: 36, weight: .medium),
        .foregroundColor: UIColor.white,
        .paragraphStyle: paragraphStyle,
      ]
      let textRect = CGRect(x: 0, y: 8, width: size, height: size - 8)
      (initial as NSString).draw(in: textRect, withAttributes: attrs)
    }

    guard let pngData = image.pngData() else { return }

    do {
      let fileURL = URL(fileURLWithPath: NSTemporaryDirectory())
        .appendingPathComponent("avatar_initial_\(UUID().uuidString).png")
      try pngData.write(to: fileURL)

      if let attachment = try? UNNotificationAttachment(
        identifier: "sender_initial",
        url: fileURL,
        options: [UNNotificationAttachmentOptionsTypeHintKey: "png"]
      ) {
        content.attachments = [attachment]
      }
    } catch {
      // Text avatar generation failed — deliver without attachment.
    }
  }

  private func deliver(content: UNNotificationContent) {
    contentHandler?(content)
  }
}
