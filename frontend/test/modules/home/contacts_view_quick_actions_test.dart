import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/home/contacts_view.dart';
import 'package:grix/modules/home/controllers/contacts_controller.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';

class _FakeImService extends ImService {}

class _FakeFriendService extends FriendService {
  _FakeFriendService() {
    friendList.assignAll([
      FriendItem(
        id: '1',
        userId: '1001',
        username: 'alice',
        nickname: 'Alice',
        remarkName: '',
        avatarUrl: '',
      ),
    ]);
  }

  @override
  Future<void> loadFriendList() async {}

  @override
  Future<void> loadFriendRequests() async {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    Get.put<ImService>(_FakeImService());
    Get.put<FriendService>(_FakeFriendService());
    Get.put<FriendQrFlowService>(FriendQrFlowService());
    Get.put(ContactsController());
  });

  tearDown(() {
    Get.reset();
  });

  Widget buildApp() {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      fallbackLocale: const Locale('en', 'US'),
      home: const ContactsView(),
    );
  }

  testWidgets('contacts function list does not contain my qr entry', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    expect(find.text('Add Friend'), findsOneWidget);
    expect(find.text('conversations_scan_user_qr'.tr), findsOneWidget);
    expect(find.text('Friend Requests'), findsOneWidget);
    expect(find.text('New Group Chat'), findsNothing);
    expect(find.text('My QR Code'), findsNothing);
  });

  testWidgets('friend row menu contains delete and block actions', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(buildApp());
    await tester.pumpAndSettle();

    await tester.tap(find.byTooltip('Friend actions').first);
    await tester.pumpAndSettle();

    expect(find.text('Delete Friend'), findsOneWidget);
    expect(find.text('Block User'), findsOneWidget);
  });
}
