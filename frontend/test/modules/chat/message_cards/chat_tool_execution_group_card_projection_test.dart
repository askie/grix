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

  _registerAccountingTests();
}

// ---------------------------------------------------------------------------
// Visible-unit accounting (resident window cap + first-screen auto-fill).
// ---------------------------------------------------------------------------

List<MessageModel> _accountingFixture() {
  // 2 texts + 4-card run (agent-1) + 1 text + 3-card run (agent-2) + 1 text.
  return [
    _textMessage(msgId: 't1', senderId: 'agent-1', text: 'hello'),
    _textMessage(msgId: 't2', senderId: 'user-1', text: 'hi'),
    for (var i = 1; i <= 4; i++)
      _toolCardMessage(
        msgId: 'a$i',
        senderId: 'agent-1',
        summaryText: 'Bash: a$i',
      ),
    _textMessage(msgId: 't3', senderId: 'agent-1', text: 'mid'),
    for (var i = 1; i <= 3; i++)
      _toolCardMessage(
        msgId: 'b$i',
        senderId: 'agent-2',
        summaryText: 'Read: b$i',
      ),
    _textMessage(msgId: 't4', senderId: 'user-1', text: 'tail'),
  ];
}

void _registerAccountingTests() {
  group('visible unit accounting', () {
    test('isToolExecutionCardContent matches encoded tool cards only', () {
      final card = _toolCardMessage(
        msgId: 'x',
        senderId: 'a',
        summaryText: 's',
      );
      expect(
        ChatToolExecutionGroupProjector.isToolExecutionCardContent(
          card.content,
        ),
        isTrue,
      );
      // Bare text without the card-URI wrapper is not a tool card.
      expect(
        ChatToolExecutionGroupProjector.isToolExecutionCardContent(
          'plain text grix://card/tool_execution without wrapper',
        ),
        isFalse,
      );
      expect(
        ChatToolExecutionGroupProjector.isToolExecutionCardContent('hello'),
        isFalse,
      );
      // tool_execution_group URIs must not collide with the tool_execution
      // needle (prefix overlap).
      expect(
        ChatToolExecutionGroupProjector.isToolExecutionCardContent(
          '[x](grix://card/tool_execution_group?d=%7B%7D)',
        ),
        isFalse,
      );
    });

    test('collapsed runs count as one unit, matching the projector', () {
      final messages = _accountingFixture();
      expect(
        ChatToolExecutionGroupProjector.visibleUnitLengths(messages),
        [1, 1, 4, 1, 3, 1],
      );
      expect(
        ChatToolExecutionGroupProjector.visibleBubbleCount(messages),
        6,
      );
      // Sanity: matches the real projection (2 groups + 4 standalone rows).
      final projection = ChatToolExecutionGroupProjector.project(
        messages,
        currentUserId: 'me',
      );
      expect(projection.overridesByIndex.length, 2);
      expect(messages.length - projection.hiddenIndexes.length, 6);
    });

    test('single tool card counts as its own unit', () {
      final messages = [
        _toolCardMessage(msgId: '1', senderId: 'a', summaryText: 's'),
        _textMessage(msgId: '2', senderId: 'a', text: 't'),
        _toolCardMessage(msgId: '3', senderId: 'a', summaryText: 's'),
      ];
      expect(
        ChatToolExecutionGroupProjector.visibleUnitLengths(messages),
        [1, 1, 1],
      );
    });

    test('prefix length keeps whole units and never splits a run', () {
      final messages = _accountingFixture();
      // 11 raw rows, 6 units: capping at all 6 units keeps everything.
      expect(
        ChatToolExecutionGroupProjector.prefixRawLengthForUnits(messages, 6),
        11,
      );
      // Capping at 5 units keeps the whole 3-card run (unit boundary).
      expect(
        ChatToolExecutionGroupProjector.prefixRawLengthForUnits(messages, 5),
        10,
      );
    });

    test('suffix start keeps the newest whole units', () {
      final messages = _accountingFixture();
      expect(
        ChatToolExecutionGroupProjector.suffixRawStartForUnits(messages, 6),
        0,
      );
      // Newest 2 units = 3-card run + tail text = 4 raw rows -> start 7.
      expect(
        ChatToolExecutionGroupProjector.suffixRawStartForUnits(messages, 2),
        7,
      );
      // Newest 3 units = mid text + 3-card run + tail text = 5 raw rows.
      expect(
        ChatToolExecutionGroupProjector.suffixRawStartForUnits(messages, 3),
        6,
      );
    });

    test('prefix of 4 units covers the first four unit lengths', () {
      final messages = _accountingFixture();
      // units [1,1,4,1,3,1] -> first 4 units = 1+1+4+1 = 7 raw rows.
      expect(
        ChatToolExecutionGroupProjector.prefixRawLengthForUnits(messages, 4),
        7,
      );
      // 3 units = 1+1+4 = 6 raw rows.
      expect(
        ChatToolExecutionGroupProjector.prefixRawLengthForUnits(messages, 3),
        6,
      );
    });
  });
}
