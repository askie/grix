import 'dart:async';

import 'package:shared_preferences/shared_preferences.dart';

import 'sentry_event_dedup_store.dart';

SentryDedupStore createSentryDedupStore() => PreferencesSentryDedupStore();

class PreferencesSentryDedupStore implements SentryDedupStore {
  PreferencesSentryDedupStore({
    SharedPreferencesAsync? preferences,
    this.window = sentryDedupWindow,
    this.pendingWindow = sentryDedupPendingWindow,
    this.maxEntries = sentryDedupMaxEntries,
    this.maxBytes = sentryDedupMaxBytes,
  }) : _preferences = preferences ?? SharedPreferencesAsync();

  static Future<void> _tail = Future<void>.value();
  final SharedPreferencesAsync _preferences;
  final Duration window;
  final Duration pendingWindow;
  final int maxEntries;
  final int maxBytes;
  final MemorySentryDedupStore _fallback = MemorySentryDedupStore();

  @override
  Future<bool> claim({
    required String fingerprint,
    required String token,
    required int now,
  }) {
    return _serialized(() async {
      try {
        final raw = await _preferences.getString(sentryDedupStorageKey);
        final state = SentryDedupState.decode(
          raw != null && raw.length <= maxBytes ? raw : null,
          maxEntries: maxEntries,
        );
        final claimed = state.claim(
          fingerprint: fingerprint,
          token: token,
          now: now,
          window: window,
          pendingWindow: pendingWindow,
          maxEntries: maxEntries,
        );
        await _preferences.setString(sentryDedupStorageKey, state.encode());
        return claimed;
      } catch (_) {
        return _fallback.claim(
          fingerprint: fingerprint,
          token: token,
          now: now,
        );
      }
    });
  }

  @override
  Future<void> complete({
    required String fingerprint,
    required String token,
    required int now,
    required bool succeeded,
  }) {
    return _serialized(() async {
      try {
        final raw = await _preferences.getString(sentryDedupStorageKey);
        final state = SentryDedupState.decode(
          raw != null && raw.length <= maxBytes ? raw : null,
          maxEntries: maxEntries,
        );
        state.complete(
          fingerprint: fingerprint,
          token: token,
          now: now,
          succeeded: succeeded,
          window: window,
          pendingWindow: pendingWindow,
          maxEntries: maxEntries,
        );
        await _preferences.setString(sentryDedupStorageKey, state.encode());
      } catch (_) {
        await _fallback.complete(
          fingerprint: fingerprint,
          token: token,
          now: now,
          succeeded: succeeded,
        );
      }
    });
  }

  static Future<T> _serialized<T>(Future<T> Function() operation) async {
    final previous = _tail;
    final completer = Completer<void>();
    _tail = completer.future;
    await previous;
    try {
      return await operation();
    } finally {
      completer.complete();
    }
  }
}
