import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';

ConversationListItem _item(
  String key,
  int activityAt, {
  bool isPinned = false,
  int pinnedAt = 0,
}) {
  final session = SessionModel(
    sessionId: key,
    updatedAt: activityAt,
    lastMessageTime: 0,
    isPinned: isPinned,
    pinnedAt: pinnedAt,
  );
  return ConversationListItem(
    groupKey: key,
    latestSession: session,
    sessions: <SessionModel>[session],
    unreadCount: 0,
    isPinned: isPinned,
    pinnedAt: pinnedAt,
  );
}

Map<String, int> _order(List<String> keys) => <String, int>{
  for (var i = 0; i < keys.length; i++) keys[i]: i,
};

const int hysteresisMs = 2000;

List<String> _keys(List<ConversationListItem> items) =>
    items.map((e) => e.groupKey).toList();

void main() {
  group('mergeLatestActivityFloor', () {
    test('旧 API 摘要不能把实时消息已置顶的会话排回去', () {
      final staleSummary = _item('A', 10000000);
      final localRealtimeSession = SessionModel(
        sessionId: 'A',
        updatedAt: 10004000,
        lastMessageTime: 10004000,
      );
      final other = _item('B', 10002000);

      final merged = ConversationsController.mergeLatestActivityFloor(
        staleSummary,
        <SessionModel>[localRealtimeSession],
      );
      final ordered = ConversationsController.reorderWithHysteresis(
        <ConversationListItem>[merged, other],
        _order(['A', 'B']),
        hysteresisMs,
      );

      expect(merged.latestSession.activityAt, 10004000);
      expect(_keys(ordered), ['A', 'B']);
    });

    test('API 摘要比本地更新时保留 API 时间', () {
      final freshSummary = _item('A', 10004000);
      final olderLocalSession = SessionModel(
        sessionId: 'A',
        updatedAt: 10000000,
        lastMessageTime: 10000000,
      );

      final merged = ConversationsController.mergeLatestActivityFloor(
        freshSummary,
        <SessionModel>[olderLocalSession],
      );

      expect(identical(merged, freshSummary), isTrue);
      expect(merged.latestSession.activityAt, 10004000);
    });

    test('本地行只有 updatedAt（无可见消息）时不得把老会话顶成最新', () {
      // 老会话摘要：最后一条可见消息在 30 天前。
      final oldSummary = _item('A', 10000000);
      // 本地行没有可见消息（lastMessageTime=0），updatedAt 却被会话活跃时间
      // 或同步兜底推到了很新——这正是「历史会话时间全变成刚刚」的来源。
      final pollutedLocal = SessionModel(
        sessionId: 'A',
        updatedAt: 99999000,
        lastMessageTime: 0,
      );

      final merged = ConversationsController.mergeLatestActivityFloor(
        oldSummary,
        <SessionModel>[pollutedLocal],
      );

      expect(identical(merged, oldSummary), isTrue);
      expect(merged.latestSession.displayTime, 10000000);
    });

    test('本地行有更新的可见消息时仍然顶高（实时置顶不受影响）', () {
      final staleSummary = _item('A', 10000000);
      final localWithNewerMessage = SessionModel(
        sessionId: 'A',
        updatedAt: 10004000,
        lastMessageTime: 10004000,
      );

      final merged = ConversationsController.mergeLatestActivityFloor(
        staleSummary,
        <SessionModel>[localWithNewerMessage],
      );

      expect(merged.latestSession.displayTime, 10004000);
    });
  });

  group('liftSummaryByLocalMessageTime', () {
    test('本地只有 last_message_time=0 的污染行时，摘要保持服务端旧时间', () {
      // 服务端摘要：老会话最后可见消息在很早以前。
      final oldSummary = ConversationListItem(
        groupKey: 'private:2:agent-old',
        latestSession: SessionModel(
          sessionId: 'server-old',
          peerId: 'agent-old',
          peerType: 2,
          type: 'private',
          updatedAt: 10000000,
          lastMessageTime: 10000000,
          lastMessage: '五月的最后一条',
        ),
        sessions: const [],
        unreadCount: 1,
        isPinned: false,
        pinnedAt: 0,
      );
      // 调用方（乐观重排）只聚合 lastMessageTime>0 的本地行；
      // 污染行 lastMessageTime=0，聚合结果为 0，不得抬高摘要。
      final skipped = ConversationsController.liftSummaryByLocalMessageTime(
        oldSummary,
        0,
      );
      expect(identical(skipped, oldSummary), isTrue);
      expect(skipped.latestSession.displayTime, 10000000);
      expect(skipped.latestSession.lastMessageTime, 10000000);
    });

    test('同组真实 lastMessageTime 仍可把摘要行顶到最新', () {
      final stale = ConversationListItem(
        groupKey: 'private:2:agent',
        latestSession: SessionModel(
          sessionId: 's-old',
          updatedAt: 10000000,
          lastMessageTime: 10000000,
        ),
        sessions: const [],
        unreadCount: 0,
        isPinned: false,
        pinnedAt: 0,
      );
      final lifted = ConversationsController.liftSummaryByLocalMessageTime(
        stale,
        10004000,
      );
      expect(lifted.latestSession.displayTime, 10004000);
      expect(lifted.latestSession.lastMessageTime, 10004000);

      // 对照行：无可见消息时间，updatedAt 介于两者之间——排序不得靠它压过真实消息。
      final other = ConversationListItem(
        groupKey: 'B',
        latestSession: SessionModel(
          sessionId: 'B',
          updatedAt: 10002000,
          lastMessageTime: 0,
        ),
        sessions: const [],
        unreadCount: 0,
        isPinned: false,
        pinnedAt: 0,
      );
      final ordered = ConversationsController.reorderWithHysteresis(
        <ConversationListItem>[other, lifted],
        _order(['B', 'private:2:agent']),
        hysteresisMs,
      );
      expect(ordered.first.groupKey, 'private:2:agent');
      expect(ordered.first.latestSession.displayTime, 10004000);
    });
  });

  group('reorderWithHysteresis', () {
    test('活跃时间差小于阈值（同档）保持当前顺序，不换位', () {
      // B 比 A 新 500ms，但落在同一个 2000ms 档内 → 不应把 B 提到 A 之上。
      final items = <ConversationListItem>[
        _item('A', 10000000),
        _item('B', 10000500),
      ];
      final result = ConversationsController.reorderWithHysteresis(
        items,
        _order(['A', 'B']),
        hysteresisMs,
      );
      expect(_keys(result), ['A', 'B']);
    });

    test('同档去抖在反方向同样稳定（防止来回横跳）', () {
      // 当前顺序为 [B, A]，A 比 B 新一点但仍同档 → 仍保持 [B, A]，不翻转。
      final items = <ConversationListItem>[
        _item('A', 10000500),
        _item('B', 10000000),
      ];
      final result = ConversationsController.reorderWithHysteresis(
        items,
        _order(['B', 'A']),
        hysteresisMs,
      );
      expect(_keys(result), ['B', 'A']);
    });

    test('活跃时间差跨档则新会话上浮', () {
      // B 比 A 新 4000ms（跨档）→ 应把 B 提到顶部。
      final items = <ConversationListItem>[
        _item('A', 10000000),
        _item('B', 10004000),
      ];
      final result = ConversationsController.reorderWithHysteresis(
        items,
        _order(['A', 'B']),
        hysteresisMs,
      );
      expect(_keys(result), ['B', 'A']);
    });

    test('置顶会话始终在最前，且组内按最新活动时间排序', () {
      // P1 活动时间更新（跨档），即便 pinnedAt 较早，也排在 P2 前面。
      // 未置顶的 A 即使活动时间最新，也必须排在所有置顶之后。
      final items = <ConversationListItem>[
        _item('A', 10004000), // 最新但未置顶
        _item('P1', 9000000, isPinned: true, pinnedAt: 100),
        _item('P2', 8000000, isPinned: true, pinnedAt: 200),
      ];
      final result = ConversationsController.reorderWithHysteresis(
        items,
        _order(['A', 'P1', 'P2']),
        hysteresisMs,
      );
      expect(_keys(result), ['P1', 'P2', 'A']);
    });

    test('置顶组同档（活动时间相近）按 pinnedAt 降序', () {
      // P1/P2 活动时间同档，pinnedAt 更晚的 P2 排前面。
      final items = <ConversationListItem>[
        _item('P1', 10000000, isPinned: true, pinnedAt: 100),
        _item('P2', 10000500, isPinned: true, pinnedAt: 200),
      ];
      final result = ConversationsController.reorderWithHysteresis(
        items,
        _order(['P1', 'P2']),
        hysteresisMs,
      );
      expect(_keys(result), ['P2', 'P1']);
    });

    test('重复应用幂等（已落位顺序不再变化）', () {
      final items = <ConversationListItem>[
        _item('B', 10004000),
        _item('A', 10000000),
      ];
      final once = ConversationsController.reorderWithHysteresis(
        items,
        _order(['A', 'B']),
        hysteresisMs,
      );
      expect(_keys(once), ['B', 'A']);
      final twice = ConversationsController.reorderWithHysteresis(
        once,
        _order(_keys(once)),
        hysteresisMs,
      );
      expect(_keys(twice), ['B', 'A']);
    });

    test('结构变化：新增条目按默认口径排位、已有条目保持稳定', () {
      // 回归：结构变化路径（新增/删除会话）同样走 reorderWithHysteresis，避免
      // 已有条目从"hysteresis 顺序"瞬间翻到"全量构建顺序"。
      final items = <ConversationListItem>[
        _item('P_new', 11000000, isPinned: true, pinnedAt: 50), // 新增置顶
        _item('A_old', 9000000),
        _item('B_new', 10000000), // 新增未置顶
        _item('P_old', 9500000, isPinned: true, pinnedAt: 100),
      ];
      // currentOrder 只包含已有的两条（新增条目不在其中）。
      final currentOrder = _order(['P_old', 'A_old']);
      final result = ConversationsController.reorderWithHysteresis(
        items,
        currentOrder,
        hysteresisMs,
      );
      // P_new 活动跨档新于 P_old → 排前；B_new 跨档新于 A_old → 排前；
      // 置顶仍全部在未置顶前面。
      expect(_keys(result), ['P_new', 'P_old', 'B_new', 'A_old']);
    });

    test('混合置顶/未置顶：与全量构建口径一致，反复落地稳定', () {
      // 回归保证：reorderWithHysteresis 与 _compareConversationItems 同口径
      // （置顶 → 活动时间 DESC → pinnedAt DESC → groupKey），避免两条路径轮流
      // 落地导致置顶组在"活动倒序"和"pinnedAt 倒序"之间反复横跳。
      final items = <ConversationListItem>[
        _item('B', 8000000),
        _item('C', 11000000),
        _item('P_late_pin', 9000000, isPinned: true, pinnedAt: 100),
        _item('P_recent', 10000000, isPinned: true, pinnedAt: 50),
      ];
      // 预期：P_recent 活动更新（跨档）→ 排在 P_late_pin 之前；
      // 未置顶的 C 即使活动时间最新，也必须排在所有置顶之后。
      const expected = ['P_recent', 'P_late_pin', 'C', 'B'];
      final once = ConversationsController.reorderWithHysteresis(
        items,
        _order(['B', 'C', 'P_late_pin', 'P_recent']),
        hysteresisMs,
      );
      expect(_keys(once), expected);
      // 用任意当前顺序再次跑，结果一致 → 不会与构建路径轮流换位。
      final twice = ConversationsController.reorderWithHysteresis(
        once,
        _order(_keys(once)),
        hysteresisMs,
      );
      expect(_keys(twice), expected);
    });
  });
}
