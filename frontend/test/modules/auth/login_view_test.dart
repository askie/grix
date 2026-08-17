import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/apple_sign_in_service.dart';
import 'package:grix/data/providers/feature_flag_service.dart';
import 'package:grix/data/providers/google_sign_in_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/qr_login_service.dart';
import 'package:grix/modules/auth/app_agreement_view.dart';
import 'package:grix/modules/auth/controllers/login_controller.dart';
import 'package:grix/modules/auth/controllers/qr_login_controller.dart';
import 'package:grix/modules/auth/login_view.dart';
import 'package:grix/modules/auth/services/login_credential_storage.dart';
import 'package:grix/shared/utils/app_region_config.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  final RxBool _loggedIn = false.obs;

  @override
  bool get isLoggedIn => _loggedIn.value;

  @override
  RxBool get isLoggedInRx => _loggedIn;

  @override
  Future<ServiceResult<void>> login(String account, String password) async {
    return ServiceResult<void>.success();
  }

  @override
  Future<ServiceResult<void>> loginWithGoogle(String idToken) async {
    return ServiceResult<void>.success();
  }
}

class _FakeImService extends ImService {
  bool connected = false;

  @override
  bool get isConnected => connected;

  @override
  void connect(String wsUrl) {
    connected = true;
  }
}

class _FakeGoogleSignInService extends GoogleSignInService {
  @override
  Future<ServiceResult<String>> signIn() async {
    return ServiceResult<String>.success(data: 'google-id-token');
  }
}

class _FakeLoginCredentialStorage extends LoginCredentialStorage {
  @override
  Future<LoginCredentialState> load(AppRegion region) async {
    return const LoginCredentialState(
      account: 'saved_user',
      password: 'saved_pwd',
    );
  }
}

