import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_message_window_owners.dart';

void main() {
  setUp(ChatMessageWindowOwners.resetForTest);

  group('ChatMessageWindowOwners', () {
    test('enter keeps the newest owner last and ignores blanks', () {
      ChatMessageWindowOwners.enter('s1', userId: '42');
      ChatMessageWindowOwners.enter(' s2 ', userId: '42');
      ChatMessageWindowOwners.enter('   ', userId: '42');
      ChatMessageWindowOwners.enter('s1', userId: '42');

      expect(ChatMessageWindowOwners.ownersForTest, ['s2', 's1']);
    });

    test(
      'latestAlive returns the previous chat after a nested chat leaves',
      () {
        ChatMessageWindowOwners.enter('pane', userId: '42');
        ChatMessageWindowOwners.enter('nested', userId: '42');
        ChatMessageWindowOwners.leave('nested');

        expect(
          ChatMessageWindowOwners.latestAlive(
            userId: '42',
            isAlive: (_) => true,
          ),
          'pane',
        );
        expect(ChatMessageWindowOwners.ownersForTest, ['pane']);
      },
    );

    test(
      'latestAlive drops dead owners and returns null when none is left',
      () {
        ChatMessageWindowOwners.enter('dead-1', userId: '42');
        ChatMessageWindowOwners.enter('alive', userId: '42');
        ChatMessageWindowOwners.enter('dead-2', userId: '42');

        expect(
          ChatMessageWindowOwners.latestAlive(
            userId: '42',
            isAlive: (sid) => sid == 'alive',
          ),
          'alive',
        );
        expect(ChatMessageWindowOwners.ownersForTest, ['dead-1', 'alive']);

        expect(
          ChatMessageWindowOwners.latestAlive(
            userId: '42',
            isAlive: (_) => false,
          ),
          isNull,
        );
        expect(ChatMessageWindowOwners.ownersForTest, isEmpty);
      },
    );

    test('latestAlive skips owners recorded for another account', () {
      ChatMessageWindowOwners.enter('mine', userId: '42');
      ChatMessageWindowOwners.enter('theirs', userId: '7');

      expect(
        ChatMessageWindowOwners.latestAlive(userId: '42', isAlive: (_) => true),
        'mine',
      );
      expect(ChatMessageWindowOwners.ownersForTest, ['mine']);
    });

    test('leave of an unknown session is a no-op', () {
      ChatMessageWindowOwners.enter('s1', userId: '42');
      ChatMessageWindowOwners.leave('missing');

      expect(ChatMessageWindowOwners.ownersForTest, ['s1']);
    });
  });
}
