import 'dart:async';
import 'dart:io';

import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import 'sentry_event_dedup_store.dart';

SentryDedupStore createSentryDedupStore() => LazyFileSentryDedupStore();

class LazyFileSentryDedupStore implements SentryDedupStore {
  Future<SentryDedupStore>? _delegate;

  Future<SentryDedupStore> _getDelegate() => _delegate ??= _create();

  Future<SentryDedupStore> _create() async {
    try {
      return FileSentryDedupStore(
        directory: await getApplicationSupportDirectory(),
      );
    } catch (_) {
      return MemorySentryDedupStore();
    }
  }

  @override
  Future<bool> claim({
    required String fingerprint,
    required String token,
    required int now,
  }) async {
    return (await _getDelegate()).claim(
      fingerprint: fingerprint,
      token: token,
      now: now,
    );
  }

  @override
  Future<void> complete({
    required String fingerprint,
    required String token,
    required int now,
    required bool succeeded,
  }) async {
    await (await _getDelegate()).complete(
      fingerprint: fingerprint,
      token: token,
      now: now,
      succeeded: succeeded,
    );
  }
}

class FileSentryDedupStore implements SentryDedupStore {
  FileSentryDedupStore({
    required Directory directory,
    this.window = sentryDedupWindow,
    this.pendingWindow = sentryDedupPendingWindow,
    this.maxEntries = sentryDedupMaxEntries,
    this.maxBytes = sentryDedupMaxBytes,
  }) : _file = File(p.join(directory.path, sentryDedupFileName)),
       _lockFile = File(p.join(directory.path, '$sentryDedupFileName.lock'));

  static Future<void> _tail = Future<void>.value();
  final File _file;
  final File _lockFile;
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
        return await _withLock((state) async {
          final claimed = state.claim(
            fingerprint: fingerprint,
            token: token,
            now: now,
            window: window,
            pendingWindow: pendingWindow,
            maxEntries: maxEntries,
          );
          await _writeAtomically(state);
          return claimed;
        });
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
        await _withLock((state) async {
          state.complete(
            fingerprint: fingerprint,
            token: token,
            now: now,
            succeeded: succeeded,
            window: window,
            pendingWindow: pendingWindow,
            maxEntries: maxEntries,
          );
          await _writeAtomically(state);
        });
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

  Future<T> _withLock<T>(
    Future<T> Function(SentryDedupState state) operation,
  ) async {
    await _lockFile.parent.create(recursive: true);
    final lock = await _lockFile.open(mode: FileMode.append);
    try {
      await lock.lock(FileLock.exclusive);
      String? raw;
      try {
        if (await _file.exists() && await _file.length() <= maxBytes) {
          raw = await _file.readAsString();
        }
      } catch (_) {
        raw = null;
      }
      return await operation(
        SentryDedupState.decode(raw, maxEntries: maxEntries),
      );
    } finally {
      try {
        await lock.unlock();
      } finally {
        await lock.close();
      }
    }
  }

  Future<void> _writeAtomically(SentryDedupState state) async {
    final temporary = File(
      '${_file.path}.$pid.${DateTime.now().microsecondsSinceEpoch}.tmp',
    );
    try {
      await temporary.writeAsString(state.encode(), flush: true);
      await temporary.rename(_file.path);
    } finally {
      if (await temporary.exists()) await temporary.delete();
    }
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
