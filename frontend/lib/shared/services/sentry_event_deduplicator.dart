import 'dart:convert';

import 'package:crypto/crypto.dart';
import 'package:sentry_flutter/sentry_flutter.dart';

import 'sentry_event_dedup_store.dart';
import 'sentry_event_dedup_store_factory.dart';

String _normalizeVolatileValues(String value) {
  var normalized = value.replaceAllMapped(
    RegExp(
      r'\b(session(?:_id)?|sid|message_id|mid|trace(?:_id)?|request(?:_id)?|rid|event(?:_id)?)\s*[=:]\s*[^\s,;]+',
      caseSensitive: false,
    ),
    (match) => '${match.group(1)}=<id>',
  );
  normalized = normalized
      .replaceAll(
        RegExp(
          r'\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b',
          caseSensitive: false,
        ),
        '<uuid>',
      )
      .replaceAll(
        RegExp(
          r'\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})\b',
          caseSensitive: false,
        ),
        '<timestamp>',
      )
      .replaceAll(
        RegExp(r'\b0x[0-9a-f]{12,}\b', caseSensitive: false),
        '<address>',
      )
      .replaceAll(RegExp(r'\b[0-9a-f]{16,}\b', caseSensitive: false), '<id>')
      .replaceAll(RegExp(r'\b\d{13}\b'), '<timestamp>');
  return normalized;
}

/// Builds a stable signature without event IDs, breadcrumbs, or user data.
String? buildSentryEventFingerprint(SentryEvent event) {
  Object? eventIdentity;
  final explicitFingerprint = event.fingerprint;
  final exceptions = event.exceptions;

  if (exceptions != null && exceptions.isNotEmpty) {
    eventIdentity = <String, Object?>{
      'exceptions': exceptions
          .map((exception) {
            final frames =
                exception.stackTrace?.frames ?? const <SentryStackFrame>[];
            final firstFrame = frames.length > 8 ? frames.length - 8 : 0;
            return <String, Object?>{
              'type': exception.type ?? '',
              'value': _normalizeVolatileValues(exception.value ?? ''),
              'frames': frames
                  .skip(firstFrame)
                  .map(
                    (frame) => <String, Object?>{
                      'file': frame.fileName ?? '',
                      'function': frame.function ?? '',
                      'line': frame.lineNo ?? 0,
                      'column': frame.colNo ?? 0,
                    },
                  )
                  .toList(growable: false),
            };
          })
          .toList(growable: false),
    };
  } else if (event.message != null) {
    eventIdentity = <String, Object?>{
      'message': _normalizeVolatileValues(event.message!.formatted),
    };
  } else if (event.throwable != null) {
    eventIdentity = <String, Object?>{
      'throwableType': event.throwable.runtimeType.toString(),
      'throwable': _normalizeVolatileValues(event.throwable.toString()),
    };
  } else if (explicitFingerprint == null || explicitFingerprint.isEmpty) {
    return null;
  }

  final source = jsonEncode(<String, Object?>{
    'release': event.release ?? '',
    'platform': event.platform ?? '',
    'logger': event.logger ?? '',
    'level': event.level?.name ?? '',
    'fingerprint':
        explicitFingerprint
            ?.map(_normalizeVolatileValues)
            .toList(growable: false) ??
        const <String>[],
    'identity': eventIdentity,
  });
  return sha256.convert(utf8.encode(source)).toString();
}

class _PendingEventClaim {
  const _PendingEventClaim(this.fingerprint, this.token);

  final String fingerprint;
  final String token;
}

/// Device-local suppression for Dart/Flutter events that survives app restarts.
/// Persistence is lazily opened on the first event, so it does not delay runApp.
class SentryEventDeduplicator {
  SentryEventDeduplicator({SentryDedupStore? store, DateTime Function()? now})
    : _store = store ?? createSentryDedupStore(),
      _now = now ?? DateTime.now;

  factory SentryEventDeduplicator.create() => SentryEventDeduplicator();

  final SentryDedupStore _store;
  final DateTime Function() _now;
  final Map<String, _PendingEventClaim> _claimsByEventId =
      <String, _PendingEventClaim>{};

  Future<SentryEvent?> beforeSend(SentryEvent event, Hint hint) async {
    final fingerprint = buildSentryEventFingerprint(event);
    if (fingerprint == null) return event;
    final token = event.eventId.toString();
    final claimed = await _store.claim(
      fingerprint: fingerprint,
      token: token,
      now: _now().millisecondsSinceEpoch,
    );
    if (!claimed) return null;
    _claimsByEventId[token] = _PendingEventClaim(fingerprint, token);
    return event;
  }

  Future<void> recordEnvelopeResult(
    SentryId? eventId, {
    required bool succeeded,
  }) async {
    if (eventId == null) return;
    final claim = _claimsByEventId.remove(eventId.toString());
    if (claim == null) return;
    await _store.complete(
      fingerprint: claim.fingerprint,
      token: claim.token,
      now: _now().millisecondsSinceEpoch,
      succeeded: succeeded,
    );
  }
}

class SentryDeduplicatingTransport implements Transport {
  SentryDeduplicatingTransport(this._delegate, this._deduplicator);

  final Transport _delegate;
  final SentryEventDeduplicator _deduplicator;

  @override
  Future<SentryId?> send(SentryEnvelope envelope) async {
    final eventId = envelope.header.eventId;
    try {
      final result = await _delegate.send(envelope);
      final succeeded = result != null && result != const SentryId.empty();
      await _deduplicator.recordEnvelopeResult(eventId, succeeded: succeeded);
      return result;
    } catch (_) {
      await _deduplicator.recordEnvelopeResult(eventId, succeeded: false);
      rethrow;
    }
  }
}
