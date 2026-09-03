import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_forward_mention_sanitizer.dart';

void main() {
  group('ChatForwardMentionSanitizer', () {
    test('converts numeric mention token to full-width @', () {
      final sanitized = ChatForwardMentionSanitizer.neutralizeNumericMentions(
        'hi @12345',
      );
      expect(sanitized, 'hi ＠12345');
    });

    test('keeps non numeric mention untouched', () {
      final sanitized = ChatForwardMentionSanitizer.neutralizeNumericMentions(
        'hi @alice',
      );
      expect(sanitized, 'hi @alice');
    });

    test('keeps numeric email segment untouched', () {
      final sanitized = ChatForwardMentionSanitizer.neutralizeNumericMentions(
        'mail a@123.com',
      );
      expect(sanitized, 'mail a@123.com');
    });

    test('keeps database connection string byte-for-byte intact', () {
      const content =
          'postgres://eshop:eshop@100.64.0.7:15432/eshop?sslmode=disable';
      expect(
        ChatForwardMentionSanitizer.neutralizeNumericMentions(content),
        content,
      );
    });

    test('keeps dotted numeric literal untouched', () {
      expect(
        ChatForwardMentionSanitizer.neutralizeNumericMentions('@1.2.3.4 down'),
        '@1.2.3.4 down',
      );
    });

    test('neutralizes every real mention in one message', () {
      final sanitized = ChatForwardMentionSanitizer.neutralizeNumericMentions(
        '@123 和 @456，见 postgres://u:p@100.64.0.7:5432/db',
      );
      expect(
        sanitized,
        '＠123 和 ＠456，见 postgres://u:p@100.64.0.7:5432/db',
      );
    });
  });
}
