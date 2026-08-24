import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/services/sentry_event_dedup_store.dart';
import 'package:grix/shared/services/sentry_event_dedup_store_factory_io.dart';
import 'package:grix/shared/services/sentry_event_deduplicator.dart';
import 'package:sentry_flutter/sentry_flutter.dart';

class _FakeTransport implements Transport {
  _FakeTransport(this.result);

  final SentryId? result;

  @override
  Future<SentryId?> send(SentryEnvelope envelope) async {
    return result;
  }
}

void main() {
  final tempDirectories = <Directory>[];

  Directory tempDirectory() {
    final directory = Directory.systemTemp.createTempSync('grix-sentry-dedup-');
    tempDirectories.add(directory);
    return directory;
  }

  tearDown(() {
    for (final directory in tempDirectories.toList()) {
      directory.deleteSync(recursive: true);
    }
    tempDirectories.clear();
  });

  test(
    'successful transport persists suppression across app restarts',
    () async {
      final directory = tempDirectory();
      var now = DateTime.fromMillisecondsSinceEpoch(1000000);
      final event = SentryEvent(
        release: 'grix@1.2.3',
        exceptions: <SentryException>[
          SentryException(
            type: 'StateError',
            value: 'failed session_id=abc123 at 1710000000000',
            stackTrace: SentryStackTrace(
              frames: <SentryStackFrame>[
                SentryStackFrame(
                  fileName: 'im_service.dart',
                  function: 'load',
                  lineNo: 42,
                ),
              ],
            ),
          ),
        ],
      );
      final first = SentryEventDeduplicator(
        store: FileSentryDedupStore(
          directory: directory,
          window: const Duration(seconds: 1),
        ),
        now: () => now,
      );

      expect(await first.beforeSend(event, Hint()), same(event));
      expect(await first.beforeSend(event, Hint()), isNull);
      await first.recordEnvelopeResult(event.eventId, succeeded: true);

      final afterRestart = SentryEventDeduplicator(
        store: FileSentryDedupStore(
          directory: directory,
          window: const Duration(seconds: 1),
        ),
        now: () => now,
      );
      expect(await afterRestart.beforeSend(event, Hint()), isNull);

      now = now.add(const Duration(seconds: 1));
      expect(await afterRestart.beforeSend(event, Hint()), same(event));
    },
  );

  test(
    'failed transport releases the claim and does not suppress a restart',
    () async {
      final directory = tempDirectory();
      final event = SentryEvent(message: SentryMessage('network send failed'));
      final first = SentryEventDeduplicator(
        store: FileSentryDedupStore(directory: directory),
      );

      expect(await first.beforeSend(event, Hint()), same(event));
      await first.recordEnvelopeResult(event.eventId, succeeded: false);
      expect(await first.beforeSend(event, Hint()), same(event));
      await first.recordEnvelopeResult(event.eventId, succeeded: false);

      final afterRestart = SentryEventDeduplicator(
        store: FileSentryDedupStore(directory: directory),
      );
      expect(await afterRestart.beforeSend(event, Hint()), same(event));
    },
  );

  test(
    'concurrent stores atomically admit one copy and preserve distinct events',
    () async {
      final directory = tempDirectory();
      final first = FileSentryDedupStore(directory: directory);
      final second = FileSentryDedupStore(directory: directory);
      final fingerprint = 'a' * 64;

      final duplicateResults = await Future.wait(<Future<bool>>[
        first.claim(fingerprint: fingerprint, token: 'one', now: 1000),
        second.claim(fingerprint: fingerprint, token: 'two', now: 1000),
      ]);
      expect(duplicateResults.where((result) => result), hasLength(1));

      final distinctResults = await Future.wait(<Future<bool>>[
        first.claim(fingerprint: 'b' * 64, token: 'three', now: 1000),
        second.claim(fingerprint: 'c' * 64, token: 'four', now: 1000),
      ]);
      expect(distinctResults, everyElement(isTrue));
      final raw = File(
        '${directory.path}/$sentryDedupFileName',
      ).readAsStringSync();
      expect(raw, contains('b' * 64));
      expect(raw, contains('c' * 64));
    },
  );

  test(
    'normalizes UUIDv7, ISO timestamps and addresses but preserves semantic codes',
    () {
      final dynamicFirst = buildSentryEventFingerprint(
        SentryEvent(
          message: SentryMessage(
            'at 2026-08-24T10:20:30.123Z id=0198d113-a40a-7a91-8e4f-0123456789ab ptr=0x7ffee1234567',
          ),
        ),
      );
      final dynamicSecond = buildSentryEventFingerprint(
        SentryEvent(
          message: SentryMessage(
            'at 2026-08-24T11:21:31.999Z id=0198d114-b50b-7b92-9f50-fedcba987654 ptr=0x7ffee9999999',
          ),
        ),
      );
      expect(dynamicFirst, dynamicSecond);

      String? fingerprint(String message) => buildSentryEventFingerprint(
        SentryEvent(message: SentryMessage(message)),
      );
      expect(fingerprint('code=10001'), isNot(fingerprint('code=10002')));
      expect(
        fingerprint('HRESULT 0x80070005'),
        isNot(fingerprint('HRESULT 0x80004005')),
      );
      expect(
        fingerprint('message=permission denied'),
        isNot(fingerprint('message=rate limited')),
      );
    },
  );

  test(
    'combines a custom Sentry fingerprint with the actual error identity',
    () {
      final first = buildSentryEventFingerprint(
        SentryEvent(
          fingerprint: <String>['install'],
          message: SentryMessage('permission denied'),
        ),
      );
      final second = buildSentryEventFingerprint(
        SentryEvent(
          fingerprint: <String>['install'],
          message: SentryMessage('network timeout'),
        ),
      );
      expect(first, isNot(second));
    },
  );

  test(
    'oversized or corrupt state recovers without persisting error text',
    () async {
      final directory = tempDirectory();
      final file = File('${directory.path}/$sentryDedupFileName');
      file.writeAsStringSync('secret error text${'x' * 1024}');
      final store = FileSentryDedupStore(directory: directory, maxBytes: 128);
      final event = SentryEvent(
        message: SentryMessage('another private error'),
      );
      final deduplicator = SentryEventDeduplicator(store: store);

      expect(await deduplicator.beforeSend(event, Hint()), same(event));
      await deduplicator.recordEnvelopeResult(event.eventId, succeeded: true);

      final persisted = file.readAsStringSync();
      expect(persisted.length, lessThan(128));
      expect(persisted, isNot(contains('secret error text')));
      expect(persisted, isNot(contains('another private error')));
    },
  );

  test(
    'transport wrapper commits accepted envelopes and releases failures',
    () async {
      final store = MemorySentryDedupStore();
      final event = SentryEvent(message: SentryMessage('boom'));
      final deduplicator = SentryEventDeduplicator(store: store);
      final sdk = SdkVersion(name: 'test', version: '1');
      final envelope = SentryEnvelope.fromEvent(event, sdk);

      expect(await deduplicator.beforeSend(event, Hint()), same(event));
      final success = SentryDeduplicatingTransport(
        _FakeTransport(event.eventId),
        deduplicator,
      );
      expect(await success.send(envelope), event.eventId);
      expect(await deduplicator.beforeSend(event, Hint()), isNull);

      final retryEvent = SentryEvent(message: SentryMessage('retry me'));
      final retryEnvelope = SentryEnvelope.fromEvent(retryEvent, sdk);
      expect(
        await deduplicator.beforeSend(retryEvent, Hint()),
        same(retryEvent),
      );
      final failure = SentryDeduplicatingTransport(
        _FakeTransport(const SentryId.empty()),
        deduplicator,
      );
      expect(await failure.send(retryEnvelope), const SentryId.empty());
      expect(
        await deduplicator.beforeSend(retryEvent, Hint()),
        same(retryEvent),
      );
    },
  );

  test(
    'valid oversized entry sets are trimmed when a duplicate returns early',
    () async {
      final directory = tempDirectory();
      final event = SentryEvent(message: SentryMessage('already sent'));
      final fingerprint = buildSentryEventFingerprint(event)!;
      final sent = <String, int>{
        for (var index = 0; index < 19; index++)
          index.toRadixString(16).padLeft(64, '0'): index,
        fingerprint: 20,
      };
      final file = File('${directory.path}/$sentryDedupFileName');
      file.writeAsStringSync(
        SentryDedupState(sent: sent).encode(),
        flush: true,
      );
      final deduplicator = SentryEventDeduplicator(
        store: FileSentryDedupStore(
          directory: directory,
          maxEntries: 4,
          window: const Duration(seconds: 100),
        ),
        now: () => DateTime.fromMillisecondsSinceEpoch(20),
      );

      expect(await deduplicator.beforeSend(event, Hint()), isNull);
      final repaired = SentryDedupState.decode(
        file.readAsStringSync(),
        maxEntries: 100,
      );
      expect(repaired.sent.length + repaired.pending.length, 4);
    },
  );

  test('does not suppress events without identifying error content', () async {
    final deduplicator = SentryEventDeduplicator(
      store: MemorySentryDedupStore(),
    );
    final event = SentryEvent(level: SentryLevel.error);

    expect(await deduplicator.beforeSend(event, Hint()), same(event));
    expect(await deduplicator.beforeSend(event, Hint()), same(event));
  });
}
