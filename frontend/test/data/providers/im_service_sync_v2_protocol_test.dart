import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';

  @override
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero}) => true;

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async => TokenRefreshStatus.ready;
}

class _RecordingSessionService extends SessionService {
  int snapshotFetches = 0;
  int snapshotSyncHeadCursor = 0;
  bool rejectMuteAsTerminal = false;
  final List<String> muteCommandIds = [];

  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    snapshotFetches++;
    return SessionSnapshotFetchResult(
      snapshots: const [],
      success: true,
      cursor: 1700000000,
      syncHeadCursor: snapshotSyncHeadCursor,
    );
  }

  @override
  Future<SessionSnapshotFetchResult> fetchSyncV2BootstrapSnapshotsResult({
    int limit = 10000,
  }) => fetchSessionSnapshotsResult(limit: limit, maxPages: 1);

  @override
  Future<SessionMuteResult> setSessionMutedResult(
    String sessionId, {
    required bool isMuted,
    String? commandId,
  }) async {
    if (commandId != null) muteCommandIds.add(commandId);
    if (rejectMuteAsTerminal) {
      return SessionMuteResult(
        sessionId: sessionId,
        isMuted: isMuted,
        code: 4003,
        httpStatus: 403,
      );
    }
    return SessionMuteResult(sessionId: sessionId, isMuted: isMuted, code: 0);
  }
}

class _RecordingSink implements WebSocketSink {
  final List<Map<String, dynamic>> packets = [];

  @override
  void add(dynamic data) {
    packets.add(jsonDecode(data as String) as Map<String, dynamic>);
  }

  @override
  Future<void> close([int? closeCode, String? closeReason]) async {}

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _FakeWebSocketChannel implements WebSocketChannel {
  _FakeWebSocketChannel({required Stream<dynamic> stream, required this.sink})
    : _stream = stream;

  final Stream<dynamic> _stream;

  @override
  final WebSocketSink sink;

  @override
  Future<void> get ready => Future<void>.value();

  @override
  Stream<dynamic> get stream => _stream;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

Future<void> _eventually(bool Function() condition, {String? reason}) async {
  final deadline = DateTime.now().add(const Duration(seconds: 5));
  while (DateTime.now().isBefore(deadline)) {
    if (condition()) return;
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  expect(condition(), isTrue, reason: reason);
}

Future<void> _eventuallyAsync(
  Future<bool> Function() condition, {
  String? reason,
}) async {
  final deadline = DateTime.now().add(const Duration(seconds: 5));
  while (DateTime.now().isBefore(deadline)) {
    if (await condition()) return;
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  expect(await condition(), isTrue, reason: reason);
}

/// A v2 connection that has sent sync_resume, with bootstrap already marked
/// complete so pre-seeded local sessions survive.
class _V2Harness {
  _V2Harness._(this.service, this.sink, this.downstream, this.generation);

  final ImService service;
  final _RecordingSink sink;
  final StreamController<dynamic> downstream;
  final String generation;

  static Future<_V2Harness> resume() async {
    await LocalDb.prepareSyncGeneration('pre-bootstrap');
    await LocalDb.markSyncBootstrapComplete(1, committedCursor: 0);
    final sink = _RecordingSink();
    final downstream = StreamController<dynamic>();
    ImService.channelConnectorForTest = (_) =>
        _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
    downstream.add(
      jsonEncode({
        'cmd': 'auth_ack',
        'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
      }),
    );
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'sync_resume'));
    final generation = sink.packets
        .firstWhere((p) => p['cmd'] == 'sync_resume')['payload']['generation']
        .toString();
    return _V2Harness._(service, sink, downstream, generation);
  }

  int get acks => sink.packets.where((p) => p['cmd'] == 'sync_ack').length;

  Future<void> sendBatch({
    required int from,
    required int next,
    required List<Map<String, dynamic>> events,
    Map<String, int>? unreadSnapshot,
  }) async {
    final acksBefore = acks;
    downstream.add(
      jsonEncode({
        'cmd': 'sync_batch',
        'payload': {
          'generation': generation,
          'from_cursor': '$from',
          'next_cursor': '$next',
          'head_cursor': '$next',
          'has_more': false,
          'events': events,
          if (unreadSnapshot != null)
            'final_state_snapshot': {'unread_by_session': unreadSnapshot},
        },
      }),
    );
    await _eventually(() => acks == acksBefore + 1);
  }

  Future<void> close() async {
    service.disconnect();
    await downstream.close();
  }
}

Future<List<Map<String, Object?>>> _outboxRows(String commandId) async {
  final db = await LocalDb.database;
  return db.query('outbox', where: 'command_id = ?', whereArgs: [commandId]);
}

Future<String?> _outboxState(String commandId) async {
  final rows = await _outboxRows(commandId);
  return rows.isEmpty ? null : rows.single['state']?.toString();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late String userId;
  late _RecordingSessionService sessionService;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    userId = 'sync_v2_protocol_${DateTime.now().microsecondsSinceEpoch}';
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(userId);
    Get.put<AuthService>(_FakeAuthService());
    sessionService = _RecordingSessionService();
    Get.put<SessionService>(sessionService);
  });

