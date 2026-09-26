import 'package:flutter_test/flutter_test.dart';

import 'package:grix/data/models/message_model.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_open_session_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_exec_approval_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_tool_execution_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_projection.dart';

void main() {
  setUp(() {
    ChatMessageCardCodec.debugResetDecodeFromMessageCount();
  });

  test('merges exec and open session interaction projections', () {
    final execApprovalEnvelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval-1',
      approvalSlug: 'req-1',
      command: 'pwd',
      host: 'gateway',
    );
    final execStatusEnvelope = ChatMessageCardCodec.buildExecStatusCard(
      status: 'resolved-allow-once',
      summary: 'Allow once selected by u_1.',
      approvalId: 'approval-1',
      decision: 'allow-once',
    );
    final openSessionEnvelope = ChatMessageCardCodec.buildAgentOpenSessionCard(
      summaryText: 'open missing cwd',
      detailText: 'send cwd',
    );
    final agentStatusEnvelope = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'session',
        status: 'success',
        summary: 'Codex session opened for /workspace/demo.',
        detailText: 'Workspace: /workspace/demo\nWorker: starting',
        referenceId: 'session-1',
      ),
    );
    final projection = ChatMessageCardProjector.project([
      MessageModel(
        msgId: 'm1',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1000,
        content: execApprovalEnvelope.content,
        extra: execApprovalEnvelope.extra,
      ),
      MessageModel(
        msgId: 'm2',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1010,
        content: execStatusEnvelope.content,
        extra: execStatusEnvelope.extra,
      ),
      MessageModel(
        msgId: 'm3',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1020,
        content: openSessionEnvelope.content,
        extra: openSessionEnvelope.extra,
      ),
      MessageModel(
        msgId: 'm4',
        sessionId: 's1',
        senderId: 'u-1',
        senderType: 1,
        createdAt: 1030,
        content: 'grix://open/session?cwd=%2Fworkspace%2Fdemo',
      ),
      MessageModel(
        msgId: 'm5',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1040,
        content: agentStatusEnvelope.content,
        extra: agentStatusEnvelope.extra,
      ),
    ]);

    expect(projection.hiddenIndexes, containsAll(<int>[1, 4]));
    expect(projection.overridesByIndex[0], isA<ChatExecApprovalCardData>());
    expect(projection.overridesByIndex[2], isA<ChatAgentOpenSessionCardData>());
    expect(
      (projection.overridesByIndex[2] as ChatAgentOpenSessionCardData)
          .displaySubmittedPath,
      '/workspace/demo',
    );
  });

  test('decodes each message at most once in unified card projection', () {
    final execApprovalEnvelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval-decode-1',
      approvalSlug: 'req-decode-1',
      command: 'pwd',
      host: 'gateway',
    );
    final execStatusEnvelope = ChatMessageCardCodec.buildExecStatusCard(
      status: 'resolved-allow-once',
      summary: 'Allow once selected by u_1.',
      approvalId: 'approval-decode-1',
      decision: 'allow-once',
    );
    final openSessionEnvelope = ChatMessageCardCodec.buildAgentOpenSessionCard(
      summaryText: 'open missing cwd',
      detailText: 'send cwd',
    );
    final agentStatusEnvelope = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'session',
        status: 'success',
        summary: 'Codex session opened for /workspace/demo.',
        detailText: 'Workspace: /workspace/demo\nWorker: starting',
        referenceId: 'session-decode-1',
      ),
    );
    final messages = <MessageModel>[
      MessageModel(
        msgId: 'm1',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1000,
        content: execApprovalEnvelope.content,
        extra: execApprovalEnvelope.extra,
      ),
      MessageModel(
        msgId: 'm2',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1010,
        content: execStatusEnvelope.content,
        extra: execStatusEnvelope.extra,
      ),
      MessageModel(
        msgId: 'm3',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1020,
        content: openSessionEnvelope.content,
        extra: openSessionEnvelope.extra,
      ),
      MessageModel(
        msgId: 'm4',
        sessionId: 's1',
        senderId: 'u-1',
        senderType: 1,
        createdAt: 1030,
        content: 'grix://open/session?cwd=%2Fworkspace%2Fdemo',
      ),
      MessageModel(
        msgId: 'm5',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1040,
        content: agentStatusEnvelope.content,
        extra: agentStatusEnvelope.extra,
      ),
    ];

    ChatMessageCardProjector.project(messages);

    expect(ChatMessageCardCodec.debugDecodeFromMessageCount, messages.length);
  });

  test('reuses decoded cards across projections when content is unchanged', () {
    final execApprovalEnvelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval-cache-1',
      approvalSlug: 'req-cache-1',
      command: 'pwd',
      host: 'gateway',
    );
    final execStatusEnvelope = ChatMessageCardCodec.buildExecStatusCard(
      status: 'resolved-allow-once',
      summary: 'Allow once selected by u_1.',
      approvalId: 'approval-cache-1',
      decision: 'allow-once',
    );
    final openSessionEnvelope = ChatMessageCardCodec.buildAgentOpenSessionCard(
      summaryText: 'open missing cwd',
      detailText: 'send cwd',
    );
    final messages = <MessageModel>[
      MessageModel(
        msgId: 'cache-m1',
        sessionId: 'cache-s1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1000,
        content: execApprovalEnvelope.content,
        extra: execApprovalEnvelope.extra,
      ),
      MessageModel(
        msgId: 'cache-m2',
        sessionId: 'cache-s1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1010,
        content: execStatusEnvelope.content,
        extra: execStatusEnvelope.extra,
      ),
      MessageModel(
        msgId: 'cache-m3',
        sessionId: 'cache-s1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1020,
        content: openSessionEnvelope.content,
        extra: openSessionEnvelope.extra,
      ),
    ];
    final decodeCache = ChatMessageCardDecodeCache();

    ChatMessageCardProjector.project(messages, decodeCache: decodeCache);
    expect(ChatMessageCardCodec.debugDecodeFromMessageCount, messages.length);

    ChatMessageCardProjector.project(messages, decodeCache: decodeCache);
    expect(ChatMessageCardCodec.debugDecodeFromMessageCount, messages.length);

    final updatedMessages = <MessageModel>[
      ...messages.take(1),
      messages[1].copyWith(content: 'not-a-card-content-any-more'),
      ...messages.skip(2),
    ];
    ChatMessageCardProjector.project(updatedMessages, decodeCache: decodeCache);

    expect(
      ChatMessageCardCodec.debugDecodeFromMessageCount,
      messages.length + 1,
    );
  });

  _registerAccountingTests();
}

