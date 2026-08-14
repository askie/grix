import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
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

/// 仅用于满足依赖注入；默认返回空快照。需要验证服务端快照路径的用例可注入 snapshots。
class _FakeSessionService extends SessionService {
  _FakeSessionService({this.snapshots = const <SessionSnapshot>[]});

  final List<SessionSnapshot> snapshots;

  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    return SessionSnapshotFetchResult(snapshots: snapshots, success: true);
  }
}

final _trackedImServices = <ImService>[];

ImService _makeImService() {
  final service = ImService();
  _trackedImServices.add(service);
  return service;
}

String _testUserId = 'preview-test-user';

/// 写入一条“服务端快照来源”的会话预览：last_message=旧摘要、last_message_time=0。
/// 这正是 snapshot 落本地时的形态（见 im_service_sessions.dart 第 278 行）。
Future<void> _seedSnapshotPreview(
  String sessionId, {
  required String summary,
  int updatedAt = 1,
}) async {
  await LocalDb.upsertSession({
    'session_id': sessionId,
    'title': '',
    'type': 'private',
    'peer_id': '',
    'peer_type': 0,
    'peer_nickname': '',
    'peer_username': '',
    'updated_at': updatedAt,
    'is_pinned': false,
    'is_muted': false,
    'pinned_at': 0,
    'unread_count': 0,
    'last_message': summary,
    'last_message_time': 0,
  });
}

