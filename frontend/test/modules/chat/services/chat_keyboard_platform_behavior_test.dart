import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_keyboard_platform_behavior.dart';

void main() {
  test('resolves native iOS keyboard behavior', () {
    final behavior = ChatKeyboardPlatformBehavior.resolve(
      isWeb: false,
      targetPlatform: TargetPlatform.iOS,
    );

    expect(behavior.kind, ChatKeyboardPlatformKind.ios);
    expect(behavior.shouldRestoreComposerFocusAfterSubmit, isFalse);
    expect(behavior.shouldApplyFocusedZeroInsetHysteresis, isTrue);
    expect(
      behavior.submitInsetRetentionDuration,
      const Duration(milliseconds: 400),
    );
  });

  test('resolves native Android keyboard behavior', () {
    final behavior = ChatKeyboardPlatformBehavior.resolve(
      isWeb: false,
      targetPlatform: TargetPlatform.android,
    );

    expect(behavior.kind, ChatKeyboardPlatformKind.android);
    expect(behavior.shouldRestoreComposerFocusAfterSubmit, isTrue);
    expect(behavior.shouldApplyFocusedZeroInsetHysteresis, isFalse);
    expect(
      behavior.submitInsetRetentionDuration,
      const Duration(milliseconds: 300),
    );
  });

  test('resolves mobile web keyboard behavior', () {
    final behavior = ChatKeyboardPlatformBehavior.resolve(
      isWeb: true,
      targetPlatform: TargetPlatform.iOS,
    );

    expect(behavior.kind, ChatKeyboardPlatformKind.webMobile);
    expect(behavior.shouldRestoreComposerFocusAfterSubmit, isFalse);
    expect(behavior.shouldApplyFocusedZeroInsetHysteresis, isFalse);
    expect(
      behavior.submitInsetRetentionDuration,
      const Duration(milliseconds: 240),
    );
  });

  test('resolves desktop web keyboard behavior', () {
    final behavior = ChatKeyboardPlatformBehavior.resolve(
      isWeb: true,
      targetPlatform: TargetPlatform.macOS,
    );

    expect(behavior.kind, ChatKeyboardPlatformKind.webDesktop);
    expect(behavior.shouldRestoreComposerFocusAfterSubmit, isTrue);
    expect(behavior.shouldApplyFocusedZeroInsetHysteresis, isFalse);
    expect(
      behavior.submitInsetRetentionDuration,
      const Duration(milliseconds: 180),
    );
  });
}
