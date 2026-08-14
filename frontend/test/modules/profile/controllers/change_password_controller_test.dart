import 'dart:collection';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/modules/auth/services/login_credential_storage.dart';
import 'package:grix/modules/profile/controllers/change_password_controller.dart';
import 'package:grix/shared/utils/app_region_config.dart';

class _FakeAuthService extends AuthService {
  final Queue<ServiceResult<void>> _sendCodeResults =
      Queue<ServiceResult<void>>();
  final Queue<ServiceResult<void>> _changePasswordResults =
      Queue<ServiceResult<void>>();

  int sendCodeCalls = 0;
  int changePasswordCalls = 0;
  int logoutCalls = 0;
  String? lastNewPassword;
  String? lastEmailCode;

  void enqueueSendCode(ServiceResult<void> result) {
    _sendCodeResults.addLast(result);
  }

  void enqueueChangePassword(ServiceResult<void> result) {
    _changePasswordResults.addLast(result);
  }

  @override
  Future<ServiceResult<void>> sendChangePasswordEmailCode() async {
    sendCodeCalls++;
    if (_sendCodeResults.isEmpty) {
      return ServiceResult<void>.success();
    }
    return _sendCodeResults.removeFirst();
  }

  @override
  Future<ServiceResult<void>> changeOwnPassword({
    required String newPassword,
    required String emailCode,
  }) async {
    changePasswordCalls++;
    lastNewPassword = newPassword;
    lastEmailCode = emailCode;
    if (_changePasswordResults.isEmpty) {
      return ServiceResult<void>.success();
    }
    return _changePasswordResults.removeFirst();
  }

  @override
  Future<void> logout({bool notifyServer = true}) async {
    logoutCalls++;
  }
}

class _FakeLoginCredentialStorage extends LoginCredentialStorage {
  LoginCredentialState loadedState = const LoginCredentialState(
    account: 'saved-account',
    password: '',
  );
  LoginCredentialState? savedState;

  @override
  Future<LoginCredentialState> load(AppRegion region) async {
    return loadedState;
  }

  @override
  Future<void> save(LoginCredentialState state, AppRegion region) async {
    savedState = state;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAuthService authService;
  late _FakeLoginCredentialStorage credentialStorage;
  late ChangePasswordController controller;

  Future<void> pumpShell(WidgetTester tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        initialRoute: AppRoutes.home,
        getPages: [
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
    Get.testMode = true;
    Get.reset();
    authService = _FakeAuthService();
    credentialStorage = _FakeLoginCredentialStorage();
    Get.put<AuthService>(authService);
    controller = ChangePasswordController(
      authService: authService,
      credentialStorage: credentialStorage,
    );
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('controller ready does not send code automatically', (
    tester,
  ) async {
    await pumpShell(tester);

    controller.onReady();
    await tester.pump();

    expect(authService.sendCodeCalls, 0);
  });

  testWidgets('sendEmailCode shows backend failure message', (tester) async {
    await pumpShell(tester);
    authService.enqueueSendCode(
      ServiceResult<void>.failure(message: 'send failed from backend'),
    );

    await controller.sendEmailCode();
    await tester.pump();

    expect(authService.sendCodeCalls, 1);
    expect(controller.errorMessage.value, 'send failed from backend');
  });

  testWidgets('changePassword validates empty fields locally', (tester) async {
    await pumpShell(tester);

    await controller.changePassword(
      newPassword: ' ',
      confirmPassword: ' ',
      emailCode: ' ',
    );

    expect(authService.changePasswordCalls, 0);
    expect(controller.errorMessage.value, 'Please enter password');
  });

  testWidgets('changePassword rejects mismatch password', (tester) async {
    await pumpShell(tester);

    await controller.changePassword(
      newPassword: 'Password123',
      confirmPassword: 'Password124',
      emailCode: '654321',
    );

    expect(authService.changePasswordCalls, 0);
    expect(controller.errorMessage.value, 'The two passwords do not match');
  });

  testWidgets('changePassword trims input before request and logs out', (
    tester,
  ) async {
    await pumpShell(tester);
    authService.enqueueChangePassword(ServiceResult<void>.success());

    await controller.changePassword(
      newPassword: ' Password123 ',
      confirmPassword: ' Password123 ',
      emailCode: ' 654321 ',
    );
    await tester.pumpAndSettle();

    expect(authService.changePasswordCalls, 1);
    expect(authService.logoutCalls, 1);
    expect(authService.lastNewPassword, 'Password123');
    expect(authService.lastEmailCode, '654321');
    expect(credentialStorage.savedState, isNotNull);
    expect(credentialStorage.savedState!.account, 'saved-account');
    expect(credentialStorage.savedState!.password, '');
    expect(controller.errorMessage.value, isNull);
    expect(Get.currentRoute, AppRoutes.login);

    await tester.pump(const Duration(seconds: 3));
    await tester.pumpAndSettle();
  });
}
