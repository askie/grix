import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/modules/auth/controllers/reset_password_controller.dart';
import 'package:grix/modules/auth/reset_password_view.dart';

class _FakeAuthService extends AuthService {
  int sendEmailCodeCalls = 0;

  @override
  Future<ServiceResult<CaptchaData>> fetchCaptcha() async {
    return ServiceResult<CaptchaData>.success(
      data: const CaptchaData(captchaId: 'captcha-id', b64s: 'invalid'),
    );
  }

  @override
  Future<ServiceResult<void>> sendEmailCode({
    required String email,
    required String scene,
    String? captchaId,
    String? captchaValue,
  }) async {
    sendEmailCodeCalls++;
    return ServiceResult<void>.success();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAuthService authService;
  late ResetPasswordController controller;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    authService = _FakeAuthService();
    Get.put<AuthService>(authService);
    controller = Get.put<ResetPasswordController>(ResetPasswordController());
  });

  tearDown(() {
    Get.reset();
  });

  Future<void> pumpPage(WidgetTester tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('en', 'US'),
        home: const ResetPasswordView(),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('password field supports plaintext toggle', (tester) async {
    await pumpPage(tester);

    TextField passwordField() =>
        tester.widget<TextField>(find.byType(TextField).at(1));

    expect(passwordField().obscureText, isTrue);
    expect(find.byIcon(Icons.visibility_outlined), findsOneWidget);

    await tester.tap(find.byIcon(Icons.visibility_outlined));
    await tester.pump();

    expect(passwordField().obscureText, isFalse);
    expect(find.byIcon(Icons.visibility_off_outlined), findsOneWidget);
  });

  testWidgets('captcha section hides after sending code and shows on resend', (
    tester,
  ) async {
    await pumpPage(tester);

    expect(find.byIcon(Icons.refresh_rounded), findsOneWidget);
    expect(find.byType(TextField), findsNWidgets(4));

    await tester.enterText(find.byType(TextField).at(0), 'user@example.com');
    await tester.enterText(find.byType(TextField).at(2), 'abcd');
    await tester.tap(find.byType(OutlinedButton));
    await tester.pumpAndSettle();

    expect(authService.sendEmailCodeCalls, 1);
    expect(controller.canRequestEmailCode, isFalse);
    expect(find.byIcon(Icons.refresh_rounded), findsNothing);
    expect(find.byType(TextField), findsNWidgets(3));

    controller.sendCodeCountdown.value = 0;
    await tester.pump();

    expect(controller.canRequestEmailCode, isTrue);
    expect(find.byIcon(Icons.refresh_rounded), findsOneWidget);
    expect(find.byType(TextField), findsNWidgets(4));

    await tester.pump(const Duration(seconds: 3));
    await tester.pumpAndSettle();
  });
}
