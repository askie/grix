import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/modules/auth/services/bind_email_prompt.dart';

class _FakeAuthService extends AuthService {
  final Rxn<User> _userState = Rxn<User>();

  @override
  User? get user => _userState.value;

  void setUser(User? value) => _userState.value = value;
}

User _user({required String id, String email = ''}) =>
    User(id: id, username: 'u$id', email: email, nickname: 'u$id');

Future<void> _pumpHost(WidgetTester tester) async {
  await tester.pumpWidget(
    GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      fallbackLocale: const Locale('en', 'US'),
      home: const Scaffold(body: SizedBox.shrink()),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAuthService auth;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    resetBindEmailPromptForTest();
    auth = _FakeAuthService();
    Get.put<AuthService>(auth);
  });

  tearDown(Get.reset);

  testWidgets('does not prompt when the account already has an email', (
    tester,
  ) async {
    auth.setUser(_user(id: '1', email: 'bound@example.com'));
    await _pumpHost(tester);

    await maybePromptBindEmail();
    await tester.pumpAndSettle();

    expect(find.byType(AlertDialog), findsNothing);
  });

  testWidgets('prompts when the only email is an Apple relay address', (
    tester,
  ) async {
    auth.setUser(_user(id: '9', email: 'abc123@privaterelay.appleid.com'));
    await _pumpHost(tester);

    unawaited(maybePromptBindEmail());
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsOneWidget);

    await tester.tap(find.widgetWithText(TextButton, 'Not now'));
    await tester.pumpAndSettle();
  });

  testWidgets('prompts once per account and again after switching', (
    tester,
  ) async {
    auth.setUser(_user(id: '1'));
    await _pumpHost(tester);

    // 弹窗打开期间 maybePromptBindEmail 不会返回，所以这里不能 await。
    unawaited(maybePromptBindEmail());
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsOneWidget);

    // 关掉弹窗（「暂不」）后同一账号本次运行内不再打扰。
    await tester.tap(find.widgetWithText(TextButton, 'Not now'));
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);

    await maybePromptBindEmail();
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);

    // 切到另一个没绑邮箱的账号仍会提示。
    auth.setUser(_user(id: '2'));
    unawaited(maybePromptBindEmail());
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsOneWidget);

    await tester.tap(find.widgetWithText(TextButton, 'Not now'));
    await tester.pumpAndSettle();
  });
}
