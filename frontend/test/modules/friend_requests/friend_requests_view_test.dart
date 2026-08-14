import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/friend_requests/bindings/friend_requests_binding.dart';
import 'package:grix/modules/friend_requests/controllers/friend_requests_controller.dart';
import 'package:grix/modules/friend_requests/friend_requests_view.dart';
import 'package:grix/modules/home/controllers/contacts_controller.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';
import 'package:grix/modules/home/contacts_view.dart';

class _FakeFriendService extends FriendService {
  @override
  Future<void> loadFriendList() async {}

  @override
  Future<void> loadFriendRequests() async {}
}

class _FakeImService extends ImService {}

FriendRequestItem _request({
  required String id,
  required String username,
  required String nickname,
  required String message,
  required int status,
}) {
  return FriendRequestItem(
    id: id,
    fromUserId: 'from-$id',
    username: username,
    nickname: nickname,
    avatarUrl: '',
    message: message,
    status: status,
    createdAt: '2026-03-11T10:00:00Z',
  );
}

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('friend requests entry opens standalone page instead of dialog',
      (WidgetTester tester) async {
    final friendService = _FakeFriendService();
    friendService.friendRequests.assignAll([
      _request(
        id: 'req-1',
        username: 'long_username',
        nickname: 'Long Nickname',
        message: 'hello world',
        status: 0,
      ),
    ]);

    Get.put<FriendService>(friendService);
    Get.put<ImService>(_FakeImService());
    Get.put<FriendQrFlowService>(FriendQrFlowService());
    Get.put(ContactsController());

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const ContactsView(),
        getPages: [
          GetPage(
            name: AppRoutes.friendRequests,
            page: () => const FriendRequestsView(),
            binding: FriendRequestsBinding(),
          ),
        ],
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('好友请求').first);
    await tester.pumpAndSettle();

    expect(find.byType(Dialog), findsNothing);
    expect(find.byType(FriendRequestsView), findsOneWidget);
  });

  testWidgets('friend request texts stay on a single line',
      (WidgetTester tester) async {
    const longNickname = '3517604972351760497235176049723517604972';
    const longUsername = 'very_long_username_1234567890_abcdefghijk';
    const longMessage = '这是一条很长很长的好友申请留言用于验证页面文本不会发生换行显示';

    final friendService = _FakeFriendService();
    friendService.friendRequests.assignAll([
      _request(
        id: 'req-2',
        username: longUsername,
        nickname: longNickname,
        message: longMessage,
        status: 0,
      ),
    ]);

    Get.put<FriendService>(friendService);
    Get.put(FriendRequestsController());

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const FriendRequestsView(),
      ),
    );
    await tester.pumpAndSettle();

    final nicknameText = tester.widget<Text>(find.text(longNickname));
    final usernameText = tester.widget<Text>(find.text('@$longUsername'));
    final messageText = tester.widget<Text>(find.text(longMessage));

    expect(nicknameText.maxLines, 1);
    expect(nicknameText.overflow, TextOverflow.ellipsis);
    expect(nicknameText.softWrap, isFalse);

    expect(usernameText.maxLines, 1);
    expect(usernameText.overflow, TextOverflow.ellipsis);
    expect(usernameText.softWrap, isFalse);

    expect(messageText.maxLines, 1);
    expect(messageText.overflow, TextOverflow.ellipsis);
    expect(messageText.softWrap, isFalse);
  });
}
