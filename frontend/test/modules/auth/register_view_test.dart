import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/auth/user_agreement_view.dart';
import 'package:grix/modules/auth/controllers/register_controller.dart';
import 'package:grix/modules/auth/register_view.dart';

class _FakeAuthService extends AuthService {
  ServiceResult<void> registerResult = ServiceResult<void>.success();

  @override
  Future<ServiceResult<void>> sendEmailCode({
    required String email,
    required String scene,
    String? captchaId,
    String? captchaValue,
  }) async {
    return ServiceResult<void>.success();
  }

  @override
  Future<ServiceResult<void>> register({
    required String email,
    required String password,
    required String emailCode,
    String region = '',
  }) async {
    return registerResult;
  }
}

class _FakeImService extends ImService {
  @override
  bool get isConnected => false;

  @override
  void connect(String wsUrl) {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAuthService authService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    authService = _FakeAuthService();
    Get.put<AuthService>(authService);
    Get.put<ImService>(_FakeImService());
    Get.put<RegisterController>(RegisterController());
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('leaves app agreement unchecked by default on register page', (
    tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.lightTheme,
        darkTheme: AppTheme.darkTheme,
        themeMode: ThemeMode.light,
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const RegisterView(),
      ),
    );
    await tester.pumpAndSettle();

    final agreementCheckbox = tester.widget<Checkbox>(
      find.byKey(const Key('auth_app_agreement_checkbox')),
    );
    final sendCodeButton = tester.widget<OutlinedButton>(
      find.widgetWithText(OutlinedButton, 'Send Code'),
    );
    final registerButton = tester.widget<ElevatedButton>(
      find.widgetWithText(ElevatedButton, 'Register'),
    );

    expect(agreementCheckbox.value, isFalse);
    expect(agreementCheckbox.activeColor, AppTheme.primaryColor);
    expect(agreementCheckbox.checkColor, Colors.white);
    expect(sendCodeButton.onPressed, isNotNull);
    expect(registerButton.onPressed, isNotNull);
    expect(find.text('User Agreement'), findsOneWidget);
    expect(find.text('Privacy Policy'), findsOneWidget);
  });

  testWidgets('blocks register until agreement is checked', (tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.lightTheme,
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const RegisterView(),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(ElevatedButton, 'Register'));
    await tester.pump();

    expect(
      find.text(
        'Please read and check the User Agreement and Privacy Policy before continuing',
      ),
      findsOneWidget,
    );
  });

  testWidgets('password field supports plaintext toggle', (tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const RegisterView(),
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

  testWidgets('register page does not show captcha fields', (tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('en', 'US'),
        home: const RegisterView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('图形验证码'), findsNothing);
    expect(find.byIcon(Icons.refresh_rounded), findsNothing);
    expect(find.byType(TextField), findsNWidgets(3));
  });

  testWidgets('opens user agreement page from register page', (tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        initialRoute: AppRoutes.register,
        getPages: [
          GetPage(name: AppRoutes.register, page: () => const RegisterView()),
          GetPage(
            name: AppRoutes.userAgreement,
            page: () => const UserAgreementView(),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('auth_user_agreement_link_button')));
    await tester.pumpAndSettle();

    expect(find.text('User Agreement'), findsWidgets);
  });

  testWidgets('shows top toast when register fails', (tester) async {
    authService.registerResult = ServiceResult<void>.failure(
      message: 'Email already exists',
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const RegisterView(),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('auth_app_agreement_checkbox')));
    await tester.pump();

    await tester.enterText(
      find.widgetWithText(TextField, 'Email'),
      'user@example.com',
    );
    await tester.enterText(
      find.widgetWithText(TextField, 'Password'),
      'password123',
    );
    await tester.enterText(
      find.widgetWithText(TextField, 'Email Code'),
      '123456',
    );

    await tester.tap(find.widgetWithText(ElevatedButton, 'Register'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 200));

    expect(find.text('Email already exists'), findsWidgets);
    expect(find.byIcon(Icons.error_outline_rounded), findsOneWidget);

    await tester.pump(const Duration(seconds: 3));
    await tester.pumpAndSettle();
  });
}
