import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart' hide Response;

import 'package:shared_preferences/shared_preferences.dart';

import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/user_settings_service.dart';

class _FakeAuthService extends AuthService {
  _FakeAuthService(this.id);

  final String id;

  @override
  String? get userId => id;

  @override
  void attachAuthInterceptor(Dio dio) {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    // 预置本地已保存的语言偏好，使 _syncLocaleFromPreference 走「本地已有偏好」
    // 分支，避免测试结束后异步触发 Get.updateLocale → scheduleWarmUpFrame
    // 导致 'inTest' 断言失败。
    SharedPreferences.setMockInitialValues({'app_locale': 'zh_CN'});
    Get.put<AuthService>(_FakeAuthService('1001'));
  });

  tearDown(() {
    Get.reset();
  });

  test('ensureSyncedWithCurrentUser parses allow_group_invite', () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
      ..interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) {
            handler.resolve(
              Response<dynamic>(
                requestOptions: options,
                statusCode: 200,
                data: {
                  'code': 0,
                  'data': {
                    'chat': {
                      'auto_delegate_agent_id': '9001',
                      'friend_add_setting': 3,
                      'allow_group_invite': false,
                    },
                  },
                },
              ),
            );
          },
        ),
      );

    final service = await UserSettingsService(dio: dio).init();
    await service.ensureSyncedWithCurrentUser();

    expect(service.autoDelegateAgentId.value, '9001');
    expect(
      service.friendAddSetting.value,
      UserSettingsService.friendAddSettingForbidden,
    );
    expect(service.allowGroupInvite.value, isFalse);
  });

  test(
    'updateAllowGroupInvite sends bool payload and updates local state',
    () async {
      Map<dynamic, dynamic>? lastPayload;
      var getCalls = 0;

      final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
        ..interceptors.add(
          InterceptorsWrapper(
            onRequest: (options, handler) {
              if (options.method == 'GET') {
                getCalls++;
                handler.resolve(
                  Response<dynamic>(
                    requestOptions: options,
                    statusCode: 200,
                    data: {
                      'code': 0,
                      'data': {
                        'chat': {
                          'friend_add_setting': 1,
                          'allow_group_invite': true,
                        },
                      },
                    },
                  ),
                );
                return;
              }

              lastPayload = options.data as Map<dynamic, dynamic>?;
              handler.resolve(
                Response<dynamic>(
                  requestOptions: options,
                  statusCode: 200,
                  data: {
                    'code': 0,
                    'data': {
                      'chat': {
                        'friend_add_setting': 1,
                        'allow_group_invite': false,
                      },
                    },
                  },
                ),
              );
            },
          ),
        );

      final service = await UserSettingsService(dio: dio).init();

      final ok = await service.updateAllowGroupInvite(false);

      expect(ok, isTrue);
      expect(getCalls, 1);
      expect(lastPayload, isNotNull);
      expect(lastPayload?['chat']['allow_group_invite'], isFalse);
      expect(service.allowGroupInvite.value, isFalse);
    },
  );
}
