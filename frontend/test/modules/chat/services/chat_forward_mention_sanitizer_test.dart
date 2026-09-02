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

    test('converts numeric email segment to avoid mention parsing', () {
      final sanitized = ChatForwardMentionSanitizer.neutralizeNumericMentions(
        'mail a@123.com',
      );
      expect(sanitized, 'mail a＠123.com');
    });
  });
}