Future<void> _seedLocalMessage(
  String sessionId, {
  required String msgId,
  required String content,
  required int createdAt,
}) async {
  await LocalDb.upsertMessage({
    'msg_id': msgId,
    'session_id': sessionId,
    'sender_id': 'agent-1',
    'sender_type': 2,
    'msg_type': 1,
    'content': content,
    'created_at': createdAt,
  });
}

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
    MessageStreamController.resetForTest();
    SharedPreferences.setMockInitialValues({});
    _testUserId = 'preview-test-${DateTime.now().microsecondsSinceEpoch}';
    Get.put<AuthService>(_FakeAuthService(_testUserId));
    Get.put<SessionService>(_FakeSessionService());
  });

  tearDown(() async {
    for (final service in _trackedImServices.reversed) {
      service.onClose();
    }
    _trackedImServices.clear();
    await LocalDb.setActiveUser(null);
    MessageStreamController.resetForTest();
    Get.reset();
  });

  group('会话列表摘要与本地可见消息一致性', () {
    test('本地存在最新可见消息时，摘要应跟随本地消息而非被快照摘要钉死', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        const sid = 'agent-session-1';
        await _seedSnapshotPreview(sid, summary: '聊天页拉不到的旧摘要');
        await _seedLocalMessage(
          sid,
          msgId: '9001',
          content: '这是聊天页能看到的最新回复',
          createdAt: 1700000000000,
        );

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);

        final session = service.sessions.firstWhere((s) => s.sessionId == sid);
        expect(
          session.lastMessage,
          '这是聊天页能看到的最新回复',
          reason:
              '会话列表摘要应等于本地最新可见消息；当前实现因 last_message_time=0 仍显示旧快照摘要。',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('快照有较新真实时间戳时，不被较旧的本地消息覆盖', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        const sid = 'agent-session-2';
        // 用 updateSessionLastMsg 写入“带真实时间戳”的较新预览（晚于本地消息）。
        await LocalDb.updateSessionLastMsg(
          sid,
          '更晚到达的真实预览',
          1700000005000,
        );
        await _seedLocalMessage(
          sid,
          msgId: '8001',
          content: '较旧的本地消息',
          createdAt: 1700000000000,
        );

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);

        final session = service.sessions.firstWhere((s) => s.sessionId == sid);
        expect(
          session.lastMessage,
          '更晚到达的真实预览',
          reason: '预览有真实较新时间戳时，应按时间取较新者，不被较旧本地消息覆盖。',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('服务端快照刷新（再次写入 time=0）后，摘要不回退、仍跟随本地可见消息', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        const sid = 'agent-session-3';
        await _seedSnapshotPreview(sid, summary: '首次快照旧摘要');
        await _seedLocalMessage(
          sid,
          msgId: '7001',
          content: '聊天页可见的最新回复',
          createdAt: 1700000000000,
        );

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);
        expect(
          service.sessions.firstWhere((s) => s.sessionId == sid).lastMessage,
          '聊天页可见的最新回复',
        );

        // 模拟周期性服务端刷新：snapshot 落本地时同样 last_message_time=0。
        await _seedSnapshotPreview(sid, summary: '服务端刷新又写回的旧摘要');
        await service.loadSessions(refreshFromServer: false);

        final session = service.sessions.firstWhere((s) => s.sessionId == sid);
        expect(
          session.lastMessage,
          '聊天页可见的最新回复',
          reason: '周期性快照刷新写入 time=0，不应把摘要重新钉死成聊天页拉不到的文本。',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('本地无任何可见消息时（新设备首登），摘要应回退展示服务端快照摘要而非空白', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        const sid = 'agent-session-empty';
        // 服务端 /sessions/list 的 last_msg 已按聊天历史同口径（per-user cutoff +
        // visible_to）过滤，是用户能打开的消息，可直接作为预览兜底。
        await _seedSnapshotPreview(sid, summary: '服务端已过滤的最后一条可见消息');
        // 故意不写入任何本地消息：模拟新设备首次登录、本端尚未拉到该会话消息的情形。

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);

        final session = service.sessions.firstWhere((s) => s.sessionId == sid);
        expect(
          session.lastMessage,
          '服务端已过滤的最后一条可见消息',
          reason:
              '新设备本地无消息时，应展示服务端快照摘要作为兜底，不能清空成占位"..."。',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });
  });

  group('纯卡片消息不覆盖摘要，但推进会话时间', () {
    test('最后一条是卡片消息时，摘要保留上一条文本、会话时间更新到卡片时间', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        const sid = 'agent-session-card';
        const textAt = 1700000000000;
        const cardAt = 1700000009000;

        await _seedLocalMessage(
          sid,
          msgId: '6001',
          content: '这是上一条可读回复',
          createdAt: textAt,
        );
        // 卡片消息到达：会话时间前移，摘要文本留空（不覆盖已有摘要）。
        await LocalDb.updateSessionLastMsg(sid, '这是上一条可读回复', textAt);
        await _seedLocalMessage(
          sid,
          msgId: '6002',
          content:
              '[工具执行](grix://card/tool_exec?tool=Read&summary=%E8%AF%BB%E6%96%87%E4%BB%B6)',
          createdAt: cardAt,
        );
        await LocalDb.updateSessionLastMsg(sid, '', cardAt);

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);

        final session = service.sessions.firstWhere((s) => s.sessionId == sid);
        expect(
          session.lastMessage,
          '这是上一条可读回复',
          reason: '纯卡片消息不做摘要，应继续显示上一条可读文本。',
        );
        expect(
          session.lastMessageTime,
          cardAt,
          reason: '卡片仍是一条真实消息，会话最后时间必须更新。',
        );
        expect(
          session.updatedAt,
          cardAt,
          reason: '会话排序时间应跟随卡片消息前移。',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('服务端快照摘要为空（会话已无可预览消息）时，清空本地摘要', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        const sid = 'agent-session-empty-snapshot';
        await LocalDb.updateSessionLastMsg(sid, '离线期间已被撤回的文本', 1700000000000);

        await Get.delete<SessionService>(force: true);
        Get.put<SessionService>(
          _FakeSessionService(
            snapshots: const [
              SessionSnapshot(
                sessionId: sid,
                title: '',
                type: 'private',
                peerId: '',
                peerType: 0,
                peerNickname: '',
                peerUsername: '',
                updatedAt: 1700000009000,
                unreadCount: 0,
                // 后端取的是"排除卡片后的最后一条可预览消息"：为空即该会话
                // 已无任何可预览消息（被撤回 / 清了历史），是权威信号。
                lastMessage: '',
              ),
            ],
          ),
        );

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);
        await service.refreshSessionsNow();
        await service.loadSessions(refreshFromServer: false);

        final session = service.sessions.firstWhere((s) => s.sessionId == sid);
        expect(
          session.lastMessage,
          '',
          reason: '服务端说没有可预览消息就是没有，旧摘要（如离线期间被撤回的文本）必须清掉。',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });
  });

  group('撤回后的摘要重算', () {
    test('撤回最新文本后，摘要降级到更早的可读文本', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        const sid = 'agent-session-revoke-downgrade';
        await _seedLocalMessage(
          sid,
          msgId: '5001',
          content: '更早的可读文本',
          createdAt: 1700000000000,
        );
        await _seedLocalMessage(
          sid,
          msgId: '5002',
          content: '最新的可读文本',
          createdAt: 1700000005000,
        );
        await LocalDb.updateSessionLastMsg(sid, '最新的可读文本', 1700000005000);

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);
        await service.applyLocalMessageRevoke(sessionId: sid, msgId: '5002');

        final session = service.sessions.firstWhere((s) => s.sessionId == sid);
        expect(
          session.lastMessage,
          '更早的可读文本',
          reason: '撤回后摘要应降级到仍然可读的更早消息。',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    test('撤回后本地只剩卡片消息时，摘要清空而不是留下被撤回的内容', () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        const sid = 'agent-session-revoke-only-card';
        await _seedLocalMessage(
          sid,
          msgId: '4001',
          content:
              '[工具执行](grix://card/tool_exec?tool=Read&summary=%E8%AF%BB%E6%96%87%E4%BB%B6)',
          createdAt: 1700000000000,
        );
        await _seedLocalMessage(
          sid,
          msgId: '4002',
          content: '待撤回的文本',
          createdAt: 1700000005000,
        );
        await LocalDb.updateSessionLastMsg(sid, '待撤回的文本', 1700000005000);

        final service = _makeImService();
        await service.loadSessions(refreshFromServer: false);
        await service.applyLocalMessageRevoke(sessionId: sid, msgId: '4002');

        final session = service.sessions.firstWhere((s) => s.sessionId == sid);
        expect(
          session.lastMessage,
          '',
          reason: '撤回的内容必须从会话列表消失；此时没有可读消息，摘要留空。',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });
  });
}