class _FakeQrLoginService extends QrLoginService {
  @override
  Future<ServiceResult<QRLoginCreateData>> createSession({
    String deviceLabel = '',
  }) async {
    return ServiceResult<QRLoginCreateData>.failure(message: 'create failed');
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  void registerDependencies() {
    Get.testMode = true;
    // 区域解析读本地存储，给个确定值，凭证恢复才能在解析完成后稳定填入。
    SharedPreferences.setMockInitialValues({'app_region': 'cn'});
    Get.reset();
    final authService = _FakeAuthService();
    final imService = _FakeImService();
    final credentialStorage = _FakeLoginCredentialStorage();
    final qrLoginService = _FakeQrLoginService();
    final googleSignInService = _FakeGoogleSignInService();
    final featureFlagService = FeatureFlagService();
    featureFlagService.features.value = ['auth_register'];
    Get.put<FeatureFlagService>(featureFlagService);
    Get.put<AuthService>(authService);
    Get.put<GoogleSignInService>(googleSignInService);
    Get.put<AppleSignInService>(AppleSignInService());
    Get.put<ImService>(imService);
    Get.put<QrLoginService>(qrLoginService);
    Get.put<LoginController>(
      LoginController(
        authService: authService,
        googleSignInService: googleSignInService,
        imService: imService,
        credentialStorage: credentialStorage,
      ),
    );
    Get.put<QrLoginController>(
      QrLoginController(
        authService: authService,
        qrLoginService: qrLoginService,
      ),
    );
  }

  tearDown(() {
    Get.reset();
  });

  testWidgets('stacks secondary actions on narrow widths without overflow', (
    tester,
  ) async {
    registerDependencies();
    await tester.binding.setSurfaceSize(const Size(240, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.lightTheme,
        darkTheme: AppTheme.darkTheme,
        themeMode: ThemeMode.light,
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const LoginView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(find.text('Create account'), findsOneWidget);
    expect(find.text('Forgot password'), findsOneWidget);
    expect(find.text('Remember me'), findsOneWidget);

    final accountField = tester.widget<TextField>(find.byType(TextField).at(0));
    final passwordField = tester.widget<TextField>(
      find.byType(TextField).at(1),
    );
    expect(accountField.controller?.text, 'saved_user');
    expect(passwordField.controller?.text, 'saved_pwd');
  });

  testWidgets('toggles password visibility from suffix icon', (tester) async {
    registerDependencies();
    await tester.binding.setSurfaceSize(const Size(240, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.lightTheme,
        darkTheme: AppTheme.darkTheme,
        themeMode: ThemeMode.light,
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const LoginView(),
      ),
    );
    await tester.pumpAndSettle();

    TextField passwordField() =>
        tester.widget<TextField>(find.byType(TextField).at(1));

    expect(passwordField().obscureText, isTrue);
    expect(find.byIcon(Icons.visibility_outlined), findsOneWidget);

    await tester.tap(find.byIcon(Icons.visibility_outlined));
    await tester.pump();

    expect(passwordField().obscureText, isFalse);
    expect(find.byIcon(Icons.visibility_off_outlined), findsOneWidget);
  });

  testWidgets('checks app agreement by default on login page', (tester) async {
    registerDependencies();
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.lightTheme,
        darkTheme: AppTheme.darkTheme,
        themeMode: ThemeMode.light,
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const LoginView(),
      ),
    );
    await tester.pumpAndSettle();

    final agreementCheckbox = tester.widget<Checkbox>(
      find.byKey(const Key('auth_app_agreement_checkbox')),
    );
    final loginButton = tester.widget<ElevatedButton>(
      find.widgetWithText(ElevatedButton, 'Sign In'),
    );
    expect(agreementCheckbox.value, isTrue);
    expect(agreementCheckbox.activeColor, AppTheme.primaryColor);
    expect(agreementCheckbox.checkColor, Colors.white);
    expect(loginButton.onPressed, isNotNull);
    expect(find.text('Continue with Google'), findsNothing);
    expect(find.text('APP Agreement'), findsOneWidget);
  });

  testWidgets('opens app agreement page from consent link', (tester) async {
    registerDependencies();
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        initialRoute: AppRoutes.login,
        getPages: [
          GetPage(
            name: AppRoutes.login,
            page: () => const LoginView(),
            binding: BindingsBuilder(() {}),
          ),
          GetPage(
            name: AppRoutes.appAgreement,
            page: () => const AppAgreementView(),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();

    await tester.ensureVisible(
      find.byKey(const Key('auth_app_agreement_link_button')),
    );
    await tester.tap(find.byKey(const Key('auth_app_agreement_link_button')));
    await tester.pumpAndSettle();

    expect(find.text('Important Risk Notice'), findsOneWidget);
    expect(find.text('APP Agreement'), findsWidgets);
  });

  testWidgets('hides credential form for qr scan entry route', (tester) async {
    registerDependencies();
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        initialRoute: '${AppRoutes.login}?sid=session_1&qt=token_1',
        getPages: [
          GetPage(
            name: AppRoutes.login,
            page: () => const LoginView(),
            binding: BindingsBuilder(() {}),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Account or Email'), findsNothing);
    expect(find.text('Password'), findsNothing);
    expect(find.text('Remember me'), findsNothing);
    expect(find.byType(ElevatedButton), findsNothing);
    expect(find.text('Continue with Google'), findsNothing);
  });

  testWidgets('hides credential form when desktop qr login is expanded', (
    tester,
  ) async {
    registerDependencies();
    await tester.binding.setSurfaceSize(const Size(900, 720));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('zh', 'CN'),
        home: const LoginView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(TextField), findsNWidgets(2));
    expect(find.byIcon(Icons.qr_code_2_rounded), findsOneWidget);

    await tester.ensureVisible(find.byIcon(Icons.qr_code_2_rounded));
    await tester.tap(find.byIcon(Icons.qr_code_2_rounded));
    await tester.pumpAndSettle();

    expect(find.byIcon(Icons.expand_less_rounded), findsOneWidget);
    expect(find.byType(TextField), findsNothing);
    expect(find.byType(ElevatedButton), findsNothing);
    expect(find.text('Google 登录'), findsNothing);
  });
}
