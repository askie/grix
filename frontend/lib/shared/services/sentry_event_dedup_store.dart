import 'dart:async';
import 'dart:convert';

const sentryDedupWindow = Duration(hours: 24);
const sentryDedupPendingWindow = Duration(minutes: 5);
const sentryDedupMaxEntries = 512;
const sentryDedupMaxBytes = 256 * 1024;
const sentryDedupStorageKey = 'sentry_event_dedup_v2';
const sentryDedupFileName = 'sentry-event-dedup-v2.json';

abstract interface class SentryDedupStore {
  Future<bool> claim({
    required String fingerprint,
    required String token,
    required int now,
  });

  Future<void> complete({
    required String fingerprint,
    required String token,
    required int now,
    required bool succeeded,
  });
}

class SentryDedupPendingEntry {
  const SentryDedupPendingEntry(this.token, this.timestamp);

  final String token;
  final int timestamp;
}

/// Shared JSON model used by Dart and the Android/iOS native filters.
/// It contains only SHA-256 signatures, random event IDs and timestamps.
class SentryDedupState {
  SentryDedupState({
    Map<String, int>? sent,
    Map<String, SentryDedupPendingEntry>? pending,
  }) : sent = sent ?? <String, int>{},
       pending = pending ?? <String, SentryDedupPendingEntry>{};

  factory SentryDedupState.decode(
    String? raw, {
    int maxEntries = sentryDedupMaxEntries,
  }) {
    if (raw == null || raw.isEmpty) return SentryDedupState();
    try {
      final decoded = jsonDecode(raw);
      if (decoded is! Map<String, dynamic> || decoded['version'] != 2) {
        return SentryDedupState();
      }
      final state = SentryDedupState();
      final sent = decoded['sent'];
      if (sent is Map<String, dynamic>) {
        for (final entry in sent.entries) {
          if (_isFingerprint(entry.key) && entry.value is int) {
            state.sent[entry.key] = entry.value as int;
          }
        }
      }
      final pending = decoded['pending'];
      if (pending is Map<String, dynamic>) {
        for (final entry in pending.entries) {
          final value = entry.value;
          if (_isFingerprint(entry.key) && value is Map<String, dynamic>) {
            final token = value['token'];
            final timestamp = value['timestamp'];
            if (token is String && token.length <= 128 && timestamp is int) {
              state.pending[entry.key] = SentryDedupPendingEntry(
                token,
                timestamp,
              );
            }
          }
        }
      }
      state.trim(maxEntries);
      return state;
    } catch (_) {
      return SentryDedupState();
    }
  }

  final Map<String, int> sent;
  final Map<String, SentryDedupPendingEntry> pending;

  bool claim({
    required String fingerprint,
    required String token,
    required int now,
    Duration window = sentryDedupWindow,
    Duration pendingWindow = sentryDedupPendingWindow,
    int maxEntries = sentryDedupMaxEntries,
  }) {
    prune(now, window: window, pendingWindow: pendingWindow);
    if (sent.containsKey(fingerprint) || pending.containsKey(fingerprint)) {
      trim(maxEntries);
      return false;
    }
    pending[fingerprint] = SentryDedupPendingEntry(token, now);
    trim(maxEntries);
    return true;
  }

  void complete({
    required String fingerprint,
    required String token,
    required int now,
    required bool succeeded,
    Duration window = sentryDedupWindow,
    Duration pendingWindow = sentryDedupPendingWindow,
    int maxEntries = sentryDedupMaxEntries,
  }) {
    prune(now, window: window, pendingWindow: pendingWindow);
    final claim = pending[fingerprint];
    if (claim?.token != token) return;
    pending.remove(fingerprint);
    if (succeeded) sent[fingerprint] = now;
    trim(maxEntries);
  }

  void prune(
    int now, {
    Duration window = sentryDedupWindow,
    Duration pendingWindow = sentryDedupPendingWindow,
  }) {
    sent.removeWhere(
      (_, timestamp) =>
          now - timestamp >= window.inMilliseconds || timestamp > now,
    );
    pending.removeWhere(
      (_, entry) =>
          now - entry.timestamp >= pendingWindow.inMilliseconds ||
          entry.timestamp > now,
    );
  }

  void trim(int maxEntries) {
    final overflow = sent.length + pending.length - maxEntries;
    if (overflow <= 0) return;
    final oldest = <({String fingerprint, int timestamp, bool pending})>[
      for (final entry in sent.entries)
        (fingerprint: entry.key, timestamp: entry.value, pending: false),
      for (final entry in pending.entries)
        (
          fingerprint: entry.key,
          timestamp: entry.value.timestamp,
          pending: true,
        ),
    ]..sort((left, right) => left.timestamp.compareTo(right.timestamp));
    for (final entry in oldest.take(overflow)) {
      if (entry.pending) {
        pending.remove(entry.fingerprint);
      } else {
        sent.remove(entry.fingerprint);
      }
    }
  }

  String encode() => jsonEncode(<String, Object?>{
    'version': 2,
    'sent': sent,
    'pending': <String, Object?>{
      for (final entry in pending.entries)
        entry.key: <String, Object?>{
          'token': entry.value.token,
          'timestamp': entry.value.timestamp,
        },
    },
  });

  static bool _isFingerprint(String value) =>
      RegExp(r'^[0-9a-f]{64}$').hasMatch(value);
}

/// Safe fallback when platform persistence cannot be opened.
class MemorySentryDedupStore implements SentryDedupStore {
  MemorySentryDedupStore({
    this.window = sentryDedupWindow,
    this.pendingWindow = sentryDedupPendingWindow,
    this.maxEntries = sentryDedupMaxEntries,
  });

  final Duration window;
  final Duration pendingWindow;
  final int maxEntries;
  final SentryDedupState _state = SentryDedupState();
  Future<void> _tail = Future<void>.value();

  @override
  Future<bool> claim({
    required String fingerprint,
    required String token,
    required int now,
  }) {
    return _serialized(
      () async => _state.claim(
        fingerprint: fingerprint,
        token: token,
        now: now,
        window: window,
        pendingWindow: pendingWindow,
        maxEntries: maxEntries,
      ),
    );
  }

  @override
  Future<void> complete({
    required String fingerprint,
    required String token,
    required int now,
    required bool succeeded,
  }) {
    return _serialized(() async {
      _state.complete(
        fingerprint: fingerprint,
        token: token,
        now: now,
        succeeded: succeeded,
        window: window,
        pendingWindow: pendingWindow,
        maxEntries: maxEntries,
      );
    });
  }

  Future<T> _serialized<T>(Future<T> Function() operation) async {
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
