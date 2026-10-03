import 'dart:collection';
import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/auth/controllers/register_controller.dart';
import 'package:grix/shared/utils/app_region_config.dart';

class _FakeAuthService extends AuthService {
  final Queue<ServiceResult<void>> _registerResponses =
      Queue<ServiceResult<void>>();
  final RxBool _loggedIn = false.obs;

  final methodsRequests = <Completer<ServiceResult<AuthMethods>>>[];
  bool holdMethods = false;
  Completer<ServiceResult<void>>? pendingCode;
  Completer<ServiceResult<void>>? pendingRegister;
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
    if (pendingRegister != null) return pendingRegister!.future;
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
    if (pendingCode != null) return pendingCode!.future;
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
    if (holdMethods) {
      final request = Completer<ServiceResult<AuthMethods>>();
      methodsRequests.add(request);
      return request.future;
    }
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
    await tester.runAsync(() => controller.refreshAuthMethods());
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
  testWidgets('closed capability blocks direct register and email code', (
    tester,
  ) async {
    await pumpShell(tester);
    controller.authMethods.value = const AuthMethods.allDisabled();
    expect(controller.canRequestEmailCode, false);
    await controller.sendEmailCode(email: 'newuser@example.com');
    await controller.register(
      email: 'newuser@example.com',
      password: 'Password123',
      emailCode: '123456',
    );
    expect(authService.sendEmailCodeCalls, 0);
    expect(authService.registerCalls, 0);
    expect(controller.errorMessage.value, contains('关闭'));
  });
  testWidgets(
    'out of order regional capability replies cannot reopen registration',
    (tester) async {
      await pumpShell(tester);
      authService.holdMethods = true;
      final old = controller.refreshAuthMethods();
      controller.switchRegion(
        controller.selectedRegion.value == AppRegion.cn
            ? AppRegion.global
            : AppRegion.cn,
      );
      expect(controller.authMethods.value.registrationEnabled, false);
      final region = controller.selectedRegion.value.name;
      authService.methodsRequests.last.complete(
        ServiceResult<AuthMethods>.success(
          data: AuthMethods.allDisabled(region: region),
        ),
      );
      await tester.pump();
      authService.methodsRequests.first.complete(
        ServiceResult<AuthMethods>.success(
          data: const AuthMethods(
            region: 'old',
            phoneLoginEnabled: true,
            phoneRegisterEnabled: true,
            registrationEnabled: true,
          ),
        ),
      );
      await old;
      expect(controller.authMethods.value.region, region);
      expect(controller.authMethods.value.registrationEnabled, false);
      expect(controller.authMethodsLoading.value, false);
    },
  );
  testWidgets('capability failure exposes retry and stays closed', (
    tester,
  ) async {
    await pumpShell(tester);
    authService.holdMethods = true;
    final pending = controller.refreshAuthMethods();
    authService.methodsRequests.single.complete(
      ServiceResult<AuthMethods>.failure(message: 'offline'),
    );
    await pending;
    expect(controller.authMethodsFailed.value, true);
    expect(controller.canRequestEmailCode, false);
    authService.holdMethods = false;
    await controller.refreshAuthMethods();
    expect(controller.authMethodsFailed.value, false);
    expect(controller.canRequestEmailCode, true);
  });
  testWidgets('switching away and back discards old code result', (
    tester,
  ) async {
    await pumpShell(tester);
    authService.pendingCode = Completer<ServiceResult<void>>();
    final pending = controller.sendEmailCode(email: 'newuser@example.com');
    final region = controller.selectedRegion.value;
    controller.switchRegion(
      region == AppRegion.cn ? AppRegion.global : AppRegion.cn,
    );
    controller.switchRegion(region);
    authService.pendingCode!.complete(ServiceResult<void>.success());
    await pending;
    expect(controller.sendCodeCountdown.value, 0);
    expect(controller.isSendingCode.value, false);
  });
  testWidgets('account grant in flight prevents switching endpoint', (
    tester,
  ) async {
    await pumpShell(tester);
    authService.pendingRegister = Completer<ServiceResult<void>>();
    final pending = controller.register(
      email: 'newuser@example.com',
      password: 'Password123',
      emailCode: '123456',
    );
    final region = controller.selectedRegion.value;
    expect(controller.isLoading.value, true);
    controller.switchRegion(
      region == AppRegion.cn ? AppRegion.global : AppRegion.cn,
    );
    expect(controller.selectedRegion.value, region);
    authService.pendingRegister!.complete(
      ServiceResult<void>.failure(message: 'offline'),
    );
    await pending;
    expect(controller.isLoading.value, false);
  });
}
