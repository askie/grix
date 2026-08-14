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

    final sessionService = _sessionServiceOrNull();
    if (sessionService == null) {
      return null;
    }

    var pagingBeforeMsgId = beforeMsgId?.trim();
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
        await LocalDb.batchInsertMessages(result.messages);
        if (emitBusEvent) {
          _emitBackfilledMessages(sid, result.messages);
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
