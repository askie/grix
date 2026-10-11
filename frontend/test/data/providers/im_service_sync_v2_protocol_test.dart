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
  _FakeAuthService(this.userIdValue);

  String userIdValue;

  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => userIdValue;

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
  Completer<void>? snapshotStarted;
  Completer<void>? snapshotGate;
  SessionSnapshotFetchResult? snapshotResult;
  int bootstrapFailuresRemaining = 0;
  int bootstrapRetryAfterSeconds = 0;
  final List<SessionSnapshotFetchResult> bootstrapPages = [];
  final List<Map<String, Object?>> bootstrapPageRequests = [];
  Completer<void>? secondBootstrapPageStarted;
  Completer<void>? secondBootstrapPageGate;
  Completer<void>? firstBootstrapPageStarted;
  Completer<void>? firstBootstrapPageGate;
  SessionSnapshotFetchResult? firstBootstrapPageResult;
  Completer<void>? muteStarted;
  Completer<void>? muteGate;
  bool rejectMuteAsTerminal = false;
  List<SessionSnapshot> bootstrapSnapshots = const [];
  final List<String> muteCommandIds = [];

  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    snapshotFetches++;
    final gate = snapshotGate;
    final result = snapshotResult;
    snapshotStarted?.complete();
    if (gate != null) await gate.future;
    if (result != null) return result;
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
  }) async {
    snapshotFetches++;
    if (bootstrapFailuresRemaining > 0) {
      bootstrapFailuresRemaining--;
      return SessionSnapshotFetchResult(
        snapshots: const [],
        success: false,
        networkError: true,
        retryAfterSeconds: bootstrapRetryAfterSeconds,
      );
    }
    return SessionSnapshotFetchResult(
      snapshots: bootstrapSnapshots,
      success: true,
      cursor: 1700000000,
      syncHeadCursor: snapshotSyncHeadCursor,
    );
  }

  @override
  Future<SessionSnapshotFetchResult> fetchSyncV2BootstrapPageResult({
    int limit = 200,
    String cursor = '',
    int? headCursor,
  }) async {
    snapshotFetches++;
    bootstrapPageRequests.add({
      'limit': limit,
      'cursor': cursor,
      'head_cursor': headCursor,
    });
    if (bootstrapPageRequests.length == 1 && firstBootstrapPageGate != null) {
      firstBootstrapPageStarted?.complete();
      await firstBootstrapPageGate!.future;
      final firstResult = firstBootstrapPageResult;
      if (firstResult != null) return firstResult;
    }
    if (bootstrapPageRequests.length == 2) {
      secondBootstrapPageStarted?.complete();
      await secondBootstrapPageGate?.future;
    }
    if (bootstrapFailuresRemaining > 0) {
      bootstrapFailuresRemaining--;
      return SessionSnapshotFetchResult(
        snapshots: const [],
        success: false,
        networkError: true,
        retryAfterSeconds: bootstrapRetryAfterSeconds,
      );
    }
    if (bootstrapPages.isNotEmpty) {
      return bootstrapPages.removeAt(0);
    }
    return SessionSnapshotFetchResult(
      snapshots: bootstrapSnapshots,
      success: true,
      cursor: 1700000000,
      syncHeadCursor: snapshotSyncHeadCursor,
    );
  }

  @override
  Future<SessionMuteResult> setSessionMutedResult(
    String sessionId, {
    required bool isMuted,
    String? commandId,
  }) async {
    if (commandId != null) muteCommandIds.add(commandId);
    final started = muteStarted;
    if (started != null && !started.isCompleted) started.complete();
    final gate = muteGate;
    if (gate != null) await gate.future;
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
        'payload': {
          'code': 0,
          'user_id': LocalDb.activeUserId,
          'active_sync': 'v2',
        },
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

SessionSnapshot _bootstrapSnapshot(String sessionId) => SessionSnapshot(
  sessionId: sessionId,
  title: sessionId,
  type: 'group',
  peerId: '',
  peerType: 0,
  peerNickname: '',
  peerUsername: '',
  updatedAt: 1700000000000,
  unreadCount: 0,
  lastMessage: '',
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late String userId;
  late _FakeAuthService authService;
  late _RecordingSessionService sessionService;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    userId = 'sync_v2_protocol_${DateTime.now().microsecondsSinceEpoch}';
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(userId);
    authService = _FakeAuthService(userId);
    Get.put<AuthService>(authService);
    sessionService = _RecordingSessionService();
    Get.put<SessionService>(sessionService);
  });

  tearDown(() async {
    ImService.channelConnectorForTest = null;
    ImService.syncV2BootstrapRetryDelayForTest = null;
    ImService.sessionSnapshotPersistedForTest = null;
    ImService.syncV2ResumeSentForTest = null;
    ImService.downstreamTaskFinishedForTest = null;
    ImService.downstreamDbOpStartedForTest = null;
    ImService.ownershipAwaitGateForTest = null;
    ImService.sessionWindowCacheNowMsForTest = null;
    Get.reset();
    await LocalDb.setActiveUser(null);
  });

  test(
    'bootstrap opens the circuit after five failures without reconnecting',
    () async {
      ImService.syncV2BootstrapRetryDelayForTest = Duration.zero;
      sessionService.bootstrapFailuresRemaining = 10;
      final sink = _RecordingSink();
      final downstream = StreamController<dynamic>();
      var connectionCount = 0;
      ImService.channelConnectorForTest = (_) {
        connectionCount++;
        return _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
      };
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

      await _eventually(() => service.syncV2BootstrapCircuitOpenForTest);
      expect(sessionService.snapshotFetches, 5);
      expect(service.syncV2BootstrapFailuresForTest, 5);
      expect(service.connectionStage, ImConnectionStage.syncFailed);
      expect(service.connectionBannerTextKey, 'sync_bootstrap_failed');
      expect(service.hasReconnectTimerForTest, isFalse);
      expect(connectionCount, 1);
      expect(sink.packets.where((p) => p['cmd'] == 'sync_resume'), isEmpty);

      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(sessionService.snapshotFetches, 5);
      expect(connectionCount, 1);

      // A duplicate auth-success callback (including one reached through an
      // immediate reconnect path) must observe the open circuit rather than
      // resetting it or issuing a sixth bootstrap request.
      await service.handleSyncV2AuthSuccessForTest();
      expect(sessionService.snapshotFetches, 5);
      expect(service.syncV2BootstrapCircuitOpenForTest, isTrue);

      service.disconnect();
      await downstream.close();
    },
  );

  test('manual sync retries an open bootstrap circuit', () async {
    ImService.syncV2BootstrapRetryDelayForTest = Duration.zero;
    sessionService.bootstrapFailuresRemaining = 5;
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
    await _eventually(() => service.syncV2BootstrapCircuitOpenForTest);

    service.syncNow();
    await _eventually(
      () => sink.packets.any((packet) => packet['cmd'] == 'sync_resume'),
    );
    expect(sessionService.snapshotFetches, 6);
    expect(service.syncV2BootstrapCircuitOpenForTest, isFalse);
    expect(service.syncV2BootstrapFailuresForTest, 0);

    service.disconnect();
    await downstream.close();
  });

  test(
    'a durably advanced page resets the consecutive failure streak',
    () async {
      ImService.syncV2BootstrapRetryDelayForTest = Duration.zero;
      sessionService.bootstrapPages.addAll([
        const SessionSnapshotFetchResult(
          snapshots: [],
          success: false,
          networkError: true,
        ),
        SessionSnapshotFetchResult(
          snapshots: [_bootstrapSnapshot('session-a')],
          success: true,
          hasMore: true,
          cursor: 1700000000,
          syncHeadCursor: 42,
          bootstrapCursor: '1',
        ),
        for (var i = 0; i < 5; i++)
          const SessionSnapshotFetchResult(
            snapshots: [],
            success: false,
            networkError: true,
          ),
      ]);
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

      await _eventually(() => service.syncV2BootstrapCircuitOpenForTest);
      expect(sessionService.snapshotFetches, 7);
      expect(service.syncV2BootstrapFailuresForTest, 5);
      expect(await LocalDb.getSyncBootstrapSessionIDs(), {'session-a'});

      service.disconnect();
      await downstream.close();
    },
  );

  test(
    'bootstrap keyset pages keep one head and reconcile only after the last page',
    () async {
      await LocalDb.upsertSession({
        'session_id': 'stale-local',
        'title': 'stale',
        'type': 'group',
        'updated_at': 1,
      });
      sessionService
        ..secondBootstrapPageStarted = Completer<void>()
        ..secondBootstrapPageGate = Completer<void>()
        ..bootstrapPages.addAll([
          SessionSnapshotFetchResult(
            snapshots: [
              _bootstrapSnapshot('session-a'),
              _bootstrapSnapshot('session-b'),
            ],
            success: true,
            hasMore: true,
            cursor: 1700000000,
            syncHeadCursor: 42,
            bootstrapCursor: 'session-b',
          ),
          SessionSnapshotFetchResult(
            snapshots: [_bootstrapSnapshot('session-c')],
            success: true,
            cursor: 1700000001,
            syncHeadCursor: 42,
          ),
        ]);
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

      await sessionService.secondBootstrapPageStarted!.future;
      final midState = await LocalDb.getSyncState();
      expect(midState.bootstrapInProgress, isTrue);
      expect(midState.bootstrapPageCursor, 'session-b');
      expect(midState.bootstrapHeadCursor, 42);
      expect(
        (await LocalDb.getSessions()).map((row) => row['session_id']),
        containsAll(['stale-local', 'session-a', 'session-b']),
      );

      sessionService.secondBootstrapPageGate!.complete();
      await _eventually(
        () => sink.packets.any((packet) => packet['cmd'] == 'sync_resume'),
      );
      expect(sessionService.bootstrapPageRequests, [
        {'limit': 200, 'cursor': '', 'head_cursor': null},
        {'limit': 200, 'cursor': 'session-b', 'head_cursor': 42},
      ]);
      final finalRows = await LocalDb.getSessions();
      expect(finalRows.map((row) => row['session_id']).toSet(), {
        'session-a',
        'session-b',
        'session-c',
      });
      final finalState = await LocalDb.getSyncState();
      expect(finalState.bootstrapInProgress, isFalse);
      expect(finalState.bootstrapCursor, 1700000000);
      expect(finalState.committedCursor, 42);

      service.disconnect();
      await downstream.close();
    },
  );

  test(
    'send_ack is consumed while paginated bootstrap is waiting on HTTP',
    () async {
      sessionService
        ..secondBootstrapPageStarted = Completer<void>()
        ..secondBootstrapPageGate = Completer<void>()
        ..bootstrapPages.addAll([
          const SessionSnapshotFetchResult(
            snapshots: [],
            success: true,
            hasMore: true,
            cursor: 1700000000,
            syncHeadCursor: 42,
            bootstrapCursor: 'next-page',
          ),
          const SessionSnapshotFetchResult(
            snapshots: [],
            success: true,
            cursor: 1700000000,
            syncHeadCursor: 42,
          ),
        ]);
      final sink = _RecordingSink();
      final downstream = StreamController<dynamic>();
      ImService.channelConnectorForTest = (_) =>
          _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
      final service = ImService();
      service.setCurrentSessionForTest('active-chat');
      service.connect('ws://127.0.0.1:1/ws');

      try {
        await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
        downstream.add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
          }),
        );
        await sessionService.secondBootstrapPageStarted!.future;

        final clientMsgId = await service.sendMessageWithClientId(
          'send during bootstrap',
          'active-chat',
        );
        expect(clientMsgId, isNotNull);
        expect(service.currentMessages.single.status, 'sending');
        downstream.add(
          jsonEncode({
            'cmd': 'send_ack',
            'payload': {
              'client_msg_id': clientMsgId,
              'msg_id': 'server-message-1',
              'created_at': 1700000001,
              'inbox_seq': 1,
            },
          }),
        );

        await _eventually(
          () =>
              service.currentMessages.single.msgId == 'server-message-1' &&
              service.currentMessages.single.status == 'success',
          reason: 'send_ack must not wait for the bootstrap HTTP page',
        );
        expect(sink.packets.where((p) => p['cmd'] == 'sync_resume'), isEmpty);
      } finally {
        final gate = sessionService.secondBootstrapPageGate!;
        if (!gate.isCompleted) gate.complete();
        service.disconnect();
        await downstream.close();
      }
    },
  );

  test(
    'reconnect starts an owned bootstrap flight and ignores the old completion',
    () async {
      sessionService
        ..firstBootstrapPageStarted = Completer<void>()
        ..firstBootstrapPageGate = Completer<void>()
        ..firstBootstrapPageResult = SessionSnapshotFetchResult(
          snapshots: [_bootstrapSnapshot('old-session')],
          success: true,
          cursor: 1700000000,
          syncHeadCursor: 41,
        )
        ..bootstrapPages.add(
          SessionSnapshotFetchResult(
            snapshots: [_bootstrapSnapshot('new-session')],
            success: true,
            cursor: 1700000001,
            syncHeadCursor: 88,
          ),
        );
      final sinks = [_RecordingSink(), _RecordingSink()];
      final downstreams = [
        StreamController<dynamic>(),
        StreamController<dynamic>(),
      ];
      var connectionIndex = 0;
      ImService.channelConnectorForTest = (_) {
        final index = connectionIndex++;
        return _FakeWebSocketChannel(
          stream: downstreams[index].stream,
          sink: sinks[index],
        );
      };
      final service = ImService();

      try {
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[0].packets.any((p) => p['cmd'] == 'auth'),
        );
        downstreams[0].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
          }),
        );
        await sessionService.firstBootstrapPageStarted!.future;

        service.disconnect();
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[1].packets.any((p) => p['cmd'] == 'auth'),
        );
        downstreams[1].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
          }),
        );

        await _eventually(
          () => sinks[1].packets.any((p) => p['cmd'] == 'sync_resume'),
          reason: 'the replacement connection must not await the old HTTP page',
        );
        expect(sessionService.bootstrapPageRequests, hasLength(2));
        final generation = sinks[1].packets
            .firstWhere(
              (p) => p['cmd'] == 'sync_resume',
            )['payload']['generation']
            .toString();
        service.setCurrentSessionForTest('new-session');
        downstreams[1].add(
          jsonEncode({
            'cmd': 'sync_batch',
            'payload': {
              'generation': generation,
              'from_cursor': '88',
              'next_cursor': '89',
              'head_cursor': '89',
              'has_more': false,
              'events': [
                {
                  'cursor': '89',
                  'kind': 'message.upsert',
                  'entity_type': 'message',
                  'entity_id': 'new-live-message',
                  'entity_version': '1',
                  'payload': {
                    'msg_id': 'new-live-message',
                    'session_id': 'new-session',
                    'sender_id': 'peer',
                    'sender_type': 1,
                    'msg_type': 1,
                    'content': 'visible on replacement connection',
                    'created_at': 1700000002000,
                  },
                },
              ],
            },
          }),
        );
        await _eventually(
          () => service.currentMessages.any(
            (message) => message.msgId == 'new-live-message',
          ),
        );
        final generationBeforeOldRelease = generation;
        final failuresBeforeOldRelease = service.syncV2BootstrapFailuresForTest;

        sessionService.firstBootstrapPageGate!.complete();
        await pumpEventQueue(times: 20);

        expect(
          service.syncV2BootstrapFailuresForTest,
          failuresBeforeOldRelease,
        );
        expect(
          sinks[1].packets
              .where(
                (packet) =>
                    packet['cmd'] == 'sync_resume' &&
                    packet['payload']['generation'] ==
                        generationBeforeOldRelease,
              )
              .length,
          1,
        );
        expect((await LocalDb.getSyncState()).committedCursor, 89);
        expect(
          service.currentMessages.map((message) => message.msgId),
          contains('new-live-message'),
        );
      } finally {
        if (!sessionService.firstBootstrapPageGate!.isCompleted) {
          sessionService.firstBootstrapPageGate!.complete();
        }
        service.disconnect();
        for (final downstream in downstreams) {
          await downstream.close();
        }
      }
    },
  );

  test(
    'background disconnect lets the replacement owner flush a blocked outbox',
    () async {
      const commandId = 'background-mute-command';
      await LocalDb.enqueueOutboxCommand(
        commandId: commandId,
        commandKind: 'session.mute',
        payload: {'session_id': 'background-session', 'is_muted': true},
      );
      await LocalDb.prepareSyncGeneration('background-pre-bootstrap');
      await LocalDb.markSyncBootstrapComplete(1, committedCursor: 0);
      sessionService
        ..muteStarted = Completer<void>()
        ..muteGate = Completer<void>();
      final sinks = [_RecordingSink(), _RecordingSink()];
      final downstreams = [
        StreamController<dynamic>(),
        StreamController<dynamic>(),
      ];
      var connectionIndex = 0;
      ImService.channelConnectorForTest = (_) {
        final index = connectionIndex++;
        return _FakeWebSocketChannel(
          stream: downstreams[index].stream,
          sink: sinks[index],
        );
      };
      final service = ImService();
      Completer<void>? oldGate;

      try {
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[0].packets.any((packet) => packet['cmd'] == 'auth'),
        );
        downstreams[0].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
          }),
        );
        await sessionService.muteStarted!.future;
        expect(service.syncOutboxFlushInFlightForTest, isTrue);

        oldGate = sessionService.muteGate!;
        sessionService.muteGate = null;
        service.suspendForAppBackground();
        expect(service.syncOutboxFlushInFlightForTest, isFalse);
        service.syncNow();
        await _eventually(
          () => sinks[1].packets.any((packet) => packet['cmd'] == 'auth'),
        );
        downstreams[1].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
          }),
        );

        await _eventually(
          () => sessionService.muteCommandIds.length == 2,
          reason: 'the replacement connection must own a separate flush',
        );
        await _eventually(
          () =>
              sinks[1].packets.any((packet) => packet['cmd'] == 'sync_resume'),
        );
        final generation = sinks[1].packets
            .firstWhere(
              (packet) => packet['cmd'] == 'sync_resume',
            )['payload']['generation']
            .toString();
        downstreams[1].add(
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
                  'kind': 'session.upsert',
                  'entity_type': 'session',
                  'entity_id': 'background-session',
                  'entity_version': '1',
                  'command_id': commandId,
                  'payload': {
                    'session_id': 'background-session',
                    'title': 'Background',
                    'updated_at': 1700000001000,
                  },
                },
              ],
            },
          }),
        );
        await _eventuallyAsync(
          () async => (await _outboxRows(commandId)).isEmpty,
        );
        oldGate.complete();
        await pumpEventQueue(times: 20);
        expect(sessionService.muteCommandIds, [commandId, commandId]);
        expect(await _outboxRows(commandId), isEmpty);
      } finally {
        if (oldGate != null && !oldGate.isCompleted) oldGate.complete();
        final gate = sessionService.muteGate;
        if (gate != null && !gate.isCompleted) gate.complete();
        service.disconnect();
        for (final downstream in downstreams) {
          await downstream.close();
        }
      }
    },
  );

  test(
    'network disconnect lets the replacement owner flush a blocked outbox',
    () async {
      const commandId = 'network-disconnect-mute-command';
      await LocalDb.enqueueOutboxCommand(
        commandId: commandId,
        commandKind: 'session.mute',
        payload: {'session_id': 'network-session', 'is_muted': true},
      );
      await LocalDb.prepareSyncGeneration('network-pre-bootstrap');
      await LocalDb.markSyncBootstrapComplete(1, committedCursor: 0);
      sessionService
        ..muteStarted = Completer<void>()
        ..muteGate = Completer<void>();
      final sinks = [_RecordingSink(), _RecordingSink()];
      final downstreams = [
        StreamController<dynamic>(),
        StreamController<dynamic>(),
      ];
      var connectionIndex = 0;
      ImService.channelConnectorForTest = (_) {
        final index = connectionIndex++;
        return _FakeWebSocketChannel(
          stream: downstreams[index].stream,
          sink: sinks[index],
        );
      };
      final service = ImService();
      Completer<void>? oldGate;
      try {
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[0].packets.any((packet) => packet['cmd'] == 'auth'),
        );
        downstreams[0].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
          }),
        );
        await sessionService.muteStarted!.future;
        expect(service.syncOutboxFlushInFlightForTest, isTrue);

        oldGate = sessionService.muteGate!;
        sessionService.muteGate = null;
        service.handleDisconnectForTest(
          finalStage: ImConnectionStage.reconnecting,
        );
        expect(service.syncOutboxFlushInFlightForTest, isFalse);
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[1].packets.any((packet) => packet['cmd'] == 'auth'),
        );
        downstreams[1].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
          }),
        );
        await _eventually(
          () => sessionService.muteCommandIds.length == 2,
          reason: 'network replacement must own a separate flush',
        );
        oldGate.complete();
        await pumpEventQueue(times: 20);
        expect(sessionService.muteCommandIds, [commandId, commandId]);
      } finally {
        if (oldGate != null && !oldGate.isCompleted) oldGate.complete();
        service.disconnect();
        for (final downstream in downstreams) {
          await downstream.close();
        }
      }
    },
  );

  test(
    'v1 auth REST snapshot cannot publish into a replacement account',
    () async {
      final oldGate = Completer<void>();
      sessionService
        ..snapshotStarted = Completer<void>()
        ..snapshotGate = oldGate
        ..snapshotResult = SessionSnapshotFetchResult(
          snapshots: [_bootstrapSnapshot('account-a-rest-session')],
          success: true,
          cursor: 1700000100,
        );
      final sinks = [_RecordingSink(), _RecordingSink()];
      final downstreams = [
        StreamController<dynamic>(),
        StreamController<dynamic>(),
      ];
      var connectionIndex = 0;
      ImService.channelConnectorForTest = (_) {
        final index = connectionIndex++;
        return _FakeWebSocketChannel(
          stream: downstreams[index].stream,
          sink: sinks[index],
        );
      };
      final service = ImService();

      try {
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[0].packets.any((packet) => packet['cmd'] == 'auth'),
        );
        downstreams[0].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v1'},
          }),
        );
        await sessionService.snapshotStarted!.future;

        await service.resetForAccountSwitch();
        final accountB = '${userId}_rest_owner_b';
        await LocalDb.setActiveUser(accountB);
        await LocalDb.upsertSession({
          'session_id': 'account-b-existing',
          'title': 'B survives',
          'type': 'group',
          'updated_at': 1700000200000,
          'unread_count': 4,
        });
        sessionService
          ..snapshotStarted = null
          ..snapshotGate = null
          ..snapshotResult = SessionSnapshotFetchResult(
            snapshots: [_bootstrapSnapshot('account-b-valid-session')],
            success: true,
            cursor: 1700000200,
          );

        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[1].packets.any((packet) => packet['cmd'] == 'auth'),
        );
        downstreams[1].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '2002', 'active_sync': 'v1'},
          }),
        );
        oldGate.complete();

        await _eventuallyAsync(() async {
          final rows = await LocalDb.getSessions();
          return rows.any(
            (row) => row['session_id'] == 'account-b-valid-session',
          );
        });
        final bRows = await LocalDb.getSessions();
        expect(
          bRows.map((row) => row['session_id']),
          containsAll(['account-b-existing', 'account-b-valid-session']),
        );
        expect(
          bRows.map((row) => row['session_id']),
          isNot(contains('account-a-rest-session')),
        );
        expect((await LocalDb.getSyncState()).committedCursor, 0);
        expect(
          service.resolveSessionTypeById('account-a-rest-session'),
          'private',
        );
      } finally {
        if (!oldGate.isCompleted) oldGate.complete();
        service.disconnect();
        for (final downstream in downstreams) {
          await downstream.close();
        }
      }
    },
  );

  test(
    'started stream finish cannot continue its session projection after owner switch',
    () async {
      const sessionId = 'stream-owner-session';
      final sinks = [_RecordingSink(), _RecordingSink()];
      final downstreams = [
        StreamController<dynamic>.broadcast(),
        StreamController<dynamic>.broadcast(),
      ];
      var connectionIndex = 0;
      ImService.channelConnectorForTest = (_) {
        final index = connectionIndex++;
        return _FakeWebSocketChannel(
          stream: downstreams[index].stream,
          sink: sinks[index],
        );
      };
      final service = ImService();
      final queueGate = Completer<void>();
      final firstWriteStarted = Completer<void>();
      final oldTaskFinished = Completer<void>();
      final validTaskFinished = Completer<void>();
      var finishedCount = 0;
      ImService.downstreamDbOpStartedForTest = (op) {
        if (op == 'upsertMessage(stream_finish_untracked)' &&
            !firstWriteStarted.isCompleted) {
          firstWriteStarted.complete();
        }
      };
      ImService.downstreamTaskFinishedForTest = (command) {
        if (command != 'stream_finish') return;
        finishedCount++;
        if (finishedCount == 1 && !oldTaskFinished.isCompleted) {
          oldTaskFinished.complete();
        }
        if (finishedCount == 2 && !validTaskFinished.isCompleted) {
          validTaskFinished.complete();
        }
      };

      try {
        await LocalDb.prepareSyncGeneration('stream-owner-pre-bootstrap');
        await LocalDb.markSyncBootstrapComplete(1, committedCursor: 0);
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[0].packets.any((packet) => packet['cmd'] == 'auth'),
        );
        downstreams[0].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
          }),
        );
        await _eventually(() => service.isAuthenticated);
        await _eventually(
          () =>
              sinks[0].packets.any((packet) => packet['cmd'] == 'sync_resume'),
        );
        service.setCurrentSessionForTest(sessionId);

        final blocked = LocalDb.blockQueueForTest(queueGate.future);
        await pumpEventQueue();
        downstreams[0].add(
          jsonEncode({
            'cmd': 'stream_finish',
            'payload': {
              'msg_id': 'account-a-finish',
              'session_id': sessionId,
              'sender_id': 'account-a-agent',
              'sender_type': 2,
              'final_content': 'must not enter account B',
              'created_at': 1700000001000,
            },
          }),
        );
        await firstWriteStarted.future;

        service.disconnect();
        final accountB = '${userId}_stream_owner_b';
        final switchAccount = LocalDb.setActiveUser(accountB);
        queueGate.complete();
        await blocked;
        await switchAccount;
        await oldTaskFinished.future;

        final bDb = await LocalDb.database;
        expect(
          await bDb.query(
            'messages',
            where: 'content = ?',
            whereArgs: ['must not enter account B'],
          ),
          isEmpty,
        );
        expect(
          await bDb.query(
            'sessions',
            where: 'last_message = ?',
            whereArgs: ['must not enter account B'],
          ),
          isEmpty,
        );
        expect(
          service.currentMessages.map((message) => message.msgId),
          isNot(contains('account-a-finish')),
        );

        ImService.downstreamDbOpStartedForTest = null;
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[1].packets.any((packet) => packet['cmd'] == 'auth'),
        );
        downstreams[1].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '2002', 'active_sync': 'v1'},
          }),
        );
        await _eventually(() => service.isAuthenticated);
        downstreams[1].add(
          jsonEncode({
            'cmd': 'stream_finish',
            'payload': {
              'msg_id': 'account-b-finish',
              'session_id': sessionId,
              'sender_id': 'account-b-agent',
              'sender_type': 2,
              'final_content': 'valid B finish',
              'created_at': 1700000002000,
            },
          }),
        );
        await validTaskFinished.future;
        expect(
          (await LocalDb.getLatestMessages(
            sessionId,
          )).map((row) => row['msg_id']),
          contains('account-b-finish'),
        );
        expect(
          service.currentMessages.map((message) => message.msgId),
          contains('account-b-finish'),
        );
      } finally {
        if (!queueGate.isCompleted) queueGate.complete();
        service.disconnect();
        for (final downstream in downstreams) {
          await downstream.close();
        }
      }
    },
  );

  test(
    'started pull sync cannot continue its projection after owner switch',
    () async {
      const sessionId = 'pull-owner-session';
      final sinks = [_RecordingSink(), _RecordingSink()];
      final downstreams = [
        StreamController<dynamic>.broadcast(),
        StreamController<dynamic>.broadcast(),
      ];
      var connectionIndex = 0;
      ImService.channelConnectorForTest = (_) {
        final index = connectionIndex++;
        return _FakeWebSocketChannel(
          stream: downstreams[index].stream,
          sink: sinks[index],
        );
      };
      sessionService.snapshotResult = const SessionSnapshotFetchResult(
        snapshots: [],
        success: true,
        cursor: 1,
      );
      final service = ImService();
      final queueGate = Completer<void>();
      final firstWriteStarted = Completer<void>();
      final oldTaskFinished = Completer<void>();
      final validTaskFinished = Completer<void>();
      var finishedCount = 0;
      ImService.downstreamDbOpStartedForTest = (op) {
        if (op == 'batchInsertMessages(pull_sync_resp)' &&
            !firstWriteStarted.isCompleted) {
          firstWriteStarted.complete();
        }
      };
      ImService.downstreamTaskFinishedForTest = (command) {
        if (command != 'pull_sync_resp') return;
        finishedCount++;
        if (finishedCount == 1 && !oldTaskFinished.isCompleted) {
          oldTaskFinished.complete();
        }
        if (finishedCount == 2 && !validTaskFinished.isCompleted) {
          validTaskFinished.complete();
        }
      };

      try {
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[0].packets.any((packet) => packet['cmd'] == 'auth'),
        );
        downstreams[0].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v1'},
          }),
        );
        await _eventually(() => service.isAuthenticated);
        service.setCurrentSessionForTest(sessionId);

        final blocked = LocalDb.blockQueueForTest(queueGate.future);
        await pumpEventQueue();
        downstreams[0].add(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': false,
              'messages': [
                {
                  'inbox_seq': 9,
                  'msg_id': 'account-a-pull-message',
                  'session_id': sessionId,
                  'sender_id': 'account-a-peer',
                  'sender_type': 1,
                  'msg_type': 1,
                  'content': 'must not project into B',
                  'created_at': 1700000001000,
                },
              ],
              'unread_snapshot': {sessionId: 5},
            },
          }),
        );
        await firstWriteStarted.future;

        service.disconnect();
        final accountB = '${userId}_pull_owner_b';
        final switchAccount = LocalDb.setActiveUser(accountB);
        queueGate.complete();
        await blocked;
        await switchAccount;
        await oldTaskFinished.future;

        final bDb = await LocalDb.database;
        expect(
          await bDb.query(
            'messages',
            where: 'content = ?',
            whereArgs: ['must not project into B'],
          ),
          isEmpty,
        );
        expect(
          await bDb.query(
            'sessions',
            where: 'last_message = ?',
            whereArgs: ['must not project into B'],
          ),
          isEmpty,
        );
        expect(service.resolvePullSyncCursorForTest(0), 0);
        expect(
          service.currentMessages.map((message) => message.msgId),
          isNot(contains('account-a-pull-message')),
        );
        expect(
          sinks[1].packets.where((packet) => packet['cmd'] == 'push_ack'),
          isEmpty,
        );

        await service.resetForAccountSwitch();
        ImService.downstreamDbOpStartedForTest = null;
        service.setCurrentSessionForTest(sessionId);
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[1].packets.any((packet) => packet['cmd'] == 'auth'),
        );
        downstreams[1].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '2002', 'active_sync': 'v1'},
          }),
        );
        await _eventually(() => service.isAuthenticated);
        downstreams[1].add(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': false,
              'messages': [
                {
                  'inbox_seq': 1,
                  'msg_id': 'account-b-pull-message',
                  'session_id': sessionId,
                  'sender_id': 'account-b-peer',
                  'sender_type': 1,
                  'msg_type': 1,
                  'content': 'valid B pull',
                  'created_at': 1700000002000,
                },
              ],
              'unread_snapshot': {sessionId: 0},
            },
          }),
        );
        await validTaskFinished.future;
        expect(
          (await LocalDb.getLatestMessages(
            sessionId,
          )).map((row) => row['msg_id']),
          contains('account-b-pull-message'),
        );
        expect(
          service.currentMessages.map((message) => message.msgId),
          contains('account-b-pull-message'),
        );
        expect(service.resolvePullSyncCursorForTest(0), 1);
      } finally {
        if (!queueGate.isCompleted) queueGate.complete();
        service.disconnect();
        for (final downstream in downstreams) {
          await downstream.close();
        }
      }
    },
  );

  test(
    'bootstrap snapshot loop cannot continue into a replacement account',
    () async {
      final firstPersisted = Completer<void>();
      final releaseFirst = Completer<void>();
      ImService.sessionSnapshotPersistedForTest = (sessionId) async {
        if (sessionId != 'account-a-first') return;
        firstPersisted.complete();
        await releaseFirst.future;
      };
      final completedFirstPageGate = Completer<void>()..complete();
      sessionService
        ..firstBootstrapPageStarted = Completer<void>()
        ..firstBootstrapPageGate = completedFirstPageGate
        ..firstBootstrapPageResult = SessionSnapshotFetchResult(
          snapshots: [
            _bootstrapSnapshot('account-a-first'),
            _bootstrapSnapshot('account-a-second'),
          ],
          success: true,
          cursor: 1700000000,
          syncHeadCursor: 50,
        );
      final service = ImService()..setActiveSyncModeForTest('v2');
      final oldBootstrap = service.handleSyncV2AuthSuccessForTest();

      await firstPersisted.future;
      await service.resetForAccountSwitch();
      final accountB = '${userId}_replacement';
      await LocalDb.setActiveUser(accountB);
      releaseFirst.complete();
      await oldBootstrap;

      final bDb = await LocalDb.database;
      expect(
        await bDb.query('sessions', where: "session_id LIKE 'account-a-%'"),
        isEmpty,
      );
      expect(await bDb.query('messages'), isEmpty);
      expect(await bDb.query('sync_entity_versions'), isEmpty);
      expect(await bDb.query('outbox'), isEmpty);
      expect((await LocalDb.getSyncState()).committedCursor, 0);
      expect(service.resolveSessionTypeById('account-a-second'), 'private');

      ImService.sessionSnapshotPersistedForTest = null;
      sessionService
        ..firstBootstrapPageGate = null
        ..firstBootstrapPageResult = null
        ..bootstrapPages.add(
          SessionSnapshotFetchResult(
            snapshots: [_bootstrapSnapshot('account-b-session')],
            success: true,
            cursor: 1700000001,
            syncHeadCursor: 70,
          ),
        );
      final sink = _RecordingSink();
      final downstream = StreamController<dynamic>();
      ImService.channelConnectorForTest = (_) =>
          _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
      service.connect('ws://127.0.0.1:1/ws');
      await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
      downstream.add(
        jsonEncode({
          'cmd': 'auth_ack',
          'payload': {'code': 0, 'user_id': '2002', 'active_sync': 'v2'},
        }),
      );
      await _eventually(
        () => sink.packets.any((p) => p['cmd'] == 'sync_resume'),
      );
      expect(
        (await LocalDb.getSessions()).map((row) => row['session_id']),
        contains('account-b-session'),
      );
      // Let post-auth background work finish while the test-owned services
      // are still registered.
      await pumpEventQueue(times: 20);
      service.disconnect();
      await downstream.close();
    },
  );

  test(
    'stale downstream work cannot write or ACK through a replacement account',
    () async {
      await LocalDb.enqueueOutboxCommand(
        commandId: 'account-a-mute',
        commandKind: 'session.mute',
        payload: {'session_id': 'shared-session', 'is_muted': true},
      );
      sessionService
        ..muteStarted = Completer<void>()
        ..muteGate = Completer<void>();
      final oldPushFinished = Completer<void>();
      ImService.downstreamTaskFinishedForTest = (command) {
        if (command == 'push_msg' && !oldPushFinished.isCompleted) {
          oldPushFinished.complete();
        }
      };
      final sinks = [_RecordingSink(), _RecordingSink()];
      final downstreams = [
        StreamController<dynamic>(),
        StreamController<dynamic>(),
      ];
      var connectionIndex = 0;
      ImService.channelConnectorForTest = (_) {
        final index = connectionIndex++;
        return _FakeWebSocketChannel(
          stream: downstreams[index].stream,
          sink: sinks[index],
        );
      };
      final service = ImService();

      try {
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[0].packets.any((p) => p['cmd'] == 'auth'),
        );
        downstreams[0].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v1'},
          }),
        );
        await sessionService.muteStarted!.future;
        downstreams[0].add(
          jsonEncode({
            'cmd': 'push_msg',
            'payload': {
              'msg_id': 'account-a-stale-message',
              'session_id': 'shared-session',
              'sender_id': 'account-a-peer',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'must not cross accounts',
              'created_at': 1700000001000,
              'inbox_seq': 1,
            },
          }),
        );
        await _eventually(
          () => sinks[0].packets.any(
            (packet) =>
                packet['cmd'] == 'push_ack' &&
                packet['payload']['msg_id'] == 'account-a-stale-message',
          ),
        );

        await service.resetForAccountSwitch();
        expect(service.syncOutboxFlushInFlightForTest, isFalse);
        final accountB = '${userId}_downstream_b';
        await LocalDb.setActiveUser(accountB);
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sinks[1].packets.any((p) => p['cmd'] == 'auth'),
        );
        downstreams[1].add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': '2002', 'active_sync': 'v1'},
          }),
        );
        await _eventually(() => service.isAuthenticated);
        service.setCurrentSessionForTest('shared-session');

        sessionService.muteGate!.complete();
        await oldPushFinished.future.timeout(const Duration(seconds: 5));

        expect(
          await LocalDb.getMessageByMsgId('account-a-stale-message'),
          isNull,
        );
        expect(
          service.currentMessages.map((message) => message.msgId),
          isNot(contains('account-a-stale-message')),
        );
        expect(
          sinks[1].packets.where(
            (packet) =>
                packet['cmd'] == 'push_ack' &&
                packet['payload']['msg_id'] == 'account-a-stale-message',
          ),
          isEmpty,
        );

        downstreams[1].add(
          jsonEncode({
            'cmd': 'push_msg',
            'payload': {
              'msg_id': 'account-b-live-message',
              'session_id': 'shared-session',
              'sender_id': 'account-b-peer',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'belongs to account B',
              'created_at': 1700000002000,
              'inbox_seq': 1,
            },
          }),
        );
        await _eventually(
          () => service.currentMessages.any(
            (message) => message.msgId == 'account-b-live-message',
          ),
        );
        expect(
          await LocalDb.getMessageByMsgId('account-b-live-message'),
          isNotNull,
        );
      } finally {
        if (!(sessionService.muteGate?.isCompleted ?? true)) {
          sessionService.muteGate!.complete();
        }
        service.disconnect();
        for (final downstream in downstreams) {
          await downstream.close();
        }
      }
    },
  );

  test(
    'SQLite reload preserves client identity so socket ACK replaces the temp row',
    () async {
      final harness = await _V2Harness.resume();
      final service = harness.service;
      service.setCurrentSessionForTest('ack-reload-session');
      final clientMsgId = await service.sendMessageWithClientId(
        'persisted before ACK',
        'ack-reload-session',
      );
      expect(clientMsgId, isNotNull);
      expect(service.activeSendAckTimerCountForTest, 1);

      ImService.sessionWindowCacheNowMsForTest = 1000;
      service.leaveSession('ack-reload-session');
      ImService.sessionWindowCacheNowMsForTest = 1000000000;
      service.enterSession('ack-reload-session');
      await _eventually(
        () =>
            service.isInitialHistoryReady && service.currentMessages.isNotEmpty,
      );
      expect(service.currentMessages, hasLength(1));
      expect(service.currentMessages.single.clientMsgId, clientMsgId);

      harness.downstream.add(
        jsonEncode({
          'cmd': 'send_ack',
          'payload': {
            'client_msg_id': clientMsgId,
            'msg_id': 'ack-reload-server-message',
            'created_at': 1700000003000,
            'inbox_seq': 1,
          },
        }),
      );
      await _eventually(
        () =>
            service.currentMessages.length == 1 &&
            service.currentMessages.single.msgId ==
                'ack-reload-server-message' &&
            service.currentMessages.single.status == 'success',
      );
      expect(await _outboxRows(clientMsgId!), isEmpty);
      expect(service.activeSendAckTimerCountForTest, 0);
      expect(await LocalDb.getMessageByLocalSeq(clientMsgId), isNull);
      expect(
        (await LocalDb.getLatestMessages(
          'ack-reload-session',
        )).map((row) => row['msg_id']).toList(),
        ['ack-reload-server-message'],
      );
      await harness.close();
    },
  );

  test(
    'production-shaped v2 receipt reconciles a real sendMessage with lost ACK',
    () async {
      const sessionId = 'lost-ack-session';
      final harness = await _V2Harness.resume();
      final service = harness.service;
      service.setCurrentSessionForTest(sessionId);

      try {
        final clientMsgId = await service.sendMessageWithClientId(
          'accepted before receipt',
          sessionId,
        );
        expect(clientMsgId, isNotNull);
        await _eventually(
          () => harness.sink.packets.any(
            (packet) =>
                packet['cmd'] == 'send_msg' &&
                packet['payload']['client_msg_id'] == clientMsgId,
          ),
        );
        expect(
          (await LocalDb.getMessageByLocalSeq(clientMsgId!))?['sender_id'],
          'me',
        );
        expect(service.activeSendAckTimerCountForTest, 1);

        harness.downstream.add(
          jsonEncode({
            'cmd': 'sync_batch',
            'payload': {
              'generation': harness.generation,
              'from_cursor': '0',
              'next_cursor': '1',
              'head_cursor': '1',
              'has_more': false,
              'events': [
                {
                  'cursor': '1',
                  'kind': 'message.upsert',
                  'entity_type': 'message',
                  'entity_id': 'lost-ack-server-message',
                  'entity_version': '1',
                  'command_id': clientMsgId,
                  'payload': {
                    'msg_id': 'lost-ack-server-message',
                    'session_id': sessionId,
                    'sender_id': userId,
                    'sender_type': 1,
                    'msg_type': 1,
                    'content': 'accepted before receipt',
                    'status': 'success',
                    'created_at': 1700000002000,
                  },
                },
              ],
            },
          }),
        );
        await _eventually(
          () => harness.sink.packets.any((p) => p['cmd'] == 'sync_ack'),
        );

        expect(await _outboxRows(clientMsgId), isEmpty);
        expect(service.activeSendAckTimerCountForTest, 0);
        expect(
          service.currentMessages.map((message) => message.msgId).toList(),
          ['lost-ack-server-message'],
        );
        expect(service.currentMessages.single.status, 'success');
        expect(
          (await LocalDb.getLatestMessages(
            sessionId,
          )).map((row) => row['msg_id']).toList(),
          ['lost-ack-server-message'],
        );
      } finally {
        await harness.close();
      }
    },
  );

  test(
    'older production receipt publishes the newer authoritative edit',
    () async {
      const clientMsgId = 'edited-receipt-command';
      const sessionId = 'edited-receipt-session';
      const serverMsgId = 'edited-receipt-server-message';
      await LocalDb.insertLocalStubWithOutbox(
        message: {
          'msg_id': 'temp_$clientMsgId',
          'session_id': sessionId,
          'sender_id': 'me',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'body as sent',
          'status': 'sending',
          'local_seq': clientMsgId,
          'created_at': 1700000001000,
        },
        commandId: clientMsgId,
        commandKind: 'send_msg',
        payload: {
          'session_id': sessionId,
          'client_msg_id': clientMsgId,
          'msg_type': 1,
          'content': 'body as sent',
        },
      );
      final harness = await _V2Harness.resume();
      try {
        harness.service.setCurrentSessionForTest(sessionId);
        await harness.service.loadInitialWindowForTest(sessionId);
        expect(harness.service.currentMessages.single.content, 'body as sent');

        // Archive/history has already persisted a newer edit, but deliberately
        // does not publish it into the active window. The stale send receipt
        // must reconcile from the final authoritative DB row.
        await LocalDb.applyArchiveMessages([
          {
            'msg_id': serverMsgId,
            'session_id': sessionId,
            'sender_id': userId,
            'sender_type': 1,
            'msg_type': 1,
            'content': 'edited authoritative body',
            'status': 'success',
            'state_version': '2',
            'created_at': 1700000003000,
          },
        ]);

        await harness.sendBatch(
          from: 0,
          next: 1,
          events: [
            {
              'cursor': '1',
              'kind': 'message.upsert',
              'entity_type': 'message',
              'entity_id': serverMsgId,
              'entity_version': '1',
              'command_id': clientMsgId,
              'payload': {
                'msg_id': serverMsgId,
                'session_id': sessionId,
                'sender_id': userId,
                'sender_type': 1,
                'msg_type': 1,
                'content': 'body as sent',
                'status': 'success',
                'created_at': 1700000002000,
              },
            },
          ],
        );

        expect(await _outboxRows(clientMsgId), isEmpty);
        expect(harness.service.activeSendAckTimerCountForTest, 0);
        expect(harness.service.currentMessages, hasLength(1));
        expect(harness.service.currentMessages.single.msgId, serverMsgId);
        expect(
          harness.service.currentMessages.single.content,
          'edited authoritative body',
        );
        final rows = await LocalDb.getLatestMessages(sessionId);
        expect(rows, hasLength(1));
        expect(rows.single['content'], 'edited authoritative body');
      } finally {
        await harness.close();
      }
    },
  );

  test(
    'older production receipt settles without reviving a newer revoke',
    () async {
      const clientMsgId = 'revoked-receipt-command';
      const sessionId = 'revoked-receipt-session';
      const serverMsgId = 'revoked-receipt-server-message';
      await LocalDb.insertLocalStubWithOutbox(
        message: {
          'msg_id': 'temp_$clientMsgId',
          'session_id': sessionId,
          'sender_id': 'me',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'body later revoked',
          'status': 'sending',
          'local_seq': clientMsgId,
          'created_at': 1700000001000,
        },
        commandId: clientMsgId,
        commandKind: 'send_msg',
        payload: {
          'session_id': sessionId,
          'client_msg_id': clientMsgId,
          'msg_type': 1,
          'content': 'body later revoked',
        },
      );
      final harness = await _V2Harness.resume();
      try {
        harness.service.setCurrentSessionForTest(sessionId);
        await harness.service.loadInitialWindowForTest(sessionId);
        expect(harness.service.currentMessages, hasLength(1));
        await LocalDb.applyArchiveMessages([
          {
            'msg_id': serverMsgId,
            'session_id': sessionId,
            'sender_id': userId,
            'sender_type': 1,
            'msg_type': 1,
            'content': '',
            'is_revoked': true,
            'state_version': '2',
            'created_at': 1700000003000,
          },
        ]);

        await harness.sendBatch(
          from: 0,
          next: 1,
          events: [
            {
              'cursor': '1',
              'kind': 'message.upsert',
              'entity_type': 'message',
              'entity_id': serverMsgId,
              'entity_version': '1',
              'command_id': clientMsgId,
              'payload': {
                'msg_id': serverMsgId,
                'session_id': sessionId,
                'sender_id': userId,
                'sender_type': 1,
                'msg_type': 1,
                'content': 'body later revoked',
                'status': 'success',
                'created_at': 1700000002000,
              },
            },
          ],
        );

        expect(await _outboxRows(clientMsgId), isEmpty);
        expect(harness.service.activeSendAckTimerCountForTest, 0);
        expect(harness.service.currentMessages, isEmpty);
        expect(await LocalDb.getLatestMessages(sessionId), isEmpty);

        // Duplicate delivery is an idempotent cursor advance and cannot
        // resurrect either the optimistic or canonical row.
        await harness.sendBatch(
          from: 1,
          next: 2,
          events: [
            {
              'cursor': '2',
              'kind': 'message.upsert',
              'entity_type': 'message',
              'entity_id': serverMsgId,
              'entity_version': '1',
              'command_id': clientMsgId,
              'payload': {
                'msg_id': serverMsgId,
                'session_id': sessionId,
                'sender_id': userId,
                'sender_type': 1,
                'msg_type': 1,
                'content': 'body later revoked',
                'status': 'success',
                'created_at': 1700000002000,
              },
            },
          ],
        );
        expect(harness.service.currentMessages, isEmpty);
        expect(await LocalDb.getLatestMessages(sessionId), isEmpty);

        // A deduplicated send can still produce its normal ACK after the
        // receipt. It must share the same authoritative settlement rule and
        // remain a no-op once the command has already been reconciled.
        await harness.service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'send_ack',
            'payload': {
              'client_msg_id': clientMsgId,
              'msg_id': serverMsgId,
              'created_at': 1700000002000,
              'inbox_seq': 1,
            },
          }),
        );
        expect(await _outboxRows(clientMsgId), isEmpty);
        expect(harness.service.activeSendAckTimerCountForTest, 0);
        expect(harness.service.currentMessages, isEmpty);
        expect(await LocalDb.getLatestMessages(sessionId), isEmpty);
      } finally {
        await harness.close();
      }
    },
  );

  test('normal send ACK cannot overwrite a newer authoritative edit', () async {
    const sessionId = 'late-ack-edit-session';
    const serverMsgId = 'late-ack-edit-server-message';
    final harness = await _V2Harness.resume();
    try {
      harness.service.setCurrentSessionForTest(sessionId);
      final clientMsgId = await harness.service.sendMessageWithClientId(
        'body as sent',
        sessionId,
      );
      expect(clientMsgId, isNotNull);
      await LocalDb.applyArchiveMessages([
        {
          'msg_id': serverMsgId,
          'session_id': sessionId,
          'sender_id': userId,
          'sender_type': 1,
          'msg_type': 1,
          'content': 'newer authoritative edit',
          'status': 'success',
          'state_version': '2',
          'created_at': 1700000003000,
        },
      ]);

      harness.downstream.add(
        jsonEncode({
          'cmd': 'send_ack',
          'payload': {
            'client_msg_id': clientMsgId,
            'msg_id': serverMsgId,
            'created_at': 1700000002000,
            'inbox_seq': 1,
          },
        }),
      );
      await _eventually(
        () => harness.service.activeSendAckTimerCountForTest == 0,
      );

      expect(harness.service.currentMessages, hasLength(1));
      expect(harness.service.currentMessages.single.msgId, serverMsgId);
      expect(
        harness.service.currentMessages.single.content,
        'newer authoritative edit',
      );
      expect(await _outboxRows(clientMsgId!), isEmpty);
      expect(
        (await LocalDb.getLatestMessages(sessionId)).single['content'],
        'newer authoritative edit',
      );
    } finally {
      await harness.close();
    }
  });

  test(
    'late ACK cannot revive a newer revoke and invalidates an inactive cache',
    () async {
      const sessionId = 'late-ack-revoke-session';
      const serverMsgId = 'late-ack-revoke-server-message';
      final harness = await _V2Harness.resume();
      final dbGate = Completer<void>();
      Future<void>? blockedDb;
      try {
        harness.service.setCurrentSessionForTest(sessionId);
        final clientMsgId = await harness.service.sendMessageWithClientId(
          'body later revoked',
          sessionId,
        );
        expect(clientMsgId, isNotNull);
        harness.service.leaveSession(sessionId);
        expect(
          harness.service.cachedSessionWindowIdsForTest,
          contains(sessionId),
        );
        await LocalDb.applyArchiveMessages([
          {
            'msg_id': serverMsgId,
            'session_id': sessionId,
            'sender_id': userId,
            'sender_type': 1,
            'msg_type': 1,
            'content': '',
            'is_revoked': true,
            'state_version': '2',
            'created_at': 1700000003000,
          },
        ]);

        harness.downstream.add(
          jsonEncode({
            'cmd': 'send_ack',
            'payload': {
              'client_msg_id': clientMsgId,
              'msg_id': serverMsgId,
              'created_at': 1700000002000,
              'inbox_seq': 1,
            },
          }),
        );
        await _eventually(
          () => harness.service.activeSendAckTimerCountForTest == 0,
        );
        expect(await _outboxRows(clientMsgId!), isEmpty);
        expect(await LocalDb.getLatestMessages(sessionId), isEmpty);

        blockedDb = LocalDb.blockQueueForTest(dbGate.future);
        await pumpEventQueue();
        harness.service.enterSession(sessionId);
        expect(
          harness.service.currentMessages,
          isEmpty,
          reason: 'the first cached frame must not revive the pending body',
        );
      } finally {
        if (!dbGate.isCompleted) dbGate.complete();
        await blockedDb;
        await harness.close();
      }
    },
  );

  test(
    'v2 batch stays visible before render gate and survives initial snapshot',
    () async {
      const sessionId = 'v2-render-gate-session';
      await LocalDb.upsertMessage({
        'msg_id': 'render-gate-old',
        'session_id': sessionId,
        'sender_id': 'peer',
        'sender_type': 1,
        'msg_type': 1,
        'content': 'old local row',
        'created_at': 1700000001000,
      });
      final harness = await _V2Harness.resume();
      final dbBlocker = Completer<void>();
      final blocked = LocalDb.blockQueueForTest(dbBlocker.future);
      await pumpEventQueue();
      final renderGate = Completer<void>();
      harness.service.enterSession(sessionId, renderGate: renderGate.future);
      final batch = harness.sendBatch(
        from: 0,
        next: 1,
        events: [
          {
            'cursor': '1',
            'kind': 'message.upsert',
            'entity_type': 'message',
            'entity_id': 'render-gate-live',
            'entity_version': '1',
            'payload': {
              'msg_id': 'render-gate-live',
              'session_id': sessionId,
              'sender_id': 'peer',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'live while route gate is pending',
              'created_at': 1700000002000,
            },
          },
        ],
      );
      dbBlocker.complete();
      await blocked;
      await batch;
      expect(
        harness.service.currentMessages.map((message) => message.msgId),
        contains('render-gate-live'),
      );
      expect(harness.service.isInitialHistoryReady, isFalse);

      renderGate.complete();
      await _eventually(() => harness.service.isInitialHistoryReady);
      expect(
        harness.service.currentMessages.map((message) => message.msgId).toSet(),
        {'render-gate-old', 'render-gate-live'},
      );
      await harness.close();
    },
  );

  test(
    'cold reconnect resumes bootstrap from the persisted keyset cursor',
    () async {
      ImService.syncV2BootstrapRetryDelayForTest = const Duration(hours: 1);
      sessionService.bootstrapPages.addAll([
        SessionSnapshotFetchResult(
          snapshots: [
            _bootstrapSnapshot('session-a'),
            _bootstrapSnapshot('session-b'),
          ],
          success: true,
          hasMore: true,
          cursor: 1700000000,
          syncHeadCursor: 73,
          bootstrapCursor: 'session-b',
        ),
        const SessionSnapshotFetchResult(
          snapshots: [],
          success: false,
          networkError: true,
        ),
      ]);

      final firstSink = _RecordingSink();
      final firstDownstream = StreamController<dynamic>();
      ImService.channelConnectorForTest = (_) => _FakeWebSocketChannel(
        stream: firstDownstream.stream,
        sink: firstSink,
      );
      final firstService = ImService();
      firstService.connect('ws://127.0.0.1:1/ws');
      await _eventually(() => firstSink.packets.any((p) => p['cmd'] == 'auth'));
      firstDownstream.add(
        jsonEncode({
          'cmd': 'auth_ack',
          'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
        }),
      );
      await _eventually(() => sessionService.snapshotFetches == 2);
      final interruptedState = await LocalDb.getSyncState();
      expect(interruptedState.bootstrapInProgress, isTrue);
      expect(interruptedState.bootstrapPageCursor, 'session-b');
      expect(interruptedState.bootstrapHeadCursor, 73);
      firstService.disconnect();
      await firstDownstream.close();

      sessionService.bootstrapPages.add(
        SessionSnapshotFetchResult(
          snapshots: [_bootstrapSnapshot('session-c')],
          success: true,
          cursor: 1700009999,
          syncHeadCursor: 73,
        ),
      );
      final secondSink = _RecordingSink();
      final secondDownstream = StreamController<dynamic>();
      ImService.channelConnectorForTest = (_) => _FakeWebSocketChannel(
        stream: secondDownstream.stream,
        sink: secondSink,
      );
      final secondService = ImService();
      secondService.connect('ws://127.0.0.1:1/ws');
      await _eventually(
        () => secondSink.packets.any((p) => p['cmd'] == 'auth'),
      );
      secondDownstream.add(
        jsonEncode({
          'cmd': 'auth_ack',
          'payload': {'code': 0, 'user_id': '1001', 'active_sync': 'v2'},
        }),
      );
      await _eventually(
        () =>
            secondSink.packets.any((packet) => packet['cmd'] == 'sync_resume'),
      );

      expect(sessionService.bootstrapPageRequests.last, {
        'limit': 200,
        'cursor': 'session-b',
        'head_cursor': 73,
      });
      expect(
        (await LocalDb.getSessions()).map((row) => row['session_id']).toSet(),
        {'session-a', 'session-b', 'session-c'},
      );
      final completedState = await LocalDb.getSyncState();
      expect(completedState.bootstrapInProgress, isFalse);
      expect(completedState.bootstrapCursor, 1700000000);
      expect(completedState.committedCursor, 73);

      secondService.disconnect();
      await secondDownstream.close();
    },
  );

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

  test('bootstrap persists snapshot recent_messages into LocalDb', () async {
    sessionService.snapshotSyncHeadCursor = 42;
    sessionService.bootstrapSnapshots = [
      const SessionSnapshot(
        sessionId: 'boot-session',
        title: 'Boot',
        type: 'private',
        peerId: '1002',
        peerType: 1,
        peerNickname: 'Peer',
        peerUsername: '',
        updatedAt: 1700000000000,
        unreadCount: 1,
        lastMessage: 'first hello',
        lastMessageTime: 1700000000000,
        recentMessages: [
          {
            'msg_id': '7001',
            'session_id': 'boot-session',
            'sender_id': '1002',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'first hello',
            'created_at': 1700000000000,
            'state_version': '3',
          },
          {
            'msg_id': '7000',
            'session_id': 'boot-session',
            'sender_id': '1001',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'earlier',
            'created_at': 1699999999000,
            'state_version': '1',
          },
        ],
      ),
    ];
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

    // Pre-head messages land with the snapshot, in chronological order.
    final messages = await LocalDb.getLatestMessages('boot-session');
    expect(messages.map((row) => row['msg_id'].toString()), ['7000', '7001']);
    expect(messages.last['content'], 'first hello');
    // The bootstrap cursor commit is unchanged by the message writes.
    expect((await LocalDb.getSyncState()).committedCursor, 42);
    final bootSession = service.sessions.singleWhere(
      (session) => session.sessionId == 'boot-session',
    );
    expect(bootSession.lastMessage, 'first hello');

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

  test(
    'old owner terminal outbox rejection cannot roll back the new account',
    () async {
      const sessionId = 'shared-terminal-session';
      const commandId = 'account-a-terminal-mute';
      final accountA = userId;
      final accountB = '${userId}_terminal_b';
      await LocalDb.setActiveUser(accountB);
      await LocalDb.upsertSession({
        'session_id': sessionId,
        'title': 'Account B',
        'type': 'group',
        'is_muted': 1,
        'updated_at': 1700000005000,
      });
      await LocalDb.setActiveUser(accountA);
      await LocalDb.upsertSession({
        'session_id': sessionId,
        'title': 'Account A',
        'type': 'group',
        'is_muted': 0,
        'updated_at': 1700000000000,
      });
      await LocalDb.applySessionCommandWithOutbox(
        sessionId: sessionId,
        sessionValues: {'is_muted': 1},
        commandId: commandId,
        commandKind: 'session.mute',
        payload: {
          'session_id': sessionId,
          'is_muted': true,
          'previous_is_muted': false,
        },
      );
      await LocalDb.prepareSyncGeneration('pre-bootstrap');
      await LocalDb.markSyncBootstrapComplete(1, committedCursor: 0);
      sessionService.rejectMuteAsTerminal = true;
      sessionService.muteStarted = Completer<void>();
      sessionService.muteGate = Completer<void>();
      final sink = _RecordingSink();
      final downstream = StreamController<dynamic>();
      ImService.channelConnectorForTest = (_) =>
          _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
      final service = ImService();
      final dbGate = Completer<void>();
      Future<void>? blockedDb;
      try {
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(() => sink.packets.any((p) => p['cmd'] == 'auth'));
        downstream.add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': accountA, 'active_sync': 'v2'},
          }),
        );
        await sessionService.muteStarted!.future;

        blockedDb = LocalDb.blockQueueForTest(dbGate.future);
        await pumpEventQueue();
        final switchAccount = LocalDb.setActiveUser(accountB);
        sessionService.muteGate!.complete();
        await pumpEventQueue(times: 10);
        dbGate.complete();
        await blockedDb;
        await switchAccount;
        await pumpEventQueue(times: 20);

        final session = (await LocalDb.getSessions()).singleWhere(
          (row) => row['session_id'] == sessionId,
        );
        expect(
          session['is_muted'],
          1,
          reason: 'A rejection must not apply its previous value to B',
        );
        expect(await LocalDb.getPendingOutboxCommands(), isEmpty);

        await LocalDb.setActiveUser(accountA);
        final accountASession = (await LocalDb.getSessions()).singleWhere(
          (row) => row['session_id'] == sessionId,
        );
        expect(accountASession['is_muted'], 1);
        expect(await _outboxState(commandId), 'pending');
      } finally {
        if (!(sessionService.muteGate?.isCompleted ?? true)) {
          sessionService.muteGate!.complete();
        }
        if (!dbGate.isCompleted) dbGate.complete();
        await blockedDb;
        service.disconnect();
        await downstream.close();
      }
    },
  );

  test(
    'public session reload cannot publish an A snapshot after B loaded',
    () async {
      const sessionId = 'shared-reload-session';
      final accountA = userId;
      final accountB = '${userId}_reload_b';
      await LocalDb.upsertSession({
        'session_id': sessionId,
        'title': 'Account A title',
        'type': 'group',
        'updated_at': 1700000001000,
      });
      await LocalDb.setActiveUser(accountB);
      await LocalDb.upsertSession({
        'session_id': sessionId,
        'title': 'Account B title',
        'type': 'group',
        'updated_at': 1700000002000,
      });
      await LocalDb.upsertMessage({
        'msg_id': 'account-b-preview',
        'session_id': sessionId,
        'sender_id': 'peer-b',
        'sender_type': 1,
        'msg_type': 1,
        'content': 'Account B preview',
        'created_at': 1700000002000,
      });
      await LocalDb.setActiveUser(accountA);
      authService.userIdValue = accountA;

      final firstRead = Completer<void>();
      final releaseA = Completer<void>();
      var blockedA = false;
      ImService.ownershipAwaitGateForTest = (stage) async {
        if (stage != 'load_sessions_rows' || blockedA) return;
        blockedA = true;
        firstRead.complete();
        await releaseA.future;
      };
      final service = ImService();
      final publishedTitles = <String>[];
      final worker = ever(service.sessions, (sessions) {
        publishedTitles.addAll(sessions.map((session) => session.title));
      });
      try {
        final staleLoad = service.loadSessions(refreshFromServer: false);
        await firstRead.future;
        await LocalDb.setActiveUser(accountB);
        authService.userIdValue = accountB;
        await service.loadSessions(refreshFromServer: false);
        expect(service.sessions.single.title, 'Account B title');

        releaseA.complete();
        await staleLoad;
        expect(
          service.sessions.single.title,
          'Account B title',
          reason: 'the completed A read must not overwrite B publication',
        );
        expect(publishedTitles.last, 'Account B title');
      } finally {
        worker.dispose();
        if (!releaseA.isCompleted) releaseA.complete();
        service.disconnect();
      }
    },
  );

  for (final variant in const [
    (
      label: 'deleted',
      keyPrefix: 'deleted_sessions_',
      gate: 'deleted_sessions_prefs_loaded',
    ),
    (
      label: 'revoked',
      keyPrefix: 'revoked_sessions_',
      gate: 'revoked_sessions_prefs_loaded',
    ),
  ]) {
    test(
      'stale ${variant.label} loader cannot replace the new account cache',
      () async {
        final accountA = userId;
        final accountB = '${userId}_${variant.label}_b';
        final aSession = '${variant.label}-only-a';
        final bSession = '${variant.label}-only-b';
        final prefs = await SharedPreferences.getInstance();
        await prefs.setString(
          '${variant.keyPrefix}$accountA',
          jsonEncode({aSession: 1700000001000}),
        );
        await prefs.setString(
          '${variant.keyPrefix}$accountB',
          jsonEncode({bSession: 1700000002000}),
        );

        final prefsRead = Completer<void>();
        final releaseA = Completer<void>();
        var blockedA = false;
        ImService.ownershipAwaitGateForTest = (stage) async {
          if (stage != variant.gate || blockedA) return;
          blockedA = true;
          prefsRead.complete();
          await releaseA.future;
        };
        final service = ImService();
        try {
          final staleLoad = service.loadSessionsForCurrentUser();
          await prefsRead.future;
          await LocalDb.setActiveUser(accountB);
          authService.userIdValue = accountB;
          await service.loadSessionsForCurrentUser();

          bool contains(String sessionId) => variant.label == 'deleted'
              ? service.isSessionLocallyDeletedForTest(sessionId)
              : service.isSessionLocallyRevokedForTest(sessionId);
          expect(contains(bSession), isTrue);
          expect(contains(aSession), isFalse);

          releaseA.complete();
          await staleLoad;
          expect(contains(bSession), isTrue);
          expect(
            contains(aSession),
            isFalse,
            reason: 'the A prefs result must not publish into B memory',
          );
        } finally {
          if (!releaseA.isCompleted) releaseA.complete();
          service.disconnect();
        }
      },
    );
  }

  test(
    'stale inbox cursor load cannot overwrite B or consume its push',
    () async {
      final accountB = '${userId}_cursor_b';
      await LocalDb.upsertMessage({
        'msg_id': 'cursor-a-existing',
        'session_id': 'cursor-a-session',
        'sender_id': 'peer-a',
        'sender_type': 1,
        'msg_type': 1,
        'content': 'A existing',
        'inbox_seq': 50,
        'created_at': 1700000001000,
      });
      final maxRead = Completer<void>();
      final releaseA = Completer<void>();
      var blockedA = false;
      ImService.ownershipAwaitGateForTest = (stage) async {
        if (stage != 'inbox_max_loaded' || blockedA) return;
        blockedA = true;
        maxRead.complete();
        await releaseA.future;
      };
      final service = ImService();
      try {
        final stalePush = service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'push_msg',
            'payload': {
              'msg_id': 'cursor-a-stale',
              'session_id': 'cursor-a-session',
              'sender_id': 'peer-a',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'stale A push',
              'inbox_seq': 51,
              'created_at': 1700000002000,
            },
          }),
        );
        await maxRead.future;

        await LocalDb.setActiveUser(accountB);
        authService.userIdValue = accountB;
        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'push_msg',
            'payload': {
              'msg_id': 'cursor-b-live',
              'session_id': 'cursor-b-session',
              'sender_id': 'peer-b',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'live B push',
              'inbox_seq': 2,
              'created_at': 1700000003000,
            },
          }),
        );
        expect(service.lastInboxSeqCursorForTest, 2);

        releaseA.complete();
        await stalePush;
        expect(service.lastInboxSeqCursorForTest, 2);
        expect(
          (await LocalDb.getLatestMessages(
            'cursor-b-session',
          )).map((row) => row['msg_id']),
          ['cursor-b-live'],
        );
        expect(await LocalDb.getLatestMessages('cursor-a-session'), isEmpty);
        await _eventuallyAsync(() async {
          final currentPrefs = await SharedPreferences.getInstance();
          return currentPrefs.getInt('inbox_seq_cursor_$accountB') == 2;
        });
      } finally {
        if (!releaseA.isCompleted) releaseA.complete();
        service.disconnect();
      }
    },
  );

  test(
    'live v2 delta cannot combine an A session row with a B preview',
    () async {
      const sessionId = 'shared-delta-session';
      final accountB = '${userId}_delta_b';
      final harness = await _V2Harness.resume();
      await harness.sendBatch(from: 0, next: 0, events: const []);
      final rowRead = Completer<void>();
      final releaseDelta = Completer<void>();
      ImService.ownershipAwaitGateForTest = (stage) async {
        if (stage != 'sync_v2_delta_session_row:$sessionId') return;
        if (!rowRead.isCompleted) rowRead.complete();
        await releaseDelta.future;
      };
      try {
        final acksBefore = harness.acks;
        final applying = harness.service.applySyncV2BatchForTest({
          'generation': harness.generation,
          'from_cursor': '0',
          'next_cursor': '1',
          'head_cursor': '1',
          'has_more': false,
          'events': [
            {
              'cursor': '1',
              'kind': 'session.upsert',
              'entity_type': 'session',
              'entity_id': sessionId,
              'entity_version': '1',
              'payload': {
                'session_id': sessionId,
                'title': 'Account A delta title',
                'type': 'group',
                'updated_at': 1700000001000,
              },
            },
          ],
        });
        await rowRead.future;
        await LocalDb.setActiveUser(accountB);
        authService.userIdValue = accountB;
        await LocalDb.upsertSession({
          'session_id': sessionId,
          'title': 'Account B title',
          'type': 'group',
          'updated_at': 1700000002000,
        });
        await LocalDb.upsertMessage({
          'msg_id': 'account-b-delta-preview',
          'session_id': sessionId,
          'sender_id': 'peer-b',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'Account B delta preview',
          'created_at': 1700000002000,
        });
        releaseDelta.complete();
        await applying;

        expect(
          harness.service.sessions.where(
            (session) => session.sessionId == sessionId,
          ),
          isEmpty,
          reason: 'the stale A delta must not publish into B memory',
        );
        expect(
          harness.acks,
          acksBefore,
          reason: 'no stale ACK on the old owner',
        );
        await harness.service.loadSessions(refreshFromServer: false);
        expect(harness.service.sessions.single.title, 'Account B title');
        expect(
          harness.service.sessions.single.lastMessage,
          'Account B delta preview',
        );
      } finally {
        if (!releaseDelta.isCompleted) releaseDelta.complete();
        await harness.close();
      }
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
    'v1 auth resend preserves restricted type and visibility fields',
    () async {
      const sessionId = 'v1-restricted-resend-session';
      const clientMsgId = 'v1-restricted-resend-command';
      await LocalDb.insertLocalStubWithOutbox(
        message: {
          'msg_id': 'temp_$clientMsgId',
          'session_id': sessionId,
          'sender_id': 'me',
          'sender_type': 1,
          'msg_type': 7,
          'content': 'restricted reconnect body',
          'extra': {'scope': 'restricted'},
          'quoted_message_id': 'quoted-v1-message',
          'visible_to': ['2001', '2002'],
          'status': 'sending',
          'local_seq': clientMsgId,
          'created_at': 1700000000000,
        },
        commandId: clientMsgId,
        commandKind: 'send_msg',
        payload: {
          'session_id': sessionId,
          'client_msg_id': clientMsgId,
          'msg_type': 7,
          'content': 'restricted reconnect body',
          'extra': {'scope': 'restricted'},
          'quoted_message_id': 'quoted-v1-message',
          'visible_to': ['2001', '2002'],
        },
      );
      final sink = _RecordingSink();
      final downstream = StreamController<dynamic>();
      ImService.channelConnectorForTest = (_) =>
          _FakeWebSocketChannel(stream: downstream.stream, sink: sink);
      final service = ImService();
      try {
        service.setCurrentSessionForTest(sessionId);
        await service.loadInitialWindowForTest(sessionId);
        service.connect('ws://127.0.0.1:1/ws');
        await _eventually(
          () => sink.packets.any((packet) => packet['cmd'] == 'auth'),
        );
        downstream.add(
          jsonEncode({
            'cmd': 'auth_ack',
            'payload': {'code': 0, 'user_id': userId, 'active_sync': 'v1'},
          }),
        );
        await _eventually(
          () =>
              sink.packets
                  .where(
                    (packet) =>
                        packet['cmd'] == 'send_msg' &&
                        packet['payload']['client_msg_id'] == clientMsgId,
                  )
                  .length >=
              2,
        );
        final sends = sink.packets.where(
          (packet) =>
              packet['cmd'] == 'send_msg' &&
              packet['payload']['client_msg_id'] == clientMsgId,
        );
        expect(sends, isNotEmpty);
        for (final packet in sends) {
          expect(packet['payload']['msg_type'], 7);
          expect(packet['payload']['extra'], {'scope': 'restricted'});
          expect(packet['payload']['quoted_message_id'], 'quoted-v1-message');
          expect(packet['payload']['visible_to'], ['2001', '2002']);
        }
      } finally {
        service.disconnect();
        await downstream.close();
      }
    },
  );

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

  test('v2 resume declares compound_v1 after sync_v2', () async {
    final harness = await _V2Harness.resume();
    final resume = harness.sink.packets.firstWhere(
      (p) => p['cmd'] == 'sync_resume',
    );

    expect(resume['payload']['capabilities'], ['sync_v2', 'compound_v1']);

    await harness.close();
  });

  test(
    'live compound event re-projects its session without a full reload',
    () async {
      const sid = '0b6a4c1e-5d2f-4e7a-8c39-2f1d6b8e4a70';
      await LocalDb.upsertSession({
        'session_id': sid,
        'title': 'Compound room',
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
            'entity_id': sid,
            'entity_version': '6',
            'payload': {
              'session_id': sid,
              'unread_count': 1,
              'last_read_msg_id': 2102896936847151104,
              'state_version': 6,
            },
          },
        ],
        unreadSnapshot: {sid: 1},
      );
      await _eventually(
        () => service.sessionsLoadTick.value == tickBeforeCatchUp + 1,
      );
      final tickAfterCatchUp = service.sessionsLoadTick.value;

      // One compound event spanning cursors 2..4, and no final snapshot, so
      // the unread shown can only come from the nested object.
      await harness.sendBatch(
        from: 1,
        next: 4,
        events: [
          {
            'cursor': '4',
            'first_cursor': '2',
            'kind': 'message.upsert',
            'entity_type': 'message',
            'entity_id': '2102897316570075136',
            'entity_version': '1',
            'payload': {
              'msg_id': '2102897316570075136',
              'session_id': sid,
              'sender_id': '1002',
              'sender_type': 2,
              'msg_type': 1,
              'content': 'compound live',
              'extra': <String, dynamic>{},
              'is_deleted': false,
              'is_revoked': false,
              'state_version': '1',
              'created_at': '2026-09-24T07:06:04.976123+08:00',
              'session': {
                'session_id': sid,
                'owner_id': '1001',
                'session_type': 2,
                'group_name': 'Compound room',
                'allow_member_invite': true,
                'all_members_muted': false,
                'last_msg_id': '2102897316570075136',
                'last_msg_summary': 'compound live',
                'moderation_status': 1,
                'banned_reason': '',
                'is_deleted': false,
                'state_version': '9',
                'created_at': '2026-09-01T08:00:00+08:00',
                'updated_at': '2026-09-24T07:06:04.976123+08:00',
              },
              'unread': {
                'unread_count': 3,
                'last_read_msg_id': 2102896936847151104,
                'state_version': 7,
              },
            },
          },
        ],
      );

      final live = service.sessions.singleWhere((s) => s.sessionId == sid);
      expect(live.unreadCount, 3);
      expect(live.lastMessage, 'compound live');
      expect(service.notificationUnread, 3);
      expect((await LocalDb.getSyncState()).committedCursor, 4);
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(service.sessionsLoadTick.value, tickAfterCatchUp);

      await harness.close();
    },
  );
}
