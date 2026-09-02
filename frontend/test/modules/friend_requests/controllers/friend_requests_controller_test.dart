import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/modules/friend_requests/controllers/friend_requests_controller.dart';

class _FakeFriendService extends FriendService {
  int loadFriendRequestsCalls = 0;
  int handleFriendRequestCalls = 0;
  String? lastHandledRequestId;
  bool? lastHandledAccept;
  Object? errorToThrow;

  @override
  Future<void> loadFriendRequests() async {
    loadFriendRequestsCalls++;
  }

  @override
  Future<bool> handleFriendRequest(String requestId, bool accept) async {
    if (errorToThrow != null) {
      throw errorToThrow!;
    }
    handleFriendRequestCalls++;
    lastHandledRequestId = requestId;
    lastHandledAccept = accept;
    return true;
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

  test('friend requests controller loads requests during init', () {
    final friendService = _FakeFriendService();
    Get.put<FriendService>(friendService);

    Get.put(FriendRequestsController());

    expect(friendService.loadFriendRequestsCalls, 1);
  });

  test(
    'friend requests controller delegates request handling to service',
    () async {
      final friendService = _FakeFriendService();
      Get.put<FriendService>(friendService);
      final controller = Get.put(FriendRequestsController());
      final request = FriendRequestItem(
        id: 'req-1',
        fromUserId: 'user-1',
        username: 'tester',
        nickname: 'Tester',
        avatarUrl: '',
        message: 'hello',
        status: 0,
        createdAt: '2026-03-11T10:00:00Z',
      );

      final result = await controller.handleRequest(request, true);

      expect(result, isTrue);
      expect(friendService.handleFriendRequestCalls, 1);
      expect(friendService.lastHandledRequestId, 'req-1');
      expect(friendService.lastHandledAccept, isTrue);
    },
  );

  test(
    'friend requests controller clears processing state on exception',
    () async {
      final friendService = _FakeFriendService()
        ..errorToThrow = StateError('boom');
      Get.put<FriendService>(friendService);
      final controller = Get.put(FriendRequestsController());
      final request = FriendRequestItem(
        id: 'req-2',
        fromUserId: 'user-2',
        username: 'tester2',
        nickname: 'Tester2',
        avatarUrl: '',
        message: 'hello',
        status: 0,
        createdAt: '2026-03-11T10:00:00Z',
      );

      final result = await controller.handleRequest(request, true);

      expect(result, isFalse);
      expect(controller.isProcessing('req-2'), isFalse);
    },
  );
}