  tearDown(() async {
    ImService.channelConnectorForTest = null;
    Get.reset();
    await LocalDb.setActiveUser(null);
  });

  test(
    'v2 negotiation resumes once and ACKs only after durable commit',
    () async {
      final sink = _RecordingSink();
      final downstream = StreamController<dynamic>();
      ImService.channelConnectorForTest = (_) =>
          _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
      final service = ImService();
      service.connect('ws://127.0.0.1:1/ws');

      await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
      final auth = sink.packets.firstWhere((p) => p['cmd'] == 'auth');
      expect(auth['payload']['capabilities'], contains('sync_v2'));

      downstream.add(
        jsonEncode({
          'cmd': 'auth_ack',
          'payload': {
            'code': 0,
            'user_id': '1001',
            'active_sync': 'v2',
            'capabilities': ['sync_v2'],
          },
        }),
      );
      await _eventually(
        () => sink.packets.any((p) => p['cmd'] == 'sync_resume'),
      );
      final resumes = sink.packets.where((p) => p['cmd'] == 'sync_resume');
      expect(resumes, hasLength(1));
      final generation = resumes.single['payload']['generation'].toString();
      expect(resumes.single['payload']['committed_cursor'], '0');
      expect(sink.packets.where((p) => p['cmd'] == 'pull_sync'), isEmpty);
      expect(sessionService.snapshotFetches, 1);

      downstream.add(
        jsonEncode({
          'cmd': 'sync_batch',
          'payload': {
            'generation': generation,
            'from_cursor': '0',
            'next_cursor': '1',
            'head_cursor': '1',
            'has_more': false,
            'events': [
              {
                'cursor': '1',
                'kind': 'message.upsert',
                'entity_type': 'message',
                'entity_id': '501',
                'entity_version': '1',
                'payload': {
                  'msg_id': '501',
                  'session_id': 'session-5',
                  'sender_id': '1002',
                  'sender_type': 1,
                  'msg_type': 1,
                  'content': 'committed before ack',
                  'created_at': 1700000000000,
                },
              },
            ],
            'final_state_snapshot': {
              'unread_by_session': {'session-5': 1},
            },
          },
        }),
      );
      await _eventually(() => sink.packets.any((p) => p['cmd'] == 'sync_ack'));

      final state = await LocalDb.getSyncState();
      final messages = await LocalDb.getLatestMessages('session-5');
      final ack = sink.packets.firstWhere((p) => p['cmd'] == 'sync_ack');
      expect(state.committedCursor, 1);
      expect(messages.single['content'], 'committed before ack');
      expect(ack['payload']['generation'], generation);
      expect(ack['payload']['committed_cursor'], '1');

      service.disconnect();
      await downstream.close();
    },
  );

