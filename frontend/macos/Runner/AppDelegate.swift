import Cocoa
import FlutterMacOS

@main
class AppDelegate: FlutterAppDelegate {
  override func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
    // 返回 false 以支持关闭窗口时最小化到托盘（由 Dart 层 window_manager 控制）
    return false
  }

  override func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
    // window_manager.setPreventClose(true) 会让 windowShouldClose 返回 false，
    // AppKit 随即取消 terminate。Sparkle 点「安装并重启」走的就是 NSApp.terminate()，
    // 进程退不掉，Updater 一直等，最后弹 Update Error。
    // 关窗到托盘不走这条路径（preventClose + shouldTerminateAfterLastWindowClosed=false）。
    for window in sender.windows {
      window.delegate = nil
    }
    return .terminateNow
  }

  override func applicationSupportsSecureRestorableState(_ app: NSApplication) -> Bool {
    return true
  }
}
