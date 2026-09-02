import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/modules/auth/widgets/bind_email_dialog.dart';

class _FakeAuthService extends AuthService {
  _FakeAuthService({this.bindOk = true});

  final bool bindOk;
  String? sentTo;
  String? boundEmail;
  String? boundCode;
  int profileRefreshCount = 0;

  @override
  Future<ServiceResult<void>> sendBindEmailCode({required String email}) async {
    sentTo = email;
    return ServiceResult<void>.success();
  }

  @override
  Future<ServiceResult<void>> bindEmail({
    required String email,
    required String code,
  }) async {
    boundEmail = email;
    boundCode = code;
    return bindOk
        ? ServiceResult<void>.success()
        : ServiceResult<void>.failure(message: 'code invalid');
  }

  @override
  Future<ServiceResult<User>> fetchCurrentUserProfile() async {
    profileRefreshCount += 1;
    return ServiceResult<User>.failure(message: 'not needed');
  }
}

/// Toast 用 3 秒定时器自行消散，测试结束前要把它跑完，否则 pending timer 会判失败。
Future<void> _drainToast(WidgetTester tester) async {
  await tester.pump(const Duration(seconds: 4));
  await tester.pumpAndSettle();
}

Future<bool?> _pumpDialog(WidgetTester tester) async {
  bool? result;
  await tester.pumpWidget(
    GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      fallbackLocale: const Locale('en', 'US'),
      home: Builder(
        builder: (context) => Scaffold(
          body: Center(
            child: ElevatedButton(
              onPressed: () async {
                result = await showBindEmailDialog(context);
              },
              child: const Text('open'),
            ),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
  return result;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAuthService auth;

  void register({bool bindOk = true}) {
    Get.testMode = true;
    Get.reset();
    auth = _FakeAuthService(bindOk: bindOk);
    Get.put<AuthService>(auth);
  }

  tearDown(Get.reset);

  testWidgets('keeps send disabled until the email looks valid', (
    tester,
  ) async {
    register();
    await _pumpDialog(tester);

    TextButton sendButton() =>
        tester.widget<TextButton>(find.widgetWithText(TextButton, 'Send code'));

    expect(sendButton().onPressed, isNull);

    await tester.enterText(find.byType(TextField).first, 'not-an-email');
    await tester.pump();
    expect(sendButton().onPressed, isNull);

    await tester.enterText(find.byType(TextField).first, 'user@example.com');
    await tester.pump();
    expect(sendButton().onPressed, isNotNull);
  });

  testWidgets('sends the code and binds the email', (tester) async {
    register();
    await _pumpDialog(tester);

    await tester.enterText(find.byType(TextField).first, ' user@example.com ');
    await tester.pump();
    await tester.tap(find.widgetWithText(TextButton, 'Send code'));
    await tester.pumpAndSettle();
    expect(auth.sentTo, 'user@example.com');
    await _drainToast(tester);

    await tester.enterText(find.byType(TextField).last, '123456');
    await tester.pump();
    await tester.tap(find.widgetWithText(TextButton, 'Bind'));
    await tester.pumpAndSettle();

    expect(auth.boundEmail, 'user@example.com');
    expect(auth.boundCode, '123456');
    expect(auth.profileRefreshCount, 1);
    expect(find.byType(AlertDialog), findsNothing);
    await _drainToast(tester);
  });

  testWidgets('stays open when binding fails', (tester) async {
    register(bindOk: false);
    await _pumpDialog(tester);

    await tester.enterText(find.byType(TextField).first, 'user@example.com');
    await tester.enterText(find.byType(TextField).last, '000000');
    await tester.pump();
    await tester.tap(find.widgetWithText(TextButton, 'Bind'));
    await tester.pumpAndSettle();

    expect(find.byType(AlertDialog), findsOneWidget);
    await _drainToast(tester);
  });
}
