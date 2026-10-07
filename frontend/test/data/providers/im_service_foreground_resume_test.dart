import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
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
}

class _RecordingSink implements WebSocketSink {
  final List<Map<String, dynamic>> packets = [];
  bool hangOnClose = false;

  @override
  void add(dynamic data) {
    packets.add(jsonDecode(data as String) as Map<String, dynamic>);
  }

  @override
  Future<void> close([int? closeCode, String? closeReason]) =>
      hangOnClose ? Completer<void>().future : Future<void>.value();

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _FakeChannel implements WebSocketChannel {
  final downstream = StreamController<dynamic>();

  @override
  final _RecordingSink sink = _RecordingSink();

  @override
  Stream<dynamic> get stream => downstream.stream;

  @override
  Future<void> get ready => Future<void>.value();

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);

  List<Map<String, dynamic>> get resumes =>
      sink.packets.where((p) => p['cmd'] == 'sync_resume').toList();

  void receive(String cmd, Map<String, dynamic> payload) {
    downstream.add(jsonEncode({'cmd': cmd, 'payload': payload}));
  }
}

Future<void> _eventually(bool Function() condition) async {
  final deadline = DateTime.now().add(const Duration(seconds: 1));
  while (!condition() && DateTime.now().isBefore(deadline)) {
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  expect(condition(), isTrue);
}

Future<void> _finishCatchUp(_FakeChannel channel, {int cursor = 7}) async {
  final acks = channel.sink.packets.where((p) => p['cmd'] == 'sync_ack').length;
  channel.receive('sync_batch', {
    'generation': channel.resumes.last['payload']['generation'],
    'from_cursor': '$cursor',
    'next_cursor': '$cursor',
    'head_cursor': '$cursor',
    'has_more': false,
    'events': <Map<String, dynamic>>[],
    'final_state_snapshot': {'unread_by_session': <String, int>{}},
  });
  await _eventually(
    () =>
        channel.sink.packets.where((p) => p['cmd'] == 'sync_ack').length > acks,
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late ImService service;
  late List<_FakeChannel> channels;
  late int nowMs;
  final originalClock = ImService.nowMsProvider;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    nowMs = 100000;
    ImService.nowMsProvider = () => nowMs;
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(
      'foreground_resume_${DateTime.now().microsecondsSinceEpoch}',
    );
    await LocalDb.prepareSyncGeneration('seed');
    await LocalDb.markSyncBootstrapComplete(1, committedCursor: 7);
    Get.put<AuthService>(_FakeAuthService());
    channels = [];
    ImService.channelConnectorForTest = (_) {
      final channel = _FakeChannel();
      channels.add(channel);
      return channel;
    };
    service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await _eventually(
      () =>
          channels.isNotEmpty &&
          channels.first.sink.packets.any((p) => p['cmd'] == 'auth'),
    );
  });

  tearDown(() async {
    service.onClose();
    for (final channel in channels) {
      await channel.downstream.close();
    }
    ImService.channelConnectorForTest = null;
    ImService.nowMsProvider = originalClock;
    debugDefaultTargetPlatformOverride = null;
    Get.reset();
    await LocalDb.setActiveUser(null);
  });

  Future<void> authenticate(_FakeChannel channel) async {
    channel.receive('auth_ack', {
      'code': 0,
      'user_id': '1001',
      'active_sync': 'v2',
    });
    await _eventually(() => channel.resumes.length == 1);
  }

  test('Android recycles a silent writable socket within one second', () async {
    final channel = channels.single;
    // This channel accepts all writes and never produces any response, even
    // to auth or re_auth. Its close also hangs, as a frozen transport can do.
    channel.sink.hangOnClose = true;
    expect(service.isConnected, isTrue);
    service.setRealtimeAppState('background');
    nowMs += 4000;
    // hidden followed by paused must not reset the original background time.
    service.setRealtimeAppState('background');
    nowMs += 1000;
    final elapsed = Stopwatch()..start();
    service.setRealtimeAppState('foreground');
    await _eventually(() => channels.length == 2);
    expect(elapsed.elapsed, lessThan(const Duration(seconds: 1)));
    expect(channel.sink.packets.where((p) => p['cmd'] == 're_auth'), isEmpty);
  });

  test(
    'Android recycles an authenticated socket silent after background',
    () async {
      final channel = channels.single;
      await authenticate(channel);
      await _finishCatchUp(channel);
      channel.sink.hangOnClose = true;
      service.setRealtimeAppState('background');
      nowMs += const Duration(minutes: 5).inMilliseconds;
      service.setRealtimeAppState('foreground');
      await _eventually(() => channels.length == 2);
      await authenticate(channels.last);
      expect(channels.last.resumes, hasLength(1));
      expect(channels.last.resumes.single['payload']['committed_cursor'], '7');
    },
  );

  test(
    'healthy foreground resume uses the current cursor and is throttled',
    () async {
      final channel = channels.single;
      await authenticate(channel);
      await _finishCatchUp(channel);
      final originalGeneration =
          channel.resumes.single['payload']['generation'];
      // Move the durable cursor after auth so the resume cannot use a cached
      // bootstrap position. This live message must commit and ACK first.
      channel.receive('sync_batch', {
        'generation': originalGeneration,
        'from_cursor': '7',
        'next_cursor': '8',
        'head_cursor': '8',
        'has_more': false,
        'events': [
          {
            'cursor': '8',
            'kind': 'message.upsert',
            'entity_type': 'message',
            'entity_id': '801',
            'entity_version': '1',
            'payload': {
              'msg_id': '801',
              'session_id': 'session-8',
              'sender_id': '1002',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'live before background',
              'created_at': 1700000000000,
            },
          },
        ],
      });
      await _eventually(
        () => channel.sink.packets.any(
          (p) =>
              p['cmd'] == 'sync_ack' && p['payload']['committed_cursor'] == '8',
        ),
      );
      nowMs += 5000;
      service.setRealtimeAppState('background');
      nowMs += 1000;
      service.setRealtimeAppState('foreground');
      await _eventually(() => channel.resumes.length == 2);
      final resume = channel.resumes.last['payload'];
      expect(resume['committed_cursor'], '8');
      expect(resume['generation'], isNot(originalGeneration));
      expect((await LocalDb.getSyncState()).generation, resume['generation']);
      // A batch already in transit for the replaced generation must be ignored.
      channel.receive('sync_batch', {
        'generation': originalGeneration,
        'from_cursor': '8',
        'next_cursor': '9',
        'head_cursor': '9',
        'has_more': false,
        'events': <Map<String, dynamic>>[],
      });
      await _finishCatchUp(channel, cursor: 8);
      expect((await LocalDb.getSyncState()).committedCursor, 8);
      // Complete the first response so this tests the time throttle as well as
      // the in-flight guard. Two immediate lifecycle transitions add no request.
      service.setRealtimeAppState('background');
      service.setRealtimeAppState('foreground');
      service.setRealtimeAppState('background');
      service.setRealtimeAppState('foreground');
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(channel.resumes, hasLength(2));
      expect(channels, hasLength(1));
      nowMs += 5000;
      service.setRealtimeAppState('background');
      service.setRealtimeAppState('foreground');
      await _eventually(() => channel.resumes.length == 3);
    },
  );

  test(
    'cold start and initial catch-up do not send duplicate resumes',
    () async {
      final channel = channels.single;
      await authenticate(channel);
      service.setRealtimeAppState('foreground');
      nowMs += 5000;
      service.setRealtimeAppState('background');
      service.setRealtimeAppState('foreground');
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(channel.resumes, hasLength(1));
      expect(channels, hasLength(1));
      expect(
        channel.sink.packets.where((p) => p['cmd'] == 'pull_sync'),
        isEmpty,
      );
    },
  );

  test('reconnect supersedes a queued foreground resume', () async {
    final channel = channels.single;
    await authenticate(channel);
    await _finishCatchUp(channel);
    nowMs += 5000;
    service.setRealtimeAppState('background');
    service.setRealtimeAppState('foreground');
    // Recycle before the queued DB work can run, then authenticate the new
    // transport. The old request must never reach the replacement socket.
    service.suspendForAppBackground();
    service.setRealtimeAppState('foreground');
    service.ensureConnected();
    await _eventually(() => channels.length == 2);
    await authenticate(channels.last);
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(channel.resumes, hasLength(1));
    expect(channels.last.resumes, hasLength(1));
  });

  test(
    'returning to background cancels queued work without changing generation',
    () async {
      final channel = channels.single;
      await authenticate(channel);
      await _finishCatchUp(channel);
      final generation = channel.resumes.single['payload']['generation'];
      nowMs += 5000;
      service.setRealtimeAppState('background');
      service.setRealtimeAppState('foreground');
      service.setRealtimeAppState('background');
      await Future<void>.delayed(const Duration(milliseconds: 50));
      expect(channel.resumes, hasLength(1));
      expect((await LocalDb.getSyncState()).generation, generation);
      service.setRealtimeAppState('foreground');
      await _eventually(() => channel.resumes.length == 2);
    },
  );

  test(
    'iOS keeps its live socket and disconnected resume uses auth once',
    () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
      final channel = channels.single;
      await authenticate(channel);
      await _finishCatchUp(channel);
      service.setRealtimeAppState('background');
      nowMs += const Duration(minutes: 5).inMilliseconds;
      service.setRealtimeAppState('foreground');
      await _eventually(() => channel.resumes.length == 2);
      expect(channels, hasLength(1));
      await _finishCatchUp(channel);
      service.suspendForAppBackground();
      service.setRealtimeAppState('foreground');
      service.ensureConnected();
      await _eventually(() => channels.length == 2);
      await authenticate(channels.last);
      expect(channels.last.resumes, hasLength(1));
      expect(channels.last.resumes.single['payload']['committed_cursor'], '7');
    },
  );
}
