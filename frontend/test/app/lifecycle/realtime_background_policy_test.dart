import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/lifecycle/realtime_background_policy.dart';

void main() {
  group('realtimeBackgroundSuspendDelay', () {
    test('uses short grace periods on mobile platforms', () {
      expect(
        realtimeBackgroundSuspendDelay(
          isWeb: false,
          targetPlatform: TargetPlatform.android,
        ),
        const Duration(seconds: 25),
      );
      expect(
        realtimeBackgroundSuspendDelay(
          isWeb: false,
          targetPlatform: TargetPlatform.iOS,
        ),
        const Duration(seconds: 8),
      );
    });

    test('returns zero on desktop and web', () {
      expect(
        realtimeBackgroundSuspendDelay(
          isWeb: false,
          targetPlatform: TargetPlatform.macOS,
        ),
        Duration.zero,
      );
      expect(
        realtimeBackgroundSuspendDelay(
          isWeb: true,
          targetPlatform: TargetPlatform.android,
        ),
        Duration.zero,
      );
    });
  });

  group('shouldSuspendRealtimeForBackground', () {
    test('suspends realtime on mobile platforms', () {
      expect(
        shouldSuspendRealtimeForBackground(
          isWeb: false,
          targetPlatform: TargetPlatform.android,
        ),
        isTrue,
      );
      expect(
        shouldSuspendRealtimeForBackground(
          isWeb: false,
          targetPlatform: TargetPlatform.iOS,
        ),
        isTrue,
      );
    });

    test('keeps realtime active on desktop platforms', () {
      expect(
        shouldSuspendRealtimeForBackground(
          isWeb: false,
          targetPlatform: TargetPlatform.macOS,
        ),
        isFalse,
      );
      expect(
        shouldSuspendRealtimeForBackground(
          isWeb: false,
          targetPlatform: TargetPlatform.windows,
        ),
        isFalse,
      );
      expect(
        shouldSuspendRealtimeForBackground(
          isWeb: false,
          targetPlatform: TargetPlatform.linux,
        ),
        isFalse,
      );
    });

    test('never suspends realtime on web', () {
      expect(
        shouldSuspendRealtimeForBackground(
          isWeb: true,
          targetPlatform: TargetPlatform.android,
        ),
        isFalse,
      );
    });
  });
}
