import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';

class _FakeImService extends ImService {
  final peerPinCalls = <Map<String, Object>>[];
  final sessionPinCalls = <Map<String, Object>>[];

  @override
  bool get isConnected => true;

  @override
  Future<void> refreshSessionsNow() async {}

  @override
  Future<void> refreshSessionsWindowNow() async {}

  @override
  Future<void> refreshSessionsIfStale({
    Duration maxAge = const Duration(seconds: 45),
  }) async {}

  @override
  Future<bool> loadMoreSessionWindowIfNeeded({bool force = false}) async {
    return false;
  }

  @override
  Future<bool> setPeerPinned({
    required String peerId,
    required List<String> sessionIds,
    required bool isPinned,
  }) async {
    peerPinCalls.add({
      'peerId': peerId,
      'sessionIds': List<String>.from(sessionIds),
      'isPinned': isPinned,
    });
    return true;
  }

  @override
  Future<bool> setSessionPinned(
    String sessionId, {
    required bool isPinned,
  }) async {
    sessionPinCalls.add({'sessionId': sessionId, 'isPinned': isPinned});
    return true;
  }
}

SessionModel _session({
  required String id,
  required String type,
  String peerId = '',
  int peerType = 1,
  bool pinned = false,
  int activity = 1000,
}) {
  return SessionModel(
    sessionId: id,
    title: id,
    type: type,
    peerId: peerId,
    peerType: peerType,
    updatedAt: activity,
    lastMessageTime: activity,
    isPinned: pinned,
    pinnedAt: pinned ? activity : 0,
  );
}

void main() {
  late _FakeImService imService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    UserImageCacheManager.setDisabledForTest(true);
    imService = _FakeImService();
    Get.put<ImService>(imService);
    Get.put<FriendQrFlowService>(FriendQrFlowService());
  });

  tearDown(() {
    UserImageCacheManager.setDisabledForTest(false);
    Get.reset();
  });

  group('聊天页菜单置顶 — 按 sessionId 对齐首页分组', () {
    test('私聊多线程：任一线程都能找到同一对端分组，置顶走对端级', () async {
      final a1 = _session(id: 'a1', type: 'private', peerId: 'alice');
      final a2 = _session(
        id: 'a2',
        type: 'private',
        peerId: 'alice',
        activity: 2000,
      );
      final g1 = _session(id: 'g1', type: 'group');
      imService.sessions.assignAll([a1, a2, g1]);
      final controller = Get.put(ConversationsController());

      final item = controller.findConversationItemBySession('a1');
      expect(item, isNotNull);
      expect(item!.groupKey, 'private:1:alice');
      expect(
        controller.findConversationItemBySession('a2')?.groupKey,
        'private:1:alice',
      );
      expect(controller.isConversationPinnedBySession('a1'), isFalse);

      final ok = await controller.setConversationPinnedBySession(
        'a1',
        isPinned: true,
      );
      expect(ok, isTrue);
      expect(imService.peerPinCalls, hasLength(1));
      expect(imService.peerPinCalls.single['peerId'], 'alice');
      expect(
        imService.peerPinCalls.single['sessionIds'],
        unorderedEquals(['a1', 'a2']),
      );
      expect(imService.sessionPinCalls, isEmpty);
    });

    test('群聊：置顶走会话级', () async {
      final g1 = _session(id: 'g1', type: 'group', pinned: true);
      imService.sessions.assignAll([g1]);
      final controller = Get.put(ConversationsController());

      expect(
        controller.findConversationItemBySession('g1')?.groupKey,
        'session:g1',
      );
      expect(controller.isConversationPinnedBySession('g1'), isTrue);

      final ok = await controller.setConversationPinnedBySession(
        'g1',
        isPinned: false,
      );
      expect(ok, isTrue);
      expect(imService.peerPinCalls, isEmpty);
      expect(imService.sessionPinCalls.single, {
        'sessionId': 'g1',
        'isPinned': false,
      });
    });

    test('未知会话：返回 false 且不发请求', () async {
      final controller = Get.put(ConversationsController());
      expect(controller.findConversationItemBySession('missing'), isNull);
      expect(controller.isConversationPinnedBySession('missing'), isFalse);
      final ok = await controller.setConversationPinnedBySession(
        'missing',
        isPinned: true,
      );
      expect(ok, isFalse);
      expect(imService.peerPinCalls, isEmpty);
      expect(imService.sessionPinCalls, isEmpty);
    });
  });
}
