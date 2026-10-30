import 'package:flutter/foundation.dart';
import 'package:flutter/scheduler.dart';
import 'package:sentry_flutter/sentry_flutter.dart';

class PrivateChatOpenPerfLogger {
  const PrivateChatOpenPerfLogger._();

  static const argumentKey = 'private_chat_open_perf';
  static const _prefix = '[PrivateChatOpenPerf]';
  static const _conversationListSource = 'conversation_list';
  static final _conversationFrames = <String, _ConversationOpenFrameWatch>{};

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

  /// Conversation-list tap → chat. Debug and profile only; release returns an
  /// empty map so route code skips the trace entirely.
  static Map<String, dynamic> startConversationListOpen({
    required String sessionId,
    required String type,
  }) {
    if (kReleaseMode) return const <String, dynamic>{};
    final sid = sessionId.trim();
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final trace = <String, dynamic>{
      'trace_id': '$nowMs:$_conversationListSource:$sid',
      'started_at_ms': nowMs,
      'last_event_at_ms': nowMs,
      'source': _conversationListSource,
      'session_id': sid,
      'peer_id': '',
      'peer_type': 0,
      'route_type': type.trim(),
    };
    _watchConversationFrames(trace);
    markIfConversationList(trace, 'tap');
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
    final fields = _stamp(trace, event, data);
    if (fields == null) return;
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

  /// List-open milestones. No-op in release and for other traces. Does not
  /// send Sentry breadcrumbs; frame timings are local logs only.
  static void markIfConversationList(
    Map<String, dynamic>? trace,
    String event, {
    Map<String, Object?> data = const <String, Object?>{},
  }) {
    if (kReleaseMode) return;
    if (trace == null || trace.isEmpty) return;
    if (trace['source']?.toString() != _conversationListSource) return;
    final fields = _stamp(trace, event, data);
    if (fields == null) return;
    debugPrint('$_prefix ${_formatFields(fields)}');
    if (event == 'transition_settled') {
      _conversationFrames[trace['trace_id']?.toString() ?? '']?.markSettled();
    }
  }

  static Map<String, Object?>? _stamp(
    Map<String, dynamic>? trace,
    String event,
    Map<String, Object?> data,
  ) {
    if (trace == null || trace.isEmpty) return null;
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final startedAtMs = _toInt(trace['started_at_ms']);
    final lastEventAtMs = _toInt(trace['last_event_at_ms']);
    final elapsedMs = startedAtMs > 0 ? nowMs - startedAtMs : 0;
    final sincePrevMs = lastEventAtMs > 0 ? nowMs - lastEventAtMs : 0;
    trace['last_event_at_ms'] = nowMs;
    return <String, Object?>{
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
  }

  static void _watchConversationFrames(Map<String, dynamic> trace) {
    final traceId = trace['trace_id']?.toString() ?? '';
    if (traceId.isEmpty) return;
    _stopConversationFrames(traceId);
    final watch = _ConversationOpenFrameWatch(
      traceId,
      _toInt(trace['started_at_ms']),
    );
    _conversationFrames[traceId] = watch;
    SchedulerBinding.instance.addTimingsCallback(watch.onTimings);
  }

  static void _stopConversationFrames(String traceId) {
    final watch = _conversationFrames.remove(traceId);
    if (watch == null) return;
    SchedulerBinding.instance.removeTimingsCallback(watch.onTimings);
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

class _ConversationOpenFrameWatch {
  _ConversationOpenFrameWatch(this.traceId, this.startedAtMs);

  final String traceId;
  final int startedAtMs;
  var settled = false;
  var settledBatches = 0;
  var _stopScheduled = false;

  late final TimingsCallback onTimings = _handleTimings;

  void _handleTimings(List<FrameTiming> timings) {
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final elapsedMs = startedAtMs > 0 ? nowMs - startedAtMs : 0;
    for (final timing in timings) {
      final buildMs = timing.buildDuration.inMicroseconds / 1000;
      final rasterMs = timing.rasterDuration.inMicroseconds / 1000;
      final totalMs = timing.totalSpan.inMicroseconds / 1000;
      debugPrint(
        '[ChatOpenFrame] trace_id=$traceId elapsed_ms=$elapsedMs '
        'phase=${settled ? 'after_settle' : 'transition'} '
        'build_ms=${buildMs.toStringAsFixed(2)} '
        'raster_ms=${rasterMs.toStringAsFixed(2)} '
        'total_ms=${totalMs.toStringAsFixed(2)} '
        'ui_over_16ms=${buildMs > 16}',
      );
    }
    if (!settled || timings.isEmpty) return;
    settledBatches += 1;
    if (settledBatches >= 2) {
      PrivateChatOpenPerfLogger._stopConversationFrames(traceId);
    }
  }

  void markSettled() {
    if (settled) return;
    settled = true;
    if (_stopScheduled) return;
    _stopScheduled = true;
    // Keep two frames after settle so the completing frame's timings, which
    // Flutter reports one frame late, are still logged. The next batch is
    // tagged after_settle and is not part of the transition budget.
    SchedulerBinding.instance.addPostFrameCallback((_) {
      SchedulerBinding.instance.addPostFrameCallback((_) {
        PrivateChatOpenPerfLogger._stopConversationFrames(traceId);
      });
    });
    SchedulerBinding.instance.scheduleFrame();
  }
}
