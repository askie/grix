import 'dart:async';
import 'dart:collection';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/apple_sign_in_service.dart';
import 'package:grix/data/providers/feature_flag_service.dart';
import 'package:grix/data/providers/google_sign_in_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/auth/controllers/login_controller.dart';
import 'package:grix/modules/auth/services/login_credential_storage.dart';
import 'package:grix/shared/utils/app_region_config.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  final RxBool _loggedIn = false.obs;
  final Queue<Future<ServiceResult<void>>> _loginResponses =
      Queue<Future<ServiceResult<void>>>();
  int loginCalls = 0;
  int googleLoginCalls = 0;
  String? lastBaseUrl;

  @override
  bool get isLoggedIn => _loggedIn.value;

  @override
  RxBool get isLoggedInRx => _loggedIn;

  @override
  void updateBaseUrl(String baseUrl) {
    lastBaseUrl = baseUrl;
    super.updateBaseUrl(baseUrl);
  }

  void enqueueLogin(Future<ServiceResult<void>> response) {
    _loginResponses.addLast(response);
  }

  void markLoggedIn() {
    _loggedIn.value = true;
  }

  @override
  Future<ServiceResult<void>> login(String account, String password) async {
    loginCalls++;
    if (_loginResponses.isEmpty) {
      return ServiceResult<void>.success();
    }
    return _loginResponses.removeFirst();
  }

  @override
  Future<ServiceResult<void>> loginWithGoogle(String idToken) async {
    googleLoginCalls++;
    if (_loginResponses.isEmpty) {
      return ServiceResult<void>.success();
    }
    return _loginResponses.removeFirst();
  }

  // 测试场景里不连真后端；返回开关全开，避免触发 dio 请求挂起 pending timer。
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

// 测试场景里不连真后端；只统计切区时是否触发了刷新，不做真实网络请求。
class _FakeFeatureFlagService extends FeatureFlagService {
  int refreshCalls = 0;

  @override
  void refresh() {
    refreshCalls++;
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

  // LoginController 登录成功后走的是 ensureConnected()（而非 connect()）；
  // 必须在此打桩，否则会落到真实实现去开真 WebSocket 并留下重连定时器，
  // 导致后续测试 "did not complete" 超时，同时 connectCalls 也统计不到。
  @override
  void ensureConnected() {
    connectCalls++;
    connected = true;
  }

  // 真实实现会在 App 端持久化 _wsUrl 并参与连接决策；测试里无需任何副作用。
  @override
  void updateWsEndpoint(String url) {}
}

class _FakeGoogleSignInService extends GoogleSignInService {
  _FakeGoogleSignInService();

  final Queue<Future<ServiceResult<String>>> _responses =
      Queue<Future<ServiceResult<String>>>();
  int signInCalls = 0;

  void enqueueResponse(Future<ServiceResult<String>> response) {
    _responses.addLast(response);
  }

  @override
  Future<ServiceResult<String>> signIn() async {
    signInCalls++;
    if (_responses.isEmpty) {
      return ServiceResult<String>.success(data: 'google-id-token');
    }
    return _responses.removeFirst();
  }
}

class _SavedCall {
  final LoginCredentialState state;
  final AppRegion region;
  _SavedCall(this.state, this.region);
}

class _FakeLoginCredentialStorage extends LoginCredentialStorage {
  LoginCredentialState loadedState = const LoginCredentialState();
  AppRegion? lastLoadedRegion;
  final List<_SavedCall> savedCalls = <_SavedCall>[];

  @override
  Future<LoginCredentialState> load(AppRegion region) async {
    lastLoadedRegion = region;
    return loadedState;
  }

  @override
  Future<void> save(LoginCredentialState state, AppRegion region) async {
    savedCalls.add(_SavedCall(state, region));
  }

  List<LoginCredentialState> get savedStates =>
      savedCalls.map((c) => c.state).toList();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAuthService authService;
  late _FakeGoogleSignInService googleSignInService;
  late _FakeImService imService;
  late _FakeFeatureFlagService featureFlagService;
  late _FakeLoginCredentialStorage credentialStorage;
  late LoginController controller;

