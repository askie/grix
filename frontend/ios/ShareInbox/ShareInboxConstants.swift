import Foundation

enum ShareInboxConstants {
  static let appGroupId = "group.pub.dhf.grix"
  static let inboxFolderName = "share_inbox"
  static let manifestFileName = "manifest.json"
  static let maxShareFileBytes: Int64 = 50 * 1024 * 1024

  static func inboxRootURL() -> URL? {
    guard let container = FileManager.default.containerURL(
      forSecurityApplicationGroupIdentifier: appGroupId
    ) else {
      return nil
    }
    let inbox = container.appendingPathComponent(inboxFolderName, isDirectory: true)
    try? FileManager.default.createDirectory(at: inbox, withIntermediateDirectories: true)
    return inbox
  }
}
