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
}
