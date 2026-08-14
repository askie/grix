// 连真实本地后端的账号切换端到端验证（默认跳过，CI 不跑）。
//
// 运行前置：本地后端已起（make dev-up 或至少 api 服务 + PG/Redis），
// 且存在两个测试账号（可用 /v1/auth/register 注册）。运行方式：
//
//   flutter test test/data/providers/auth_service_account_switch_live_test.dart \
//     --dart-define=LIVE_BACKEND=1 \
//     --dart-define=LIVE_ACCOUNT_A=switch_a@test.local \
//     --dart-define=LIVE_ACCOUNT_B=switch_b@test.local \
//     --dart-define=LIVE_PASSWORD=SwitchTest123
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

const bool _live = String.fromEnvironment('LIVE_BACKEND') == '1';
const String _baseUrl = String.fromEnvironment(
  'LIVE_API_BASE_URL',
  defaultValue: 'http://127.0.0.1:27180/v1',
);
const String _accountA = String.fromEnvironment(
  'LIVE_ACCOUNT_A',
  defaultValue: 'switch_a@test.local',
);
const String _accountB = String.fromEnvironment(
  'LIVE_ACCOUNT_B',
  defaultValue: 'switch_b@test.local',
);
const String _password = String.fromEnvironment(
  'LIVE_PASSWORD',
  defaultValue: 'SwitchTest123',
);

Future<Map<String, dynamic>> _loginPayload(String account) async {
  final dio = Dio(BaseOptions(baseUrl: _baseUrl));
  final resp = await dio.post(
    '/auth/login',
    data: {
      'account': account,
      'password': _password,
      'device_id': 'live-switch-test-device',
      'platform': 'ios',
    },
  );
  final body = Map<String, dynamic>.from(resp.data as Map);
  if (body['code'] != 0) {
    throw StateError('live login failed for $account: ${body['msg']}');
  }
  return Map<String, dynamic>.from(body['data'] as Map);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late AuthService service;
  HttpOverrides? prevOverrides;

  setUp(() async {
    // flutter_test 默认装 MockHttpOverrides 拒绝所有真实网络，这里恢复真实网络。
    prevOverrides = HttpOverrides.current;
    HttpOverrides.global = null;
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    service = AuthService();
    await service.init();
    service.updateBaseUrl(_baseUrl);
  });

  tearDown(() {
    HttpOverrides.global = prevOverrides;
    Get.reset();
  });

  test(
    'live: switch between two real accounts rotates tokens via real refresh',
    () async {
      // 登录 A、B 两个真实账号（B 为当前）。
      final payloadA = await _loginPayload(_accountA);
      expect(await service.applyAuthPayloadForTest(payloadA), isTrue);
      final userIdA = service.userId!;

      final payloadB = await _loginPayload(_accountB);
      expect(await service.applyAuthPayloadForTest(payloadB), isTrue);
      final userIdB = service.userId!;
      expect(userIdB, isNot(userIdA));

      final savedTokenA = (await service.listSavedAccounts())
          .firstWhere((a) => a.userId == userIdA)
          .refreshToken;

      // 切回 A：真实调用 /auth/refresh，验证凭证有效并轮转。
      final outcome = await service.switchToSavedAccount(userIdA);
      expect(outcome, AccountSwitchOutcome.success);
      expect(service.userId, userIdA);
      expect(service.isLoggedIn, isTrue);
      // refresh 轮转后新 refresh token 已回写列表。
      final savedA = (await service.listSavedAccounts())
          .firstWhere((a) => a.userId == userIdA);
      expect(savedA.refreshToken, isNotEmpty);
      expect(savedA.refreshToken, isNot(savedTokenA));
      // B 的凭证保持有效，可再切回。
      final back = await service.switchToSavedAccount(userIdB);
      expect(back, AccountSwitchOutcome.success);
      expect(service.userId, userIdB);
    },
    skip: _live ? false : 'live backend test, run with LIVE_BACKEND=1',
    timeout: const Timeout(Duration(minutes: 2)),
  );

  test(
    'live: switching with revoked refresh token degrades to needLogin',
    () async {
      final payloadA = await _loginPayload(_accountA);
      expect(await service.applyAuthPayloadForTest(payloadA), isTrue);
      final userIdA = service.userId!;

      final payloadB = await _loginPayload(_accountB);
      expect(await service.applyAuthPayloadForTest(payloadB), isTrue);

      // 篡改 A 的 refresh token 模拟凭证被吊销/过期。
      final savedA = (await service.listSavedAccounts())
          .firstWhere((a) => a.userId == userIdA);
      await service.applyAuthPayloadForTest({
        'access_token': service.token,
        'refresh_token': service.refreshToken,
        'expires_in': 3600,
        'user': {'id': service.userId, 'username': 'keep'},
      });
      // 直接改存储里的 A 凭证为无效值。
      final store = await SharedPreferences.getInstance();
      final raw = store.getString('saved_accounts_v1')!;
      final tampered = raw.replaceAll(
        savedA.refreshToken,
        'invalid-refresh-token',
      );
      await store.setString('saved_accounts_v1', tampered);

      final outcome = await service.switchToSavedAccount(userIdA);
      expect(outcome, AccountSwitchOutcome.needLogin);
      // 凭证已被清空标记为需重登。
      final after = (await service.listSavedAccounts())
          .firstWhere((a) => a.userId == userIdA);
      expect(after.needsRelogin, isTrue);
    },
    skip: _live ? false : 'live backend test, run with LIVE_BACKEND=1',
    timeout: const Timeout(Duration(minutes: 2)),
  );
}