// ---------------------------------------------------------------------------
// Visible-window accounting (resident window cap + first-screen auto-fill).
// ---------------------------------------------------------------------------

MessageModel _accountingMessage(
  String msgId,
  String content, {
  String senderId = 'agent-1',
  int senderType = 2,
  Map<String, dynamic> extra = const {},
}) {
  return MessageModel(
    msgId: msgId,
    sessionId: 'acc-s1',
    senderId: senderId,
    senderType: senderType,
    createdAt: 1000,
    content: content,
    extra: extra,
  );
}

MessageModel _accountingText(String msgId) =>
    _accountingMessage(msgId, 'text $msgId');

MessageModel _accountingToolCard(String msgId, {String senderId = 'agent-1'}) {
  final envelope = ChatMessageCardCodec.encode(
    ChatToolExecutionCardData(summaryText: 'Bash: $msgId'),
  );
  return _accountingMessage(
    msgId,
    envelope.content,
    senderId: senderId,
    extra: envelope.extra,
  );
}

List<int> _units(List<MessageModel> messages) =>
    ChatMessageCardProjector.visibleUnitLengths(
      messages,
      currentUserId: 'me',
    );

void _registerAccountingTests() {
  group('visible window accounting', () {
    test('internal directives count as zero visible units', () {
      final units = _units([
        _accountingText('t1'),
        _accountingMessage('d1', '/approve req-1', senderId: 'me'),
        _accountingMessage(
          'd2',
          'grix://open/session?cwd=/tmp/ws',
          senderId: 'me',
        ),
        _accountingText('t2'),
      ]);
      // Directives merge into the adjacent visible unit: t1, t2 each own one
      // unit; raw lengths fold the hidden rows in.
      expect(units, [3, 1]);
      expect(
        ChatMessageCardProjector.visibleBubbleCount(
          [
            _accountingText('t1'),
            _accountingMessage('d1', '/approve req-1', senderId: 'me'),
          ],
          currentUserId: 'me',
        ),
        1,
      );
    });

    test('collapsed tool run plus hidden followers stays one atomic unit', () {
      final units = _units([
        _accountingText('t1'),
        _accountingToolCard('c1'),
        _accountingToolCard('c2'),
        _accountingToolCard('c3'),
        _accountingMessage('d1', '/approve req-1', senderId: 'me'),
        _accountingText('t2'),
      ]);
      // text + 3-card group + trailing directive merged into the group unit
      // + text.
      expect(units, [1, 4, 1]);
      expect(units.reduce((a, b) => a + b), 6);
    });

    test('exec status folded into its in-window approval counts zero', () {
      final approval = ChatMessageCardCodec.buildExecApprovalCard(
        approvalId: 'approval-1',
        approvalSlug: 'req-1',
        command: 'pwd',
        host: 'gateway',
      );
      final status = ChatMessageCardCodec.buildExecStatusCard(
        status: 'resolved-allow-once',
        summary: 'Allow once selected.',
        approvalId: 'approval-1',
        decision: 'allow-once',
      );
      final units = _units([
        _accountingMessage('a1', approval.content, extra: approval.extra),
        _accountingMessage('s1', status.content, extra: status.extra),
        _accountingText('t1'),
      ]);
      expect(units, [2, 1]);
    });

    test('exec status without in-window approval stays visible', () {
      final status = ChatMessageCardCodec.buildExecStatusCard(
        status: 'resolved-allow-once',
        summary: 'Allow once selected.',
        approvalId: 'approval-missing',
        decision: 'allow-once',
      );
      final units = _units([
        _accountingText('t1'),
        _accountingMessage('s1', status.content, extra: status.extra),
      ]);
      expect(units, [1, 1]);
    });

    test('all-hidden window keeps a single accounting unit', () {
      final units = _units([
        _accountingMessage('d1', '/approve req-1', senderId: 'me'),
        _accountingMessage('d2', '/approve req-2', senderId: 'me'),
      ]);
      expect(units, [2]);
    });

    test('prefix/suffix keep whole units and sum to the raw length', () {
      const accounting = ChatWindowVisibilityAccounting(
        unitLengths: [1, 4, 1, 2],
        hiddenLeaderByIndex: <int, int>{},
      );
      expect(
        ChatMessageCardProjector.prefixRawLengthForUnits(accounting, 4),
        8,
      );
      expect(
        ChatMessageCardProjector.prefixRawLengthForUnits(accounting, 2),
        5,
      );
      expect(
        ChatMessageCardProjector.suffixDropCountForUnits(accounting, 4),
        0,
      );
      // Newest 2 units = 1 + 2 raw rows -> drop the leading 5.
      expect(
        ChatMessageCardProjector.suffixDropCountForUnits(accounting, 2),
        5,
      );
    });

    test('bottom trim extends the cut to keep a leader with its followers',
        () {
      // Units: [1, 1, 1]; hidden row 3 (inside dropped unit 3) folds into
      // leader row 1 (kept unit 1). Keeping only 2 units would separate
      // them, so the cut extends to cover the follower's host unit.
      const accounting = ChatWindowVisibilityAccounting(
        unitLengths: [1, 1, 2],
        hiddenLeaderByIndex: <int, int>{3: 1},
      );
      expect(
        ChatMessageCardProjector.prefixRawLengthForUnits(accounting, 2),
        4,
      );
      // No cross-cut link -> plain unit boundary.
      const unlinked = ChatWindowVisibilityAccounting(
        unitLengths: [1, 1, 2],
        hiddenLeaderByIndex: <int, int>{},
      );
      expect(
        ChatMessageCardProjector.prefixRawLengthForUnits(unlinked, 2),
        2,
      );
    });

    test('top trim pulls the cut back so a kept follower keeps its leader',
        () {
      // Units: [1, 1, 2]; hidden row 2 (kept suffix side) folds into leader
      // row 0 (drop side). Dropping 1 unit would orphan the follower, so the
      // drop shrinks to zero.
      const accounting = ChatWindowVisibilityAccounting(
        unitLengths: [1, 1, 2],
        hiddenLeaderByIndex: <int, int>{2: 0},
      );
      expect(
        ChatMessageCardProjector.suffixDropCountForUnits(accounting, 2),
        0,
      );
      const unlinked = ChatWindowVisibilityAccounting(
        unitLengths: [1, 1, 2],
        hiddenLeaderByIndex: <int, int>{},
      );
      // Newest 2 units = 1 + 2 raw rows -> drop the leading single-row unit.
      expect(
        ChatMessageCardProjector.suffixDropCountForUnits(unlinked, 2),
        1,
      );
    });

    test('cut alignment saturates cleanly at both window ends', () {
      // Leader in the FIRST unit, follower in the last: every cut crosses
      // the link, so top trim drops nothing and bottom trim keeps all.
      const accounting = ChatWindowVisibilityAccounting(
        unitLengths: [1, 1, 2],
        hiddenLeaderByIndex: <int, int>{3: 0},
      );
      expect(
        ChatMessageCardProjector.suffixDropCountForUnits(accounting, 2),
        0,
      );
      expect(
        ChatMessageCardProjector.suffixDropCountForUnits(accounting, 1),
        0,
      );
      expect(
        ChatMessageCardProjector.prefixRawLengthForUnits(accounting, 1),
        4,
      );
      expect(
        ChatMessageCardProjector.prefixRawLengthForUnits(accounting, 2),
        4,
      );
    });
  });
}
