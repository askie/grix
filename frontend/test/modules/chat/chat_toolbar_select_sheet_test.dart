import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/chat_view.dart';

void main() {
  test('chatToolbarSelectUsesSheet switches at the long-list threshold', () {
    expect(chatToolbarSelectUsesSheet(0), isFalse);
    expect(chatToolbarSelectUsesSheet(kChatToolbarSelectSheetMinOptions - 1), isFalse);
    expect(chatToolbarSelectUsesSheet(kChatToolbarSelectSheetMinOptions), isTrue);
    expect(chatToolbarSelectUsesSheet(120), isTrue);
  });
}
