import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/saved_account_store.dart';
import 'package:grix/modules/profile/account_switch_view.dart';
import 'package:grix/modules/profile/controllers/account_switch_controller.dart';

class _FakeAuthService extends AuthService {
  _FakeAuthService(this.savedAccounts);

  final List<SavedAccount> savedAccounts;
  final switchedTo = <String>[];
  final removed = <String>[];
  AccountSwitchOutcome switchOutcome = AccountSwitchOutcome.success;
  String currentUserIdOverride = '1001';

  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => currentUserIdOverride;

  @override
  Future<List<SavedAccount>> listSavedAccounts() async => savedAccounts;

  @override
  Future<AccountSwitchOutcome> switchToSavedAccount(
    String targetUserId,
  ) async {
    switchedTo.add(targetUserId);
    return switchOutcome;
  }

  @override
  Future<void> removeSavedAccount(String targetUserId) async {
    removed.add(targetUserId);
    savedAccounts.removeWhere((a) => a.userId == targetUserId);
  }
}

SavedAccount buildAccount(
  String userId, {
  String refreshToken = 'refresh',
  String nickname = '',
}) {
  return SavedAccount(
    userId: userId,
    username: 'user_$userId',
    nickname: nickname.isNotEmpty ? nickname : 'nick_$userId',
    email: 'u$userId@example.com',
    accessToken: refreshToken.isEmpty ? '' : 'access_$userId',
    refreshToken: refreshToken,
  );
}

Future<void> _pumpAccountSwitchView(
  WidgetTester tester,
  _FakeAuthService authService,
) async {
  Get.put<AuthService>(authService);
  await tester.pumpWidget(
    GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      initialRoute: AppRoutes.accountSwitch,
      getPages: [
        GetPage(
          name: AppRoutes.accountSwitch,
          page: () => const AccountSwitchView(),
          binding: BindingsBuilder(() {
            Get.lazyPut(() => AccountSwitchController());
          }),
        ),
        GetPage(name: AppRoutes.home, page: () => const SizedBox.shrink()),
        GetPage(name: AppRoutes.login, page: () => const SizedBox.shrink()),
      ],
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('renders accounts with current mark, expired hint and add entry',
      (WidgetTester tester) async {
    final authService = _FakeAuthService([
      buildAccount('1001', nickname: 'Alice'),
      buildAccount('2002', nickname: 'Bob', refreshToken: ''),
    ]);

    await _pumpAccountSwitchView(tester, authService);

    expect(find.text('Alice'), findsOneWidget);
    expect(find.text('Bob'), findsOneWidget);
    // 当前账号（1001）带勾选标记。
    expect(find.byIcon(Icons.check_circle_rounded), findsOneWidget);
    // 过期账号（2002）展示重登提示。
    expect(find.text('Sign-in expired, please sign in again'), findsOneWidget);
    expect(find.text('Add Account'), findsOneWidget);
  });

  testWidgets('tapping another account triggers switch and navigates home',
      (WidgetTester tester) async {
    final authService = _FakeAuthService([
      buildAccount('1001', nickname: 'Alice'),
      buildAccount('2002', nickname: 'Bob'),
    ]);

    await _pumpAccountSwitchView(tester, authService);
    await tester.tap(find.text('Bob'));
    await tester.pumpAndSettle();

    expect(authService.switchedTo, ['2002']);
    expect(Get.currentRoute, AppRoutes.home);
  });

  testWidgets('tapping current account does not switch', (
    WidgetTester tester,
  ) async {
    final authService = _FakeAuthService([
      buildAccount('1001', nickname: 'Alice'),
    ]);

    await _pumpAccountSwitchView(tester, authService);
    await tester.tap(find.text('Alice'));
    await tester.pumpAndSettle();

    expect(authService.switchedTo, isEmpty);
  });

  testWidgets('remove flow asks for confirmation before deleting', (
    WidgetTester tester,
  ) async {
    final authService = _FakeAuthService([
      buildAccount('1001', nickname: 'Alice'),
      buildAccount('2002', nickname: 'Bob'),
    ]);

    await _pumpAccountSwitchView(tester, authService);
    // 每行一个删除按钮，点第二行（Bob）。
    await tester.tap(find.byIcon(Icons.delete_outline_rounded).last);
    await tester.pumpAndSettle();

    expect(find.text('Remove Account'), findsOneWidget);
    // 取消不删除。
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(authService.removed, isEmpty);

    // 再次进入并确认删除。
    await tester.tap(find.byIcon(Icons.delete_outline_rounded).last);
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(ElevatedButton, 'Remove'));
    await tester.pumpAndSettle();

    expect(authService.removed, ['2002']);
    expect(find.text('Bob'), findsNothing);
  });
}
