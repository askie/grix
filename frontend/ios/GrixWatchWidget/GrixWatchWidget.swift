import SwiftUI
import WidgetKit

/// 复杂功能 / Smart Stack：待处理数与运行中数。
/// 数据由手表 App 每次刷新后写进 App Group，扩展只读，不自己联网 —— 凭证留在
/// 手表 App 的钥匙串里。
struct WatchCountsEntry: TimelineEntry {
  let date: Date
  let pending: Int
  let running: Int
}

struct WatchCountsProvider: TimelineProvider {
  func placeholder(in context: Context) -> WatchCountsEntry {
    WatchCountsEntry(date: Date(), pending: 0, running: 0)
  }

  func getSnapshot(in context: Context, completion: @escaping (WatchCountsEntry) -> Void) {
    completion(currentEntry())
  }

  func getTimeline(in context: Context, completion: @escaping (Timeline<WatchCountsEntry>) -> Void) {
    // 手表 App 刷新后会主动 reload，这里只留一个兜底刷新点。
    completion(Timeline(entries: [currentEntry()], policy: .after(Date().addingTimeInterval(900))))
  }

  private func currentEntry() -> WatchCountsEntry {
    let defaults = UserDefaults(suiteName: "group.pub.dhf.grix.watch")
    return WatchCountsEntry(
      date: Date(),
      pending: defaults?.integer(forKey: "pending_count") ?? 0,
      running: defaults?.integer(forKey: "running_count") ?? 0
    )
  }
}

struct GrixWatchWidgetView: View {
  var entry: WatchCountsEntry

  var body: some View {
    VStack(spacing: 2) {
      Text("\(entry.pending)")
        .font(.title2)
        .fontWeight(.semibold)
        .foregroundStyle(entry.pending > 0 ? .orange : .primary)
      Text("待处理").font(.caption2).foregroundStyle(.secondary)
      Text("\(entry.running) 运行中").font(.caption2).foregroundStyle(.secondary)
    }
    .containerBackground(.clear, for: .widget)
  }
}

struct GrixWatchWidget: Widget {
  var body: some WidgetConfiguration {
    StaticConfiguration(kind: "GrixWatchWidget", provider: WatchCountsProvider()) { entry in
      GrixWatchWidgetView(entry: entry)
    }
    .configurationDisplayName("Grix")
    .description("待处理与运行中的任务数")
  }
}

@main
struct GrixWatchWidgetBundle: WidgetBundle {
  var body: some Widget {
    GrixWatchWidget()
  }
}
