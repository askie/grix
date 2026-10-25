import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/modules/auth/controllers/phone_login_controller.dart';

class _FakeAuthService extends AuthService {
  _FakeAuthService({required this.challenge});

  final bool challenge;
  int loginCalls = 0;
  final List<String> codes = [];
  final List<String?> tickets = [];

  @override
  Future<ServiceResult<PhoneLoginResult>> phoneLoginWithCode({
    required String phoneE164,
    String code = '',
    String? registrationTicket,
  }) async {
    loginCalls++;
    codes.add(code);
    tickets.add(registrationTicket);
    if (challenge && loginCalls == 1) {
      return ServiceResult<PhoneLoginResult>.success(
        data: const PhoneLoginResult(
          requiresRegistrationConfirmation: true,
          registrationTicket: 'ticket-from-server',
        ),
      );
    }
    return ServiceResult<PhoneLoginResult>.success(
      data: const PhoneLoginResult(),
    );
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(Get.reset);

  Future<void> pumpApp(WidgetTester tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const Scaffold(body: SizedBox.shrink()),
        getPages: [
          GetPage(
            name: AppRoutes.home,
            page: () => const Scaffold(body: Text('Home')),
          ),
        ],
      ),
    );
  }

  PhoneLoginController controller(_FakeAuthService auth) {
    return PhoneLoginController(authService: auth, mode: PhoneFlowMode.login)
      ..phone.value = '13800138000'
      ..code.value = '654321';
  }

  testWidgets('unregistered phone cancel does not create account', (
    tester,
  ) async {
    final auth = _FakeAuthService(challenge: true);
    final c = controller(auth);
    await pumpApp(tester);

    final submitting = c.submit();
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('phone_registration_confirmation_dialog')),
      findsOneWidget,
    );
    expect(find.textContaining('not registered'), findsOneWidget);

    await tester.tap(find.byKey(const Key('phone_registration_cancel_button')));
    await tester.pumpAndSettle();
    await submitting;

    expect(auth.loginCalls, 1);
    expect(auth.tickets, [null]);
    expect(find.text('Home'), findsNothing);
  });

  testWidgets('unregistered phone confirm reuses registration ticket', (
    tester,
  ) async {
    final auth = _FakeAuthService(challenge: true);
    final c = controller(auth);
    await pumpApp(tester);

    final submitting = c.submit();
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(const Key('phone_registration_confirm_button')),
    );
    await tester.pumpAndSettle();
    await submitting;

    expect(auth.loginCalls, 2);
    expect(auth.codes, ['654321', '']);
    expect(auth.tickets, [null, 'ticket-from-server']);
    expect(find.text('Home'), findsOneWidget);
  });

  testWidgets('registered phone logs in without confirmation dialog', (
    tester,
  ) async {
    final auth = _FakeAuthService(challenge: false);
    final c = controller(auth);
    await pumpApp(tester);

    await c.submit();
    await tester.pumpAndSettle();

    expect(auth.loginCalls, 1);
    expect(
      find.byKey(const Key('phone_registration_confirmation_dialog')),
      findsNothing,
    );
    expect(find.text('Home'), findsOneWidget);
  });
}
