import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/home/controllers/contacts_controller.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';

class _FakeFriendService extends FriendService {
  int loadFriendListCalls = 0;
  int loadFriendRequestsCalls = 0;
  int sendFriendRequestCalls = 0;
  int deleteFriendCalls = 0;
  int blockUserCalls = 0;
  String lastToUserId = '';
  String lastToUsername = '';
  String lastDeletedFriendUserId = '';
  String lastBlockedUserId = '';
  FriendRequestSendResult sendResult = const FriendRequestSendResult(
    success: true,
    autoApproved: false,
  );
  bool deleteFriendResult = true;
  bool blockUserResult = true;

  @override
  Future<void> loadFriendList() async {
    loadFriendListCalls++;
  }

  @override
  Future<void> loadFriendRequests() async {
    loadFriendRequestsCalls++;
  }

  @override
  Future<FriendRequestSendResult> sendFriendRequest({
    String? toUserId,
    String? toUsername,
    String message = '',
  }) async {
    sendFriendRequestCalls++;
    lastToUserId = (toUserId ?? '').trim();
    lastToUsername = (toUsername ?? '').trim();
    return sendResult;
  }

  @override
  Future<bool> deleteFriend(String friendUserId) async {
    deleteFriendCalls++;
    lastDeletedFriendUserId = friendUserId;
    return deleteFriendResult;
  }

  @override
  Future<bool> blockUser(String blockedUserId) async {
    blockUserCalls++;
    lastBlockedUserId = blockedUserId;
    return blockUserResult;
  }
}

class _FakeImService extends ImService {
  int refreshSessionsNowCalls = 0;

  @override
  Future<void> refreshSessionsNow() async {
    refreshSessionsNowCalls++;
  }
}

class _TrackingContactsController extends ContactsController {
  int createChatCalls = 0;
  FriendItem? lastCreateChatFriend;

  @override
  Future<void> createChat(FriendItem friend) async {
    createChatCalls++;
    lastCreateChatFriend = friend;
  }
}

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(() {
    Get.reset();
  });

  test('refreshContacts loads friends and requests', () async {
    final friendService = _FakeFriendService();
    Get.put<FriendService>(friendService);
    Get.put<ImService>(_FakeImService());
    Get.put<FriendQrFlowService>(FriendQrFlowService());

    final controller = Get.put(ContactsController());
    await controller.refreshContacts();

    expect(friendService.loadFriendListCalls, 1);
    expect(friendService.loadFriendRequestsCalls, 1);
  });

  test('sendFriendRequest uses user id and marks request as sent', () async {
    final friendService = _FakeFriendService();
    Get.put<FriendService>(friendService);
    Get.put<ImService>(_FakeImService());
    Get.put<FriendQrFlowService>(FriendQrFlowService());

    final controller = Get.put(_TrackingContactsController());
    final success = await controller.sendFriendRequest(
      UserSearchItem(
        id: '1001',
        username: 'alice',
        nickname: 'Alice',
        avatarUrl: '',
      ),
    );

    expect(success, isTrue);
    expect(friendService.sendFriendRequestCalls, 1);
    expect(friendService.lastToUserId, '1001');
    expect(friendService.lastToUsername, '');
    expect(controller.sentUsernames.contains('alice'), isTrue);
    expect(controller.createChatCalls, 0);
  });

  test('sendFriendRequest opens chat when auto-approved', () async {
    final friendService = _FakeFriendService()
      ..sendResult = const FriendRequestSendResult(
        success: true,
        autoApproved: true,
      );
    Get.put<FriendService>(friendService);
    Get.put<ImService>(_FakeImService());
    Get.put<FriendQrFlowService>(FriendQrFlowService());

    final controller = Get.put(_TrackingContactsController());
    final success = await controller.sendFriendRequest(
      UserSearchItem(
        id: '2002',
        username: 'bob',
        nickname: 'Bob',
        avatarUrl: 'https://example.com/bob.png',
      ),
    );

    expect(success, isTrue);
    expect(friendService.sendFriendRequestCalls, 1);
    expect(friendService.lastToUserId, '2002');
    expect(controller.sentUsernames.contains('bob'), isFalse);
    expect(controller.createChatCalls, 1);
    expect(controller.lastCreateChatFriend?.userId, '2002');
    expect(controller.lastCreateChatFriend?.username, 'bob');
  });

  test('deleteFriend removes relation through provider', () async {
    final friendService = _FakeFriendService();
    final imService = _FakeImService();
    Get.put<FriendService>(friendService);
    Get.put<ImService>(imService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());

    final controller = Get.put(ContactsController());
    final success = await controller.deleteFriend(
      FriendItem(
        id: '1',
        userId: '3001',
        username: 'charlie',
        nickname: 'Charlie',
        remarkName: '',
        avatarUrl: '',
      ),
    );

    expect(success, isTrue);
    expect(friendService.deleteFriendCalls, 1);
    expect(friendService.lastDeletedFriendUserId, '3001');
    expect(imService.refreshSessionsNowCalls, 1);
  });

  test('blockUser delegates to provider and refreshes sessions', () async {
    final friendService = _FakeFriendService();
    final imService = _FakeImService();
    Get.put<FriendService>(friendService);
    Get.put<ImService>(imService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());

    final controller = Get.put(ContactsController());
    final success = await controller.blockUser(
      FriendItem(
        id: '2',
        userId: '4001',
        username: 'david',
        nickname: 'David',
        remarkName: '',
        avatarUrl: '',
      ),
    );

    expect(success, isTrue);
    expect(friendService.blockUserCalls, 1);
    expect(friendService.lastBlockedUserId, '4001');
    expect(imService.refreshSessionsNowCalls, 1);
  });
}
