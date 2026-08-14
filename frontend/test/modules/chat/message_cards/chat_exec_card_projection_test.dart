import 'package:flutter_test/flutter_test.dart';

import 'package:grix/data/models/message_model.dart';
import 'package:grix/modules/chat/message_cards/models/chat_exec_approval_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_exec_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_exec_card_projection.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';

void main() {
  test(
    'projects resolution and execution statuses into original approval card',
    () {
      final approvalEnvelope = ChatMessageCardCodec.buildExecApprovalCard(
        approvalId: 'approval_full_123',
        approvalSlug: 'req_123',
        command: 'pwd',
        host: 'gateway',
      );
      final resolutionEnvelope = ChatMessageCardCodec.buildExecStatusCard(
        status: 'resolved-allow-once',
        summary: 'Allow once selected by u_1.',
        approvalId: 'approval_full_123',
        approvalCommandId: 'req_123',
        decision: 'allow-once',
        resolvedById: 'u_1',
      );
      final statusEnvelope = ChatMessageCardCodec.buildExecStatusCard(
        status: 'finished',
        summary:
            'Exec finished (gateway id=approval_full_123, session=sess_456, code 0)',
        approvalId: 'approval_full_123',
        host: 'gateway',
        sessionId: 'sess_456',
        exitLabel: 'code 0',
      );

      final projection = ChatExecCardProjector.project([
        MessageModel(
          msgId: '1001',
          sessionId: 'session-1',
          senderId: 'agent-1',
          createdAt: 1000,
          content: approvalEnvelope.content,
          extra: approvalEnvelope.extra,
        ),
        MessageModel(
          msgId: '1002',
          sessionId: 'session-1',
          senderId: 'agent-1',
          createdAt: 1500,
          content: resolutionEnvelope.content,
          extra: resolutionEnvelope.extra,
        ),
        MessageModel(
          msgId: '1003',
          sessionId: 'session-1',
          senderId: 'agent-1',
          createdAt: 2000,
          content: statusEnvelope.content,
          extra: statusEnvelope.extra,
        ),
      ]);

      expect(projection.hiddenIndexes, containsAll(<int>{1, 2}));
      expect(projection.overridesByIndex[0], isA<ChatExecApprovalCardData>());

      final resolvedCard =
          projection.overridesByIndex[0] as ChatExecApprovalCardData;
      expect(resolvedCard.isResolved, isTrue);
      expect(resolvedCard.resolutionStatus, isA<ChatExecStatusCardData>());
      expect(resolvedCard.resolutionStatus!.status, 'resolved-allow-once');
      expect(resolvedCard.executionStatus, isA<ChatExecStatusCardData>());
      expect(resolvedCard.executionStatus!.status, 'finished');
    },
  );

  test(
    'keeps standalone exec status when no matching approval card exists',
    () {
      final statusEnvelope = ChatMessageCardCodec.buildExecStatusCard(
        status: 'denied',
        summary: 'Exec denied (gateway id=approval_full_123, approval-timeout)',
        approvalId: 'approval_full_123',
        host: 'gateway',
        reason: 'approval-timeout',
      );

      final projection = ChatExecCardProjector.project([
        MessageModel(
          msgId: '1002',
          sessionId: 'session-1',
          senderId: 'agent-1',
          createdAt: 2000,
          content: statusEnvelope.content,
          extra: statusEnvelope.extra,
        ),
      ]);

      expect(projection.hiddenIndexes, isEmpty);
      expect(projection.overridesByIndex, isEmpty);
    },
  );

  test('projects approval unavailable into original approval card', () {
    final approvalEnvelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval_full_888',
      approvalSlug: 'req_888',
      command: 'pwd',
      host: 'gateway',
    );
    final unavailableEnvelope = ChatMessageCardCodec.buildExecStatusCard(
      status: 'approval-unavailable',
      summary: 'Approval is no longer available.',
      approvalId: 'approval_full_888',
      reason: 'expired',
    );

    final projection = ChatExecCardProjector.project([
      MessageModel(
        msgId: '2001',
        sessionId: 'session-1',
        senderId: 'agent-1',
        createdAt: 1000,
        content: approvalEnvelope.content,
        extra: approvalEnvelope.extra,
      ),
      MessageModel(
        msgId: '2002',
        sessionId: 'session-1',
        senderId: 'agent-1',
        createdAt: 1500,
        content: unavailableEnvelope.content,
        extra: unavailableEnvelope.extra,
      ),
    ]);

    expect(projection.hiddenIndexes, contains(1));
    final resolvedCard =
        projection.overridesByIndex[0] as ChatExecApprovalCardData;
    expect(resolvedCard.resolutionStatus, isNotNull);
    expect(resolvedCard.resolutionStatus!.status, 'approval-unavailable');
  });

  test('projects expired approval into original approval card', () {
    final approvalEnvelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval_full_999',
      approvalSlug: 'req_999',
      command: 'pwd',
      host: 'gateway',
    );
    final expiredEnvelope = ChatMessageCardCodec.buildExecStatusCard(
      status: 'approval-expired',
      summary: 'Exec approval expired.',
      approvalId: 'approval_full_999',
      warningText: 'This approval request is no longer valid.',
    );

    final projection = ChatExecCardProjector.project([
      MessageModel(
        msgId: '3001',
        sessionId: 'session-1',
        senderId: 'agent-1',
        createdAt: 1000,
        content: approvalEnvelope.content,
        extra: approvalEnvelope.extra,
      ),
      MessageModel(
        msgId: '3002',
        sessionId: 'session-1',
        senderId: 'agent-1',
        createdAt: 1500,
        content: expiredEnvelope.content,
        extra: expiredEnvelope.extra,
      ),
    ]);

    expect(projection.hiddenIndexes, contains(1));
    final resolvedCard =
        projection.overridesByIndex[0] as ChatExecApprovalCardData;
    expect(resolvedCard.resolutionStatus, isNotNull);
    expect(resolvedCard.resolutionStatus!.status, 'approval-expired');
  });
}
