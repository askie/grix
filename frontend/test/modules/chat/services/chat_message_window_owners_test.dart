import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_message_window_owners.dart';

void main() {
  setUp(ChatMessageWindowOwners.resetForTest);

  group('ChatMessageWindowOwners', () {
    test('enter keeps the newest owner last and ignores blanks', () {
      ChatMessageWindowOwners.enter('s1');
      ChatMessageWindowOwners.enter(' s2 ');
      ChatMessageWindowOwners.enter('   ');
      ChatMessageWindowOwners.enter('s1');

      expect(ChatMessageWindowOwners.ownersForTest, ['s2', 's1']);
    });

    test(
      'latestAlive returns the previous chat after a nested chat leaves',
      () {
        ChatMessageWindowOwners.enter('pane');
        ChatMessageWindowOwners.enter('nested');
        ChatMessageWindowOwners.leave('nested');

        expect(ChatMessageWindowOwners.latestAlive((_) => true), 'pane');
        expect(ChatMessageWindowOwners.ownersForTest, ['pane']);
      },
    );

    test(
      'latestAlive drops dead owners and returns null when none is left',
      () {
        ChatMessageWindowOwners.enter('dead-1');
        ChatMessageWindowOwners.enter('alive');
        ChatMessageWindowOwners.enter('dead-2');

        expect(
          ChatMessageWindowOwners.latestAlive((sid) => sid == 'alive'),
          'alive',
        );
        expect(ChatMessageWindowOwners.ownersForTest, ['dead-1', 'alive']);

        expect(ChatMessageWindowOwners.latestAlive((_) => false), isNull);
        expect(ChatMessageWindowOwners.ownersForTest, isEmpty);
      },
    );

    test('leave of an unknown session is a no-op', () {
      ChatMessageWindowOwners.enter('s1');
      ChatMessageWindowOwners.leave('missing');

      expect(ChatMessageWindowOwners.ownersForTest, ['s1']);
    });
  });
}
