import Cocoa
import FlutterMacOS
import Sparkle

extension SUAppcast {
    public func toDictionary() -> NSDictionary {
        let dict: NSDictionary = [
            "items": self.items.map({ item in
                return item.toDictionary()
            }),
        ]
        return dict;
    }
}

extension SUAppcastItem {
    
    
    public func toDictionary() -> NSDictionary {
        let dict: NSDictionary = [
            "versionString": self.versionString,
            "displayVersionString": self.displayVersionString,
            "fileURL": self.fileURL?.absoluteString ?? "",
            "contentLength": self.contentLength,
            "infoURL": self.infoURL?.absoluteString ?? "",
            "title":self.title ?? "",
            "dateString": self.dateString ?? "",
            "releaseNotesURL":self.releaseNotesURL?.absoluteString ?? "",
            "itemDescription":self.itemDescription ?? "",
            "itemDescriptionFormat": self.itemDescriptionFormat ?? "",
            "fullReleaseNotesURL": self.fullReleaseNotesURL ?? "",
            "minimumSystemVersion": self.minimumSystemVersion ?? "",
            "minimumOperatingSystemVersionIsOK": self.minimumOperatingSystemVersionIsOK,
            "maximumSystemVersion": self.maximumSystemVersion ?? "",
            "maximumOperatingSystemVersionIsOK": self.maximumOperatingSystemVersionIsOK,
            "channel": self.channel ?? "",
        ]
        return dict;
    }
}

public class AutoUpdater: NSObject, SPUUpdaterDelegate {
    var _userDriver: SPUStandardUserDriver?
    var _updater: SPUUpdater?
    var feedURL: URL?
    public var onEvent:((String, NSDictionary) -> Void)?

    /// 已下载完成、等待应用退出后安装的更新。
    ///
    /// `updater(_:willInstallUpdateOnQuit:immediateInstallationBlock:)` 返回 true 表示
    /// 由宿主接管安装时机，Sparkle 会挂起当前更新周期并且**不再启动新的更新周期**
    /// （见 SPUUpdaterDelegate 头文件说明）。所以这个待安装状态必须能被 Dart 侧查询到，
    /// 否则长时间不退出的进程会永远停在这里，之后所有 checkForUpdates 都被静默忽略。
    private var _pendingInstallItem: SUAppcastItem?
    private var _pendingInstallHandler: (() -> Void)?
    private var _pendingInstallSince: Date?
    
    override init() {
        super.init()
        let hostBundle: Bundle = Bundle.main
        
        _userDriver = SPUStandardUserDriver(hostBundle: hostBundle, delegate: nil)
        _updater = SPUUpdater(
            hostBundle: hostBundle,
            applicationBundle: hostBundle,
            userDriver: _userDriver!,
            delegate: self
        )
        _updater?.clearFeedURLFromUserDefaults()
        try? _updater?.start()
    }
    
    public func feedURLString(for updater: SPUUpdater) -> String? {
        return feedURL?.absoluteString
    }

    public func setFeedURL(_ feedURL: URL?) {
        self.feedURL = feedURL
        try? _updater?.start()
    }
    
    public func checkForUpdates() {
        _updater?.checkForUpdates()
    }
    
    public func checkForUpdatesInBackground() {
        _updater?.checkForUpdatesInBackground()
    }
    
    public func setScheduledCheckInterval(_ interval: Int) {
        _updater?.updateCheckInterval = TimeInterval(interval)
    }

    /// 当前更新器状态。`hasPendingInstall` 为 true 时，新的 checkForUpdates 不会有任何反应，
    /// 调用方必须自己把"已下载待重启"这件事告诉用户。
    public func updateSessionStatus() -> NSDictionary {
        var dict: [String: Any] = [
            "sessionInProgress": _updater?.sessionInProgress ?? false,
            "canCheckForUpdates": _updater?.canCheckForUpdates ?? false,
            "hasPendingInstall": _pendingInstallHandler != nil,
        ]
        if let item = _pendingInstallItem {
            dict["pendingVersion"] = item.displayVersionString
            dict["pendingBuild"] = item.versionString
        }
        if let since = _pendingInstallSince {
            dict["pendingSinceEpochMs"] = Int(since.timeIntervalSince1970 * 1000)
        }
        return dict as NSDictionary
    }

    /// 立即安装已下载的更新并重启应用。没有待安装更新时返回 false。
    public func installPendingUpdate() -> Bool {
        guard let handler = _pendingInstallHandler else { return false }
        // Sparkle 2.3+ 允许重复调用（应用可能取消退出），所以这里不清空 handler。
        handler()
        return true
    }
    
    // SPUUpdaterDelegate
    
    public func updater(_ updater: SPUUpdater, didAbortWithError error: Error) {
        let data: NSDictionary = [
            "error": error.localizedDescription,
        ]
        _emitEvent("error", data);
    }
    
    public func updater(_ updater: SPUUpdater, didFinishLoading appcast: SUAppcast) {
        let data: NSDictionary = [
            "appcast": appcast.toDictionary()
        ]
        _emitEvent("checking-for-update", data)
    }
    
    public func updater(_ updater: SPUUpdater, didFindValidUpdate item: SUAppcastItem) {
        let data: NSDictionary = [
            "appcastItem": item.toDictionary()
        ]
        _emitEvent("update-available", data)
    }
    
    public func updaterDidNotFindUpdate(_ updater: SPUUpdater, error: Error) {
        let data: NSDictionary = [
            "error": error.localizedDescription,
        ]
        _emitEvent("update-not-available", data)
    }
    
    public func updater(_ updater: SPUUpdater, didDownloadUpdate item: SUAppcastItem) {
        let data: NSDictionary = [
            "appcastItem": item.toDictionary()
        ]
        _emitEvent("update-downloaded", data)
    }
    
    public func updater(_ updater: SPUUpdater, willInstallUpdateOnQuit item: SUAppcastItem, immediateInstallationBlock immediateInstallHandler: @escaping () -> Void) -> Bool {
        // 返回 true = 由宿主决定安装时机。上游插件也返回 true，但把 immediateInstallHandler
        // 丢掉了，于是更新周期被永久挂起、又没有任何入口能装上——必须留着它。
        // 同一个更新可能被回调多次（应用取消退出时），这时保留最早的时间戳，
        // 换了新版本才重新计时——挂起时长决定什么时候提醒用户重启。
        if _pendingInstallItem?.versionString != item.versionString {
            _pendingInstallSince = Date()
        }
        _pendingInstallItem = item
        _pendingInstallHandler = immediateInstallHandler
        let data: NSDictionary = [
            "appcastItem": item.toDictionary()
        ]
        _emitEvent("before-quit-for-update", data)
        return true
    }
    
    public func _emitEvent(_ eventName: String, _ data: NSDictionary) {
        if (onEvent != nil) {
            onEvent!(eventName, data)
        }
    }
}
