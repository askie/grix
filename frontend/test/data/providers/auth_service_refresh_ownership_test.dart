import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:shared_preferences/shared_preferences.dart';

enum _ARefreshResponse { success, invalid, temporary }

class _RefreshGateAdapter implements HttpClientAdapter {
  _RefreshGateAdapter({this.aResponse = _ARefreshResponse.success});

  final _ARefreshResponse aResponse;
  final aStarted = Completer<void>();
  final releaseA = Completer<void>();
  final requestedRefreshTokens = <String>[];

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    final body = Map<String, dynamic>.from(options.data as Map);
    final refresh = body['refresh_token']?.toString() ?? '';
    requestedRefreshTokens.add(refresh);
    if (refresh == 'refresh-a') {
      if (!aStarted.isCompleted) aStarted.complete();
      await releaseA.future;
      if (aResponse == _ARefreshResponse.invalid) {
        return ResponseBody.fromString(
          jsonEncode({'code': 40101, 'msg': 'refresh family expired'}),
          401,
          headers: {
            Headers.contentTypeHeader: [Headers.jsonContentType],
          },
        );
      }
      if (aResponse == _ARefreshResponse.temporary) {
        return ResponseBody.fromString(
          jsonEncode({'code': 50001, 'msg': 'temporary refresh failure'}),
          503,
          headers: {
            Headers.contentTypeHeader: [Headers.jsonContentType],
          },
        );
      }
      return _jsonResponse(
        accessToken: 'access-a-rotated',
        refreshToken: 'refresh-a-rotated',
        userId: 'account-a',
      );
    }
    if (refresh == 'refresh-b') {
      return _jsonResponse(
        accessToken: 'access-b-rotated',
        refreshToken: 'refresh-b-rotated',
        userId: 'account-b',
      );
    }
    return ResponseBody.fromString(
      jsonEncode({'code': 40101, 'msg': 'invalid refresh family'}),
      401,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }
}

