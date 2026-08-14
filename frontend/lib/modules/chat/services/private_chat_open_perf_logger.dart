import 'package:flutter/foundation.dart';
import 'package:sentry_flutter/sentry_flutter.dart';

class PrivateChatOpenPerfLogger {
  const PrivateChatOpenPerfLogger._();

  static const argumentKey = 'private_chat_open_perf';
  static const _prefix = '[PrivateChatOpenPerf]';

  static Map<String, dynamic> start({
    required String peerId,
    required int peerType,
    String source = 'profile_start_chat',
  }) {
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final trace = <String, dynamic>{
      'trace_id': '$nowMs:$peerType:$peerId',
      'started_at_ms': nowMs,
      'last_event_at_ms': nowMs,
      'source': source,
      'peer_id': peerId,
      'peer_type': peerType,
    };
    mark(trace, 'start');
    return trace;
  }

  static Map<String, dynamic> fork(
    Map<String, dynamic>? trace, {
    String sessionId = '',
  }) {
    final next = Map<String, dynamic>.from(trace ?? const <String, dynamic>{});
    if (sessionId.trim().isNotEmpty) {
      next['session_id'] = sessionId.trim();
    }
    return next;
  }

  static void mark(
    Map<String, dynamic>? trace,
    String event, {
    Map<String, Object?> data = const <String, Object?>{},
  }) {
    if (trace == null || trace.isEmpty) {
      return;
    }
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final startedAtMs = _toInt(trace['started_at_ms']);
    final lastEventAtMs = _toInt(trace['last_event_at_ms']);
    final elapsedMs = startedAtMs > 0 ? nowMs - startedAtMs : 0;
    final sincePrevMs = lastEventAtMs > 0 ? nowMs - lastEventAtMs : 0;
    trace['last_event_at_ms'] = nowMs;

    final fields = <String, Object?>{
      'event': event,
      'trace_id': trace['trace_id']?.toString() ?? '',
      'elapsed_ms': elapsedMs,
      'since_prev_ms': sincePrevMs,
      'source': trace['source']?.toString() ?? '',
      'peer_id': trace['peer_id']?.toString() ?? '',
      'peer_type': trace['peer_type'],
      'session_id': trace['session_id']?.toString() ?? '',
      ...data,
    };
    debugPrint('$_prefix ${_formatFields(fields)}');
    Sentry.addBreadcrumb(
      Breadcrumb(
        category: 'private_chat_open_perf',
        message: event,
        data: fields,
        level: SentryLevel.info,
      ),
    );
  }

  static int _toInt(Object? value) {
    if (value is int) return value;
    if (value is num) return value.toInt();
    return int.tryParse(value?.toString() ?? '') ?? 0;
  }

  static String _formatFields(Map<String, Object?> fields) {
    return fields.entries
        .where(
          (entry) => entry.value != null && entry.value.toString().isNotEmpty,
        )
        .map((entry) => '${entry.key}=${entry.value}')
        .join(' ');
  }
}
