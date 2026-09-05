import ActivityKit
import SwiftUI
import WidgetKit

/// Agent 运行卡片：锁屏横幅 + 灵动岛，watchOS 11+ 的 Smart Stack 复用同一张
/// （`supplementalActivityFamilies`，手表端不需要任何代码）。
///
/// 卡片上不做按钮：批准 / 拒绝 / 停止 / 回复的按钮要跑 App Intent、要拿 App 的
/// 访问令牌，这些通知横幅和手表 App 已经覆盖了。这里只负责"一眼看到在跑什么、
/// 是不是在等我、结束没有"，点一下打开会话。
///
/// 部署目标是 iOS 18：手表 Smart Stack 那张卡靠 `supplementalActivityFamilies`
/// 撑起来，而这个修饰符从 iOS 18 才有。低版本系统不加载这个扩展，主人看到的
/// 就是原来的通知，不会报错。
@available(iOS 18.0, *)
struct GrixRunActivityWidget: Widget {
  var body: some WidgetConfiguration {
    ActivityConfiguration(for: GrixRunAttributes.self) { context in
      GrixRunActivityView(
        attributes: context.attributes,
        state: context.state
      )
      .widgetURL(sessionURL(context.attributes.sessionId))
    } dynamicIsland: { context in
      let phase = GrixRunPhase(rawPhase: context.state.phase)
      return DynamicIsland {
        DynamicIslandExpandedRegion(.leading) {
          GrixRunPhaseIcon(phase: phase)
            .padding(.leading, 4)
        }
        DynamicIslandExpandedRegion(.trailing) {
          Text(context.attributes.agentName)
            .font(.caption)
            .foregroundStyle(.secondary)
            .lineLimit(1)
            .padding(.trailing, 4)
        }
        DynamicIslandExpandedRegion(.bottom) {
          VStack(alignment: .leading, spacing: 2) {
            Text(displayTitle(context.attributes, context.state))
              .font(.subheadline.weight(.medium))
              .lineLimit(1)
            if !context.state.detail.isEmpty {
              Text(context.state.detail)
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(2)
            }
          }
          .frame(maxWidth: .infinity, alignment: .leading)
        }
      } compactLeading: {
        GrixRunPhaseIcon(phase: phase)
      } compactTrailing: {
        // 只有"在等主人"值得占用右边那一小格；在跑和已结束靠左边的图标就够了。
        if phase.isWaiting {
          Image(systemName: "exclamationmark")
            .foregroundStyle(GrixRunPhaseStyle.tint(phase))
        }
      } minimal: {
        GrixRunPhaseIcon(phase: phase)
      }
      .widgetURL(sessionURL(context.attributes.sessionId))
      .keylineTint(GrixRunPhaseStyle.tint(phase))
    }
    .supplementalActivityFamilies([.small])
  }

  /// 点卡片走 App 已有的那条"打开某个会话"链路（AppDelegate 收 grix:// 后转给
  /// push_tap 通道），不另起一套导航。
  private func sessionURL(_ sessionId: String) -> URL? {
    guard
      let encoded = sessionId.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed),
      !encoded.isEmpty
    else { return nil }
    return URL(string: "grix://session/\(encoded)")
  }
}

/// 标题兜底：会话还没起名字时用 agent 名字顶上，卡片不能只剩一个图标。
@available(iOS 16.2, *)
private func displayTitle(
  _ attributes: GrixRunAttributes,
  _ state: GrixRunAttributes.ContentState
) -> String {
  state.title.isEmpty ? attributes.agentName : state.title
}

/// 锁屏横幅与手表 Smart Stack 共用的视图。手表那张卡宽度只有手机的一半，
/// 用 activityFamily 收掉副标题和第二行。
@available(iOS 18.0, *)
struct GrixRunActivityView: View {
  @Environment(\.activityFamily) private var activityFamily

  let attributes: GrixRunAttributes
  let state: GrixRunAttributes.ContentState

  private var phase: GrixRunPhase { GrixRunPhase(rawPhase: state.phase) }

  var body: some View {
    HStack(alignment: .center, spacing: 10) {
      GrixRunPhaseIcon(phase: phase)
        .font(.title3)
      VStack(alignment: .leading, spacing: 2) {
        Text(displayTitle(attributes, state))
          .font(activityFamily == .small ? .caption.weight(.semibold) : .subheadline.weight(.semibold))
          .lineLimit(1)
        Text(subtitle)
          .font(.caption2)
          .foregroundStyle(.secondary)
          .lineLimit(activityFamily == .small ? 1 : 2)
      }
      Spacer(minLength: 0)
    }
    .padding(activityFamily == .small ? 8 : 14)
    .activityBackgroundTint(nil)
  }

  /// 副标题优先给后端下发的原因（等待什么、为什么失败），没有就退回 agent 名字。
  private var subtitle: String {
    state.detail.isEmpty ? attributes.agentName : state.detail
  }
}

@available(iOS 16.2, *)
struct GrixRunPhaseIcon: View {
  let phase: GrixRunPhase

  var body: some View {
    Image(systemName: GrixRunPhaseStyle.symbol(phase))
      .foregroundStyle(GrixRunPhaseStyle.tint(phase))
      .symbolRenderingMode(.hierarchical)
  }
}

/// 阶段的图标与配色。卡片上不写状态文字：文案要跟着主人的语言走，而扩展里没有
/// App 那套 i18n；图标和颜色本身不用翻译，需要文字的地方一律用后端已经本地化好的
/// title / detail。
enum GrixRunPhaseStyle {
  static func symbol(_ phase: GrixRunPhase) -> String {
    switch phase {
    case .running: return "arrow.triangle.2.circlepath"
    case .waitingApproval: return "hand.raised.fill"
    case .waitingQuestion: return "questionmark.bubble.fill"
    case .completed: return "checkmark.circle.fill"
    case .failed: return "exclamationmark.triangle.fill"
    case .stopped: return "stop.circle.fill"
    }
  }

  static func tint(_ phase: GrixRunPhase) -> Color {
    switch phase {
    case .running: return .blue
    case .waitingApproval, .waitingQuestion: return .orange
    case .completed: return .green
    case .failed: return .red
    case .stopped: return .gray
    }
  }
}
