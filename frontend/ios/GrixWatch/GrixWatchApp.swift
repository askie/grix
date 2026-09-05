import SwiftUI

@main
struct GrixWatchApp: App {
  @StateObject private var store = WatchStore()

  init() {
    // WCSession 必须在 App 启动时就激活，否则手机推来的凭证无处落地。
    WatchCredentialProvider.shared.activate()
  }

  var body: some Scene {
    WindowGroup {
      RootView().environmentObject(store)
    }
  }
}

struct RootView: View {
  @EnvironmentObject private var store: WatchStore

  var body: some View {
    TabView {
      InboxView()
      AgentsView()
      QuickSendView()
    }
    .task { await store.refresh() }
  }
}

/// token 过期时唯一能给用户的动作提示：手表不刷新凭证。
struct ResyncNotice: View {
  var body: some View {
    VStack(spacing: 6) {
      Image(systemName: "iphone.gen3")
      Text("打开 iPhone 上的 Grix 重新同步")
        .font(.footnote)
        .multilineTextAlignment(.center)
    }
    .foregroundStyle(.secondary)
  }
}
