import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  _FakeAuthService(this.userIdValue);

  final String userIdValue;

  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => userIdValue;

  @override
  String? get token => 'test_access_token';

  @override
  Future<void> logout({bool notifyServer = true}) async {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    final userId = 'sync-unread-${DateTime.now().microsecondsSinceEpoch}';
    Get.put<AuthService>(_FakeAuthService(userId));
  });

  tearDown(() async {
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  Future<ImService> makeService(String sid, {required int unread}) async {
    final service = ImService();
    addTearDown(service.onClose);
    service.sessions.value = [
      SessionModel(
        sessionId: sid,
        updatedAt: 1,
        unreadCount: unread,
        lastMessageTime: 1,
      ),
    ];
    return service;
  }

  test('syncSessionUnreadCountsFromServer 将服务端未读数写入本地会话', () async {
    await LocalDb.setActiveUser(Get.find<AuthService>().userId!);
    const sid = 's1';
    final service = await makeService(sid, unread: 0);

    await service.syncSessionUnreadCountsFromServer([
      SessionModel(
        sessionId: sid,
        updatedAt: 2,
        unreadCount: 3,
        lastMessageTime: 2,
      ),
    ]);

    final local = service.sessions.firstWhere((s) => s.sessionId == sid);
    expect(local.unreadCount, 3);
  });

  test('syncSessionUnreadCountsFromServer 尊重 clearUnread 产生的本地 override', () async {
    await LocalDb.setActiveUser(Get.find<AuthService>().userId!);
    const sid = 's1';
    final service = await makeService(sid, unread: 5);

    // 用户已读：本地 override 为 0，服务端旧值仍为 5。
    service.clearUnread(sid);
    await Future<void>.delayed(Duration.zero);
    expect(service.sessions.firstWhere((s) => s.sessionId == sid).unreadCount, 0);

    await service.syncSessionUnreadCountsFromServer([
      SessionModel(
        sessionId: sid,
        updatedAt: 2,
        unreadCount: 5,
        lastMessageTime: 2,
      ),
    ]);

    final local = service.sessions.firstWhere((s) => s.sessionId == sid);
    expect(local.unreadCount, 0);
  });

  test('syncSessionUnreadCountsFromServer 尊重 markUnread 产生的本地 override', () async {
    await LocalDb.setActiveUser(Get.find<AuthService>().userId!);
    const sid = 's1';
    final service = await makeService(sid, unread: 0);

    // 用户手动标未读：本地 override 为 1，服务端已读后为 0。
    service.markUnread(sid);
    await Future<void>.delayed(Duration.zero);
    expect(service.sessions.firstWhere((s) => s.sessionId == sid).unreadCount, 1);

    await service.syncSessionUnreadCountsFromServer([
      SessionModel(
        sessionId: sid,
        updatedAt: 2,
        unreadCount: 0,
        lastMessageTime: 2,
      ),
    ]);

    final local = service.sessions.firstWhere((s) => s.sessionId == sid);
    expect(local.unreadCount, 1);
  });

  test('syncSessionUnreadCountsFromServer 批量更新只触发一次 sessions 通知', () async {
    await LocalDb.setActiveUser(Get.find<AuthService>().userId!);
    final service = ImService();
    addTearDown(service.onClose);
    service.sessions.value = [
      for (var i = 0; i < 5; i++)
        SessionModel(
          sessionId: 's$i',
          updatedAt: i + 1,
          unreadCount: 0,
          lastMessageTime: i + 1,
        ),
    ];

    var notifications = 0;
    final worker = ever(service.sessions, (_) => notifications++);
    addTearDown(worker.dispose);

    await service.syncSessionUnreadCountsFromServer([
      for (var i = 0; i < 5; i++)
        SessionModel(
          sessionId: 's$i',
          updatedAt: i + 10,
          unreadCount: i + 1,
          lastMessageTime: i + 10,
        ),
    ]);

    expect(notifications, 1);
    for (var i = 0; i < 5; i++) {
      expect(
        service.sessions.firstWhere((s) => s.sessionId == 's$i').unreadCount,
        i + 1,
      );
    }
  });
}