ResponseBody _jsonResponse({
  required String accessToken,
  required String refreshToken,
  required String userId,
}) {
  return ResponseBody.fromString(
    jsonEncode({
      'code': 0,
      'data': {
        'access_token': accessToken,
        'refresh_token': refreshToken,
        'expires_in': 7200,
        'user': {'id': userId, 'username': userId, 'nickname': userId},
      },
    }),
    200,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

class _ResetOnlyImService extends ImService {
  int resetCalls = 0;

  @override
  Future<void> resetForAccountSwitch() async {
    resetCalls++;
  }

  @override
  Future<void> loadSessionsForCurrentUser() async {}
}

class _ManualTimer implements Timer {
  _ManualTimer(this.duration, this.callback);

  final Duration duration;
  final void Function() callback;
  bool _active = true;
  int _tick = 0;

  @override
  bool get isActive => _active;

  @override
  int get tick => _tick;

  @override
  void cancel() {
    _active = false;
  }

  void fire() {
    if (!_active) return;
    _active = false;
    _tick++;
    callback();
  }
}

class _RefreshTimerHarness {
  final timers = <_ManualTimer>[];

  List<_ManualTimer> get activeTimers =>
      timers.where((timer) => timer.isActive).toList(growable: false);

  Future<T> run<T>(Future<T> Function() body) {
    return runZoned(
      body,
      zoneSpecification: ZoneSpecification(
        createTimer: (self, parent, zone, duration, callback) {
          if (duration.inMilliseconds >=
              const Duration(seconds: 30).inMilliseconds) {
            final timer = _ManualTimer(duration, callback);
            timers.add(timer);
            return timer;
          }
          return parent.createTimer(zone, duration, callback);
        },
      ),
    );
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({
      'access_token': 'access-a',
      'refresh_token': 'refresh-a',
      'access_expires_at_ms': DateTime.now().millisecondsSinceEpoch + 3600000,
      'user_id': 'account-a',
      'username': 'account-a',
      'nickname': 'Account A',
    });
  });

  tearDown(() async {
    Get.reset();
    await LocalDb.setActiveUser(null);
  });

  test(
    'real AuthService drops an old refresh after a new account session is applied',
    () async {
      final adapter = _RefreshGateAdapter();
      final imService = _ResetOnlyImService();
      Get.put<ImService>(imService);
      final service = AuthService();
      service.dioForTest.httpClientAdapter = adapter;
      await service.init();
      expect(service.userId, 'account-a');

      final refreshA = service.ensureTokenFreshStatus(force: true);
      await adapter.aStarted.future.timeout(const Duration(seconds: 5));
      Future<TokenRefreshStatus>? refreshB;
      try {
        final appliedB = await service.applyAuthPayloadForTest({
          'access_token': 'access-b',
          'refresh_token': 'refresh-b',
          'expires_in': 7200,
          'user': {
            'id': 'account-b',
            'username': 'account-b',
            'nickname': 'Account B',
          },
        });
        expect(appliedB, isTrue);
        expect(LocalDb.activeUserId, 'account-b');
        refreshB = service.ensureTokenFreshStatus(force: true);
        await pumpEventQueue(times: 20);

        expect(adapter.requestedRefreshTokens, [
          'refresh-a',
          'refresh-b',
        ], reason: 'account B must not reuse account A refresh flight');
        expect(await refreshB, TokenRefreshStatus.ready);
        adapter.releaseA.complete();
        expect(
          await refreshA,
          TokenRefreshStatus.temporaryFailure,
          reason: 'a stale successful response is not current-session success',
        );

        expect(service.userId, 'account-b');
        expect(service.token, 'access-b-rotated');
        expect(service.refreshToken, 'refresh-b-rotated');
        expect(LocalDb.activeUserId, 'account-b');
        final prefs = await SharedPreferences.getInstance();
        expect(prefs.getString('user_id'), 'account-b');
        expect(prefs.getString('access_token'), 'access-b-rotated');
        expect(prefs.getString('refresh_token'), 'refresh-b-rotated');
      } finally {
        if (!adapter.releaseA.isCompleted) adapter.releaseA.complete();
        await refreshA;
        if (refreshB != null) await refreshB;
      }
    },
  );

  for (final response in const [
    _ARefreshResponse.invalid,
    _ARefreshResponse.temporary,
  ]) {
    test(
      'real AuthService ignores stale account-A ${response.name} refresh response',
      () async {
        final adapter = _RefreshGateAdapter(aResponse: response);
        Get.put<ImService>(_ResetOnlyImService());
        final service = AuthService();
        service.dioForTest.httpClientAdapter = adapter;
        await service.init();

        final refreshA = service.ensureTokenFreshStatus(force: true);
        await adapter.aStarted.future.timeout(const Duration(seconds: 5));
        try {
          expect(
            await service.applyAuthPayloadForTest({
              'access_token': 'access-b',
              'refresh_token': 'refresh-b',
              'expires_in': 7200,
              'user': {
                'id': 'account-b',
                'username': 'account-b',
                'nickname': 'Account B',
              },
            }),
            isTrue,
          );
          adapter.releaseA.complete();
          expect(await refreshA, TokenRefreshStatus.temporaryFailure);

          expect(service.userId, 'account-b');
          expect(service.token, 'access-b');
          expect(service.refreshToken, 'refresh-b');
          expect(LocalDb.activeUserId, 'account-b');
          final prefs = await SharedPreferences.getInstance();
          expect(prefs.getString('user_id'), 'account-b');
          expect(prefs.getString('access_token'), 'access-b');
          expect(prefs.getString('refresh_token'), 'refresh-b');
        } finally {
          if (!adapter.releaseA.isCompleted) adapter.releaseA.complete();
          await refreshA;
        }
      },
    );
  }

  for (final response in _ARefreshResponse.values) {
    test(
      'stale scheduled ${response.name} refresh cannot own the replacement account timer',
      () async {
        final timerHarness = _RefreshTimerHarness();
        await timerHarness.run(() async {
          final adapter = _RefreshGateAdapter(aResponse: response);
          Get.put<ImService>(_ResetOnlyImService());
          final service = AuthService();
          service.dioForTest.httpClientAdapter = adapter;
          await service.init();

          final scheduledA = service.runScheduledRefreshAttemptForTest();
          await adapter.aStarted.future.timeout(const Duration(seconds: 5));
          try {
            expect(
              await service.applyAuthPayloadForTest({
                'access_token': 'access-b',
                'refresh_token': 'refresh-b',
                'expires_in': 7200,
                'user': {
                  'id': 'account-b',
                  'username': 'account-b',
                  'nickname': 'Account B',
                },
              }),
              isTrue,
            );
            expect(timerHarness.activeTimers, hasLength(1));

            adapter.releaseA.complete();
            await scheduledA;

            expect(service.userId, 'account-b');
            expect(service.refreshToken, 'refresh-b');
            expect(adapter.requestedRefreshTokens, ['refresh-a']);
            expect(
              timerHarness.activeTimers,
              hasLength(1),
              reason:
                  'stale A completion must not retain B expiry and add an unowned retry timer',
            );

            await service.logout(notifyServer: false);
            expect(
              timerHarness.activeTimers,
              isEmpty,
              reason: 'logout must cancel every timer owned by the old session',
            );
          } finally {
            if (!adapter.releaseA.isCompleted) adapter.releaseA.complete();
            await scheduledA;
          }
        });
      },
    );
  }

  test('current scheduled temporary refresh keeps one retry timer', () async {
    final timerHarness = _RefreshTimerHarness();
    await timerHarness.run(() async {
      final adapter = _RefreshGateAdapter(
        aResponse: _ARefreshResponse.temporary,
      );
      Get.put<ImService>(_ResetOnlyImService());
      final service = AuthService();
      service.dioForTest.httpClientAdapter = adapter;
      await service.init();

      for (final timer in timerHarness.activeTimers) {
        timer.cancel();
      }
      final scheduled = service.runScheduledRefreshAttemptForTest();
      await adapter.aStarted.future.timeout(const Duration(seconds: 5));
      adapter.releaseA.complete();
      await scheduled;

      expect(timerHarness.activeTimers, hasLength(1));
      expect(
        timerHarness.activeTimers.single.duration,
        const Duration(seconds: 30),
      );
      await service.suspendCurrentSessionLocally();
      expect(timerHarness.activeTimers, isEmpty);
    });
  });

  test('current scheduled refresh rotation keeps one expiry timer', () async {
    final timerHarness = _RefreshTimerHarness();
    await timerHarness.run(() async {
      final adapter = _RefreshGateAdapter();
      Get.put<ImService>(_ResetOnlyImService());
      final service = AuthService();
      service.dioForTest.httpClientAdapter = adapter;
      await service.init();

      final scheduled = service.runScheduledRefreshAttemptForTest();
      await adapter.aStarted.future.timeout(const Duration(seconds: 5));
      adapter.releaseA.complete();
      await scheduled;

      expect(service.userId, 'account-a');
      expect(service.token, 'access-a-rotated');
      expect(service.refreshToken, 'refresh-a-rotated');
      expect(adapter.requestedRefreshTokens, ['refresh-a']);
      expect(timerHarness.activeTimers, hasLength(1));
      await service.logout(notifyServer: false);
      expect(timerHarness.activeTimers, isEmpty);
    });
  });

  test(
    'logout invalidates a blocked real refresh before it can persist',
    () async {
      final adapter = _RefreshGateAdapter();
      Get.put<ImService>(_ResetOnlyImService());
      final service = AuthService();
      service.dioForTest.httpClientAdapter = adapter;
      await service.init();

      final refreshA = service.ensureTokenFreshStatus(force: true);
      await adapter.aStarted.future.timeout(const Duration(seconds: 5));
      try {
        await service.logout(notifyServer: false);
        adapter.releaseA.complete();
        expect(await refreshA, TokenRefreshStatus.temporaryFailure);

        expect(service.isLoggedIn, isFalse);
        expect(service.userId, isNull);
        expect(service.token, isNull);
        expect(service.refreshToken, isNull);
        expect(LocalDb.activeUserId, isNull);
        final prefs = await SharedPreferences.getInstance();
        expect(prefs.getString('user_id'), isNull);
        expect(prefs.getString('access_token'), isNull);
        expect(prefs.getString('refresh_token'), isNull);
      } finally {
        if (!adapter.releaseA.isCompleted) adapter.releaseA.complete();
        await refreshA;
      }
    },
  );
}
