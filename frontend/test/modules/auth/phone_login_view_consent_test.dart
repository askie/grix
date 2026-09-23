import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/auth/controllers/phone_login_controller.dart';
import 'package:grix/modules/auth/phone_login_view.dart';
import 'package:grix/modules/auth/user_agreement_view.dart';

class _FakeAuthService extends AuthService {
  @override
  Future<ServiceResult<AuthMethods>> fetchAuthMethods({
    required String region,
  }) async {
    return ServiceResult<AuthMethods>.success(
      data: const AuthMethods(
        region: 'cn',
        phoneLoginEnabled: true,
        phoneRegisterEnabled: true,
      ),
    );
  }
}

class _FakeImService extends ImService {
  @override
  bool get isConnected => false;

  @override
  void connect(String wsUrl) {}
}

Future<void> _declineAgreementDialog(WidgetTester tester) async {
  expect(find.byKey(const Key('auth_app_agreement_dialog')), findsOneWidget);
  await tester.tap(
    find.byKey(const Key('auth_app_agreement_dialog_disagree_button')),
  );
  await tester.pumpAndSettle();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    Get.put<AuthService>(_FakeAuthService());
    Get.put<ImService>(_FakeImService());
    Get.put<PhoneLoginController>(
      PhoneLoginController(mode: PhoneFlowMode.login),
    );
  });

  tearDown(Get.reset);

  testWidgets('phone login leaves agreement unchecked by default', (
    tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const PhoneLoginView(),
      ),
    );
    await tester.pumpAndSettle();
    await _declineAgreementDialog(tester);

    final checkbox = tester.widget<Checkbox>(
      find.byKey(const Key('auth_app_agreement_checkbox')),
    );
    expect(checkbox.value, isFalse);
    expect(find.text('User Agreement'), findsOneWidget);
    expect(find.text('Privacy Policy'), findsOneWidget);
  });

  testWidgets('phone login blocks submit until agreement checked', (
    tester,
  ) async {
    final controller = Get.find<PhoneLoginController>();
    controller.phone.value = '13800138000';
    controller.code.value = '123456';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const PhoneLoginView(),
      ),
    );
    await tester.pumpAndSettle();
    await _declineAgreementDialog(tester);

    await tester.tap(find.byType(FilledButton));
    await tester.pump();

    expect(
      find.text(
        'Please read and check the User Agreement and Privacy Policy before continuing',
      ),
      findsOneWidget,
    );
  });

  testWidgets('phone login opens user agreement from consent link', (
    tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        initialRoute: AppRoutes.phoneLogin,
        getPages: [
          GetPage(
            name: AppRoutes.phoneLogin,
            page: () => const PhoneLoginView(),
          ),
          GetPage(
            name: AppRoutes.userAgreement,
            page: () => const UserAgreementView(),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();
    await _declineAgreementDialog(tester);

    await tester.tap(find.byKey(const Key('auth_user_agreement_link_button')));
    await tester.pumpAndSettle();

    expect(find.text('User Agreement'), findsWidgets);
  });

  testWidgets('phone bind mode does not pop agreement dialog', (tester) async {
    Get.delete<PhoneLoginController>();
    Get.put<PhoneLoginController>(
      PhoneLoginController(mode: PhoneFlowMode.bind),
    );
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const PhoneLoginView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('auth_app_agreement_dialog')), findsNothing);
  });
}
