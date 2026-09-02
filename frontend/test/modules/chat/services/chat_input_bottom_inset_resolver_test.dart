import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_input_bottom_inset_resolver.dart';

void main() {
  group('ChatInputBottomInsetResolver', () {
    test('uses the larger resting bottom inset when keyboard is hidden', () {
      final resolution = ChatInputBottomInsetResolver.resolve(
        viewPaddingBottom: 34,
        systemGestureInsetBottom: 16,
        liveKeyboardInsetBottom: 0,
        retainedKeyboardInsetBottom: 0,
        platformViewportObstructionBottom: 0,
        minBottomSpacing: 8,
      );

      expect(resolution.keyboardInset, 0);
      expect(resolution.restingBottomInset, 34);
      expect(resolution.inputBottomInset, 34);
    });

    test(
      'covers Android gesture navigation even without safe area padding',
      () {
        final resolution = ChatInputBottomInsetResolver.resolve(
          viewPaddingBottom: 0,
          systemGestureInsetBottom: 24,
          liveKeyboardInsetBottom: 0,
          retainedKeyboardInsetBottom: 0,
          platformViewportObstructionBottom: 0,
          minBottomSpacing: 8,
        );

        expect(resolution.keyboardInset, 0);
        expect(resolution.restingBottomInset, 24);
        expect(resolution.inputBottomInset, 24);
      },
    );

    test('prefers the larger of live and retained keyboard insets', () {
      final resolution = ChatInputBottomInsetResolver.resolve(
        viewPaddingBottom: 34,
        systemGestureInsetBottom: 16,
        liveKeyboardInsetBottom: 280,
        retainedKeyboardInsetBottom: 300,
        platformViewportObstructionBottom: 0,
        minBottomSpacing: 8,
      );

      expect(resolution.keyboardInset, 300);
      expect(resolution.restingBottomInset, 34);
      expect(resolution.inputBottomInset, 8);
    });

    test(
      'uses viewport obstruction when web keyboard does not update viewInsets',
      () {
        final resolution = ChatInputBottomInsetResolver.resolve(
          viewPaddingBottom: 0,
          systemGestureInsetBottom: 0,
          liveKeyboardInsetBottom: 0,
          retainedKeyboardInsetBottom: 0,
          platformViewportObstructionBottom: 260,
          minBottomSpacing: 8,
        );

        expect(resolution.keyboardInset, 260);
        expect(resolution.restingBottomInset, 8);
        expect(resolution.inputBottomInset, 8);
      },
    );
  });
}
