import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/modules/auth/controllers/phone_login_controller.dart';

class _FakeAuthService extends AuthService {
  _FakeAuthService(this._methods);

  final AuthMethods _methods;
  int fetchCalls = 0;
  String? lastRegion;

  @override
  Future<ServiceResult<AuthMethods>> fetchAuthMethods({
    required String region,
  }) async {
    fetchCalls++;
    lastRegion = region;
    return ServiceResult<AuthMethods>.success(data: _methods);
  }
}

void main() {
  tearDown(Get.reset);

  test('login mode: enabled → canSendCode follows phone length', () async {
    final auth = _FakeAuthService(
      const AuthMethods(
        region: 'cn',
        phoneLoginEnabled: true,
        phoneRegisterEnabled: true,
      ),
    );
    final c = PhoneLoginController(
      authService: auth,
      mode: PhoneFlowMode.login,
    );
    c.onInit();
    // 让 onInit 中的 _refreshAuthMethods 跑完
    await Future<void>.delayed(Duration.zero);

    expect(c.authMethodsLoaded.value, true);
    expect(c.phoneLoginAllowed, true);
    expect(auth.fetchCalls, 1);

    // 没输入号码时不可发码
    expect(c.canSendCode, false);

    // 输入合法号码后可发码
    c.phone.value = '13800138000';
    expect(c.canSendCode, true);
  });

  test(
    'login mode: disabled → canSendCode false even with valid phone',
    () async {
      final auth = _FakeAuthService(
        const AuthMethods(
          region: 'cn',
          phoneLoginEnabled: false,
          phoneRegisterEnabled: false,
        ),
      );
      final c = PhoneLoginController(
        authService: auth,
        mode: PhoneFlowMode.login,
      );
      c.onInit();
      await Future<void>.delayed(Duration.zero);

      expect(c.authMethodsLoaded.value, true);
      expect(c.phoneLoginAllowed, false);

      c.phone.value = '13800138000';
      // login mode 下开关关了 → canSendCode 必为 false
      expect(c.canSendCode, false);
    },
  );

  test(
    'bind mode: phoneLoginAllowed ignores switch (always allowed)',
    () async {
      final auth = _FakeAuthService(
        const AuthMethods(
          region: 'cn',
          phoneLoginEnabled: false,
          phoneRegisterEnabled: false,
        ),
      );
      final c = PhoneLoginController(
        authService: auth,
        mode: PhoneFlowMode.bind,
      );
      c.onInit();
      await Future<void>.delayed(Duration.zero);

      // bind 模式恒为允许（已登录用户主动绑定不受 phone_login 开关控制）
      expect(c.phoneLoginAllowed, true);

      c.phone.value = '13800138000';
      expect(c.canSendCode, true);
    },
  );
}
