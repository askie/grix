import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/lifecycle/realtime_background_policy.dart';

void main() {
  group('shouldReconnectRealtimeOnForeground', () {
    test('recycles Android sockets at the five-second boundary', () {
      expect(
        shouldReconnectRealtimeOnForeground(
          backgroundDuration: const Duration(milliseconds: 4999),
          isWeb: false,
          targetPlatform: TargetPlatform.android,
        ),
        isFalse,
      );
      expect(
        shouldReconnectRealtimeOnForeground(
          backgroundDuration: const Duration(seconds: 5),
          isWeb: false,
          targetPlatform: TargetPlatform.android,
        ),
        isTrue,
      );
    });

    test('preserves iOS, desktop and web connections', () {
      for (final platform in TargetPlatform.values) {
        if (platform == TargetPlatform.android) continue;
        expect(
          shouldReconnectRealtimeOnForeground(
            backgroundDuration: const Duration(minutes: 5),
            isWeb: false,
            targetPlatform: platform,
          ),
          isFalse,
        );
      }
      expect(
        shouldReconnectRealtimeOnForeground(
          backgroundDuration: const Duration(minutes: 5),
          isWeb: true,
          targetPlatform: TargetPlatform.android,
        ),
        isFalse,
      );
    });
  });

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
