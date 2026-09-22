part of 'im_service.dart';

extension _ImServiceMessageSync on ImService {
  /// Fetch remote history and write it into LocalDb.
  ///
  /// This is the history backfill boundary: callers may use the result to
  /// decide whether to reread LocalDb, but remote rows must not be rendered
  /// directly.
  Future<_RemoteHistorySyncResult?> _syncSessionHistoryBackfill({
    required String sessionId,
    String? beforeMsgId,
    required int limit,
    bool emitBusEvent = true,
  }) async {
    final sid = sessionId.trim();
    if (sid.isEmpty || limit <= 0) {
      return null;
    }

    final normalizedBefore = beforeMsgId?.trim() ?? '';
    final flightKey = '$sid|$normalizedBefore|$limit|$emitBusEvent';
    final existing = _historySyncInFlight[flightKey];
    if (existing != null) {
      return existing;
    }

    late final Future<_RemoteHistorySyncResult?> flight;
    flight =
        _performSessionHistoryBackfill(
          sessionId: sid,
          beforeMsgId: normalizedBefore,
          limit: limit,
          emitBusEvent: emitBusEvent,
        ).whenComplete(() {
          if (identical(_historySyncInFlight[flightKey], flight)) {
            _historySyncInFlight.remove(flightKey);
          }
        });
    _historySyncInFlight[flightKey] = flight;
    return flight;
  }

  Future<_RemoteHistorySyncResult?> _performSessionHistoryBackfill({
    required String sessionId,
    required String beforeMsgId,
    required int limit,
    required bool emitBusEvent,
  }) async {
    final sid = sessionId;

    final sessionService = _sessionServiceOrNull();
    if (sessionService == null) {
      return null;
    }

    var pagingBeforeMsgId = beforeMsgId;
    for (var i = 0; i < ImService._maxRemoteHistoryEmptyPageSkips; i++) {
      final result = await sessionService.fetchMessageHistoryResult(
        sessionId: sid,
        beforeMsgId: pagingBeforeMsgId,
        limit: limit,
      );
      if (!result.success) {
        return const _RemoteHistorySyncResult(
          hasMore: true,
          requestFailed: true,
        );
      }

      if (result.messages.isNotEmpty) {
        final writeResult = await LocalDb.applyArchiveMessages(result.messages);
        if (!writeResult.persisted) {
          return const _RemoteHistorySyncResult(
            hasMore: true,
            requestFailed: true,
          );
        }
        if (emitBusEvent && writeResult.hasChanges) {
          _emitBackfilledMessages(sid, writeResult.changedRows);
          for (final msgId in writeResult.deletedMessageIds) {
            LocalDbChangeBus.instance.emitMessageChange(
              LocalMessageRevoked(sessionId: sid, msgId: msgId),
            );
            if (_isCurrentSession(sid)) {
              removeMessageFromCurrentSession(msgId);
            }
          }
        }
        return _RemoteHistorySyncResult(hasMore: result.hasMore);
      }

      if (!result.hasMore) {
        return const _RemoteHistorySyncResult(hasMore: false);
      }

      final nextBeforeMsgId = result.nextBeforeMsgId.trim();
      if (nextBeforeMsgId.isEmpty || nextBeforeMsgId == pagingBeforeMsgId) {
        return const _RemoteHistorySyncResult(hasMore: true);
      }
      pagingBeforeMsgId = nextBeforeMsgId;
    }

    return const _RemoteHistorySyncResult(hasMore: true);
  }

  void _emitBackfilledMessages(
    String sessionId,
    List<Map<String, dynamic>> rows,
  ) {
    final ids = rows
        .map((r) => r['msg_id']?.toString().trim() ?? '')
        .where((id) => id.isNotEmpty)
        .toList();
    if (ids.isEmpty) return;

    final maxTs = rows
        .map((r) => _normalizeMessageCreatedAt(_toInt(r['created_at'])))
        .fold<int>(0, (a, b) => a > b ? a : b);
    LocalDbChangeBus.instance.emitMessageChange(
      LocalMessagesInserted(
        sessionId: sessionId,
        msgIds: ids,
        maxCreatedAt: maxTs,
        rows: rows,
      ),
    );
  }
}