  Future<void> pumpShell(WidgetTester tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('en', 'US'),
        initialRoute: AppRoutes.login,
        getPages: [
          GetPage(
            name: AppRoutes.login,
            page: () => const Scaffold(body: SizedBox.shrink()),
          ),
          GetPage(
            name: AppRoutes.home,
            page: () => const Scaffold(body: SizedBox.shrink()),
          ),
        ],
      ),
    );
    await tester.pump();
  }

  setUp(() {
    // 让区域解析确定为中国大陆，下面的默认区域断言才稳定。
    SharedPreferences.setMockInitialValues({'app_region': 'cn'});
    Get.reset();
    Get.addTranslations(AppTranslations().keys);
    Get.locale = const Locale('zh', 'CN');
    authService = _FakeAuthService();
    googleSignInService = _FakeGoogleSignInService();
    imService = _FakeImService();
    featureFlagService = _FakeFeatureFlagService();
    credentialStorage = _FakeLoginCredentialStorage();
    Get.put<AuthService>(authService);
    Get.put<GoogleSignInService>(googleSignInService);
    Get.put<AppleSignInService>(AppleSignInService());
    Get.put<ImService>(imService);
    Get.put<FeatureFlagService>(featureFlagService);
    controller = Get.put(
      LoginController(
        authService: authService,
        googleSignInService: googleSignInService,
        imService: imService,
        credentialStorage: credentialStorage,
        submitTimeout: const Duration(milliseconds: 20),
      ),
    );
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('login timeout unlocks retry', (tester) async {
    await pumpShell(tester);

    final stalled = Completer<ServiceResult<void>>();
    authService.enqueueLogin(stalled.future);
    authService.enqueueLogin(Future.value(ServiceResult<void>.success()));

    unawaited(controller.login(account: 'user', password: 'pwd'));
    await tester.pump();

    expect(controller.isLoading.value, isTrue);

    await tester.pump(const Duration(milliseconds: 30));

    expect(controller.isLoading.value, isFalse);
    expect(controller.errorMessage.value, '请求超时，请稍后重试');
    expect(authService.loginCalls, 1);

    await controller.login(
      account: 'user',
      password: 'pwd',
      saveCredentials: true,
    );
    await tester.pump();

    expect(authService.loginCalls, 2);
    expect(imService.connectCalls, 1);
    expect(controller.errorMessage.value, isNull);
    expect(Get.currentRoute, AppRoutes.home);
    expect(credentialStorage.savedStates, hasLength(1));
    expect(credentialStorage.savedStates.single.account, 'user');
    expect(credentialStorage.savedStates.single.password, 'pwd');
  });

  testWidgets('late auth state change still navigates away from login', (
    tester,
  ) async {
    await pumpShell(tester);

    final stalled = Completer<ServiceResult<void>>();
    authService.enqueueLogin(stalled.future);

    unawaited(controller.login(account: 'user', password: 'pwd'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 30));

    expect(controller.isLoading.value, isFalse);
    expect(Get.currentRoute, AppRoutes.login);

    authService.markLoggedIn();
    await tester.pump();

    expect(imService.connectCalls, 1);
    expect(controller.errorMessage.value, isNull);
    expect(Get.currentRoute, AppRoutes.home);
    expect(credentialStorage.savedStates, isEmpty);
  });

  testWidgets(
    'login re-pins the region endpoint before request even if base url was clobbered',
    (tester) async {
      await pumpShell(tester);

      // 模拟共享 baseUrl 在退出再登录的路径上被其他入口残留到全球区。
      authService.updateBaseUrl('https://grix.im/v1');
      authService.lastBaseUrl = null;

      authService.enqueueLogin(Future.value(ServiceResult<void>.success()));
      await controller.login(account: 'user', password: 'pwd');
      await tester.pump();

      // 发起登录请求前必须把端点重新钉回当前区域（中国大陆区），而不是停留在全球区。
      expect(authService.lastBaseUrl, resolveRegionApiBaseUrl(AppRegion.cn));
      expect(authService.lastBaseUrl, isNot('https://grix.im/v1'));
      expect(authService.loginCalls, 1);
    },
  );

  testWidgets('load saved credentials returns stored account and password', (
    tester,
  ) async {
    await pumpShell(tester);
    credentialStorage.loadedState = const LoginCredentialState(
      account: 'saved_user',
      password: 'saved_pwd',
    );

    // loadSavedCredentials 内部 await 真实 SharedPreferences（区域解析）。
    // testWidgets 默认在 FakeAsync 区运行，平台通道回调不会被裸 await 驱动，
    // 必须用 runAsync 切到真实异步区，否则 Future 永不完成、测试挂死。
    final restored = await tester.runAsync(
      () => controller.loadSavedCredentials(),
    );

    expect(restored!.account, 'saved_user');
    expect(restored.password, 'saved_pwd');
  });

  testWidgets('loadSavedCredentials passes current region to storage', (
    tester,
  ) async {
    await pumpShell(tester);

    // 默认区域是中国大陆。
    // runAsync：loadSavedCredentials 依赖真实 SharedPreferences 区域解析，
    // 需切到真实异步区驱动平台通道，否则在 FakeAsync 下裸 await 挂死。
    await tester.runAsync(() => controller.loadSavedCredentials());
    expect(credentialStorage.lastLoadedRegion, AppRegion.cn);

    // 切换到全球区后，再次加载应传入 global。
    controller.switchRegion(AppRegion.global);
    await tester.runAsync(() => controller.loadSavedCredentials());
    expect(credentialStorage.lastLoadedRegion, AppRegion.global);
  });

  testWidgets('switchRegion refreshes feature gates for the new region', (
    tester,
  ) async {
    await pumpShell(tester);
    final callsBeforeSwitch = featureFlagService.refreshCalls;

    controller.switchRegion(AppRegion.global);

    // 苹果/谷歌登录按钮由 FeatureFlagService 的 gate 控制，切区后必须
    // 重新拉取，否则会残留旧区域的开关状态（对应老郭反馈的 bug）。
    expect(featureFlagService.refreshCalls, callsBeforeSwitch + 1);
  });

  testWidgets(
    'loadSavedCredentials waits for async region resolution before reading',
    (tester) async {
      // 模拟记住的区域是全球区：登录页一进来不手动切换，直接读凭证，
      // 必须等区域解析完成后按全球区读取，而不是用初始默认值中国大陆。
      SharedPreferences.setMockInitialValues({'app_region': 'global'});
      Get.delete<LoginController>(force: true);
      final freshController = Get.put(
        LoginController(
          authService: authService,
          googleSignInService: googleSignInService,
          imService: imService,
          credentialStorage: credentialStorage,
          submitTimeout: const Duration(milliseconds: 20),
        ),
      );

      // runAsync：等待真实 SharedPreferences 区域解析（FakeAsync 下裸 await 会挂死）。
      await tester.runAsync(() => freshController.loadSavedCredentials());

      expect(credentialStorage.lastLoadedRegion, AppRegion.global);
    },
  );

  testWidgets('login with saveCredentials passes current region to storage', (
    tester,
  ) async {
    await pumpShell(tester);

    // 切换到全球区登录，保存的凭证应归属 global。
    controller.switchRegion(AppRegion.global);
    authService.enqueueLogin(Future.value(ServiceResult<void>.success()));
    await controller.login(
      account: 'gb_user',
      password: 'gb_pwd',
      saveCredentials: true,
    );
    await tester.pump();

    expect(credentialStorage.savedCalls, hasLength(1));
    expect(credentialStorage.savedCalls.single.region, AppRegion.global);
    expect(credentialStorage.savedCalls.single.state.account, 'gb_user');
  });

  testWidgets('login saves credentials under cn region by default', (
    tester,
  ) async {
    await pumpShell(tester);

    authService.enqueueLogin(Future.value(ServiceResult<void>.success()));
    await controller.login(
      account: 'cn_user',
      password: 'cn_pwd',
      saveCredentials: true,
    );
    await tester.pump();

    expect(credentialStorage.savedCalls, hasLength(1));
    expect(credentialStorage.savedCalls.single.region, AppRegion.cn);
  });

  testWidgets(
    'credentials persist even when login flips state and controller closes '
    'before the request returns',
    (tester) async {
      await pumpShell(tester);

      // 复现真机时序：authService.login() 在返回成功之前就把登录态置真，
      // 触发 ever worker 抢先导航并销毁控制器；保存凭证必须不被 isClosed 拦截。
      final gate = Completer<ServiceResult<void>>();
      authService.enqueueLogin(gate.future);

      unawaited(
        controller.login(
          account: 'race_user',
          password: 'race_pwd',
          saveCredentials: true,
        ),
      );
      await tester.pump();

      // login 仍在 await 中：此刻置登录态，worker 会导航离开登录页。
      authService.markLoggedIn();
      await tester.pump();

      // 模拟 GetX 在 offAllNamed 清栈时回收 LoginController（lazyPut 无 fenix）。
      await Get.delete<LoginController>(force: true);
      expect(controller.isClosed, isTrue);

      // 控制器销毁后，login() 才返回成功。
      gate.complete(ServiceResult<void>.success());
      await tester.pump();
      await tester.pump();

      // 守卫：即便控制器已销毁，本次登录的凭证仍必须落盘，且归属请求前的区域。
      expect(credentialStorage.savedCalls, hasLength(1));
      expect(credentialStorage.savedCalls.single.state.account, 'race_user');
      expect(credentialStorage.savedCalls.single.state.password, 'race_pwd');
      expect(credentialStorage.savedCalls.single.region, AppRegion.cn);
    },
  );

  testWidgets(
    'google login signs in first, then exchanges token with backend',
    (tester) async {
      await pumpShell(tester);

      googleSignInService.enqueueResponse(
        Future.value(ServiceResult<String>.success(data: 'google-id-token')),
      );
      authService.enqueueLogin(Future.value(ServiceResult<void>.success()));

      await controller.loginWithGoogle();
      await tester.pump();

      expect(googleSignInService.signInCalls, 1);
      expect(authService.googleLoginCalls, 1);
      expect(imService.connectCalls, 1);
      expect(Get.currentRoute, AppRoutes.home);
    },
  );

  testWidgets('google login surfaces sign-in failure before backend exchange', (
    tester,
  ) async {
    await pumpShell(tester);

    googleSignInService.enqueueResponse(
      Future.value(
        ServiceResult<String>.failure(message: 'Google login was cancelled'),
      ),
    );

    await controller.loginWithGoogle();
    await tester.pump();

    expect(googleSignInService.signInCalls, 1);
    expect(authService.googleLoginCalls, 0);
    expect(controller.errorMessage.value, 'Google login was cancelled');
  });

  testWidgets('password rejected (401) shows cross-region hint', (
    tester,
  ) async {
    await pumpShell(tester);

    authService.enqueueLogin(
      Future.value(
        ServiceResult<void>.failure(
          message: '邮箱或密码错误',
          code: 10002,
          httpStatus: 401,
        ),
      ),
    );

    await controller.login(account: 'user', password: 'wrong');
    await tester.pump();

    expect(controller.showCrossRegionHint.value, isTrue);
    // 默认区域 cn，另一侧是 global。
    expect(controller.otherRegion, AppRegion.global);

    // 再次发起登录时提示先清空。
    authService.enqueueLogin(Future.value(ServiceResult<void>.success()));
    await controller.login(account: 'user', password: 'right');
    await tester.pump();
    expect(controller.showCrossRegionHint.value, isFalse);
  });

  testWidgets('non-401 login failure does not show cross-region hint', (
    tester,
  ) async {
    await pumpShell(tester);

    authService.enqueueLogin(
      Future.value(ServiceResult<void>.failure(message: '网络错误', httpStatus: 0)),
    );

    await controller.login(account: 'user', password: 'pwd');
    await tester.pump();

    expect(controller.errorMessage.value, '网络错误');
    expect(controller.showCrossRegionHint.value, isFalse);
  });

  testWidgets('switching region clears cross-region hint', (tester) async {
    await pumpShell(tester);

    authService.enqueueLogin(
      Future.value(
        ServiceResult<void>.failure(message: '邮箱或密码错误', httpStatus: 401),
      ),
    );
    await controller.login(account: 'user', password: 'wrong');
    await tester.pump();
    expect(controller.showCrossRegionHint.value, isTrue);

    controller.switchToOtherRegion();
    await tester.pump();
    expect(controller.showCrossRegionHint.value, isFalse);
    expect(controller.selectedRegion.value, AppRegion.global);
  });
}