  test(
    'v2 catch-up publishes the session projection once on the final batch',
    () async {
      final sink = _RecordingSink();
      final downstream = StreamController<dynamic>();
      ImService.channelConnectorForTest = (_) =>
          _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
      final service = ImService();
      service.connect('ws://127.0.0.1:1/ws');

      await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
      downstream.add(
        jsonEncode({
          'cmd': 'auth_ack',
          'payload': {
            'code': 0,
            'user_id': '1001',
            'active_sync': 'v2',
            'capabilities': ['sync_v2'],
          },
        }),
      );
      await _eventually(
        () => sink.packets.any((p) => p['cmd'] == 'sync_resume'),
      );
      final generation = sink.packets
          .firstWhere((p) => p['cmd'] == 'sync_resume')['payload']['generation']
          .toString();
      final loadTickBefore = service.sessionsLoadTick.value;

      Map<String, dynamic> sessionEvent(int cursor, String sid) => {
        'cursor': '$cursor',
        'kind': 'session.upsert',
        'entity_type': 'session',
        'entity_id': sid,
        'entity_version': '1',
        'payload': {
          'session_id': sid,
          'session_type': 1,
          'updated_at': 1700000000000 + cursor,
        },
      };
      Map<String, dynamic> unreadEvent(int cursor, String sid, int unread) => {
        'cursor': '$cursor',
        'kind': 'session.unread_set',
        'entity_type': 'session_member',
        'entity_id': sid,
        'entity_version': '$cursor',
        'payload': {'session_id': sid, 'unread_count': unread},
      };

      // Batch 1/3: history replay drives session-a to unread=3.
      downstream.add(
        jsonEncode({
          'cmd': 'sync_batch',
          'payload': {
            'generation': generation,
            'from_cursor': '0',
            'next_cursor': '2',
            'head_cursor': '6',
            'has_more': true,
            'events': [
              sessionEvent(1, 'session-a'),
              unreadEvent(2, 'session-a', 3),
            ],
          },
        }),
      );
      await _eventually(
        () => sink.packets.where((p) => p['cmd'] == 'sync_ack').length == 1,
      );
      // Batch 2/3: a read on another device drops it to 0 in history.
      downstream.add(
        jsonEncode({
          'cmd': 'sync_batch',
          'payload': {
            'generation': generation,
            'from_cursor': '2',
            'next_cursor': '4',
            'head_cursor': '6',
            'has_more': true,
            'events': [
              unreadEvent(3, 'session-a', 0),
              sessionEvent(4, 'session-b'),
            ],
          },
        }),
      );
      await _eventually(
        () => sink.packets.where((p) => p['cmd'] == 'sync_ack').length == 2,
      );
      // Intermediate batches are durable but never republish the projection:
      // the tab badge must not walk through 3 -> 0 -> 5 during catch-up.
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(service.sessionsLoadTick.value, loadTickBefore);
      expect(
        service.sessions.where((s) => s.sessionId == 'session-a'),
        isEmpty,
      );
      expect(service.notificationUnread, 0);

      // Final batch carries the authoritative snapshot.
      downstream.add(
        jsonEncode({
          'cmd': 'sync_batch',
          'payload': {
            'generation': generation,
            'from_cursor': '4',
            'next_cursor': '6',
            'head_cursor': '6',
            'has_more': false,
            'events': [
              unreadEvent(5, 'session-a', 5),
              unreadEvent(6, 'session-b', 2),
            ],
            'final_state_snapshot': {
              'unread_by_session': {'session-a': 5, 'session-b': 2},
            },
          },
        }),
      );
      await _eventually(
        () => sink.packets.where((p) => p['cmd'] == 'sync_ack').length == 3,
      );
      await _eventually(
        () => service.sessionsLoadTick.value == loadTickBefore + 1,
      );
      expect(service.notificationUnread, 7);
      expect(
        service.sessions.map((s) => s.sessionId),
        containsAll(['session-a', 'session-b']),
      );
      // Settled: nothing else republishes after the final batch.
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(service.sessionsLoadTick.value, loadTickBefore + 1);

      service.disconnect();
      await downstream.close();
    },
  );

  test('first v2 resume starts at the snapshot event head', () async {
    sessionService.snapshotSyncHeadCursor = 42;
    final sink = _RecordingSink();
    final downstream = StreamController<dynamic>();
    ImService.channelConnectorForTest = (_) =>
        _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
    downstream.add(
      jsonEncode({
        'cmd': 'auth_ack',
        'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
      }),
    );
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'sync_resume'));

    final resume = sink.packets.firstWhere((p) => p['cmd'] == 'sync_resume');
    expect(resume['payload']['committed_cursor'], '42');
    expect((await LocalDb.getSyncState()).committedCursor, 42);

