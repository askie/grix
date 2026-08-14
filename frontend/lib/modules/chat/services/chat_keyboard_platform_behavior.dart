import 'package:flutter/foundation.dart';

enum ChatKeyboardPlatformKind { ios, android, webMobile, webDesktop, desktop }

@immutable
class ChatKeyboardPlatformBehavior {
  const ChatKeyboardPlatformBehavior({
    required this.kind,
    required this.submitInsetRetentionDuration,
    required this.focusedZeroInsetHysteresisDuration,
    required this.shouldRestoreComposerFocusAfterSubmit,
  });

  factory ChatKeyboardPlatformBehavior.resolve({
    bool? isWeb,
    TargetPlatform? targetPlatform,
  }) {
    final resolvedIsWeb = isWeb ?? kIsWeb;
    final resolvedTargetPlatform = targetPlatform ?? defaultTargetPlatform;
    if (resolvedIsWeb) {
      switch (resolvedTargetPlatform) {
        case TargetPlatform.iOS:
        case TargetPlatform.android:
          return const ChatKeyboardPlatformBehavior(
            kind: ChatKeyboardPlatformKind.webMobile,
            submitInsetRetentionDuration: Duration(milliseconds: 240),
            focusedZeroInsetHysteresisDuration: Duration.zero,
            shouldRestoreComposerFocusAfterSubmit: false,
          );
        case TargetPlatform.macOS:
        case TargetPlatform.windows:
        case TargetPlatform.linux:
        case TargetPlatform.fuchsia:
          return const ChatKeyboardPlatformBehavior(
            kind: ChatKeyboardPlatformKind.webDesktop,
            submitInsetRetentionDuration: Duration(milliseconds: 180),
            focusedZeroInsetHysteresisDuration: Duration.zero,
            shouldRestoreComposerFocusAfterSubmit: true,
          );
      }
    }

    switch (resolvedTargetPlatform) {
      case TargetPlatform.iOS:
        return const ChatKeyboardPlatformBehavior(
          kind: ChatKeyboardPlatformKind.ios,
          submitInsetRetentionDuration: Duration(milliseconds: 400),
          focusedZeroInsetHysteresisDuration: Duration(milliseconds: 150),
          shouldRestoreComposerFocusAfterSubmit: false,
        );
      case TargetPlatform.android:
      case TargetPlatform.fuchsia:
        return const ChatKeyboardPlatformBehavior(
          kind: ChatKeyboardPlatformKind.android,
          submitInsetRetentionDuration: Duration(milliseconds: 300),
          focusedZeroInsetHysteresisDuration: Duration.zero,
          shouldRestoreComposerFocusAfterSubmit: true,
        );
      case TargetPlatform.macOS:
      case TargetPlatform.windows:
      case TargetPlatform.linux:
        return const ChatKeyboardPlatformBehavior(
          kind: ChatKeyboardPlatformKind.desktop,
          submitInsetRetentionDuration: Duration(milliseconds: 300),
          focusedZeroInsetHysteresisDuration: Duration.zero,
          shouldRestoreComposerFocusAfterSubmit: true,
        );
    }
  }

  final ChatKeyboardPlatformKind kind;
  final Duration submitInsetRetentionDuration;
  final Duration focusedZeroInsetHysteresisDuration;
  final bool shouldRestoreComposerFocusAfterSubmit;

  bool get shouldApplyFocusedZeroInsetHysteresis =>
      focusedZeroInsetHysteresisDuration > Duration.zero;
}
