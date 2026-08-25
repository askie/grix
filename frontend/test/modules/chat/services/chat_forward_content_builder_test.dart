import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/models/chat_forward_message_item.dart';
import 'package:grix/modules/chat/services/chat_forward_content_builder.dart';

void main() {
  group('ChatForwardContentBuilder', () {
    test('combines accompanying message and conversation card content', () {
      final content = ChatForwardContentBuilder.buildConversationCardContent(
        cardContent: '[会话卡片](grix://card/example)',
        accompanyingMessage: '  请查看这个会话。\n有问题随时联系我。  ',
      );

      expect(
        content,
        '请查看这个会话。\n有问题随时联系我。\n\n'
        '[会话卡片](grix://card/example)',
      );
    });

    test('keeps card-only forwarding when accompanying message is blank', () {
      final content = ChatForwardContentBuilder.buildConversationCardContent(
        cardContent: '[会话卡片](grix://card/example)',
        accompanyingMessage: ' \n ',
      );

      expect(content, '[会话卡片](grix://card/example)');
    });

    test('returns empty string when no message', () {
      final content = ChatForwardContentBuilder.buildMergedContent(
        messages: const <ChatForwardMessageItem>[],
        title: 'Forwarded messages',
        senderLabel: 'Sender',
        timeLabel: 'Time',
        emptyContentPlaceholder: '[Empty message]',
      );

      expect(content, isEmpty);
    });

    test('builds merged message with stable header and separator', () {
      final content = ChatForwardContentBuilder.buildMergedContent(
        messages: const <ChatForwardMessageItem>[
          ChatForwardMessageItem(
            senderName: 'Alice',
            content: 'Hello',
            createdAt: 1700000000000,
          ),
          ChatForwardMessageItem(
            senderName: 'Bob',
            content: 'World',
            createdAt: 1700000300000,
          ),
        ],
        title: 'Forwarded messages',
        senderLabel: 'Sender',
        timeLabel: 'Time',
        emptyContentPlaceholder: '[Empty message]',
      );

      expect(content, contains('[Forwarded messages]'));
      expect(content, contains('1. Sender: Alice'));
      expect(content, contains('2. Sender: Bob'));
      expect(content, contains('\n---\n'));
      expect(content, contains('Hello'));
      expect(content, contains('World'));
    });

    test('uses placeholder when message body is empty', () {
      final content = ChatForwardContentBuilder.buildMergedContent(
        messages: const <ChatForwardMessageItem>[
          ChatForwardMessageItem(
            senderName: 'Alice',
            content: '',
            createdAt: 1700000000000,
          ),
        ],
        title: 'Forwarded messages',
        senderLabel: 'Sender',
        timeLabel: 'Time',
        emptyContentPlaceholder: '[Empty message]',
      );

      expect(content, contains('[Empty message]'));
    });

    test('writes source session id and message id when provided', () {
      final content = ChatForwardContentBuilder.buildMergedContent(
        messages: const <ChatForwardMessageItem>[
          ChatForwardMessageItem(
            senderName: 'Alice',
            content: 'Hello',
            createdAt: 1700000000000,
            sessionId: 'sess-1',
            messageId: 'msg-1',
          ),
        ],
        title: 'Forwarded messages',
        senderLabel: 'Sender',
        timeLabel: 'Time',
        emptyContentPlaceholder: '[Empty message]',
        sessionIdLabel: 'Session ID',
        messageIdLabel: 'Message ID',
      );

      expect(content, contains('Session ID: sess-1\nMessage ID: msg-1\nHello'));
    });

    test('omits id lines when ids are blank', () {
      final content = ChatForwardContentBuilder.buildMergedContent(
        messages: const <ChatForwardMessageItem>[
          ChatForwardMessageItem(
            senderName: 'Alice',
            content: 'Hello',
            createdAt: 1700000000000,
          ),
        ],
        title: 'Forwarded messages',
        senderLabel: 'Sender',
        timeLabel: 'Time',
        emptyContentPlaceholder: '[Empty message]',
      );

      expect(content, isNot(contains('Session ID')));
      expect(content, isNot(contains('Message ID')));
    });
  });
}
