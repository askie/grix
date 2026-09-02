import 'dart:collection';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/auth/controllers/register_controller.dart';

class _FakeAuthService extends AuthService {
  final Queue<ServiceResult<void>> _registerResponses =
      Queue<ServiceResult<void>>();
  final RxBool _loggedIn = false.obs;

  int registerCalls = 0;
  int sendEmailCodeCalls = 0;
  String? lastSendCodeScene;
  String? lastCaptchaId;
  String? lastCaptchaValue;
  bool markLoggedInOnRegister = true;

  @override
  bool get isLoggedIn => _loggedIn.value;

  @override
  RxBool get isLoggedInRx => _loggedIn;

  void enqueueRegister(ServiceResult<void> result) {
    _registerResponses.addLast(result);
  }

  @override
  Future<ServiceResult<void>> register({
    required String email,
    required String password,
    required String emailCode,
    String region = '',
  }) async {
    registerCalls++;
    if (_registerResponses.isNotEmpty) {
      final result = _registerResponses.removeFirst();
      if (result.ok && markLoggedInOnRegister) {
        _loggedIn.value = true;
      }
      return result;
    }
    if (markLoggedInOnRegister) {
      _loggedIn.value = true;
    }
    return ServiceResult<void>.success();
  }

  @override
  Future<ServiceResult<void>> sendEmailCode({
    required String email,
    required String scene,
    String? captchaId,
    String? captchaValue,
  }) async {
    sendEmailCodeCalls++;
    lastSendCodeScene = scene;
    lastCaptchaId = captchaId;
    lastCaptchaValue = captchaValue;
    return ServiceResult<void>.success();
  }

  // 同 login 测试：fake 出能力开关接口，避免 onInit/_initRegion 触发 dio 请求。
  @override
  Future<ServiceResult<AuthMethods>> fetchAuthMethods({
    required String region,
  }) async {
    return ServiceResult<AuthMethods>.success(
      data: AuthMethods(
        region: region,
        phoneLoginEnabled: true,
        phoneRegisterEnabled: true,
      ),
    );
  }
}

class _FakeImService extends ImService {
  bool connected = false;
  int connectCalls = 0;

  @override
  bool get isConnected => connected;

  @override
  void connect(String wsUrl) {
    connectCalls++;
    connected = true;
  }

  // RegisterController 注册成功后调用的是 ensureConnected()（端点已由
  // applyAuthPayload 预写），而非 connect()。覆写它来真实记录连接调用。
  @override
  void ensureConnected() {
    connectCalls++;
    connected = true;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAuthService authService;
  late _FakeImService imService;
  late RegisterController controller;

  Future<void> pumpShell(WidgetTester tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('en', 'US'),
        initialRoute: AppRoutes.register,
        getPages: [
          GetPage(
            name: AppRoutes.register,
            page: () => const Scaffold(body: SizedBox.shrink()),
          ),
          GetPage(
            name: AppRoutes.home,
            page: () => const Scaffold(body: SizedBox.shrink()),
          ),
          GetPage(
            name: AppRoutes.login,
            page: () => const Scaffold(body: SizedBox.shrink()),
          ),
        ],
      ),
    );
    await tester.pump();
  }

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    Get.reset();
    authService = _FakeAuthService();
    imService = _FakeImService();
    Get.put<AuthService>(authService);
    Get.put<ImService>(imService);
    // permanent: true 防止 GetMaterialApp 路由生命周期的 SmartManagement
    // 在 await 期间提前 dispose 控制器（dispose 后 isClosed=true，register
    // 会在写 errorMessage / 导航之前提前 return）。
    controller = Get.put<RegisterController>(
      RegisterController(),
      permanent: true,
    );
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('register success navigates directly to home', (tester) async {
    await pumpShell(tester);

    await controller.register(
      email: 'newuser@example.com',
      password: 'password123',
      emailCode: '123456',
    );
    await tester.pump();

    expect(authService.registerCalls, 1);
    expect(imService.connectCalls, 1);
    expect(Get.currentRoute, AppRoutes.home);
  });

  testWidgets('register success but no session stays on register page', (
    tester,
  ) async {
    await pumpShell(tester);

    authService.markLoggedInOnRegister = false;

    await controller.register(
      email: 'newuser@example.com',
      password: 'password123',
      emailCode: '123456',
    );
    await tester.pump();

    expect(authService.registerCalls, 1);
    expect(imService.connectCalls, 0);
    expect(Get.currentRoute, AppRoutes.register);
    expect(controller.errorMessage.value, '注册失败');
  });

  testWidgets('register failure stays on register page', (tester) async {
    await pumpShell(tester);

    authService.enqueueRegister(ServiceResult<void>.failure(message: '注册失败'));

    await controller.register(
      email: 'newuser@example.com',
      password: 'password123',
      emailCode: '123456',
    );
    await tester.pump();

    expect(authService.registerCalls, 1);
    expect(imService.connectCalls, 0);
    expect(Get.currentRoute, AppRoutes.register);
    expect(controller.errorMessage.value, '注册失败');
  });

  testWidgets('sendEmailCode for register does not require captcha', (
    tester,
  ) async {
    await pumpShell(tester);

    await controller.sendEmailCode(email: 'newuser@example.com');
    await tester.pump();

    expect(authService.sendEmailCodeCalls, 1);
    expect(authService.lastSendCodeScene, 'register');
    expect(authService.lastCaptchaId, isNull);
    expect(authService.lastCaptchaValue, isNull);

    controller.sendCodeCountdown.value = 0;
    await tester.pump(const Duration(seconds: 3));
    await tester.pumpAndSettle();
  });
}
