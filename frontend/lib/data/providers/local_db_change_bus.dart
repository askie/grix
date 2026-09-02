import 'dart:async';

/// Events emitted when local DB message data changes.
/// All events are emitted *after* a successful DB write.
sealed class LocalMessageChange {
  /// The session this change belongs to.
  String get sessionId;
}

/// One or more messages were inserted into the local DB.
class LocalMessagesInserted extends LocalMessageChange {
  @override
  final String sessionId;
  final List<String> msgIds;
  final int maxCreatedAt;

  /// Pre-computed message rows for synchronous consumption by the subscriber.
  /// Avoids a second DB round-trip. Null if the event was emitted from a code
  /// path that doesn't have the rows readily available.
  final List<Map<String, dynamic>>? rows;

  LocalMessagesInserted({
    required this.sessionId,
    required this.msgIds,
    required this.maxCreatedAt,
    this.rows,
  });

  @override
  String toString() =>
      'LocalMessagesInserted(sid=$sessionId, count=${msgIds.length}, maxTs=$maxCreatedAt)';
}

/// An existing message was updated (edit, ack status change, etc.).
class LocalMessageUpdated extends LocalMessageChange {
  @override
  final String sessionId;
  final String msgId;

  /// Pre-computed message row for synchronous consumption by the subscriber.
  /// Null if not available at emit time.
  final Map<String, dynamic>? row;

  /// For send_ack: the clientMsgId of the local stub being acked.
  /// Allows the subscriber to find the window entry before the msgId mapping
  /// is established.
  final String? clientMsgId;

  /// Server-assigned createdAt for send_ack reordering.
  final int? ackCreatedAt;

  LocalMessageUpdated({
    required this.sessionId,
    required this.msgId,
    this.row,
    this.clientMsgId,
    this.ackCreatedAt,
  });

  @override
  String toString() => 'LocalMessageUpdated(sid=$sessionId, msgId=$msgId)';
}

/// A message was revoked / deleted.
class LocalMessageRevoked extends LocalMessageChange {
  @override
  final String sessionId;
  final String msgId;

  LocalMessageRevoked({required this.sessionId, required this.msgId});

  @override
  String toString() => 'LocalMessageRevoked(sid=$sessionId, msgId=$msgId)';
}

/// A session-level change (last message preview, unread count, etc.).
class LocalSessionChanged {
  final String sessionId;

  LocalSessionChanged({required this.sessionId});

  @override
  String toString() => 'LocalSessionChanged(sid=$sessionId)';
}

/// Centralized event bus for local DB changes.
///
/// Downstream handlers (push_msg, pull_sync_resp, send_ack, etc.) emit events
/// here after a successful DB write. UI layers subscribe to react to changes
/// without coupling to specific handler code paths.
class LocalDbChangeBus {
  LocalDbChangeBus._();
  static final LocalDbChangeBus _instance = LocalDbChangeBus._();

  /// Global singleton — accessible from both LocalDb write paths and ImService.
  static LocalDbChangeBus get instance => _instance;

  /// Sync delivery guarantees ordering — events are dispatched inside the
  /// emit call, so the subscriber processes them before the emitter continues.
  /// This prevents race conditions between insert and update events (e.g.
  /// pull_sync insert followed by edit), and ensures the subscriber never
  /// misses an event emitted between two synchronous operations.
  final _messageController = StreamController<LocalMessageChange>.broadcast(
    sync: true,
  );
  final _sessionController = StreamController<LocalSessionChanged>.broadcast(
    sync: true,
  );

  /// Stream of all message-level changes (insert, update, revoke).
  Stream<LocalMessageChange> get messageChanges => _messageController.stream;

  /// Stream of all session-level changes.
  Stream<LocalSessionChanged> get sessionChanges => _sessionController.stream;

  /// Emit a message change event. Called after a successful DB write.
  void emitMessageChange(LocalMessageChange change) {
    if (!_messageController.isClosed) {
      _messageController.add(change);
    }
  }

  /// Emit a session change event. Called after a successful DB write.
  void emitSessionChange(LocalSessionChanged change) {
    if (!_sessionController.isClosed) {
      _sessionController.add(change);
    }
  }

  /// Dispose all streams. Should only be called during test teardown or app
  /// shutdown — NOT during account switch (use _cancelDbChangeSubscription
  /// instead to unsubscribe without closing the bus).
  void dispose() {
    _messageController.close();
    _sessionController.close();
  }
}
