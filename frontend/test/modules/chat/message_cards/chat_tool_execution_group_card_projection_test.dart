import 'package:flutter_test/flutter_test.dart';

import 'package:grix/data/models/message_model.dart';
import 'package:grix/modules/chat/message_cards/models/chat_tool_execution_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_tool_execution_group_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/modules/chat/message_cards/services/chat_tool_execution_group_card_projection.dart';

MessageModel _toolCardMessage({
  required String msgId,
  required String senderId,
  required String summaryText,
  String detailText = '',
  int senderType = 2,
}) {
  final envelope = ChatMessageCardCodec.encode(
    ChatToolExecutionCardData(summaryText: summaryText, detailText: detailText),
  );
  return MessageModel(
    msgId: msgId,
    sessionId: 'session-1',
    senderId: senderId,
    senderType: senderType,
    createdAt: 1000,
    content: envelope.content,
    extra: envelope.extra,
  );
}

MessageModel _textMessage({
  required String msgId,
  required String senderId,
  required String text,
  int senderType = 2,
}) {
  return MessageModel(
    msgId: msgId,
    sessionId: 'session-1',
    senderId: senderId,
    senderType: senderType,
    createdAt: 1000,
    content: text,
  );
}

void main() {
  test('groups 3 consecutive tool cards from same sender', () {
    final projection = ChatToolExecutionGroupProjector.project([
      _toolCardMessage(
        msgId: '1',
        senderId: 'agent-1',
        summaryText: 'Read: file_a.dart',
      ),
      _toolCardMessage(
        msgId: '2',
        senderId: 'agent-1',
        summaryText: 'Edit: file_b.dart',
        detailText: 'old -> new',
      ),
      _toolCardMessage(
        msgId: '3',
        senderId: 'agent-1',
        summaryText: 'Bash: ls -la',
      ),
    ], currentUserId: 'user-1');

    expect(projection.hiddenIndexes, containsAll(<int>{1, 2}));
    expect(
      projection.overridesByIndex[0],
      isA<ChatToolExecutionGroupCardData>(),
    );

    final groupCard =
        projection.overridesByIndex[0] as ChatToolExecutionGroupCardData;
    expect(groupCard.count, 3);
    expect(groupCard.displayCard.displaySummaryText, 'Bash: ls -la');
    expect(groupCard.children.first.displaySummaryText, 'Read: file_a.dart');
  });

  test('leaves single tool card ungrouped', () {
    final projection = ChatToolExecutionGroupProjector.project([
      _toolCardMessage(
        msgId: '1',
        senderId: 'agent-1',
        summaryText: 'Read: file.dart',
      ),
    ], currentUserId: 'user-1');

    expect(projection.hiddenIndexes, isEmpty);
    expect(projection.overridesByIndex, isEmpty);
  });

  test('does not group tool cards from different senders', () {
    final projection = ChatToolExecutionGroupProjector.project([
      _toolCardMessage(
        msgId: '1',
        senderId: 'agent-1',
        summaryText: 'Read: file_a.dart',
      ),
      _toolCardMessage(
        msgId: '2',
        senderId: 'agent-2',
        summaryText: 'Edit: file_b.dart',
      ),
    ], currentUserId: 'user-1');

    expect(projection.hiddenIndexes, isEmpty);
    expect(projection.overridesByIndex, isEmpty);
  });

  test('splits groups when text message breaks the sequence', () {
    final projection = ChatToolExecutionGroupProjector.project([
      _toolCardMessage(
        msgId: '1',
        senderId: 'agent-1',
        summaryText: 'Read: file_a.dart',
      ),
      _textMessage(
        msgId: '2',
        senderId: 'agent-1',
        text: 'Let me check that file.',
      ),
      _toolCardMessage(
        msgId: '3',
        senderId: 'agent-1',
        summaryText: 'Edit: file_b.dart',
      ),
    ], currentUserId: 'user-1');

    expect(projection.hiddenIndexes, isEmpty);
    expect(projection.overridesByIndex, isEmpty);
  });

  test('groups multiple runs independently', () {
    final projection = ChatToolExecutionGroupProjector.project([
      _toolCardMessage(
        msgId: '1',
        senderId: 'agent-1',
        summaryText: 'Read: a.dart',
      ),
      _toolCardMessage(
        msgId: '2',
        senderId: 'agent-1',
        summaryText: 'Edit: b.dart',
      ),
      _textMessage(
        msgId: '3',
        senderId: 'agent-1',
        text: 'Done with first batch.',
      ),
      _toolCardMessage(
        msgId: '4',
        senderId: 'agent-1',
        summaryText: 'Read: c.dart',
      ),
      _toolCardMessage(
        msgId: '5',
        senderId: 'agent-1',
        summaryText: 'Bash: test',
      ),
      _toolCardMessage(
        msgId: '6',
        senderId: 'agent-1',
        summaryText: 'Edit: d.dart',
      ),
    ], currentUserId: 'user-1');

    expect(projection.hiddenIndexes, containsAll(<int>{1, 5}));
    expect(
      projection.overridesByIndex[0],
      isA<ChatToolExecutionGroupCardData>(),
    );
    expect(
      projection.overridesByIndex[3],
      isA<ChatToolExecutionGroupCardData>(),
    );

    final firstGroup =
        projection.overridesByIndex[0] as ChatToolExecutionGroupCardData;
    expect(firstGroup.count, 2);

    final secondGroup =
        projection.overridesByIndex[3] as ChatToolExecutionGroupCardData;
    expect(secondGroup.count, 3);
    expect(secondGroup.displayCard.displaySummaryText, 'Edit: d.dart');
  });

  test('returns empty for messages without tool cards', () {
    final projection = ChatToolExecutionGroupProjector.project([
      _textMessage(msgId: '1', senderId: 'user-1', text: 'Hello'),
      _textMessage(msgId: '2', senderId: 'agent-1', text: 'Hi there'),
    ], currentUserId: 'user-1');

    expect(projection.hiddenIndexes, isEmpty);
    expect(projection.overridesByIndex, isEmpty);
  });

  test('returns empty for empty message list', () {
    final projection = ChatToolExecutionGroupProjector.project(
      [],
      currentUserId: 'user-1',
    );

    expect(projection.hiddenIndexes, isEmpty);
    expect(projection.overridesByIndex, isEmpty);
  });

  test('groups exactly 2 consecutive cards', () {
    final projection = ChatToolExecutionGroupProjector.project([
      _toolCardMessage(
        msgId: '1',
        senderId: 'agent-1',
        summaryText: 'Read: a.dart',
      ),
      _toolCardMessage(
        msgId: '2',
        senderId: 'agent-1',
        summaryText: 'Edit: b.dart',
      ),
    ], currentUserId: 'user-1');

    expect(projection.hiddenIndexes, {1});
    final groupCard =
        projection.overridesByIndex[0] as ChatToolExecutionGroupCardData;
    expect(groupCard.count, 2);
  });
}
