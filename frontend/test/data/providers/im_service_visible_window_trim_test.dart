import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/message_cards/models/chat_tool_execution_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_tool_execution_group_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/modules/chat/message_cards/services/chat_tool_execution_group_card_projection.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';
}

class _FakeSessionService extends SessionService {
  @override
  bool get isInitialized => true;

  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) async {
    return const SessionMessageHistoryResult(
      code: 0,
      messages: [],
      hasMore: false,
    );
  }
}

const _sid = 'visible-window-trim-session';
const _senderId = 'agent-1';

Map<String, dynamic> _textRow(int seq) {
  return {
    'msg_id': 'vw-text-$seq',
    'session_id': _sid,
    'sender_id': _senderId,
    'sender_type': 2,
    'msg_type': 1,
    'content': 'visible_window_text_$seq',
    'created_at': 1735689600000 + seq,
    'status': 'sent',
    'state_version': '1',
  };
}

Map<String, dynamic> _toolRow(int seq) {
  final envelope = ChatMessageCardCodec.encode(
    ChatToolExecutionCardData(summaryText: 'Bash: vw_step_$seq'),
  );
  return {
    'msg_id': 'vw-tool-$seq',
    'session_id': _sid,
    'sender_id': _senderId,
    'sender_type': 2,
    'msg_type': 1,
    'content': envelope.content,
    'extra': envelope.extra,
    'created_at': 1735689600000 + seq,
    'status': 'sent',
    'state_version': '1',
  };
}

/// Zero-height internal directives (approval slash commands / open-session
/// directives), hidden by `isInternalDirectiveMessage`.
Map<String, dynamic> _directiveRow(int seq) {
  final content = seq.isOdd
      ? '/approve req-$seq'
      : 'grix://open/session?cwd=/tmp/ws_$seq';
  return {
    'msg_id': 'vw-directive-$seq',
    'session_id': _sid,
    'sender_id': '1001',
    'sender_type': 1,
    'msg_type': 1,
    'content': content,
    'created_at': 1735689600000 + seq,
    'status': 'sent',
    'state_version': '1',
  };
}

Map<String, dynamic> _execApprovalRow(int pair) {
  final envelope = ChatMessageCardCodec.buildExecApprovalCard(
    approvalId: 'approval-$pair',
    approvalSlug: 'req-$pair',
    command: 'echo $pair',
    host: 'gateway',
  );
  return {
    'msg_id': 'vw-exec-approval-$pair',
    'session_id': _sid,
    'sender_id': _senderId,
    'sender_type': 2,
    'msg_type': 1,
    'content': envelope.content,
    'extra': envelope.extra,
    // Pairs follow the 150 texts: keep ordering strictly increasing.
    'created_at': 1735689600000 + 10000 + pair * 10,
    'status': 'sent',
    'state_version': '1',
  };
}

Map<String, dynamic> _execStatusRow(int pair) {
  final envelope = ChatMessageCardCodec.buildExecStatusCard(
    status: 'resolved-allow-once',
    summary: 'Allow once selected.',
    approvalId: 'approval-$pair',
    decision: 'allow-once',
  );
  return {
    'msg_id': 'vw-exec-status-$pair',
    'session_id': _sid,
    'sender_id': _senderId,
    'sender_type': 2,
    'msg_type': 1,
    'content': envelope.content,
    'extra': envelope.extra,
    'created_at': 1735689600000 + 10000 + pair * 10 + 1,
    'status': 'sent',
    'state_version': '1',
  };
}

