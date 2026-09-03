import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/chat_numeric_mention_resolver.dart';

void main() {
  group('ChatNumericMentionResolver.extractNumericMentionUserIds', () {
    test('extracts a plain numeric mention', () {
      expect(
        ChatNumericMentionResolver.extractNumericMentionUserIds('hi @123 there'),
        <String>['123'],
      );
    });

    test('extracts a mention after chinese text', () {
      expect(
        ChatNumericMentionResolver.extractNumericMentionUserIds('你好@123'),
        <String>['123'],
      );
    });

    test('ignores an @ glued to a word', () {
      expect(
        ChatNumericMentionResolver.extractNumericMentionUserIds('user@123'),
        isEmpty,
      );
    });

    test('ignores a database connection string', () {
      expect(
        ChatNumericMentionResolver.extractNumericMentionUserIds(
          'postgres://eshop:eshop@100.64.0.7:15432/eshop?sslmode=disable',
        ),
        isEmpty,
      );
    });

    test('ignores a dotted numeric literal', () {
      expect(
        ChatNumericMentionResolver.extractNumericMentionUserIds('@1.2.3.4 down'),
        isEmpty,
      );
    });

    test('ignores an @ glued after a dot', () {
      expect(
        ChatNumericMentionResolver.extractNumericMentionUserIds('v1.@123'),
        isEmpty,
      );
    });
  });

  group('ChatNumericMentionResolver.replaceNumericMentions', () {
    String? resolve(String userId) => userId == '123' ? '四喜' : null;

    test('replaces a real mention with the display name', () {
      expect(
        ChatNumericMentionResolver.replaceNumericMentions(
          '@123 在吗',
          resolveDisplayName: resolve,
        ),
        '@四喜 在吗',
      );
    });

    test('keeps a connection string intact', () {
      const content = 'db postgres://u:p@123.64.0.7:5432/db';
      expect(
        ChatNumericMentionResolver.replaceNumericMentions(
          content,
          resolveDisplayName: resolve,
        ),
        content,
      );
    });
  });
}
