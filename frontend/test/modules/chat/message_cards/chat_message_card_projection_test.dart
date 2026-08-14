import 'package:flutter_test/flutter_test.dart';

import 'package:grix/data/models/message_model.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_open_session_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_exec_approval_card_data.dart';
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
}
