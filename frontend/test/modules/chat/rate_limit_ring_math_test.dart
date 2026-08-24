import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/rate_limit_ring_math.dart';

void main() {
  group('resolveRateLimitWindowDuration', () {
    test('recognizes Cursor monthly M and API for dual time ring', () {
      expect(
        resolveRateLimitWindowDuration(
          localAction: 'get_rate_limits',
          itemId: 'rate_limit_monthly',
          centerText: 'M',
        ),
        const Duration(days: 30),
      );
      expect(
        resolveRateLimitWindowDuration(
          localAction: 'get_rate_limits',
          itemId: 'rate_limit_api',
          centerText: 'API',
        ),
        const Duration(days: 30),
      );
    });

    test('keeps 5H / 7D and codex primary/secondary windows', () {
      expect(
        resolveRateLimitWindowDuration(
          localAction: 'get_rate_limits',
          itemId: 'rate_limit_5h',
          centerText: '5H',
        ),
        const Duration(hours: 5),
      );
      expect(
        resolveRateLimitWindowDuration(
          localAction: 'get_rate_limits',
          itemId: 'rate_limit_7d',
          centerText: '7D',
        ),
        const Duration(days: 7),
      );
      expect(
        resolveRateLimitWindowDuration(
          localAction: 'get_rate_limits',
          itemId: 'rate_limit_primary',
          centerText: '5H',
        ),
        const Duration(hours: 5),
      );
      expect(
        resolveRateLimitWindowDuration(
          localAction: 'get_rate_limits',
          itemId: 'rate_limit_secondary',
          centerText: '7D',
        ),
        const Duration(days: 7),
      );
    });

    test('uses an explicit window for an extra rate limit', () {
      expect(
        resolveRateLimitWindowDuration(
          localAction: 'get_rate_limits',
          itemId: 'rate_limit_extra_0',
          centerText: '96',
          progressWindowMinutes: 300,
        ),
        const Duration(hours: 5),
      );
      expect(
        resolveRateLimitWindowDuration(
          localAction: 'get_rate_limits',
          itemId: 'rate_limit_extra_1',
          centerText: '32',
          progressWindowMinutes: 10080,
        ),
        const Duration(days: 7),
      );
    });

    test('returns null for non rate-limit progress', () {
      expect(
        resolveRateLimitWindowDuration(
          localAction: 'thread_compact',
          itemId: 'thread_compact',
          centerText: '42',
        ),
        isNull,
      );
    });
  });

  group('parseRateLimitResetTime', () {
    test('parses unix seconds and milliseconds', () {
      final sec = parseRateLimitResetTime('1787455773');
      expect(sec, DateTime.fromMillisecondsSinceEpoch(1787455773 * 1000));

      final ms = parseRateLimitResetTime('1700000000000');
      expect(ms, DateTime.fromMillisecondsSinceEpoch(1700000000000));
    });

    test('parses ISO8601', () {
      final t = parseRateLimitResetTime('2026-08-08T00:00:00Z');
      expect(t?.isUtc, isTrue);
      expect(t?.year, 2026);
    });
  });

  group('computeRateLimitTimePercent', () {
    test('maps remaining window to elapsed percent', () {
      final now = DateTime.utc(2026, 8, 1);
      final reset = now.add(const Duration(days: 15));
      final pct = computeRateLimitTimePercent(
        reset,
        const Duration(days: 30),
        now,
      );
      expect(pct, closeTo(50.0, 0.01));
    });
  });
}