Future<void> _waitUntil(
  bool Function() cond, {
  Duration timeout = const Duration(seconds: 10),
  String? description,
}) async {
  final deadline = DateTime.now().add(timeout);
  while (!cond()) {
    if (DateTime.now().isAfter(deadline)) {
      fail('Timed out waiting for: ${description ?? 'condition'}');
    }
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
}

/// Pages the whole local history into the window exactly like the first-screen
/// auto-fill loop does: page while older history exists, stop when a page adds
/// nothing and the (unawaited) remote backfill has settled hasOlder=false.
Future<void> _pageLocalHistoryIntoWindow(ImService svc) async {
  for (var round = 0; round < 40 && svc.hasOlderMessages; round++) {
    final before = svc.currentMessages.length;
    await svc.loadOlderForCurrentSession();
    // Let the unawaited older-window backfill resolve (fake: hasMore=false).
    await Future<void>.delayed(const Duration(milliseconds: 30));
    if (svc.currentMessages.length == before) {
      // Empty local page: wait once more for the backfill flag, then stop
      // instead of spinning on an exhausted local DB.
      await Future<void>.delayed(const Duration(milliseconds: 60));
      if (svc.currentMessages.length == before) break;
    }
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late ImService imService;
  late String userId;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    userId = 'vw-trim-${DateTime.now().microsecondsSinceEpoch}';
    Get.put<AuthService>(_FakeAuthService());
    Get.put<SessionService>(_FakeSessionService());
    await LocalDb.setActiveUser(userId);
    imService = ImService();
    Get.put<ImService>(imService);
  });

  tearDown(() async {
    imService.onClose();
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  Future<void> enterAndDrain() async {
    imService.enterSession(_sid);
    await _waitUntil(
      () => imService.currentMessages.isNotEmpty,
      description: 'initial local window loaded',
    );
    await _pageLocalHistoryIntoWindow(imService);
  }

  test(
    '尾部超长连续工具卡组折叠时不把最新消息裁出驻留窗口',
    () async {
      // 20 text rows followed by 260 consecutive tool-execution cards from the
      // same sender: 280 raw rows > 200-row resident cap, but only 21 visible
      // bubbles (20 texts + 1 collapsed group).
      await LocalDb.batchInsertMessages([
        for (var seq = 1; seq <= 20; seq++) _textRow(seq),
        for (var seq = 21; seq <= 280; seq++) _toolRow(seq),
      ]);

      await enterAndDrain();

      final messages = imService.currentMessages;
      expect(
        messages.length,
        280,
        reason: '折叠后的可见气泡远未达到驻留上限，不应裁剪任何消息',
      );
      expect(messages.map((m) => m.msgId).toSet().length, 280);
      expect(
        messages.last.msgId,
        'vw-tool-280',
        reason: '最新消息必须保留在窗口内',
      );
      expect(messages.first.msgId, 'vw-text-1');
      expect(imService.hasNewerMessages, isFalse);

      final projection = ChatToolExecutionGroupProjector.project(
        List<MessageModel>.of(messages),
        currentUserId: '1001',
      );
      final group =
          projection.overridesByIndex.values.single
              as ChatToolExecutionGroupCardData;
      expect(group.count, 260);
      expect(group.children.length, 260);
      expect(group.children.last.summaryText, 'Bash: vw_step_280');
    },
  );

  test(
    '关联现象：长工具组分页后，已落库的两条最新回复仍留在窗口内（同根验证）',
    () async {
      // 复刻 42efe6f2 会话形态：260 连续工具卡之后跟着 2 条最新文字回复。
      // 改动前向上分页/填窗让窗口超过 200 raw 行，底部裁剪把这两条回复裁出
      // 窗口——DB 已同步但聊天页不可见；窗口尖端的偏差同时会让已读边界
      // 落后于真实最新，表现为角标先 0 再反弹 2。
      await LocalDb.batchInsertMessages([
        for (var seq = 1; seq <= 20; seq++) _textRow(seq),
        for (var seq = 21; seq <= 280; seq++) _toolRow(seq),
        _textRow(281),
        _textRow(282),
      ]);

      await enterAndDrain();

      final messages = imService.currentMessages;
      expect(messages.length, 282);
      expect(messages.map((m) => m.msgId).toSet().length, 282);
      expect(
        messages.last.msgId,
        'vw-text-282',
        reason: '最新回复不得因长工具组折叠而被裁出窗口',
      );
      expect(
        messages[messages.length - 2].msgId,
        'vw-text-281',
      );
      expect(imService.hasNewerMessages, isFalse);
    },
  );

  test(
    '中部超长工具卡组向上分页时不裁掉其后方的最新正文',
    () async {
      // 10 texts + a 230-card tool run + 10 newest texts: 250 raw rows,
      // 21 visible bubbles.
      await LocalDb.batchInsertMessages([
        for (var seq = 1; seq <= 10; seq++) _textRow(seq),
        for (var seq = 11; seq <= 240; seq++) _toolRow(seq),
        for (var seq = 241; seq <= 250; seq++) _textRow(seq),
      ]);

      await enterAndDrain();

      final messages = imService.currentMessages;
      expect(messages.length, 250);
      expect(
        messages.last.msgId,
        'vw-text-250',
        reason: '工具卡组后方的最新正文必须保留',
      );
      expect(messages.first.msgId, 'vw-text-1');
      expect(imService.hasNewerMessages, isFalse);

      final projection = ChatToolExecutionGroupProjector.project(
        List<MessageModel>.of(messages),
        currentUserId: '1001',
      );
      final group =
          projection.overridesByIndex.values.single
              as ChatToolExecutionGroupCardData;
      expect(group.count, 230);
    },
  );

  test(
    '普通文本历史向上分页仍按驻留上限裁剪并保持 hasNewerMessages',
    () async {
      await LocalDb.batchInsertMessages([
        for (var seq = 1; seq <= 250; seq++) _textRow(seq),
      ]);

      await enterAndDrain();

      final messages = imService.currentMessages;
      expect(messages.length, 200);
      expect(messages.map((m) => m.msgId).toSet().length, 200);
      expect(imService.hasNewerMessages, isTrue);
      expect(messages.first.msgId, 'vw-text-1');
      // 裁剪边界在最新侧：窗口保留最旧的 200 个可见气泡。
      expect(messages.last.msgId, 'vw-text-200');
    },
  );

  test(
    '裁剪不在折叠工具卡组中间断开（边界对齐整组）',
    () async {
      // 205 texts + 30-card tail group: 235 raw rows, 206 visible bubbles
      // (> 200 cap) -> bottom trim keeps the oldest 200 visible bubbles; the
      // tail group must be dropped as a whole, never split.
      await LocalDb.batchInsertMessages([
        for (var seq = 1; seq <= 205; seq++) _textRow(seq),
        for (var seq = 206; seq <= 235; seq++) _toolRow(seq),
      ]);

      await enterAndDrain();

      final messages = imService.currentMessages;
      expect(messages.length, 200);
      expect(messages.every((m) => !m.msgId.startsWith('vw-tool-')), isTrue);
      expect(messages.last.msgId, 'vw-text-200');
      expect(imService.hasNewerMessages, isTrue);
    },
  );

  test(
    '零高度内部指令不占驻留名额，不裁掉任何消息',
    () async {
      // 20 texts + 260 internal directives (all zero-height in ChatView):
      // 280 raw rows, only 20 visible bubbles — nothing may be trimmed.
      await LocalDb.batchInsertMessages([
        for (var seq = 1; seq <= 20; seq++) _textRow(seq),
        for (var seq = 21; seq <= 280; seq++) _directiveRow(seq),
      ]);

      await enterAndDrain();

      final messages = imService.currentMessages;
      expect(
        messages.length,
        280,
        reason: '内部指令在 ChatView 零高度，不应占用驻留窗口名额',
      );
      expect(messages.map((m) => m.msgId).toSet().length, 280);
      expect(messages.last.msgId, 'vw-directive-280');
      expect(imService.hasNewerMessages, isFalse);
      expect(imService.currentWindowVisibleBubbleCount, 20);
    },
  );

  test(
    'exec 审批+状态折叠对计 1 个单元，裁剪不拆对',
    () async {
      // 150 texts + 60 approval/status pairs. Each pair renders as one bubble
      // (the status folds into its approval card): 210 visible units over the
      // 200 cap, so the newest 10 units are trimmed — as whole pairs.
      await LocalDb.batchInsertMessages([
        for (var seq = 1; seq <= 150; seq++) _textRow(seq),
        for (var pair = 1; pair <= 60; pair++) ...[
          _execApprovalRow(pair),
          _execStatusRow(pair),
        ],
      ]);

      await enterAndDrain();

      final messages = imService.currentMessages;
      expect(messages.length, 150 + 50 * 2);
      expect(messages.first.msgId, 'vw-text-1');
      // The kept tail is a complete pair: newest kept row is a status card
      // whose approval card is also in the window.
      expect(messages.last.msgId, 'vw-exec-status-50');
      expect(
        messages.any((m) => m.msgId == 'vw-exec-approval-50'),
        isTrue,
        reason: '状态卡不得脱离其审批卡单独保留',
      );
      expect(
        messages.every((m) => !m.msgId.contains('exec') || int.parse(m.msgId.split('-').last) <= 50),
        isTrue,
      );
      expect(imService.hasNewerMessages, isTrue);
    },
  );
}
