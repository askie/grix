import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/account_info/controllers/account_info_controller.dart';

class _FakeImService extends ImService {
  @override
  bool get isConnected => true;
}

class _FakeFriendService extends FriendService {
  @override
  Future<String?> fetchUserProfile(String userId) async => null;
}

void _openProfile(String peerId, String nickname) {
  Get.toNamed(
    AppRoutes.accountInfo,
    arguments: {
      'peer_id': peerId,
      'peer_type': '1',
      'nickname': nickname,
      'username': nickname.toLowerCase(),
    },
    parameters: {'peer_id': peerId, 'peer_type': '1'},
  );
}

Future<void> _pumpApp(WidgetTester tester) async {
  Get.put<ImService>(_FakeImService());
  Get.put<FriendService>(_FakeFriendService());
  final accountPage = AppRoutes.routes.firstWhere(
    (page) => page.name == AppRoutes.accountInfo,
  );
  await tester.pumpWidget(
    GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      initialRoute: '/stub',
      getPages: [
        GetPage(
          name: '/stub',
          page: () => const Scaffold(body: Text('STUB')),
        ),
        accountPage,
      ],
    ),
  );
  await tester.pumpAndSettle();
}

bool _anyAccountInfoControllerRegistered() {
  // Tagged instances are keyed as "AccountInfoController<tag>".
  return Get.isRegistered<AccountInfoController>() ||
      List.generate(
        64,
        (i) => Get.isRegistered<AccountInfoController>(
          tag: 'route_account_info_${i + 1}',
        ),
      ).any((registered) => registered);
}

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(() => Get.reset());

  testWidgets('profile pushed above another profile shows its own peer', (
    tester,
  ) async {
    await _pumpApp(tester);
    _openProfile('1', 'Alice');
    await tester.pumpAndSettle();
    expect(find.text('Alice'), findsWidgets);

    _openProfile('2', 'Bob');
    await tester.pumpAndSettle();
    expect(find.text('Bob'), findsWidgets);
    expect(find.text('Alice'), findsNothing);

    Get.back();
    await tester.pumpAndSettle();
    expect(find.text('Alice'), findsWidgets);
    expect(find.text('Bob'), findsNothing);
  });

  testWidgets(
    'controller is released even when a non-GetX route is pushed in the same tick',
    (tester) async {
      await _pumpApp(tester);
      final ctx = Get.context!;
      _openProfile('1', 'Alice');
      showDialog<void>(context: ctx, builder: (_) => const Text('DLG'));
      await tester.pumpAndSettle();
      Navigator.of(ctx, rootNavigator: true).pop();
      await tester.pumpAndSettle();
      expect(find.text('Alice'), findsWidgets);

      Get.back();
      await tester.pumpAndSettle();
      expect(_anyAccountInfoControllerRegistered(), isFalse);

      _openProfile('2', 'Bob');
      await tester.pumpAndSettle();
      expect(find.text('Bob'), findsWidgets);
      expect(find.text('Alice'), findsNothing);
    },
  );

  testWidgets('reopening after pop shows the newly requested peer', (
    tester,
  ) async {
    await _pumpApp(tester);
    _openProfile('1', 'Alice');
    await tester.pumpAndSettle();
    Get.back();
    await tester.pumpAndSettle();
    expect(_anyAccountInfoControllerRegistered(), isFalse);

    _openProfile('2', 'Bob');
    await tester.pumpAndSettle();
    expect(find.text('Bob'), findsWidgets);
    expect(find.text('Alice'), findsNothing);
  });
}