    service.disconnect();
    await downstream.close();
  });

  test(
    'cursor mismatch disconnects without ACK for deterministic replay',
    () async {
      final sink = _RecordingSink();
      final downstream = StreamController<dynamic>();
      ImService.channelConnectorForTest = (_) =>
          _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
      final service = ImService();
      service.connect('ws://127.0.0.1:1/ws');
      await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
      downstream.add(
        jsonEncode({
          'cmd': 'auth_ack',
          'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
        }),
      );
      await _eventually(
        () => sink.packets.any((p) => p['cmd'] == 'sync_resume'),
      );
      final generation = sink.packets
          .firstWhere((p) => p['cmd'] == 'sync_resume')['payload']['generation']
          .toString();

      downstream.add(
        jsonEncode({
          'cmd': 'sync_batch',
          'payload': {
            'generation': generation,
            'from_cursor': '5',
            'next_cursor': '5',
            'head_cursor': '5',
            'has_more': false,
            'events': const [],
          },
        }),
      );
      await _eventually(() => !service.isConnected);

      expect(sink.packets.where((p) => p['cmd'] == 'sync_ack'), isEmpty);
      expect((await LocalDb.getSyncState()).committedCursor, 0);
      service.disconnect();
      await downstream.close();
    },
  );

  test('v2 session mutation stays in outbox until stream receipt', () async {
    final sink = _RecordingSink();
    final downstream = StreamController<dynamic>();
    ImService.channelConnectorForTest = (_) =>
        _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
    await LocalDb.upsertSession({
      'session_id': 'session-outbox',
      'title': 'Outbox',
      'type': 'group',
      'is_muted': 0,
      'updated_at': 1700000000000,
    });
    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
    downstream.add(
      jsonEncode({
        'cmd': 'auth_ack',
        'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
      }),
    );
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'sync_resume'));
    final generation = sink.packets
        .firstWhere((p) => p['cmd'] == 'sync_resume')['payload']['generation']
        .toString();

    expect(
      await service.setSessionMuted('session-outbox', isMuted: true),
      isTrue,
    );
    await _eventually(() => sessionService.muteCommandIds.isNotEmpty);
    final commandId = sessionService.muteCommandIds.single;
    final db = await LocalDb.database;
    expect(
      await db.query(
        'outbox',
        where: "command_id = ? AND state = 'pending'",
        whereArgs: [commandId],
      ),
      hasLength(1),
    );
    final optimistic = (await LocalDb.getSessions()).singleWhere(
      (row) => row['session_id'] == 'session-outbox',
    );
    expect(optimistic['is_muted'], 1);

    downstream.add(
      jsonEncode({
        'cmd': 'sync_batch',
        'payload': {
          'generation': generation,
          'from_cursor': '0',
          'next_cursor': '1',
          'head_cursor': '1',
          'has_more': false,
          'events': [
            {
              'cursor': '1',
              'kind': 'session.mute_changed',
              'entity_type': 'session_member',
              'entity_id': 'session-outbox',
              'entity_version': '1',
              'command_id': commandId,
              'payload': {'session_id': 'session-outbox', 'is_muted': true},
            },
          ],
        },
      }),
    );
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'sync_ack'));
    expect(
      await db.query(
        'outbox',
        where: "command_id = ? AND state = 'pending'",
        whereArgs: [commandId],
      ),
      isEmpty,
    );

    service.disconnect();
    await downstream.close();
  });

  test(
    'terminal outbox failure rolls back and does not block the queue',
    () async {
      sessionService.rejectMuteAsTerminal = true;
      final sink = _RecordingSink();
      final downstream = StreamController<dynamic>();
      ImService.channelConnectorForTest = (_) =>
          _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
      await LocalDb.upsertSession({
        'session_id': 'session-terminal',
        'title': 'Terminal',
        'type': 'group',
        'is_muted': 0,
        'updated_at': 1700000000000,
      });
      await LocalDb.applySessionCommandWithOutbox(
        sessionId: 'session-terminal',
        sessionValues: {'is_muted': 1},
        commandId: 'terminal-mute',
        commandKind: 'session.mute',
        payload: {
          'session_id': 'session-terminal',
          'is_muted': true,
          'previous_is_muted': false,
        },
      );
      await LocalDb.enqueueOutboxCommand(
        commandId: 'after-terminal-read',
        commandKind: 'session_read',
        payload: {'session_id': 'session-terminal', 'last_read_msg_id': '0'},
      );
      await LocalDb.prepareSyncGeneration('pre-bootstrap');
      await LocalDb.markSyncBootstrapComplete(1, committedCursor: 0);
      final service = ImService();
      service.connect('ws://127.0.0.1:1/ws');
      await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
      downstream.add(
        jsonEncode({
          'cmd': 'auth_ack',
          'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
        }),
      );
      await _eventually(
        () => sink.packets.any((p) => p['cmd'] == 'sync_resume'),
      );

      await _eventuallyAsync(
        () async => (await LocalDb.getPendingOutboxCommands()).every(
          (command) => command.commandId != 'terminal-mute',
        ),
      );
      await _eventually(
        () => sink.packets.any(
          (packet) =>
              packet['cmd'] == 'session_read' &&
              packet['payload']['command_id'] == 'after-terminal-read',
        ),
      );
      final session = (await LocalDb.getSessions()).singleWhere(
        (row) => row['session_id'] == 'session-terminal',
      );
      expect(session['is_muted'], 0);

      service.disconnect();
      await downstream.close();
    },
  );

  test('v1 history reset does not leak into the v2 outbox', () async {
    final sink = _RecordingSink();
    final downstream = StreamController<dynamic>();
    ImService.channelConnectorForTest = (_) =>
        _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
    downstream.add(
      jsonEncode({
        'cmd': 'auth_ack',
        'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v1'},
      }),
    );
    await _eventually(() => service.isAuthenticated);

    await service.deleteConversation('legacy-session');
    await _eventually(
      () => sink.packets.any((p) => p['cmd'] == 'session_history_reset'),
    );
    final db = await LocalDb.database;
    expect(
      await db.query('outbox', where: "command_kind = 'session_history_reset'"),
      isEmpty,
    );

    service.disconnect();
    await downstream.close();
  });

  test(
    'v1 rollback drains REST commands persisted by an earlier v2 run',
    () async {
      const commandId = 'rollback-session-mute';
      await LocalDb.enqueueOutboxCommand(
        commandId: commandId,
        commandKind: 'session.mute',
        payload: const {'session_id': 'rollback-session', 'is_muted': true},
      );
      final sink = _RecordingSink();
      final downstream = StreamController<dynamic>();
      ImService.channelConnectorForTest = (_) =>
          _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
      final service = ImService();
      service.connect('ws://127.0.0.1:1/ws');
      await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
      downstream.add(
        jsonEncode({
          'cmd': 'auth_ack',
          'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v1'},
        }),
      );

      await _eventually(
        () => sessionService.muteCommandIds.contains(commandId),
      );
      final db = await LocalDb.database;
      await _eventuallyAsync(
        () async => (await db.query(
          'outbox',
          where: 'command_id = ?',
          whereArgs: [commandId],
        )).isEmpty,
      );

      expect(
        await db.query(
          'outbox',
          where: 'command_id = ?',
          whereArgs: [commandId],
        ),
        isEmpty,
      );

      service.disconnect();
      await downstream.close();
    },
  );

  test('v2 history reset uses one durable outbox transport', () async {
    final sink = _RecordingSink();
    final downstream = StreamController<dynamic>();
    ImService.channelConnectorForTest = (_) =>
        _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
    downstream.add(
      jsonEncode({
        'cmd': 'auth_ack',
        'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
      }),
    );
    await _eventually(() => sink.packets.any((p) => p['cmd'] == 'sync_resume'));

    await service.deleteConversation('v2-session');
    await _eventually(
      () => sink.packets.any((p) => p['cmd'] == 'session_history_reset'),
    );
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(
      sink.packets.where((p) => p['cmd'] == 'session_history_reset'),
      hasLength(1),
    );
    final db = await LocalDb.database;
    expect(
      await db.query(
        'outbox',
        where: "command_kind = 'session_history_reset' AND state = 'pending'",
      ),
      hasLength(1),
    );

    service.disconnect();
    await downstream.close();
  });

  test(
    'caught-up live batch re-projects only the sessions it changed',
    () async {
      await LocalDb.upsertSession({
        'session_id': 'live-a',
        'title': 'Live A',
        'type': 'group',
        'updated_at': 1700000000000,
      });
      await LocalDb.upsertSession({
        'session_id': 'live-b',
        'title': 'Live B',
        'type': 'group',
        'updated_at': 1700000000000,
      });
      final harness = await _V2Harness.resume();
      final service = harness.service;
      final tickBeforeCatchUp = service.sessionsLoadTick.value;

      // Resume catch-up ends on its has_more=false batch: one full reload.
      await harness.sendBatch(
        from: 0,
        next: 1,
        events: [
          {
            'cursor': '1',
            'kind': 'session.unread_set',
            'entity_type': 'session_member',
            'entity_id': 'live-a',
            'entity_version': '1',
            'payload': {'session_id': 'live-a', 'unread_count': 2},
          },
        ],
        unreadSnapshot: {'live-a': 2},
      );
      await _eventually(
        () => service.sessionsLoadTick.value == tickBeforeCatchUp + 1,
      );
      expect(
        service.sessions.map((s) => s.sessionId),
        containsAll(['live-a', 'live-b']),
      );
      final tickAfterCatchUp = service.sessionsLoadTick.value;

      // A row only the database knows about: any full reload would list it.
      await LocalDb.upsertSession({
        'session_id': 'db-only',
        'title': 'DB only',
        'type': 'group',
        'updated_at': 1700000000500,
      });

      await harness.sendBatch(
        from: 1,
        next: 3,
        events: [
          {
            'cursor': '2',
            'kind': 'message.upsert',
            'entity_type': 'message',
            'entity_id': '901',
            'entity_version': '1',
            'payload': {
              'msg_id': '901',
              'session_id': 'live-b',
              'sender_id': '1002',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'live delta',
              'created_at': 1700000001000,
            },
          },
          {
            'cursor': '3',
            'kind': 'session.remove',
            'entity_type': 'session',
            'entity_id': 'live-a',
            'entity_version': '2',
            'tombstone': true,
            'payload': {'reason': 'history_reset', 'deleted_at': 1700000001000},
          },
        ],
        unreadSnapshot: {'live-b': 4},
      );

      final ids = service.sessions.map((s) => s.sessionId).toList();
      expect(ids, contains('live-b'));
      expect(ids, isNot(contains('live-a')));
      expect(ids, isNot(contains('db-only')));
      final liveB = service.sessions.singleWhere(
        (s) => s.sessionId == 'live-b',
      );
      expect(liveB.unreadCount, 4);
      expect(liveB.lastMessage, 'live delta');
      expect(liveB.lastMessageTime, 1700000001000);
      expect(service.notificationUnread, 4);
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(service.sessionsLoadTick.value, tickAfterCatchUp);

      await harness.close();
    },
  );

  test('4003 history reset ack rejects its pending outbox command', () async {
    final harness = await _V2Harness.resume();
    final service = harness.service;

    await service.deleteConversation('reset-denied');
    await _eventually(
      () =>
          harness.sink.packets.any((p) => p['cmd'] == 'session_history_reset'),
    );
    final sent = harness.sink.packets.firstWhere(
      (p) => p['cmd'] == 'session_history_reset',
    );
    final commandId = sent['payload']['command_id'].toString();
    expect(await _outboxState(commandId), 'pending');

    // Today's server does not echo command_id; the session id must suffice.
    harness.downstream.add(
      jsonEncode({
        'cmd': 'session_history_reset_ack',
        'seq': sent['seq'],
        'payload': {
          'session_id': 'reset-denied',
          'code': 4003,
          'msg': 'permission denied',
        },
      }),
    );
    await _eventuallyAsync(
      () async => await _outboxState(commandId) == 'rejected',
    );
    expect(service.isSessionLocallyDeletedForTest('reset-denied'), isFalse);
    expect(
      await LocalDb.getPendingSessionHistoryResetCommands('reset-denied'),
      isEmpty,
    );

    await harness.close();
  });

  test(
    'history reset ack settles resends without a registered deleted_at',
    () async {
      final harness = await _V2Harness.resume();
      // Pending resets whose local delete mark has since been cleared: the
      // ack cannot be matched through deleted_at.
      await LocalDb.enqueueOutboxCommand(
        commandId: 'history_reset:orphan:1700000000000',
        commandKind: 'session_history_reset',
        payload: {'session_id': 'orphan', 'deleted_at': 1700000000000},
      );
      await LocalDb.enqueueOutboxCommand(
        commandId: 'history_reset:orphan:1700000005000',
        commandKind: 'session_history_reset',
        payload: {'session_id': 'orphan', 'deleted_at': 1700000005000},
      );
      await LocalDb.enqueueOutboxCommand(
        commandId: 'history_reset:orphan-2:1700000000000',
        commandKind: 'session_history_reset',
        payload: {'session_id': 'orphan-2', 'deleted_at': 1700000000000},
      );

      void ack(Map<String, dynamic> payload) => harness.downstream.add(
        jsonEncode({'cmd': 'session_history_reset_ack', 'payload': payload}),
      );

      // No echoed command_id: the oldest pending reset of that session only.
      ack({'session_id': 'orphan', 'code': 0});
      await _eventuallyAsync(
        () async =>
            await _outboxState('history_reset:orphan:1700000000000') ==
            'acknowledged',
      );
      expect(
        await _outboxState('history_reset:orphan:1700000005000'),
        'pending',
      );
      expect(
        await _outboxState('history_reset:orphan-2:1700000000000'),
        'pending',
      );

      // Echoed command_id: exactly that command.
      ack({
        'session_id': 'orphan-2',
        'code': 0,
        'command_id': 'history_reset:orphan-2:1700000000000',
      });
      await _eventuallyAsync(
        () async =>
            await _outboxState('history_reset:orphan-2:1700000000000') ==
            'acknowledged',
      );
      expect(
        await _outboxState('history_reset:orphan:1700000005000'),
        'pending',
      );

      // 5001 is a server-side save failure and keeps the command queued.
      ack({'session_id': 'orphan', 'code': 5001, 'msg': 'save failed'});
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(
        await _outboxState('history_reset:orphan:1700000005000'),
        'pending',
      );

      await harness.close();
    },
  );

  test(
    'outbox command is rejected instead of resent after 20 attempts',
    () async {
      await LocalDb.enqueueOutboxCommand(
        commandId: 'history_reset:stuck:1700000000000',
        commandKind: 'session_history_reset',
        payload: {'session_id': 'stuck', 'deleted_at': 1700000000000},
      );
      await LocalDb.enqueueOutboxCommand(
        commandId: 'history_reset:retrying:1700000000000',
        commandKind: 'session_history_reset',
        payload: {'session_id': 'retrying', 'deleted_at': 1700000000000},
      );
      final db = await LocalDb.database;
      await db.update(
        'outbox',
        {'attempt_count': 20},
        where: 'command_id = ?',
        whereArgs: ['history_reset:stuck:1700000000000'],
      );
      await db.update(
        'outbox',
        {'attempt_count': 19},
        where: 'command_id = ?',
        whereArgs: ['history_reset:retrying:1700000000000'],
      );

      final harness = await _V2Harness.resume();
      bool sentCommand(String commandId) => harness.sink.packets.any(
        (p) =>
            p['cmd'] == 'session_history_reset' &&
            p['payload']['command_id'] == commandId,
      );

      await _eventually(
        () => sentCommand('history_reset:retrying:1700000000000'),
      );
      await _eventuallyAsync(
        () async =>
            await _outboxState('history_reset:stuck:1700000000000') ==
            'rejected',
      );
      expect(sentCommand('history_reset:stuck:1700000000000'), isFalse);
      final retrying = await _outboxRows(
        'history_reset:retrying:1700000000000',
      );
      expect(retrying.single['state'], 'pending');
      expect(retrying.single['attempt_count'], 20);

      // The rejected receipt keeps a reconnect from re-enqueueing it.
      await LocalDb.enqueueOutboxCommand(
        commandId: 'history_reset:stuck:1700000000000',
        commandKind: 'session_history_reset',
        payload: {'session_id': 'stuck', 'deleted_at': 1700000000000},
      );
      expect(
        await _outboxState('history_reset:stuck:1700000000000'),
        'rejected',
      );

      await harness.close();
    },
  );
}
