import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/data/models/session_activity_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/local_db_change_bus.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/message_cards/models/chat_message_card_type.dart';
import 'package:grix/modules/chat/message_cards/models/chat_thinking_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/modules/call/call_controller.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  _FakeAuthService(this.userIdValue);

  final String userIdValue;
  int logoutCalls = 0;

  /// 默认按"凭证刷得动"应答。不 stub 的话会落到真实实现，因 refreshToken 为空被
  /// 直接判成会话失效——那是测试桩不完整，不是被测逻辑的行为。
  TokenRefreshStatus refreshStatus = TokenRefreshStatus.ready;

  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => userIdValue;

  @override
  String? get token => 'test_access_token';

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async {
    return refreshStatus;
  }

  @override
  Future<void> logout({bool notifyServer = true}) async {
    logoutCalls++;
  }
}

String _testUserId = 'im-order-bootstrap';

String _nextTestUserId(String prefix) {
  return '$prefix-${DateTime.now().microsecondsSinceEpoch}';
}

String _pendingReadStatesKey() => 'pending_read_states_$_testUserId';

String _deletedSessionsKey() => 'deleted_sessions_$_testUserId';

String _revokedSessionsKey() => 'revoked_sessions_$_testUserId';

class _FakeSessionService extends SessionService {
  int detailCalls = 0;
  int snapshotCalls = 0;
  int historyCalls = 0;
  SessionDetailResult detailResult = const SessionDetailResult(
    code: 50001,
    message: 'not set',
  );
  List<SessionSnapshot> snapshots = const [];
  Completer<SessionMessageHistoryResult>? historyCompleter;
  SessionMessageHistoryResult historyResult =
      const SessionMessageHistoryResult();

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    detailCalls++;
    return detailResult;
  }

  @override
  Future<List<SessionSnapshot>> fetchSessionSnapshots({
    int limit = 200,
    int maxPages = 5,
  }) async {
    snapshotCalls++;
    return snapshots;
  }

  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    snapshotCalls++;
    return SessionSnapshotFetchResult(snapshots: snapshots, success: true);
  }

  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) async {
    historyCalls++;
    final completer = historyCompleter;
    if (completer != null) {
      return completer.future;
    }
    return historyResult;
  }
}

class _FakeFriendService extends FriendService {
  final cachedNicknames = <String, String>{};
  final fetchedNicknames = <String, String>{};
  int fetchProfileCalls = 0;

  @override
  String? getUserNickname(String userId) {
    return cachedNicknames[userId];
  }

  @override
  Future<String?> fetchUserProfile(String userId) async {
    fetchProfileCalls++;
    return fetchedNicknames[userId];
  }
}

class _SpyImService extends ImService {
  int deleteConversationCalls = 0;
  String? deletedSessionID;
  int retryPacketCalls = 0;
  String? retriedRemoteSessionId;
  String? retriedRemoteMsgId;
  int packetDispatchCalls = 0;
  String? dispatchedContent;
  String? dispatchedSessionId;
  Map<String, dynamic>? dispatchedExtra;
  String? dispatchedQuotedMessageId;
  bool? dispatchedDelegateOrigin;

  @override
  Future<void> deleteConversation(String sessionId) async {
    deleteConversationCalls++;
    deletedSessionID = sessionId.trim();
    sessions.removeWhere((s) => s.sessionId == deletedSessionID);
  }

  @override
  void dispatchRetryMessagePacket({
    required String sessionId,
    required String msgId,
  }) {
    retryPacketCalls++;
    retriedRemoteSessionId = sessionId;
    retriedRemoteMsgId = msgId;
  }

  @override
  void dispatchSendMessagePacket({
    required String sessionId,
    required String clientMsgId,
    required String content,
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    required bool delegateOrigin,
  }) {
    packetDispatchCalls++;
    dispatchedContent = content;
    dispatchedSessionId = sessionId;
    dispatchedExtra = extra == null ? null : Map<String, dynamic>.from(extra);
    dispatchedQuotedMessageId = quotedMessageId;
    dispatchedDelegateOrigin = delegateOrigin;
  }
}

class _SpyCallController extends CallController {
  Map<String, dynamic>? inviteAckPayload;

  @override
  void onCallInviteAck(Map<String, dynamic> payload) {
    inviteAckPayload = payload;
  }
}

Future<Map<String, dynamic>> _readPendingReadStates() async {
  final prefs = await SharedPreferences.getInstance();
  final raw = prefs.getString(_pendingReadStatesKey());
  if (raw == null || raw.trim().isEmpty) {
    return <String, dynamic>{};
  }
  final decoded = jsonDecode(raw);
  if (decoded is Map<String, dynamic>) {
    return decoded;
  }
  return Map<String, dynamic>.from(decoded as Map);
}

Future<void> _expectPendingReadStateEventually(
  String sessionId,
  String lastReadMsgId,
) async {
  for (var i = 0; i < 10; i++) {
    final pending = await _readPendingReadStates();
    if (pending[sessionId] == lastReadMsgId) {
      return;
    }
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  final pending = await _readPendingReadStates();
  expect(pending[sessionId], lastReadMsgId);
}

Future<void> _expectPendingReadStateClearedEventually(String sessionId) async {
  for (var i = 0; i < 10; i++) {
    final pending = await _readPendingReadStates();
    if (!pending.containsKey(sessionId)) {
      return;
    }
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  final pending = await _readPendingReadStates();
  expect(pending.containsKey(sessionId), isFalse);
}

MessageModel _msg({
  required String msgId,
  required int createdAt,
  String sessionId = 's1',
  String senderId = 'peer',
  int msgType = 1,
  String content = '',
  String? clientMsgId,
  String? status,
  String? agentDeliveryStatus,
  List<String>? visibleTo,
}) {
  return MessageModel(
    msgId: msgId,
    sessionId: sessionId,
    senderId: senderId,
    msgType: msgType,
    content: content,
    createdAt: createdAt,
    clientMsgId: clientMsgId,
    status: status,
    agentDeliveryStatus: agentDeliveryStatus,
    visibleTo: visibleTo,
  );
}

final _trackedImServices = <ImService>[];

T _trackImService<T extends ImService>(T service) {
  _trackedImServices.add(service);
  return service;
}

ImService _makeImService() => _trackImService(ImService());

_SpyImService _makeSpyImService() => _trackImService(_SpyImService());

void main() {
  late _FakeAuthService authService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    MessageStreamController.resetForTest();
    SharedPreferences.setMockInitialValues({});
    _testUserId = _nextTestUserId('im-order');
    authService = _FakeAuthService(_testUserId);
    Get.put<AuthService>(authService);
  });

  tearDown(() async {
    for (final service in _trackedImServices.reversed) {
      service.onClose();
    }
    _trackedImServices.clear();
    await LocalDb.setActiveUser(null);
    ImService.initialRenderHydrationStartedForTest = null;
    ImService.sessionWindowCacheNowMsForTest = null;
    ImService.failDbWriteOpForTest = null;
    MessageStreamController.resetForTest();
    Get.reset();
  });

  test(
    'agent state keeps the newest positive connection epoch on reordered packets',
    () async {
      final service = _makeImService();
      final leaseUntil = DateTime.now().millisecondsSinceEpoch + 60000;

      Future<void> sync({
        required String state,
        required bool connected,
        required int leaseUntil,
        int? connectionEpoch,
      }) {
        final extra = <String, dynamic>{
          'connected': connected,
          'lease_until': leaseUntil,
        };
        if (connectionEpoch != null) {
          extra['connection_epoch'] = connectionEpoch;
        }
        return service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'agent_state_sync',
            'payload': {
              'agent_id': 'agent-epoch-order',
              'state': state,
              'extra': extra,
            },
          }),
        );
      }

      await sync(
        state: 'online',
        connected: true,
        leaseUntil: leaseUntil,
        connectionEpoch: 2,
      );
      await sync(
        state: 'offline',
        connected: false,
        leaseUntil: 0,
        connectionEpoch: 1,
      );
      await sync(state: 'offline', connected: false, leaseUntil: 0);

      expect(service.agentStates['agent-epoch-order'], {
        'state': 'online',
        'lease_until': leaseUntil,
        'connection_epoch': 2,
      });

      await sync(
        state: 'offline',
        connected: false,
        leaseUntil: 0,
        connectionEpoch: 3,
      );
      expect(service.agentStates['agent-epoch-order'], {
        'state': 'offline',
        'lease_until': 0,
        'connection_epoch': 3,
      });
    },
  );

  test(
    'agent state keeps legacy epoch zero last-write-wins before a positive epoch',
    () async {
      final service = _makeImService();
      final leaseUntil = DateTime.now().millisecondsSinceEpoch + 60000;

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_state_sync',
          'payload': {
            'agent_id': 'agent-legacy-order',
            'state': 'online',
            'extra': {'connected': true, 'lease_until': leaseUntil},
          },
        }),
      );
      expect(service.agentStates['agent-legacy-order']?['state'], 'online');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_state_sync',
          'payload': {
            'agent_id': 'agent-legacy-order',
            'state': 'offline',
            'extra': {'connected': false, 'lease_until': 0},
          },
        }),
      );

      expect(service.agentStates['agent-legacy-order'], {
        'state': 'offline',
        'lease_until': 0,
        'connection_epoch': 0,
      });
    },
  );

  test(
    'agent state lease expiry preserves the positive connection epoch',
    () async {
      final service = _makeImService();
      final leaseUntil = DateTime.now().millisecondsSinceEpoch + 100;

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_state_sync',
          'payload': {
            'agent_id': 'agent-lease-epoch',
            'state': 'online',
            'extra': {
              'connected': true,
              'lease_until': leaseUntil,
              'connection_epoch': 7,
            },
          },
        }),
      );
      expect(service.agentStates['agent-lease-epoch']?['state'], 'online');

      await Future<void>.delayed(const Duration(milliseconds: 180));

      expect(service.agentStates['agent-lease-epoch'], {
        'state': 'offline',
        'lease_until': 0,
        'connection_epoch': 7,
      });

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_state_sync',
          'payload': {
            'agent_id': 'agent-lease-epoch',
            'state': 'online',
            'extra': {
              'connected': true,
              'lease_until': DateTime.now().millisecondsSinceEpoch + 60000,
            },
          },
        }),
      );
      expect(service.agentStates['agent-lease-epoch'], {
        'state': 'offline',
        'lease_until': 0,
        'connection_epoch': 7,
      });
    },
  );

  test(
    'upsertUIMessageForTest keeps chronological order on out-of-order insert',
    () {
      final service = _makeImService();

      service.upsertUIMessageForTest(_msg(msgId: 'm3', createdAt: 3000));
      service.upsertUIMessageForTest(_msg(msgId: 'm1', createdAt: 1000));
      service.upsertUIMessageForTest(_msg(msgId: 'm2', createdAt: 2000));

      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'm1',
        'm2',
        'm3',
      ]);
    },
  );

  test('upsertUIMessageForTest replaces same msg_id and reorders safely', () {
    final service = _makeImService();

    service.upsertUIMessageForTest(_msg(msgId: 'm1', createdAt: 1000));
    service.upsertUIMessageForTest(_msg(msgId: 'm2', createdAt: 2000));
    service.upsertUIMessageForTest(
      _msg(msgId: 'm1', createdAt: 3000, content: 'updated'),
    );

    expect(service.currentMessages.length, 2);
    expect(service.currentMessages.map((e) => e.msgId).toList(), ['m2', 'm1']);
    expect(
      service.currentMessages.firstWhere((e) => e.msgId == 'm1').content,
      'updated',
    );
  });

  test(
    'upsertUIMessageForTest preserves existing agent delivery status when replacement omits it',
    () {
      final service = _makeImService();

      service.upsertUIMessageForTest(
        _msg(
          msgId: 'm1',
          createdAt: 1000,
          content: '@OpenClaw',
          agentDeliveryStatus: 'received',
        ),
      );
      service.upsertUIMessageForTest(
        _msg(msgId: 'm1', createdAt: 1000, content: '@OpenClaw'),
      );

      expect(service.currentMessages, hasLength(1));
      expect(service.currentMessages.first.agentDeliveryStatus, 'received');
    },
  );

  test(
    'upsertUIMessageForTest preserves existing visibleTo when replacement omits it',
    () {
      final service = _makeImService();

      // 流式占位/stream_finish 已带 visible_to（锁标记消息）。
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'm1',
          createdAt: 1000,
          content: 'hidden reply',
          visibleTo: ['42'],
        ),
      );
      // 后续快照/兜底路径的同 msg_id 数据缺省 visible_to，不应把锁标记冲掉。
      service.upsertUIMessageForTest(
        _msg(msgId: 'm1', createdAt: 1000, content: 'hidden reply'),
      );

      expect(service.currentMessages, hasLength(1));
      expect(service.currentMessages.first.visibleTo, ['42']);
    },
  );

  test(
    'upsertUIMessageForTest does not preserve transient failed status onto authoritative server message',
    () {
      final service = _makeImService();

      service.upsertUIMessageForTest(
        _msg(
          msgId: 'temp_cid-merge-1',
          clientMsgId: 'cid-merge-1',
          createdAt: 1000,
          content: 'recall this',
          senderId: 'me',
          status: 'failed',
        ),
      );
      service.upsertUIMessageForTest(
        _msg(
          msgId: '1001',
          clientMsgId: 'cid-merge-1',
          createdAt: 1000,
          content: 'recall this',
          senderId: 'me',
        ),
      );

      expect(service.currentMessages, hasLength(1));
      expect(service.currentMessages.first.msgId, '1001');
      expect(service.currentMessages.first.status ?? '', isEmpty);
    },
  );

  test(
    'push_msg keeps UI order even when downstream arrival is out of order',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': '2',
            'session_id': 's1',
            'sender_id': 'u2',
            'msg_type': 1,
            'content': 'newer',
            'created_at': 2000,
            'inbox_seq': 2,
          },
        }),
      );
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': '1',
            'session_id': 's1',
            'sender_id': 'u2',
            'msg_type': 1,
            'content': 'older',
            'created_at': 1000,
            'inbox_seq': 1,
          },
        }),
      );

      expect(service.currentMessages.map((e) => e.msgId).toList(), ['1', '2']);
    },
  );

  test(
    'stream placeholder push_msg advances activity time but keeps preview text',
    () async {
      final userId =
          'placeholder_preview_user_${DateTime.now().millisecondsSinceEpoch}';
      await LocalDb.setActiveUser(userId);

      try {
        await LocalDb.upsertMessage({
          'msg_id': 'visible-1',
          'session_id': 's-placeholder-preview',
          'sender_id': 'u2',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'visible latest',
          'created_at': 1700000001000,
        });
        await LocalDb.updateSessionLastMsg(
          's-placeholder-preview',
          'visible latest',
          1700000001000,
        );

        final service = _makeImService();
        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'push_msg',
            'payload': {
              'msg_id': 'placeholder-1',
              'session_id': 's-placeholder-preview',
              'sender_id': 'agent-1',
              'sender_type': 2,
              'msg_type': 4,
              'content': '',
              'created_at': 1700000002000,
              'inbox_seq': 2,
            },
          }),
        );

        // 工具/占位消息：preview 文本保持上一条人类可读消息不变，
        // 但活跃时间必须前移，让会话在列表中上浮——与 pull_sync_resp 口径一致。
        final sessionRow = await LocalDb.getSessionRecord(
          's-placeholder-preview',
        );
        expect(sessionRow?['last_message'], 'visible latest');
        expect(sessionRow?['last_message_time'], 1700000002000);
        expect(sessionRow?['updated_at'], 1700000002000);

        await service.loadSessions(refreshFromServer: false);
        expect(service.sessions, hasLength(1));
        expect(service.sessions.single.lastMessage, 'visible latest');
        expect(service.sessions.single.lastMessageTime, 1700000002000);
        expect(service.sessions.single.updatedAt, 1700000002000);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'retryMessage retries acked message in place when agent delivery failed',
    () async {
      final service = _makeSpyImService();
      service.currentMessages.assignAll([
        MessageModel(
          msgId: 'msg-acked',
          sessionId: 's1',
          senderId: 'me',
          content: 'retry me',
          extra: const {
            'mention_user_ids': ['u2'],
          },
          quotedMessageId: 'reply-1',
          createdAt: 1000,
          agentDeliveryStatus: 'failed',
        ),
      ]);

      await service.retryMessage(null, msgId: 'msg-acked');

      expect(service.retryPacketCalls, 1);
      expect(service.retriedRemoteSessionId, 's1');
      expect(service.retriedRemoteMsgId, 'msg-acked');
    },
  );

  test(
    'retry_msg_ack updates existing message delivery state in place',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'msg-acked',
          sessionId: 's1',
          senderId: 'me',
          createdAt: 1000,
          content: 'retry me',
          agentDeliveryStatus: 'failed',
        ),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'retry_msg_ack',
          'payload': {
            'session_id': 's1',
            'msg_id': 'msg-acked',
            'code': 0,
            'msg': 'retry accepted',
          },
        }),
      );

      expect(service.currentMessages, hasLength(1));
      expect(service.currentMessages.first.msgId, 'msg-acked');
      expect(service.currentMessages.first.agentDeliveryStatus, 'queued');
    },
  );

  test(
    'retryMessage keeps persisted mention extra for local pending message',
    () async {
      final userId =
          'retry-extra-${DateTime.now().millisecondsSinceEpoch.toString()}';
      await LocalDb.setActiveUser(userId);
      try {
        final service = _makeSpyImService();
        await LocalDb.insertLocalStub({
          'msg_id': 'temp-cid-retry-1',
          'session_id': 'group-1',
          'sender_id': 'me',
          'sender_type': 1,
          'msg_type': 1,
          'content': '@owner.user 请处理',
          'extra': {
            'mention_user_ids': ['1002'],
            'reply_mode': 'plain',
          },
          'quoted_message_id': 'reply-42',
          'status': 'failed',
          'local_seq': 'cid-retry-1',
          'created_at': 1000,
        });

        await service.retryMessage('cid-retry-1');

        expect(service.packetDispatchCalls, 1);
        expect(service.dispatchedSessionId, 'group-1');
        expect(service.dispatchedContent, '@owner.user 请处理');
        expect(service.dispatchedExtra, {
          'mention_user_ids': ['1002'],
          'reply_mode': 'plain',
        });
        expect(service.dispatchedQuotedMessageId, 'reply-42');
        expect(service.dispatchedDelegateOrigin, isFalse);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'enterSession marks current session immediately to avoid unread race on push_msg',
    () async {
      final service = _makeImService();
      service.sessions.assignAll([
        SessionModel(
          sessionId: 's1',
          type: 'private',
          updatedAt: 1000,
          unreadCount: 5,
          lastMessage: 'old',
          lastMessageTime: 1000,
        ),
      ]);

      service.enterSession('  s1  ');
      expect(service.currentSessionId, 's1');
      expect(
        service.sessions.firstWhere((s) => s.sessionId == 's1').unreadCount,
        0,
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'm-current',
            'session_id': 's1',
            'sender_id': 'u2',
            'msg_type': 1,
            'content': 'hello',
            'created_at': 2000,
            'inbox_seq': 1,
          },
        }),
      );

      final updated = service.sessions.firstWhere((s) => s.sessionId == 's1');
      expect(updated.unreadCount, 0);
      expect(service.totalUnread, 0);
      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'm-current',
      ]);
    },
  );

  test('current session push_msg clears stale unread in memory', () async {
    final service = _makeImService();
    service.setCurrentSessionForTest('s1');
    service.sessions.assignAll([
      SessionModel(
        sessionId: 's1',
        type: 'private',
        updatedAt: 1000,
        unreadCount: 4,
        lastMessage: 'old',
        lastMessageTime: 1000,
      ),
    ]);

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'push_msg',
        'payload': {
          'msg_id': 'm-stale',
          'session_id': 's1',
          'sender_id': 'u2',
          'msg_type': 1,
          'content': 'new',
          'created_at': 3000,
          'inbox_seq': 3,
        },
      }),
    );

    final updated = service.sessions.firstWhere((s) => s.sessionId == 's1');
    expect(updated.unreadCount, 0);
    expect(service.totalUnread, 0);
  });

  test(
    'agent push_msg clears stale agent_api composing indicator for the same agent',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('session-activity-clear-1');
      service.sessionActivities['session-activity-clear-1'] = [
        SessionActivityModel(
          sessionId: 'session-activity-clear-1',
          kind: 'composing',
          active: true,
          actorId: 'agent-1',
          actorType: 'agent',
          executorId: 'agent-1',
          executorType: 'agent',
          source: 'agent_api',
          refMsgId: '',
          refEventId: 'evt-1',
          statusText: '',
          updatedAt: 100,
          expiresAt: DateTime.now().millisecondsSinceEpoch + 30000,
        ),
      ];

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'inbox_seq': '1',
            'msg_id': '9001',
            'session_id': 'session-activity-clear-1',
            'session_type': 1,
            'sender_id': 'agent-1',
            'sender_type': 2,
            'msg_type': 1,
            'content': 'done',
            'created_at': DateTime.now().millisecondsSinceEpoch,
          },
        }),
      );

      expect(service.sessionActivities['session-activity-clear-1'], isNull);
    },
  );

  test(
    'agent push_msg does not clear active agent output state before terminal status',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('session-agent-output-keep-1');
      service.agentOutputStates['session-agent-output-keep-1'] = {
        'session_id': 'session-agent-output-keep-1',
        'run_id': 'run-keep-1',
        'agent_id': 'agent-keep-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 200,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'inbox_seq': '1',
            'msg_id': '9301',
            'session_id': 'session-agent-output-keep-1',
            'session_type': 1,
            'sender_id': 'agent-keep-1',
            'sender_type': 2,
            'msg_type': 1,
            'content': 'tool card message',
            'created_at': DateTime.now().millisecondsSinceEpoch,
          },
        }),
      );

      expect(
        service.agentOutputStateFor('session-agent-output-keep-1'),
        isNotNull,
      );
      expect(
        service.agentOutputStateFor('session-agent-output-keep-1')?['run_id'],
        'run-keep-1',
      );
    },
  );

  test(
    'push_msg clears stale human composing indicator for the same sender',
    () async {
      final service = _makeImService();
      service.sessionActivities['session-activity-clear-human-1'] = [
        SessionActivityModel(
          sessionId: 'session-activity-clear-human-1',
          kind: 'composing',
          active: true,
          actorId: 'u2',
          actorType: 'human',
          executorId: 'u2',
          executorType: 'human',
          source: 'client',
          refMsgId: '',
          refEventId: 'evt-human-1',
          statusText: '',
          updatedAt: 100,
          expiresAt: DateTime.now().millisecondsSinceEpoch + 30000,
        ),
      ];

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'inbox_seq': '1',
            'msg_id': '9101',
            'session_id': 'session-activity-clear-human-1',
            'session_type': 1,
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'done',
            'created_at': DateTime.now().millisecondsSinceEpoch,
          },
        }),
      );

      expect(
        service.sessionActivities['session-activity-clear-human-1'],
        isNull,
      );
    },
  );

  test(
    'push_msg clears delegate-like composing indicator when sender matches executor',
    () async {
      final service = _makeImService();
      service.sessionActivities['session-activity-clear-delegate-1'] = [
        SessionActivityModel(
          sessionId: 'session-activity-clear-delegate-1',
          kind: 'composing',
          active: true,
          actorId: 'u2',
          actorType: 'human',
          executorId: 'agent-9',
          executorType: 'agent',
          source: 'agent_api',
          refMsgId: 'stream-delegate-1',
          refEventId: 'evt-delegate-1',
          statusText: '',
          updatedAt: 120,
          expiresAt: DateTime.now().millisecondsSinceEpoch + 30000,
        ),
      ];

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'inbox_seq': '1',
            'msg_id': 'stream-delegate-1',
            'session_id': 'session-activity-clear-delegate-1',
            'session_type': 1,
            'sender_id': 'agent-9',
            'sender_type': 2,
            'msg_type': 1,
            'content': 'delegate done',
            'created_at': DateTime.now().millisecondsSinceEpoch,
          },
        }),
      );

      expect(
        service.sessionActivities['session-activity-clear-delegate-1'],
        isNull,
      );
    },
  );

  test(
    'stale session_activity_list_resp does not resurrect composing after agent push_msg resolves it',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('session-activity-clear-list-1');
      service.sessionActivities['session-activity-clear-list-1'] = [
        const SessionActivityModel(
          sessionId: 'session-activity-clear-list-1',
          kind: 'composing',
          active: true,
          actorId: 'agent-11',
          actorType: 'agent',
          executorId: 'agent-11',
          executorType: 'agent',
          source: 'agent_api',
          refMsgId: '',
          refEventId: 'evt-list-1',
          statusText: '',
          updatedAt: 100,
          expiresAt: 60000,
        ),
      ];

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'inbox_seq': '1',
            'msg_id': '9201',
            'session_id': 'session-activity-clear-list-1',
            'session_type': 1,
            'sender_id': 'agent-11',
            'sender_type': 2,
            'msg_type': 1,
            'content': 'done',
            'created_at': 200,
          },
        }),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_activity_list_resp',
          'payload': {
            'session_id': 'session-activity-clear-list-1',
            'activities': [
              {
                'session_id': 'session-activity-clear-list-1',
                'kind': 'composing',
                'active': true,
                'actor_id': 'agent-11',
                'actor_type': 'agent',
                'executor_id': 'agent-11',
                'executor_type': 'agent',
                'source': 'agent_api',
                'ref_msg_id': '',
                'ref_event_id': 'evt-list-1',
                'updated_at': 100,
                'expires_at': 60000,
              },
            ],
          },
        }),
      );

      expect(
        service.sessionActivities['session-activity-clear-list-1'],
        isNull,
      );
    },
  );

  test(
    'terminal agent_output_status clears stale agent_api composing indicator for the same agent',
    () async {
      final service = _makeImService();
      service.sessionActivities['session-activity-clear-2'] = [
        SessionActivityModel(
          sessionId: 'session-activity-clear-2',
          kind: 'composing',
          active: true,
          actorId: 'agent-2',
          actorType: 'agent',
          executorId: 'agent-2',
          executorType: 'agent',
          source: 'agent_api',
          refMsgId: '',
          refEventId: 'evt-2',
          statusText: '',
          updatedAt: 200,
          expiresAt: DateTime.now().millisecondsSinceEpoch + 30000,
        ),
      ];
      service.agentOutputStates['session-activity-clear-2'] = {
        'run_id': 'run-2',
        'session_id': 'session-activity-clear-2',
        'agent_id': 'agent-2',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 100,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_output_status',
          'payload': {
            'session_id': 'session-activity-clear-2',
            'agent_id': 'agent-2',
            'run_id': 'run-2',
            'state': 'completed',
            'can_stop': false,
            'updated_at': 300,
          },
        }),
      );

      expect(service.sessionActivities['session-activity-clear-2'], isNull);
      expect(service.agentOutputStates['session-activity-clear-2'], isNull);
    },
  );

  test(
    'terminal agent_output_status clears stale agent_api composing when agent matches executor only',
    () async {
      final service = _makeImService();
      service.sessionActivities['session-activity-clear-3'] = [
        SessionActivityModel(
          sessionId: 'session-activity-clear-3',
          kind: 'composing',
          active: true,
          actorId: 'u2',
          actorType: 'human',
          executorId: 'agent-3',
          executorType: 'agent',
          source: 'agent_api',
          refMsgId: '',
          refEventId: 'evt-3',
          statusText: '',
          updatedAt: 220,
          expiresAt: DateTime.now().millisecondsSinceEpoch + 30000,
        ),
      ];
      service.agentOutputStates['session-activity-clear-3'] = {
        'run_id': 'run-3',
        'session_id': 'session-activity-clear-3',
        'agent_id': 'agent-3',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 180,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_output_status',
          'payload': {
            'session_id': 'session-activity-clear-3',
            'agent_id': 'agent-3',
            'run_id': 'run-3',
            'state': 'completed',
            'can_stop': false,
            'updated_at': 320,
          },
        }),
      );

      expect(service.sessionActivities['session-activity-clear-3'], isNull);
      expect(service.agentOutputStates['session-activity-clear-3'], isNull);
    },
  );

  test(
    'terminal agent_output_status from an old run cannot clear the new run',
    () async {
      final service = _makeImService();
      service.sessionActivities['session-run-gate'] = [
        SessionActivityModel(
          sessionId: 'session-run-gate',
          kind: 'composing',
          active: true,
          actorId: 'agent-run-gate',
          actorType: 'agent',
          executorId: 'agent-run-gate',
          executorType: 'agent',
          source: 'agent_api',
          refMsgId: '',
          refEventId: 'new-run',
          statusText: '',
          updatedAt: 500,
          expiresAt: DateTime.now().millisecondsSinceEpoch + 30000,
        ),
      ];
      service.agentOutputStates['session-run-gate'] = {
        'run_id': 'new-run',
        'session_id': 'session-run-gate',
        'agent_id': 'agent-run-gate',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 500,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_output_status',
          'payload': {
            'session_id': 'session-run-gate',
            'agent_id': 'agent-run-gate',
            'run_id': 'old-run',
            'state': 'failed',
            'can_stop': false,
            // A retry may be observed later than the new run; run identity,
            // not wall-clock order, is the authority.
            'updated_at': 900,
          },
        }),
      );

      expect(service.sessionActivities['session-run-gate'], isNotNull);
      expect(
        service.agentOutputStates['session-run-gate']?['run_id'],
        'new-run',
      );
      expect(
        service.agentOutputStates['session-run-gate']?['state'],
        'streaming',
      );
    },
  );

  test(
    'agent_output_status orders same-run reversals by revision and new runs by generation',
    () async {
      final service = _makeImService();

      Future<void> push({
        required String runId,
        required String state,
        required int generation,
        required int revision,
        required int updatedAt,
      }) {
        return service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'agent_output_status',
            'payload': {
              'session_id': 'session-output-revision',
              'agent_id': 'agent-output-revision',
              'run_id': runId,
              'state': state,
              'can_stop': state != 'stopping',
              'dispatch_generation': generation,
              'revision': revision,
              'updated_at': updatedAt,
            },
          }),
        );
      }

      await push(
        runId: 'run-revision-1',
        state: 'stopping',
        generation: 10,
        revision: 2,
        updatedAt: 900,
      );
      // stop_result.status=failed legitimately reverses stopping -> received.
      // Its server timestamp may be lower on another node; revision wins.
      await push(
        runId: 'run-revision-1',
        state: 'received',
        generation: 10,
        revision: 3,
        updatedAt: 100,
      );
      expect(
        service.agentOutputStates['session-output-revision']?['state'],
        'received',
      );

      await push(
        runId: 'run-revision-1',
        state: 'streaming',
        generation: 10,
        revision: 2,
        updatedAt: 1000,
      );
      expect(
        service.agentOutputStates['session-output-revision']?['state'],
        'received',
      );

      await push(
        runId: 'run-revision-2',
        state: 'queued',
        generation: 11,
        revision: 1,
        updatedAt: 50,
      );
      expect(service.agentOutputStates['session-output-revision'], {
        'run_id': 'run-revision-2',
        'session_id': 'session-output-revision',
        'dispatch_generation': 11,
        'revision': 1,
        'scope': '',
        'owner_id': '',
        'agent_id': 'agent-output-revision',
        'trigger_msg_id': '',
        'stream_msg_id': '',
        'state': 'queued',
        'can_stop': true,
        'stop_reason': '',
        'updated_at': 50,
      });

      await push(
        runId: 'run-revision-1',
        state: 'failed',
        generation: 10,
        revision: 99,
        updatedAt: 2000,
      );
      expect(
        service.agentOutputStates['session-output-revision']?['run_id'],
        'run-revision-2',
      );
    },
  );

  test(
    'current session push_msg queues remote read sync for peer message',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': '3001',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'new',
            'created_at': 3000,
            'inbox_seq': 3,
          },
        }),
      );

      await Future<void>.delayed(const Duration(milliseconds: 30));
      final pending = await _readPendingReadStates();
      expect(pending['s1'], '3001');
    },
  );

  test(
    'session_read_ack does not requeue when viewing heartbeat is active',
    () async {
      final service = _makeImService();
      service.enterSession('s1');
      await Future<void>.delayed(const Duration(milliseconds: 30));

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': '4001',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'new',
            'created_at': 4000,
            'inbox_seq': 4,
          },
        }),
      );

      await _expectPendingReadStateEventually('s1', '4001');
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_read_ack',
          'payload': {
            'session_id': 's1',
            'code': 0,
            'last_read_msg_id': '4001',
          },
        }),
      );

      await _expectPendingReadStateClearedEventually('s1');
    },
  );

  test(
    'session read cursor preserves 19-digit snowflake id without precision loss',
    () async {
      final service = _makeImService();
      // 19 位雪花号，若中途转 int 在 Web(JS 53 位)会被舍入尾部。
      const bigId = '7268010353936384123';
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_read_sync',
          'payload': {
            'session_id': 's1',
            'reader_id': 'u2',
            'last_read_msg_id': bigId,
          },
        }),
      );
      expect(service.getSessionReadCursor('s1', 'u2'), bigId);
    },
  );

  test(
    'session read cursor advances by numeric comparison, not lexicographic',
    () async {
      final service = _makeImService();
      // larger20 数值更大但字典序更小（'1...' < '9...'）；只有数值比较才能正确推进/不回退。
      const smaller19 = '9000000000000000000';
      const larger20 = '10000000000000000000';
      Future<void> sync(String id) => service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_read_sync',
          'payload': {
            'session_id': 's2',
            'reader_id': 'u2',
            'last_read_msg_id': id,
          },
        }),
      );

      await sync(smaller19);
      await sync(larger20);
      expect(service.getSessionReadCursor('s2', 'u2'), larger20);

      // 数值更小的游标不得让已读位置回退。
      await sync(smaller19);
      expect(service.getSessionReadCursor('s2', 'u2'), larger20);
    },
  );

  test(
    'deleteConversation queues remote read sync for deleted session',
    () async {
      await LocalDb.setActiveUser(_testUserId);
      try {
        final service = _makeImService();
        await LocalDb.batchInsertMessages([
          {
            'msg_id': '9001',
            'session_id': 's-del-read',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'x',
            'created_at': 9001,
          },
        ]);
        await service.deleteConversation('s-del-read');

        await Future<void>.delayed(const Duration(milliseconds: 30));
        final pending = await _readPendingReadStates();
        expect(pending['s-del-read'], '9001');
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test('auth_ack queues read sync for previously deleted sessions', () async {
    SharedPreferences.setMockInitialValues({
      _deletedSessionsKey(): jsonEncode({'s-del-legacy': 1710000000000}),
    });
    await LocalDb.setActiveUser(_testUserId);
    try {
      await LocalDb.batchInsertMessages([
        {
          'msg_id': '9101',
          'session_id': 's-del-legacy',
          'sender_id': 'u2',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'x',
          'created_at': 9101,
        },
      ]);
      final service = _makeImService();

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'auth_ack',
          'payload': {'code': 0, 'user_id': _testUserId},
        }),
      );

      await Future<void>.delayed(const Duration(milliseconds: 30));
      final pending = await _readPendingReadStates();
      expect(pending['s-del-legacy'], '9101');
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });

  test(
    'current session window stays bounded while push messages keep arriving',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      final total = service.residentMessageCapForTest + 20;
      for (var i = 1; i <= total; i++) {
        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'push_msg',
            'payload': {
              'msg_id': '$i',
              'session_id': 's1',
              'sender_id': 'u2',
              'msg_type': 1,
              'content': 'msg_$i',
              'created_at': i * 1000,
              'inbox_seq': i,
            },
          }),
        );
      }

      expect(service.currentMessages.length, service.residentMessageCapForTest);
      expect(service.currentMessages.first.msgId, '21');
      expect(service.currentMessages.last.msgId, '$total');
    },
  );

  test(
    'active streaming placeholder survives leaving and reloading session',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('stream-session');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'stream-msg-1',
            'session_id': 'stream-session',
            'sender_id': '2002',
            'sender_type': 2,
            'delta_content': 'partial content',
            'chunk_seq': 1,
          },
        }),
      );

      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'stream-msg-1',
      ]);
      expect(service.currentMessages.single.msgType, 4);
      expect(service.isMessageStreaming('stream-msg-1'), isTrue);
      expect(
        MessageStreamController.peekContent('stream-msg-1'),
        'partial content',
      );

      service.leaveSession();
      expect(service.currentMessages, isEmpty);

      await service.loadInitialWindowForTest('stream-session');

      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'stream-msg-1',
      ]);
      expect(service.currentMessages.single.msgType, 4);
      expect(service.isMessageStreaming('stream-msg-1'), isTrue);
      expect(
        MessageStreamController.peekContent('stream-msg-1'),
        'partial content',
      );
    },
  );

  test(
    'stream chunk is_thinking flag lands on streaming placeholder',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('think-session');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'think-msg-1',
            'session_id': 'think-session',
            'sender_id': '2002',
            'sender_type': 2,
            'delta_content': '正在思考',
            'chunk_seq': 1,
            'is_thinking': true,
          },
        }),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'answer-msg-1',
            'session_id': 'think-session',
            'sender_id': '2002',
            'sender_type': 2,
            'delta_content': '正文回答',
            'chunk_seq': 1,
          },
        }),
      );

      final thinkMsg = service.currentMessages.firstWhere(
        (m) => m.msgId == 'think-msg-1',
      );
      final answerMsg = service.currentMessages.firstWhere(
        (m) => m.msgId == 'answer-msg-1',
      );
      expect(thinkMsg.isThinking, isTrue);
      expect(answerMsg.isThinking, isFalse);
    },
  );

  test(
    'streaming session preview publishes short visible replies and throttles updates',
    () async {
      final service = _makeImService();
      service.sessions.add(
        SessionModel(
          sessionId: 'preview-stream-session',
          peerId: '2002',
          peerType: 2,
          updatedAt: 1000,
          lastMessage: '上一条消息',
          lastMessageTime: 1000,
        ),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'preview-stream-msg',
            'session_id': 'preview-stream-session',
            'sender_id': '2002',
            'sender_type': 2,
            'delta_content': '好',
            'chunk_seq': 1,
            'created_at': 2000,
          },
        }),
      );

      expect(
        service.streamingSessionPreviewForSession('preview-stream-session'),
        isEmpty,
        reason: '首段只做短暂 token 合并，不按文字长度过滤。',
      );
      await Future<void>.delayed(const Duration(milliseconds: 230));
      expect(
        service.streamingSessionPreviewForSession('preview-stream-session'),
        '好',
        reason: '即使只有一个可见字符，也应成为互动摘要。',
      );
      expect(service.sessions.single.lastMessage, '上一条消息');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'preview-stream-msg',
            'session_id': 'preview-stream-session',
            'sender_id': '2002',
            'sender_type': 2,
            'delta_content': '的，我来',
            'chunk_seq': 2,
            'created_at': 2100,
          },
        }),
      );

      await Future<void>.delayed(const Duration(milliseconds: 100));
      expect(
        service.streamingSessionPreviewForSession('preview-stream-session'),
        '好',
        reason: '后续 token 应节流，避免会话列表逐 token 闪动。',
      );
      await Future<void>.delayed(const Duration(milliseconds: 600));
      expect(
        service.streamingSessionPreviewForSession('preview-stream-session'),
        '好的，我来',
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'preview-stream-msg',
            'session_id': 'preview-stream-session',
            'sender_id': '2002',
            'sender_type': 2,
            'final_content': '好的，我来检查一下',
            'last_chunk_seq': 2,
            'is_finish': true,
            'created_at': 2200,
          },
        }),
      );

      expect(
        service.streamingSessionPreviewForSession('preview-stream-session'),
        isEmpty,
      );
      expect(service.sessions.single.lastMessage, '好的，我来检查一下');
    },
  );

  test(
    'streaming session preview freezes after enough summarized text',
    () async {
      final service = _makeImService();
      service.sessions.add(
        SessionModel(
          sessionId: 'bounded-preview-session',
          peerId: '2002',
          peerType: 2,
          updatedAt: 1000,
          lastMessage: '上一条消息',
          lastMessageTime: 1000,
        ),
      );
      final enoughText = String.fromCharCodes(
        List<int>.filled(160, '文'.runes.single),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'bounded-preview-msg',
            'session_id': 'bounded-preview-session',
            'sender_id': '2002',
            'sender_type': 2,
            'delta_content': enoughText,
            'chunk_seq': 1,
            'created_at': 2000,
          },
        }),
      );

      final frozenPreview = service.streamingSessionPreviewForSession(
        'bounded-preview-session',
      );
      expect(frozenPreview.runes.length, 120);
      final publishedTick = service.streamingSessionPreviewTickRx.value;

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'bounded-preview-msg',
            'session_id': 'bounded-preview-session',
            'sender_id': '2002',
            'sender_type': 2,
            'delta_content': '这段后续分片不应再改变会话摘要',
            'chunk_seq': 2,
            'created_at': 2100,
          },
        }),
      );
      await Future<void>.delayed(const Duration(milliseconds: 700));

      expect(
        service.streamingSessionPreviewForSession('bounded-preview-session'),
        frozenPreview,
      );
      expect(service.streamingSessionPreviewTickRx.value, publishedTick);
    },
  );

  test(
    'streaming session preview freezes when the cutoff lands on whitespace',
    () async {
      final service = _makeImService();
      service.sessions.add(
        SessionModel(
          sessionId: 'whitespace-preview-session',
          peerId: '2002',
          peerType: 2,
          updatedAt: 1000,
          lastMessage: 'previous message',
          lastMessageTime: 1000,
        ),
      );
      final boundaryPrefix = List<String>.filled(119, 'a').join();
      final boundaryText = '$boundaryPrefix b';

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'whitespace-preview-msg',
            'session_id': 'whitespace-preview-session',
            'sender_id': '2002',
            'sender_type': 2,
            'delta_content': boundaryText,
            'chunk_seq': 1,
            'created_at': 2000,
          },
        }),
      );

      final frozenPreview = service.streamingSessionPreviewForSession(
        'whitespace-preview-session',
      );
      expect(frozenPreview, boundaryPrefix);
      final publishedTick = service.streamingSessionPreviewTickRx.value;

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'whitespace-preview-msg',
            'session_id': 'whitespace-preview-session',
            'sender_id': '2002',
            'sender_type': 2,
            'delta_content': 'later chunks must not trigger another summary',
            'chunk_seq': 2,
            'created_at': 2100,
          },
        }),
      );
      await Future<void>.delayed(const Duration(milliseconds: 700));

      expect(
        service.streamingSessionPreviewForSession('whitespace-preview-session'),
        frozenPreview,
      );
      expect(service.streamingSessionPreviewTickRx.value, publishedTick);
    },
  );

  test('thinking stream never becomes a session preview', () async {
    final service = _makeImService();
    service.sessions.add(
      SessionModel(
        sessionId: 'thinking-preview-session',
        updatedAt: 1000,
        lastMessage: '上一条消息',
        lastMessageTime: 1000,
      ),
    );

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'stream_chunk',
        'payload': {
          'msg_id': 'thinking-preview-msg',
          'session_id': 'thinking-preview-session',
          'sender_id': '2002',
          'sender_type': 2,
          'delta_content': '这是内部分析过程',
          'chunk_seq': 1,
          'is_thinking': true,
          'created_at': 2000,
        },
      }),
    );

    await Future<void>.delayed(const Duration(milliseconds: 230));
    expect(
      service.streamingSessionPreviewForSession('thinking-preview-session'),
      isEmpty,
    );
    expect(service.sessions.single.lastMessage, '上一条消息');
  });

  test(
    'stream chunk visible_to lands on placeholder and survives stream_finish',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('lock-session');

      // 流式期:隐藏消息的回复分片携带 visible_to,占位即应带锁标记数据。
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'lock-msg-1',
            'session_id': 'lock-session',
            'sender_id': '2002',
            'sender_type': 2,
            'delta_content': 'hidden reply',
            'chunk_seq': 1,
            'visible_to': ['42'],
          },
        }),
      );

      expect(service.currentMessages.single.msgType, 4);
      expect(service.currentMessages.single.visibleTo, ['42']);

      // finalize:stream_finish 同样携带 visible_to,收尾后锁标记不丢。
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'lock-msg-1',
            'session_id': 'lock-session',
            'sender_id': '2002',
            'sender_type': 2,
            'final_content': 'hidden reply done',
            'last_chunk_seq': 1,
            'is_finish': true,
            'visible_to': ['42'],
          },
        }),
      );

      expect(service.currentMessages.single.msgType, 1);
      expect(service.currentMessages.single.visibleTo, ['42']);
    },
  );

  test(
    'thinking stream finishes into a decodable grix thinking card (seam)',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('think-seam');

      const msgId = 'think-seam-1';
      // 流式期:thinking 分片携带 is_thinking,占位为 thinking 流。
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': msgId,
            'session_id': 'think-seam',
            'sender_id': '2002',
            'sender_type': 2,
            'delta_content': '分析过程',
            'chunk_seq': 1,
            'is_thinking': true,
          },
        }),
      );
      expect(
        service.currentMessages.firstWhere((m) => m.msgId == msgId).isThinking,
        isTrue,
      );

      // finalize:后端把思考内容包成 grix 思考卡片 URI 下发 stream_finish。
      final cardContent = ChatMessageCardCodec.encode(
        const ChatThinkingCardData(content: '分析过程'),
      ).content;
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': msgId,
            'session_id': 'think-seam',
            'sender_id': '2002',
            'sender_type': 2,
            'final_content': cardContent,
            'last_chunk_seq': 2,
            'is_finish': true,
          },
        }),
      );

      // 收尾后消息转为普通消息,内容为卡片 URI,解码得到同一思考卡片,无缝衔接。
      final finalized = service.currentMessages.firstWhere(
        (m) => m.msgId == msgId,
      );
      expect(finalized.msgType, 1);
      expect(finalized.content, cardContent);
      final decoded = ChatMessageCardCodec.decodeFromMessage(
        content: finalized.content,
      );
      expect(decoded, isA<ChatThinkingCardData>());
      expect(decoded!.type, ChatMessageCardType.thinking);
      expect((decoded as ChatThinkingCardData).displayContent, '分析过程');
    },
  );

  test('stream chunks keep buffering while session is inactive', () async {
    final service = _makeImService();
    service.setCurrentSessionForTest('stream-session');

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'stream_chunk',
        'payload': {
          'msg_id': 'stream-msg-2',
          'session_id': 'stream-session',
          'sender_id': '2002',
          'sender_type': 2,
          'delta_content': 'hello',
          'chunk_seq': 1,
          'created_at': 1700000001000,
        },
      }),
    );

    service.leaveSession();

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'stream_chunk',
        'payload': {
          'msg_id': 'stream-msg-2',
          'session_id': 'stream-session',
          'sender_id': '2002',
          'sender_type': 2,
          'delta_content': ' world',
          'chunk_seq': 2,
          'created_at': 1700000001000,
        },
      }),
    );

    expect(service.isMessageStreaming('stream-msg-2'), isTrue);
    expect(MessageStreamController.peekContent('stream-msg-2'), 'hello world');

    await service.loadInitialWindowForTest('stream-session');

    expect(service.currentMessages.map((e) => e.msgId).toList(), [
      'stream-msg-2',
    ]);
    expect(service.currentMessages.single.msgType, 4);
    expect(service.currentMessages.single.createdAt, 1700000001000);
    expect(MessageStreamController.peekContent('stream-msg-2'), 'hello world');
  });

  test('streaming placeholder survives when terminal agent_output_get_resp '
      'races with loadInitialMessages', () async {
    // Bug: when a terminal agent_output_get_resp (e.g. state=completed) arrives
    // during _loadInitialMessages, it clears _activeStreamingMsgIds, causing
    // _restoreStreamingPlaceholdersForSession to fail. The streaming message
    // bubble disappears even though content was being generated.
    final userId = 'stream_race_${DateTime.now().millisecondsSinceEpoch}';
    await LocalDb.setActiveUser(userId);
    try {
      final service = _makeImService();
      service.seedRealtimeStateForTest(connected: true, authenticated: true);

      // 1. Set up remote streaming in session
      service.setCurrentSessionForTest('race-s1');
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'race-stream-1',
            'session_id': 'race-s1',
            'sender_id': 'agent-race',
            'sender_type': 2,
            'delta_content': 'generating',
            'chunk_seq': 1,
            'created_at': 1700000001000,
          },
        }),
      );
      service.agentOutputStates['race-s1'] = {
        'session_id': 'race-s1',
        'run_id': 'run-race-1',
        'stream_msg_id': 'race-stream-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 1700000001000,
      };

      expect(service.isMessageStreaming('race-stream-1'), isTrue);
      expect(service.currentMessages.single.msgId, 'race-stream-1');

      // 2. Leave session — streaming state preserved in _activeStreamingMsgIds
      service.leaveSession();
      expect(service.currentMessages, isEmpty);
      expect(service.isMessageStreaming('race-stream-1'), isTrue);

      // 3. Start reloading (async — yields at DB read inside _loadInitialMessages)
      final loadFuture = service.loadInitialWindowForTest('race-s1');

      // 4. Inject terminal agent_output_get_resp during the async load.
      //    This clears _activeStreamingMsgIds, simulating the race condition
      //    where the server responds "completed" while we're loading messages.
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_output_get_resp',
          'payload': {
            'session_id': 'race-s1',
            'active': true,
            'status': {
              'session_id': 'race-s1',
              'run_id': 'run-race-1',
              'stream_msg_id': 'race-stream-1',
              'state': 'completed',
              'can_stop': false,
              'updated_at': 1700000002000,
            },
          },
        }),
      );

      // 5. Wait for load to complete
      await loadFuture;

      // 6. BUG REPRODUCTION: streaming placeholder should still be visible
      //    (either as streaming placeholder or as the placeholder about to be
      //    finalized). Without the fix, the bubble disappears because
      //    _restoreStreamingPlaceholdersForSession can't restore —
      //    _activeStreamingMsgIds was cleared by the concurrent event.
      expect(
        service.currentMessages.any((m) => m.msgId == 'race-stream-1'),
        isTrue,
        reason:
            'Streaming placeholder must survive concurrent terminal '
            'agent_output_get_resp during initial load',
      );
      expect(
        MessageStreamController.peekRecoverableContent('race-stream-1'),
        'generating',
      );
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });

  test(
    'cursor paging can load older pages and recover trimmed newer pages',
    () async {
      final userId =
          'history_test_user_${DateTime.now().millisecondsSinceEpoch}';
      await LocalDb.setActiveUser(userId);

      try {
        const baseTs = 1700000000000;
        for (var i = 1; i <= 400; i++) {
          await LocalDb.upsertMessage({
            'msg_id': '$i',
            'session_id': 's1',
            'sender_id': 'u2',
            'msg_type': 1,
            'content': 'msg_$i',
            'created_at': baseTs + (i * 1000),
          });
        }

        final service = _makeImService();
        await service.loadInitialWindowForTest('s1');

        // The first paint stays bounded to 30 recent messages.
        expect(service.currentMessages.length, 30);
        expect(service.currentMessages.first.msgId, '371');
        expect(service.currentMessages.last.msgId, '400');
        expect(service.hasOlderMessages, isTrue);
        expect(service.hasNewerMessages, isFalse);

        for (var page = 0; page < 5; page++) {
          await service.loadOlderForCurrentSession();
        }

        // 5 pages × 40 + 30 initial = 230, cap 200 → trim bottom 30.
        // window = id 171-370
        expect(
          service.currentMessages.length,
          service.residentMessageCapForTest,
        );
        expect(service.currentMessages.first.msgId, '171');
        expect(service.currentMessages.last.msgId, '370');
        expect(service.hasNewerMessages, isTrue);

        await service.loadNewerForCurrentSession();

        // Load 30 newer (id 371-400) → total 230, trim top 30.
        // window = id 201-400
        expect(
          service.currentMessages.length,
          service.residentMessageCapForTest,
        );
        expect(service.currentMessages.first.msgId, '201');
        expect(service.currentMessages.last.msgId, '400');
        expect(service.hasOlderMessages, isTrue);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'initial window reads local DB only, does not await remote history',
    () async {
      final userId =
          'history_refresh_user_${DateTime.now().millisecondsSinceEpoch}';
      await LocalDb.setActiveUser(userId);

      final sessionService = _FakeSessionService()
        ..historyResult = const SessionMessageHistoryResult(
          messages: [
            {
              'msg_id': '2',
              'session_id': 's1',
              'sender_id': 'u2',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'remote latest',
              'created_at': 1700000002000,
            },
          ],
        );
      Get.put<SessionService>(sessionService);

      try {
        await LocalDb.upsertMessage({
          'msg_id': '1',
          'session_id': 's1',
          'sender_id': 'u2',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'local old',
          'created_at': 1700000001000,
        });

        final service = _makeImService();
        await service.loadInitialWindowForTest('s1');

        // New architecture: _loadInitialMessages only reads local DB.
        // Remote history is NOT fetched synchronously — it arrives via
        // pull_sync or backfill and is consumed through the DB change bus.
        expect(service.currentMessages.map((e) => e.msgId).toList(), ['1']);
        expect(service.currentMessages.first.content, 'local old');
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test('late history reconcile cannot publish into a newer session', () async {
    final userId =
        'history_session_guard_${DateTime.now().millisecondsSinceEpoch}';
    await LocalDb.setActiveUser(userId);
    final historyCompleter = Completer<SessionMessageHistoryResult>();
    final sessionService = _FakeSessionService()
      ..historyCompleter = historyCompleter;
    Get.put<SessionService>(sessionService);

    try {
      await LocalDb.upsertMessage({
        'msg_id': 's1-local',
        'session_id': 's1',
        'sender_id': 'u2',
        'sender_type': 1,
        'msg_type': 1,
        'content': 's1 local',
        'created_at': 1700000000000,
      });
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      await service.loadInitialWindowForTest('s1');
      expect(sessionService.historyCalls, 1);

      service.leaveSession('s1');
      service.setCurrentSessionForTest('s2');
      service.upsertUIMessageForTest(
        _msg(
          msgId: 's2-current',
          sessionId: 's2',
          content: 's2 current',
          createdAt: 1700000001000,
        ),
      );

      historyCompleter.complete(
        const SessionMessageHistoryResult(
          messages: [
            {
              'msg_id': 's1-late',
              'session_id': 's1',
              'sender_id': 'u2',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'late s1 history',
              'created_at': 1700000002000,
            },
          ],
          hasMore: false,
        ),
      );
      for (var attempt = 0; attempt < 20; attempt++) {
        final stored = await LocalDb.getMessageByMsgId('s1-late');
        if (stored != null) break;
        await Future<void>.delayed(const Duration(milliseconds: 10));
      }

      expect(service.currentMessages.map((message) => message.msgId), [
        's2-current',
      ]);
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });

  test(
    'initial window renders local cache immediately without waiting for remote',
    () async {
      final userId =
          'history_fast_cache_user_${DateTime.now().millisecondsSinceEpoch}';
      await LocalDb.setActiveUser(userId);

      final sessionService = _FakeSessionService();
      Get.put<SessionService>(sessionService);

      try {
        await LocalDb.upsertMessage({
          'msg_id': '1',
          'session_id': 's1',
          'sender_id': 'u2',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'local cached',
          'created_at': 1700000001000,
        });

        final service = _makeImService();
        // New architecture: loadInitialWindowForTest returns immediately
        // after reading local DB — no remote history call.
        await service.loadInitialWindowForTest('s1');

        // Local cache renders instantly without waiting for remote history.
        expect(service.currentMessages.map((e) => e.content).toList(), [
          'local cached',
        ]);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'older page empty locally backfills through DB change bus without awaiting remote',
    () async {
      final userId =
          'history_older_bus_user_${DateTime.now().millisecondsSinceEpoch}';
      await LocalDb.setActiveUser(userId);

      final sessionService = _FakeSessionService()
        ..historyCompleter = Completer<SessionMessageHistoryResult>();
      Get.put<SessionService>(sessionService);

      try {
        await LocalDb.upsertMessage({
          'msg_id': '100',
          'session_id': 's1',
          'sender_id': 'u2',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'local newest',
          'created_at': 1700000100000,
        });

        final service = _makeImService();
        service.setCurrentSessionForTest('s1');
        await service.loadInitialWindowForTest('s1');
        expect(service.currentMessages.map((e) => e.msgId).toList(), ['100']);

        await service.loadOlderForCurrentSession();
        expect(sessionService.historyCalls, 1);
        expect(service.currentMessages.map((e) => e.msgId).toList(), ['100']);

        sessionService.historyCompleter!.complete(
          const SessionMessageHistoryResult(
            messages: [
              {
                'msg_id': '90',
                'session_id': 's1',
                'sender_id': 'u2',
                'sender_type': 1,
                'msg_type': 1,
                'content': 'remote older',
                'created_at': 1700000090000,
              },
            ],
            hasMore: false,
          ),
        );

        for (
          var i = 0;
          i < 20 && service.currentMessages.first.msgId != '90';
          i++
        ) {
          await Future<void>.delayed(const Duration(milliseconds: 10));
        }
        expect(service.currentMessages.map((e) => e.msgId).toList(), [
          '90',
          '100',
        ]);
        expect(service.hasOlderMessages, isFalse);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'fresh cached session window restores immediately then refreshes history',
    () async {
      final sessionService = _FakeSessionService()
        ..historyResult = const SessionMessageHistoryResult(
          messages: [
            {
              'msg_id': 'remote-1',
              'session_id': 's1',
              'sender_id': 'u2',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'remote refresh',
              'created_at': 1700000002000,
            },
          ],
        );
      Get.put<SessionService>(sessionService);
      final userId =
          'history_cache_user_${DateTime.now().millisecondsSinceEpoch}';
      await LocalDb.setActiveUser(userId);

      try {
        final service = _makeImService();
        service.setCurrentSessionForTest('s1');
        service.upsertUIMessageForTest(
          _msg(
            msgId: 'cached-1',
            sessionId: 's1',
            senderId: 'u2',
            createdAt: 1700000001000,
            content: 'cached message',
          ),
        );

        service.leaveSession('s1');
        expect(service.currentMessages, isEmpty);

        service.enterSession('s1');

        expect(service.currentMessages.map((e) => e.msgId).toList(), [
          'cached-1',
        ]);
        expect(service.currentMessages.single.content, 'cached message');
        expect(sessionService.historyCalls, 0);

        for (var i = 0; i < 20 && sessionService.historyCalls == 0; i++) {
          await Future<void>.delayed(const Duration(milliseconds: 10));
        }

        expect(sessionService.historyCalls, 1);
        // The local DB snapshot is empty (cached message was UI-only), so the
        // window is transiently cleared before the remote backfill repopulates
        // it. Guard against the empty window while polling.
        for (
          var i = 0;
          i < 20 &&
              (service.currentMessages.isEmpty ||
                  service.currentMessages.first.msgId != 'remote-1');
          i++
        ) {
          await Future<void>.delayed(const Duration(milliseconds: 10));
        }
        expect(service.currentMessages.map((e) => e.msgId).toList(), [
          'remote-1',
        ]);
        expect(service.currentMessages.single.content, 'remote refresh');
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test('clearUnread does not republish an already-read session', () async {
    final service = _makeImService();
    service.sessions.assignAll([
      SessionModel(
        sessionId: 'already-read',
        updatedAt: 1700000000000,
        lastMessageTime: 1700000000000,
      ),
    ]);
    var publications = 0;
    final subscription = service.sessions.listen((_) {
      publications++;
    });
    try {
      service.clearUnread('already-read');
      expect(publications, 0);
    } finally {
      await subscription.cancel();
    }
  });

  test('totalUnread excludes current session unread count', () {
    final service = _makeImService();
    service.setCurrentSessionForTest('s1');
    service.sessions.assignAll([
      SessionModel(
        sessionId: 's1',
        type: 'private',
        updatedAt: 2000,
        unreadCount: 9,
        lastMessage: 'active',
        lastMessageTime: 2000,
      ),
      SessionModel(
        sessionId: 's2',
        type: 'private',
        updatedAt: 2100,
        unreadCount: 2,
        lastMessage: 'other',
        lastMessageTime: 2100,
      ),
    ]);

    expect(service.totalUnread, 2);
  });

  test(
    'notificationUnread excludes current session and muted session unread',
    () {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.sessions.assignAll([
        SessionModel(
          sessionId: 's1',
          type: 'private',
          updatedAt: 2000,
          unreadCount: 9,
          lastMessage: 'active',
          lastMessageTime: 2000,
        ),
        SessionModel(
          sessionId: 's2',
          type: 'group',
          updatedAt: 2100,
          unreadCount: 5,
          lastMessage: 'muted',
          lastMessageTime: 2100,
          isMuted: true,
        ),
        SessionModel(
          sessionId: 's3',
          type: 'private',
          updatedAt: 2200,
          unreadCount: 2,
          lastMessage: 'visible',
          lastMessageTime: 2200,
        ),
      ]);

      expect(service.totalUnread, 7);
      expect(service.notificationUnread, 2);
    },
  );

  test(
    'first private push_msg keeps title unchanged without local hydration',
    () async {
      final fakeFriendService = _FakeFriendService()
        ..fetchedNicknames['2002'] = 'Alice';
      Get.put<FriendService>(fakeFriendService);
      final service = _makeImService();

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'm-first',
            'session_id': 'session_very_long_id_2002',
            'session_type': 1,
            'sender_id': '2002',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'hello',
            'created_at': 1000,
            'inbox_seq': 1,
          },
        }),
      );
      await Future<void>.delayed(const Duration(milliseconds: 20));

      final idx = service.sessions.indexWhere(
        (s) => s.sessionId == 'session_very_long_id_2002',
      );
      expect(idx, isNonNegative);
      expect(service.sessions[idx].title, isEmpty);
      expect(fakeFriendService.fetchProfileCalls, 0);
    },
  );

  test(
    'bound group title is preserved when first push message arrives',
    () async {
      final service = _makeImService();

      await service.bindSessionDisplayTitle(
        'group-title-first-push-1',
        '项目讨论组',
        type: 'group',
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'm-group-first',
            'session_id': 'group-title-first-push-1',
            'session_type': 2,
            'sender_id': '3001',
            'sender_type': 1,
            'msg_type': 1,
            'content': '嘿！我是小龙虾',
            'created_at': 1000,
            'inbox_seq': 1,
          },
        }),
      );
      await Future<void>.delayed(const Duration(milliseconds: 20));

      final idx = service.sessions.indexWhere(
        (s) => s.sessionId == 'group-title-first-push-1',
      );
      expect(idx, isNonNegative);
      expect(service.sessions[idx].title, '项目讨论组');
      expect(service.sessions[idx].lastMessage, '嘿！我是小龙虾');
    },
  );

  test('binding a new session title emits one sessions notification', () async {
    final service = _makeImService();
    service.sessions.value = List<SessionModel>.generate(
      200,
      (index) => SessionModel(
        sessionId: 'existing-session-$index',
        title: 'Existing $index',
        updatedAt: index,
        lastMessageTime: index,
      ),
    );

    var notificationCount = 0;
    final worker = ever<List<SessionModel>>(service.sessions, (_) {
      notificationCount++;
    });
    try {
      await service.bindSessionDisplayTitle('new-session', 'New session');
    } finally {
      worker.dispose();
    }

    expect(notificationCount, 1);
    expect(service.sessions, hasLength(201));
    expect(service.sessions.first.sessionId, 'new-session');
  });

  test(
    'resolveSessionDisplayTitleById prefers local session title over fallback',
    () {
      final service = _makeImService();
      service.sessions.assignAll([
        SessionModel(
          sessionId: 'session-title-priority-1',
          title: 'Topic Alpha',
          type: 'private',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);

      final resolved = service.resolveSessionDisplayTitleById(
        'session-title-priority-1',
        fallbackTitle: 'Alice',
        type: 'private',
      );
      expect(resolved, 'Topic Alpha');
    },
  );

  test(
    'session_member_changed rename overrides stale route fallback title',
    () async {
      final service = _makeImService();
      service.sessions.assignAll([
        SessionModel(
          sessionId: 'session-rename-sync-1',
          title: 'Old Topic',
          type: 'private',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_member_changed',
          'payload': {
            'session_id': 'session-rename-sync-1',
            'action': 'rename',
            'title': 'New Topic',
          },
        }),
      );
      await Future<void>.delayed(const Duration(milliseconds: 20));

      final idx = service.sessions.indexWhere(
        (s) => s.sessionId == 'session-rename-sync-1',
      );
      expect(idx, isNonNegative);
      expect(service.sessions[idx].title, 'New Topic');
      expect(
        service.resolveSessionDisplayTitleById(
          'session-rename-sync-1',
          fallbackTitle: 'Alice',
          type: 'private',
        ),
        'New Topic',
      );
    },
  );

  test(
    'sendMessage inserts local sending stub through DB change bus',
    () async {
      final userId =
          'send-stub-bus-${DateTime.now().millisecondsSinceEpoch.toString()}';
      await LocalDb.setActiveUser(userId);
      try {
        final service = _makeSpyImService();
        service.setCurrentSessionForTest('s1');

        await service.sendMessage('hello bus', 's1');
        for (var i = 0; i < 20 && service.currentMessages.isEmpty; i++) {
          await Future<void>.delayed(const Duration(milliseconds: 10));
        }

        expect(service.currentMessages, hasLength(1));
        final msg = service.currentMessages.single;
        expect(msg.content, 'hello bus');
        expect(msg.status, 'sending');
        expect(msg.msgId.startsWith('temp_'), isTrue);
        expect(msg.clientMsgId?.isNotEmpty, isTrue);
        expect(service.packetDispatchCalls, 1);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'sendMessage updateCurrentSessionUi false skips local window event',
    () async {
      final userId =
          'send-stub-skip-${DateTime.now().millisecondsSinceEpoch.toString()}';
      await LocalDb.setActiveUser(userId);
      try {
        final service = _makeSpyImService();
        service.setCurrentSessionForTest('s1');

        await service.sendMessage(
          'forwarded silently',
          's1',
          updateCurrentSessionUi: false,
        );
        await Future<void>.delayed(const Duration(milliseconds: 20));

        expect(service.currentMessages, isEmpty);
        expect(service.packetDispatchCalls, 1);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test('send_ack deduplicates temporary and server message id', () async {
    final service = _makeImService();
    service.upsertUIMessageForTest(
      _msg(msgId: '100', createdAt: 2000, content: 'server copy'),
    );
    service.upsertUIMessageForTest(
      _msg(
        msgId: 'temp_cid-1',
        clientMsgId: 'cid-1',
        createdAt: 2000,
        content: 'local temp',
        status: 'sending',
        senderId: 'me',
      ),
    );

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'send_ack',
        'payload': {'client_msg_id': 'cid-1', 'msg_id': '100', 'inbox_seq': 7},
      }),
    );

    expect(service.currentMessages.length, 1);
    expect(service.currentMessages.first.msgId, '100');
    expect(service.currentMessages.first.status, 'success');
  });

  test('send_ack updates msgId/status and applies server createdAt', () async {
    // ACK 到来后，UI 直接使用服务端 createdAt，保证跨端一致排序。
    final service = _makeImService();
    const baseMs = 1700000000000;

    service.upsertUIMessageForTest(_msg(msgId: 'old', createdAt: baseMs));
    service.upsertUIMessageForTest(
      _msg(
        msgId: 'temp_cid-2',
        clientMsgId: 'cid-2',
        createdAt: baseMs + 500, // 本地时间
        senderId: 'me',
        status: 'sending',
      ),
    );

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'send_ack',
        'payload': {
          'client_msg_id': 'cid-2',
          'msg_id': '200',
          'created_at': 1700000001, // 服务端时间（秒）= baseMs + 1000ms
          'inbox_seq': 8,
        },
      }),
    );

    // 消息顺序不变（200 仍在 old 之后）
    expect(service.currentMessages.map((e) => e.msgId).toList(), [
      'old',
      '200',
    ]);
    expect(service.currentMessages.last.createdAt, 1700000001 * 1000);
    expect(service.currentMessages.last.status, 'success');
  });

  test('send_nack ignores stale nack after message already acked', () async {
    final userId =
        'stale-nack-${DateTime.now().millisecondsSinceEpoch.toString()}';
    await LocalDb.setActiveUser(userId);
    try {
      final service = _makeImService();
      service.upsertUIMessageForTest(
        _msg(
          msgId: '501',
          clientMsgId: 'cid-stale-nack-1',
          createdAt: 1000,
          content: 'already acked',
          senderId: 'me',
          status: 'success',
        ),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'send_nack',
          'payload': {
            'client_msg_id': 'cid-stale-nack-1',
            'code': 5001,
            'msg': 'save message failed',
          },
        }),
      );

      expect(service.currentMessages, hasLength(1));
      expect(service.currentMessages.first.msgId, '501');
      expect(service.currentMessages.first.status, 'success');
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });

  test(
    'send_ack reorders UI by server createdAt when ack time is earlier',
    () async {
      // 竞态场景：发送 A（本地时间 T+200ms），收到 B（服务端时间 T），
      // 随后收到 A 的 ack（服务端时间 T-100ms）。
      // ACK 后应按服务端时间重排：A 早于 B，A 在前。
      // 注：时间戳必须 > 10^10（毫秒级），否则 push_msg 会将其当秒转毫秒。
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      const baseMs = 1700000000000; // 真实毫秒级时间戳

      // Step1: 发送消息 A（本地时间 T+200，sending 状态）
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'temp_cid-a',
          clientMsgId: 'cid-a',
          createdAt: baseMs + 200,
          senderId: 'me',
          status: 'sending',
          sessionId: 's1',
        ),
      );

      // Step2: 收到对方推送消息 B（服务端返回秒级时间 = T/1000，等价于 T ms）
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'b1',
            'session_id': 's1',
            'sender_id': 'peer',
            'msg_type': 1,
            'content': 'B',
            'created_at': baseMs ~/ 1000, // 秒级，会被转换为 baseMs ms
            'inbox_seq': 2,
          },
        }),
      );

      // 此时 UI：[B(baseMs), A(baseMs+200)]
      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'b1',
        'temp_cid-a',
      ]);

      // Step3: 收到 A 的 ack，服务端时间 = T-100ms（比 B 还早）
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'send_ack',
          'payload': {
            'client_msg_id': 'cid-a',
            'msg_id': 'a1',
            'created_at': (baseMs - 100) ~/ 1000, // 秒级
            'inbox_seq': 1,
          },
        }),
      );

      const ackedCreatedAt = ((baseMs - 100) ~/ 1000) * 1000;
      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'a1',
        'b1',
      ]);
      expect(service.currentMessages.first.status, 'success');
      expect(service.currentMessages.first.createdAt, ackedCreatedAt);
    },
  );

  test(
    'send_ack keeps order consistent with server createdAt across messages',
    () async {
      // 场景：用户连续发了 A 和 B。
      // ACK 后顺序应以服务端 createdAt 为准。
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      const baseMs = 1700000000000;

      // A 先发（本地 T）
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'temp_cid-a2',
          clientMsgId: 'cid-a2',
          createdAt: baseMs,
          senderId: 'me',
          status: 'sending',
          sessionId: 's1',
        ),
      );
      // B 后发（本地 T+500ms）
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'temp_cid-b2',
          clientMsgId: 'cid-b2',
          createdAt: baseMs + 500,
          senderId: 'me',
          status: 'sending',
          sessionId: 's1',
        ),
      );
      // UI: [A(baseMs), B(baseMs+500)]
      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'temp_cid-a2',
        'temp_cid-b2',
      ]);

      // A 的 ack 先到，服务端时间 T-200ms（比本地早 200ms）
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'send_ack',
          'payload': {
            'client_msg_id': 'cid-a2',
            'msg_id': 'a2',
            'created_at': (baseMs - 200) ~/ 1000, // 秒级
            'inbox_seq': 5,
          },
        }),
      );

      // B 的 ack 后到，服务端时间 T+300ms
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'send_ack',
          'payload': {
            'client_msg_id': 'cid-b2',
            'msg_id': 'b2',
            'created_at': (baseMs + 300) ~/ 1000, // 秒级
            'inbox_seq': 6,
          },
        }),
      );

      const aCreatedAt = ((baseMs - 200) ~/ 1000) * 1000;
      const bCreatedAt = ((baseMs + 300) ~/ 1000) * 1000;

      // 顺序由服务端 createdAt 决定（本例仍为 A 在 B 前）。
      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'a2',
        'b2',
      ]);
      expect(service.currentMessages[0].createdAt, aCreatedAt);
      expect(service.currentMessages[1].createdAt, bCreatedAt);
    },
  );

  test(
    'historical stream-like push_msg is not treated as active streaming',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'h1',
            'session_id': 's1',
            'sender_id': 'u2',
            'msg_type': 4,
            'content': 'historical partial',
            'created_at': 2000,
            'inbox_seq': 2,
          },
        }),
      );

      expect(service.currentMessages.length, 1);
      expect(service.currentMessages.first.msgId, 'h1');
      expect(service.currentMessages.first.msgType, 4);
      expect(service.isMessageStreaming('h1'), isFalse);
    },
  );

  test('stream protocol toggles active streaming state by msg_id', () async {
    final service = _makeImService();
    service.setCurrentSessionForTest('s1');

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'stream_chunk',
        'payload': {
          'msg_id': 's1m1',
          'session_id': 's1',
          'sender_id': 'u2',
          'chunk_seq': 2,
          'delta_content': 'B',
        },
      }),
    );
    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'stream_chunk',
        'payload': {
          'msg_id': 's1m1',
          'session_id': 's1',
          'sender_id': 'u2',
          'chunk_seq': 1,
          'delta_content': 'A',
        },
      }),
    );

    expect(service.isMessageStreaming('s1m1'), isTrue);

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'stream_finish',
        'payload': {
          'msg_id': 's1m1',
          'session_id': 's1',
          'sender_id': 'u2',
          'final_content': 'AB done',
          'created_at': 3000,
        },
      }),
    );

    expect(service.isMessageStreaming('s1m1'), isFalse);
    expect(service.currentMessages.first.msgType, 1);
    expect(service.currentMessages.first.content, 'AB done');
  });

  test(
    'missing stream chunk triggers silent background recovery attempts',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'gap-recovery-msg-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'chunk_seq': 2,
            'delta_content': 'B',
          },
        }),
      );

      expect(service.streamGapRecoveryAttemptsForTest('gap-recovery-msg-1'), 0);

      await Future<void>.delayed(const Duration(milliseconds: 2300));
      expect(service.streamGapRecoveryAttemptsForTest('gap-recovery-msg-1'), 1);

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'gap-recovery-msg-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'final_content': 'AB done',
            'created_at': 3000,
          },
        }),
      );
      await Future<void>.delayed(const Duration(milliseconds: 20));

      expect(service.streamGapRecoveryAttemptsForTest('gap-recovery-msg-1'), 0);
    },
  );

  test('non-stream ui update clears stale active streaming state', () async {
    final service = _makeImService();
    service.upsertUIMessageForTest(
      _msg(msgId: 'stale-ui-stream-1', createdAt: 1, msgType: 4),
    );
    service.debugAddStreamingMessageForTest('stale-ui-stream-1');

    service.upsertUIMessageForTest(
      _msg(
        msgId: 'stale-ui-stream-1',
        createdAt: 2,
        msgType: 1,
        content: 'final answer',
      ),
    );

    expect(service.isMessageStreaming('stale-ui-stream-1'), isFalse);
    expect(service.currentMessages.single.msgType, 1);
    expect(service.currentMessages.single.content, 'final answer');
  });

  test(
    'stream_finish falls back to buffered content when final_content is empty',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'fallback-finish',
            'session_id': 's1',
            'sender_id': 'u2',
            'chunk_seq': 1,
            'delta_content': 'streamed answer',
          },
        }),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'fallback-finish',
            'session_id': 's1',
            'sender_id': 'u2',
            'final_content': '',
            'created_at': 3000,
          },
        }),
      );

      expect(service.isMessageStreaming('fallback-finish'), isFalse);
      expect(service.currentMessages, hasLength(1));
      expect(service.currentMessages.first.msgType, 1);
      expect(service.currentMessages.first.content, 'streamed answer');
    },
  );

  test(
    'stream_finish recovers session_id from current stream placeholder when payload misses it',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'finish-no-session-id',
            'session_id': 's1',
            'sender_id': 'u2',
            'chunk_seq': 1,
            'delta_content': 'streamed fallback',
          },
        }),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'finish-no-session-id',
            'sender_id': 'u2',
            'final_content': '',
            'created_at': 3000,
          },
        }),
      );

      expect(service.isMessageStreaming('finish-no-session-id'), isFalse);
      expect(service.currentMessages, hasLength(1));
      final finalized = service.currentMessages.first;
      expect(finalized.msgType, 1);
      expect(finalized.sessionId, 's1');
      expect(finalized.content, 'streamed fallback');
    },
  );

  test(
    'stream_finish keeps stale agent output state without stream_msg_id until terminal status',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-detached-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 3000,
      };
      service.currentMessages.add(
        MessageModel(
          msgId: 'stream-detached-1',
          sessionId: 's1',
          senderId: 'agent-1',
          senderType: 2,
          msgType: 4,
          createdAt: 2000,
        ),
      );
      service.currentMessages.refresh();
      service.debugAddStreamingMessageForTest('stream-detached-1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'stream-detached-1',
            'session_id': 's1',
            'sender_id': 'agent-1',
            'sender_type': 2,
            'final_content': 'detached answer',
            'created_at': 4000,
          },
        }),
      );

      expect(service.agentOutputStateFor('s1'), isNotNull);
      expect(service.agentOutputStateFor('s1')?['run_id'], 'run-detached-1');
    },
  );

  test(
    'agent_output_get_resp removes stale local agent output state when server reports no active run',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.upsertUIMessageForTest(
        _msg(msgId: 'snapshot-stream-1', createdAt: 2000, msgType: 4),
      );
      service.debugAddStreamingMessageForTest('snapshot-stream-1');
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-stale-1',
        'stream_msg_id': 'snapshot-stream-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 3000,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_output_get_resp',
          'payload': {'session_id': 's1', 'active': false, 'resolved_at': 4000},
        }),
      );

      expect(service.isMessageStreaming('snapshot-stream-1'), isFalse);
      expect(service.agentOutputStateFor('s1'), isNull);
    },
  );

  test(
    'terminal agent_output_status clears stale streaming marker when stream finish is missing',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.upsertUIMessageForTest(
        _msg(msgId: 'terminal-stream-1', createdAt: 1000, msgType: 4),
      );
      service.debugAddStreamingMessageForTest('terminal-stream-1');
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-terminal-1',
        'stream_msg_id': 'terminal-stream-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 1000,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_output_status',
          'payload': {
            'session_id': 's1',
            'run_id': 'run-terminal-1',
            'stream_msg_id': 'terminal-stream-1',
            'state': 'completed',
            'can_stop': false,
            'updated_at': 2000,
          },
        }),
      );

      expect(service.isMessageStreaming('terminal-stream-1'), isFalse);
      expect(service.agentOutputStateFor('s1'), isNull);
    },
  );

  test(
    'late stream_chunk is ignored after the current message is already finalized',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'finalized-stream-1',
          createdAt: 1000,
          msgType: 1,
          content: 'final answer',
        ),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'finalized-stream-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'chunk_seq': 99,
            'delta_content': 'stale tail',
          },
        }),
      );

      expect(service.isMessageStreaming('finalized-stream-1'), isFalse);
      expect(service.currentMessages.single.msgType, 1);
      expect(service.currentMessages.single.content, 'final answer');
    },
  );

  test(
    'stream_finish final_content settles missing chunk gaps and ignores later chunks',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'final-authoritative-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 2,
            'chunk_seq': 1,
            'delta_content': 'A',
          },
        }),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'final-authoritative-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 2,
            'final_content': 'ABC',
            'last_chunk_seq': 1,
            'is_finish': true,
            'created_at': 2000,
          },
        }),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'final-authoritative-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 2,
            'chunk_seq': 2,
            'delta_content': 'B',
          },
        }),
      );

      expect(service.isMessageStreaming('final-authoritative-1'), isFalse);
      expect(service.currentMessages.single.msgType, 1);
      expect(service.currentMessages.single.content, 'ABC');
    },
  );

  test(
    'reload clears stale streaming marker when final message already exists in local store',
    () async {
      final service = _makeImService();
      await LocalDb.setActiveUser(_testUserId);
      try {
        service.setCurrentSessionForTest('s1');

        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'stream_chunk',
            'payload': {
              'msg_id': 'reload-finalized-1',
              'session_id': 's1',
              'sender_id': 'u2',
              'sender_type': 2,
              'chunk_seq': 1,
              'delta_content': 'partial',
            },
          }),
        );

        service.leaveSession();

        await LocalDb.upsertMessage({
          'msg_id': 'reload-finalized-1',
          'session_id': 's1',
          'sender_id': 'u2',
          'sender_type': 2,
          'msg_type': 1,
          'content': 'final from db',
          'created_at': 3000,
          'inbox_seq': 9,
        });

        await service.loadInitialWindowForTest('s1');

        expect(service.currentMessages, hasLength(1));
        expect(service.currentMessages.single.msgType, 1);
        expect(service.currentMessages.single.content, 'final from db');
        expect(service.isMessageStreaming('reload-finalized-1'), isFalse);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'agent_output_get_resp keeps active local stream marker while removing stale remote output state',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.primeLocalStreamForTest(
        sessionId: 's1',
        renderMsgId: 'local-stream-keep-1',
      );
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'local-stream-keep-1',
          sessionId: 's1',
          senderId: 'agent-local',
          msgType: 4,
          createdAt: 1000,
        ),
      );
      service.debugAddStreamingMessageForTest('local-stream-keep-1');
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-stale-remote-1',
        'stream_msg_id': 'remote-stream-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 3000,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_output_get_resp',
          'payload': {'session_id': 's1', 'active': false, 'resolved_at': 4000},
        }),
      );

      expect(service.agentOutputStateFor('s1'), isNull);
      expect(service.isMessageStreaming('local-stream-keep-1'), isTrue);
      expect(service.currentMessages.single.msgId, 'local-stream-keep-1');
      expect(service.currentMessages.single.msgType, 4);
    },
  );

  test(
    'agent_output_get_resp keeps newer local state when empty snapshot is older than current state',
    () async {
      final service = _makeImService();
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-newer-local-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 5000,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_output_get_resp',
          'payload': {'session_id': 's1', 'active': false, 'resolved_at': 4000},
        }),
      );

      expect(service.agentOutputStateFor('s1')?['run_id'], 'run-newer-local-1');
    },
  );

  test(
    'stream_finish keeps stopping agent output state for finalized stream until terminal status',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-stop-1',
        'stream_msg_id': 'stream-stop-1',
        'state': 'stopping',
        'can_stop': false,
        'updated_at': 3000,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'stream-stop-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'final_content': 'stopped answer',
            'created_at': 4000,
          },
        }),
      );

      expect(service.agentOutputStateFor('s1'), isNotNull);
      expect(service.agentOutputStateFor('s1')?['state'], 'stopping');
    },
  );

  test(
    'stream_finish keeps newer agent output state when finalized stream does not match',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-newer-1',
        'stream_msg_id': 'stream-newer-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 5000,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'stream-older-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'final_content': 'older answer',
            'created_at': 4000,
          },
        }),
      );

      expect(service.agentOutputStateFor('s1')?['run_id'], 'run-newer-1');
      expect(service.agentOutputStateFor('s1')?['state'], 'streaming');
    },
  );

  test(
    'local stop hides current stream immediately and fences late stream events',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'stream-stop-local-1',
          sessionId: 's1',
          senderId: 'u2',
          msgType: 4,
          createdAt: 1000,
        ),
      );
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-stop-local-1',
        'stream_msg_id': 'stream-stop-local-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 2000,
      };

      service.markAgentOutputStoppingLocallyForTest(
        's1',
        runId: 'run-stop-local-1',
      );

      expect(service.currentMessages, isEmpty);
      expect(service.isMessageStreaming('stream-stop-local-1'), isFalse);
      expect(service.agentOutputStateFor('s1')?['state'], 'stopping');
      expect(service.agentOutputStateFor('s1')?['can_stop'], false);

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'stream-stop-local-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'chunk_seq': 2,
            'delta_content': 'late chunk',
          },
        }),
      );
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'stream-stop-local-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'final_content': 'late final',
            'created_at': 3000,
          },
        }),
      );

      expect(service.currentMessages, isEmpty);
      expect(service.agentOutputStateFor('s1'), isNotNull);
      expect(service.agentOutputStateFor('s1')?['state'], 'stopping');
    },
  );

  test(
    'agent_output_stop_ack rejection restores hidden local stream placeholder',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'stream-stop-reject-1',
          sessionId: 's1',
          senderId: 'u2',
          msgType: 4,
          createdAt: 1000,
        ),
      );
      MessageStreamController.addChunk('stream-stop-reject-1', 'partial');
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-stop-reject-1',
        'stream_msg_id': 'stream-stop-reject-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 2000,
      };

      service.markAgentOutputStoppingLocallyForTest(
        's1',
        runId: 'run-stop-reject-1',
      );

      expect(service.currentMessages, isEmpty);

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_output_stop_ack',
          'payload': {
            'session_id': 's1',
            'run_id': 'run-stop-reject-1',
            'accepted': false,
            'msg': 'stop rejected',
            'updated_at': 3000,
          },
        }),
      );

      expect(service.currentMessages, hasLength(1));
      expect(service.currentMessages.first.msgId, 'stream-stop-reject-1');
      expect(service.currentMessages.first.msgType, 4);
      expect(service.agentOutputStateFor('s1')?['state'], 'streaming');
      expect(service.isMessageStreaming('stream-stop-reject-1'), isTrue);
      expect(
        MessageStreamController.peekContent('stream-stop-reject-1'),
        'partial',
      );
    },
  );

  test(
    'agent_output_stop_ack rejection restores hidden terminal message after finish while keeping stop state',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'stream-stop-reject-finish-1',
          sessionId: 's1',
          senderId: 'u2',
          msgType: 4,
          createdAt: 1000,
        ),
      );
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-stop-reject-finish-1',
        'stream_msg_id': 'stream-stop-reject-finish-1',
        'state': 'streaming',
        'can_stop': true,
        'updated_at': 2000,
      };

      service.markAgentOutputStoppingLocallyForTest(
        's1',
        runId: 'run-stop-reject-finish-1',
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'stream-stop-reject-finish-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'final_content': 'final after reject',
            'created_at': 4000,
          },
        }),
      );

      expect(service.currentMessages, isEmpty);
      expect(service.agentOutputStateFor('s1'), isNotNull);
      expect(service.agentOutputStateFor('s1')?['state'], 'stopping');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_output_stop_ack',
          'payload': {
            'session_id': 's1',
            'run_id': 'run-stop-reject-finish-1',
            'accepted': false,
            'msg': 'stop rejected',
            'updated_at': 5000,
          },
        }),
      );

      expect(service.currentMessages, hasLength(1));
      expect(
        service.currentMessages.first.msgId,
        'stream-stop-reject-finish-1',
      );
      expect(service.currentMessages.first.msgType, 1);
      expect(service.currentMessages.first.content, 'final after reject');
      expect(service.agentOutputStateFor('s1'), isNull);
      expect(
        service.isMessageStreaming('stream-stop-reject-finish-1'),
        isFalse,
      );
    },
  );

  test(
    'push_msg keeps streamed content when push payload content is empty',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'fallback-push',
            'session_id': 's1',
            'sender_id': 'u2',
            'chunk_seq': 1,
            'delta_content': 'streamed via chunk',
          },
        }),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'fallback-push',
            'session_id': 's1',
            'sender_id': 'u2',
            'msg_type': 1,
            'content': '',
            'created_at': 4000,
            'inbox_seq': 7,
          },
        }),
      );

      expect(service.isMessageStreaming('fallback-push'), isFalse);
      expect(service.currentMessages, hasLength(1));
      expect(service.currentMessages.first.msgType, 1);
      expect(service.currentMessages.first.content, 'streamed via chunk');
    },
  );

  test(
    'pull_sync_resp finalizes active stream with buffered content when row content is empty',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'fallback-pull-sync',
            'session_id': 's1',
            'sender_id': 'u2',
            'chunk_seq': 1,
            'delta_content': 'streamed via sync',
          },
        }),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'pull_sync_resp',
          'payload': {
            'has_more': false,
            'messages': [
              {
                'msg_id': 'fallback-pull-sync',
                'session_id': 's1',
                'sender_id': 'u2',
                'msg_type': 1,
                'content': '',
                'created_at': 4000,
                'inbox_seq': 9,
              },
            ],
          },
        }),
      );

      expect(service.isMessageStreaming('fallback-pull-sync'), isFalse);
      expect(service.currentMessages, hasLength(1));
      expect(service.currentMessages.first.msgType, 1);
      expect(service.currentMessages.first.content, 'streamed via sync');
    },
  );

  test(
    'pull_sync_resp does not overwrite finalized UI content with empty duplicate row',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_chunk',
          'payload': {
            'msg_id': 'pull-sync-duplicate',
            'session_id': 's1',
            'sender_id': 'u2',
            'chunk_seq': 1,
            'delta_content': 'final answer',
          },
        }),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'pull-sync-duplicate',
            'session_id': 's1',
            'sender_id': 'u2',
            'final_content': 'final answer',
            'created_at': 3000,
          },
        }),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'pull_sync_resp',
          'payload': {
            'has_more': false,
            'messages': [
              {
                'msg_id': 'pull-sync-duplicate',
                'session_id': 's1',
                'sender_id': 'u2',
                'msg_type': 1,
                'content': '',
                'created_at': 3000,
                'inbox_seq': 10,
              },
            ],
          },
        }),
      );

      expect(service.currentMessages, hasLength(1));
      expect(service.currentMessages.first.msgType, 1);
      expect(service.currentMessages.first.content, 'final answer');
    },
  );

  test(
    'push_msg suppresses OpenClaw tool gate diagnostics from agent content',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'diag1',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 2,
            'msg_type': 1,
            'content':
                'elevated is not available right now (runtime=direct).\n'
                'Failing gates: allowFrom (tools.elevated.allowFrom.grix)\n'
                'Fix-it keys:\n'
                '- tools.elevated.enabled',
            'created_at': 3000,
            'inbox_seq': 3,
          },
        }),
      );

      expect(service.currentMessages, isEmpty);
    },
  );

  test(
    'stream_finish applies server created_at and can reorder messages',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      const baseMs = 1700000000000;

      service.upsertUIMessageForTest(
        _msg(
          msgId: 'old-stream',
          sessionId: 's1',
          msgType: 4,
          createdAt: baseMs,
          content: '',
        ),
      );
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'new-msg',
          sessionId: 's1',
          createdAt: baseMs + 1000,
          content: 'new',
        ),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'old-stream',
            'session_id': 's1',
            'sender_id': 'u2',
            'final_content': 'summary done',
            'created_at': baseMs + 2000,
          },
        }),
      );

      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'new-msg',
        'old-stream',
      ]);
      final finalized = service.currentMessages.firstWhere(
        (m) => m.msgId == 'old-stream',
      );
      expect(finalized.msgType, 1);
      expect(finalized.createdAt, baseMs + 2000);
    },
  );

  test(
    'stream_finish suppresses OpenClaw tool gate diagnostics from final_content',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'diag-finish',
          sessionId: 's1',
          msgType: 4,
          createdAt: 2000,
        ),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': 'diag-finish',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 2,
            'final_content':
                'elevated is not available right now (runtime=direct).\n'
                'Failing gates: allowFrom (tools.elevated.allowFrom.grix)\n'
                'Fix-it keys:\n'
                '- tools.elevated.enabled',
            'created_at': 3000,
          },
        }),
      );

      expect(service.currentMessages, isEmpty);
      expect(service.isMessageStreaming('diag-finish'), isFalse);
    },
  );

  test(
    'stream_error creates local error message for unified delegate path',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_error',
          'payload': {
            'msg_id': 'e1001',
            'session_id': 's1',
            'sender_id': 'u2',
            'error_code': 5003,
            'error_msg': 'delegate failed',
            'created_at': 3000,
          },
        }),
      );

      expect(service.currentMessages.length, 1);
      expect(service.currentMessages.first.msgId, 'e1001');
      expect(service.currentMessages.first.content, 'delegate failed');
      expect(service.currentMessages.first.status, 'error');
    },
  );

  test(
    'stream_error applies server created_at and can reorder messages',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      const baseMs = 1700001000000;

      service.upsertUIMessageForTest(
        _msg(
          msgId: 'old-stream-err',
          sessionId: 's1',
          msgType: 4,
          createdAt: baseMs,
        ),
      );
      service.upsertUIMessageForTest(
        _msg(msgId: 'new-msg-err', sessionId: 's1', createdAt: baseMs + 1000),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_error',
          'payload': {
            'msg_id': 'old-stream-err',
            'session_id': 's1',
            'sender_id': 'u2',
            'error_code': 5003,
            'error_msg': 'fallback',
            'created_at': baseMs + 2000,
          },
        }),
      );

      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'new-msg-err',
        'old-stream-err',
      ]);
      final errored = service.currentMessages.firstWhere(
        (m) => m.msgId == 'old-stream-err',
      );
      expect(errored.msgType, 1);
      expect(errored.status, 'error');
      expect(errored.createdAt, baseMs + 2000);
    },
  );

  test(
    'stream_error suppresses OpenClaw tool gate diagnostics from error_msg',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_error',
          'payload': {
            'msg_id': 'diag-stream',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 2,
            'error_code': 5003,
            'error_msg':
                'elevated is not available right now (runtime=direct).\n'
                'Failing gates: allowFrom (tools.elevated.allowFrom.grix)\n'
                'Fix-it keys:\n'
                '- tools.elevated.enabled',
            'created_at': 3000,
          },
        }),
      );

      expect(service.currentMessages, isEmpty);
    },
  );

  test(
    'stream_error keeps stopping agent output state for finalized stream until terminal status',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-stop-error-1',
        'stream_msg_id': 'stream-error-1',
        'state': 'stopping',
        'can_stop': false,
        'updated_at': 3000,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_error',
          'payload': {
            'msg_id': 'stream-error-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'error_code': 5003,
            'error_msg': 'stopped with error',
            'created_at': 4000,
          },
        }),
      );

      expect(service.agentOutputStateFor('s1'), isNotNull);
      expect(service.agentOutputStateFor('s1')?['state'], 'stopping');
    },
  );

  test(
    'push_revoke clears stopping agent output state for revoked stream message',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'stream-revoke-stop-1',
          sessionId: 's1',
          senderId: 'u2',
          msgType: 4,
          createdAt: 1000,
        ),
      );
      service.agentOutputStates['s1'] = {
        'session_id': 's1',
        'run_id': 'run-revoke-stop-1',
        'stream_msg_id': 'stream-revoke-stop-1',
        'state': 'stopping',
        'can_stop': false,
        'updated_at': 3000,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_revoke',
          'payload': {
            'inbox_seq': '1',
            'msg_id': 'stream-revoke-stop-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'is_revoked': true,
          },
        }),
      );

      expect(service.currentMessages, isEmpty);
      expect(service.agentOutputStateFor('s1'), isNull);
    },
  );

  test(
    'local stream start_ack remaps placeholder msgId and avoids duplicate',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.primeLocalStreamForTest(sessionId: 's1', renderMsgId: 'local_1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'local_1',
          sessionId: 's1',
          senderId: '9001',
          msgType: 4,
          createdAt: 1000,
        ),
      );
      MessageStreamController.addChunk('local_1', 'partial');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'relay_local_stream_start_ack',
          'payload': {'code': 200, 'msg_id': '9001'},
        }),
      );

      expect(service.currentMessages.map((e) => e.msgId).toList(), ['9001']);
      expect(MessageStreamController.getStream('9001').value, 'partial');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': '9001',
            'session_id': 's1',
            'sender_id': '9001',
            'sender_type': 2,
            'msg_type': 1,
            'content': 'final answer',
            'created_at': 2000,
            'inbox_seq': 1,
          },
        }),
      );

      expect(service.currentMessages.length, 1);
      expect(service.currentMessages.first.msgId, '9001');
      expect(service.currentMessages.first.content, 'final answer');
      expect(service.currentMessages.first.msgType, 1);
    },
  );

  test('local stream finalize writes final content back into UI message', () {
    final service = _makeImService();
    service.setCurrentSessionForTest('s1');
    service.upsertUIMessageForTest(
      _msg(
        msgId: '9001',
        sessionId: 's1',
        senderId: '9001',
        msgType: 4,
        content: '',
        createdAt: 1000,
      ),
    );

    service.finalizeLocalStreamRenderMessageForTest(
      sessionId: 's1',
      msgId: '9001',
      agentId: '9001',
      finalContent: 'final answer',
    );

    expect(service.currentMessages.length, 1);
    expect(service.currentMessages.first.msgId, '9001');
    expect(service.currentMessages.first.content, 'final answer');
    expect(service.currentMessages.first.msgType, 1);
    expect(service.currentMessages.first.status, 'success');
  });

  test(
    'local stream finalize preserves quoted message id on current device',
    () {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.primeLocalStreamForTest(
        sessionId: 's1',
        renderMsgId: '9001',
        quotedMessageId: 'trigger-1',
      );

      service.finalizeLocalStreamRenderMessageForTest(
        sessionId: 's1',
        msgId: '9001',
        agentId: '9001',
        finalContent: 'final answer',
      );

      expect(service.currentMessages.length, 1);
      expect(service.currentMessages.first.quotedMessageId, 'trigger-1');
    },
  );

  test(
    'stream_finish keeps quoted message id for local agent replies',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: '9001',
          sessionId: 's1',
          senderId: '9001',
          msgType: 4,
          content: '',
          createdAt: 1000,
        ),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'stream_finish',
          'payload': {
            'msg_id': '9001',
            'session_id': 's1',
            'sender_id': '9001',
            'sender_type': 2,
            'final_content': 'final answer',
            'quoted_message_id': 'trigger-1',
            'last_chunk_seq': 2,
            'is_finish': true,
            'created_at': 2000,
          },
        }),
      );

      expect(service.currentMessages.length, 1);
      expect(service.currentMessages.first.msgType, 1);
      expect(service.currentMessages.first.quotedMessageId, 'trigger-1');
    },
  );

  test(
    'agent_delivery_error marks delegate channel unavailable for owner',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.delegateStates['s1'] = {
        'agent_id': 'a1',
        'active': true,
        'max_consecutive_replies': 3,
        'channel_unavailable': false,
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'agent_delivery_error',
          'payload': {
            'session_id': 's1',
            'owner_id': _testUserId,
            'agent_id': 'a1',
            'trigger_msg_id': 'm1',
            'scope': 'delegate',
            'code': 'agent_api_channel_unavailable',
          },
        }),
      );

      expect(service.lastAgentDeliveryErrorForTest?['session_id'], 's1');
      expect(service.lastAgentDeliveryErrorForTest?['scope'], 'delegate');
      expect(
        service.lastAgentDeliveryErrorForTest?['code'],
        'agent_api_channel_unavailable',
      );
      expect(service.delegateStates['s1']?['channel_unavailable'], true);
    },
  );

  test(
    'delegate-origin push clears delegate channel unavailable flag',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.delegateStates['s1'] = {
        'agent_id': 'a1',
        'active': true,
        'max_consecutive_replies': 3,
        'channel_unavailable': true,
        'last_error_code': 'agent_api_channel_unavailable',
      };

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'inbox_seq': '1',
            'msg_id': 'delegate-reply-1',
            'session_id': 's1',
            'sender_id': '1002',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'delegated reply',
            'extra': {'delegate_origin': true},
            'created_at': 1700002000000,
          },
        }),
      );

      expect(service.delegateStates['s1']?['channel_unavailable'], false);
      expect(service.delegateStates['s1']?['last_error_code'], isNull);
    },
  );

  test(
    'pull_sync_resp incrementally updates current session without clearing UI',
    () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'm0',
          sessionId: 's1',
          createdAt: 1000,
          content: 'existing',
        ),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'pull_sync_resp',
          'payload': {
            'has_more': false,
            'messages': [
              {
                'inbox_seq': 1,
                'msg_id': 'm1',
                'session_id': 's1',
                'sender_id': 'u2',
                'sender_type': 1,
                'msg_type': 1,
                'content': 'synced',
                'created_at': 2000,
              },
            ],
          },
        }),
      );

      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'm0',
        'm1',
      ]);
      expect(service.currentMessages.last.content, 'synced');
    },
  );

  test('pull_sync cursor never rolls back after local deletion-like gap', () {
    final service = _makeImService();

    service.observeInboxSeqForTest(120);
    final afterDeleteCursor = service.resolvePullSyncCursorForTest(0);
    final afterNewLocalData = service.resolvePullSyncCursorForTest(150);

    expect(afterDeleteCursor, 120);
    expect(afterNewLocalData, 150);
  });

  test(
    'initial inbox cursor resets to 0 when local message store is empty but persisted cursor exists',
    () {
      final service = _makeImService();
      final resolved = service.resolveInitialInboxSeqCursorForTest(
        localMaxInboxSeq: 0,
        persistedInboxSeq: 120,
        localMessageCount: 0,
      );
      expect(resolved, 0);
    },
  );

  test(
    'initial inbox cursor keeps persisted cursor when local messages still exist',
    () {
      final service = _makeImService();
      final resolved = service.resolveInitialInboxSeqCursorForTest(
        localMaxInboxSeq: 0,
        persistedInboxSeq: 120,
        localMessageCount: 3,
      );
      expect(resolved, 120);
    },
  );

  test(
    'initial inbox cursor keeps bootstrap floor when local store is empty',
    () {
      final service = _makeImService();
      final resolved = service.resolveInitialInboxSeqCursorForTest(
        localMaxInboxSeq: 0,
        persistedInboxSeq: 120,
        localMessageCount: 0,
        bootstrapInboxSeqFloor: 150,
      );
      expect(resolved, 150);
    },
  );

  test(
    'locally deleted session remains suppressed even if snapshot updatedAt is newer',
    () async {
      final fakeSessionService = _FakeSessionService()
        ..snapshots = const [
          SessionSnapshot(
            sessionId: 's-del-1',
            title: 'Old Chat',
            type: 'private',
            peerId: '2002',
            peerType: 1,
            peerNickname: 'peer',
            peerUsername: 'peer',
            updatedAt: 9999999999999,
            unreadCount: 0,
            lastMessage: '',
          ),
        ];
      Get.put<SessionService>(fakeSessionService);

      final service = _makeImService();
      await service.deleteConversation('s-del-1');

      final prefs = await SharedPreferences.getInstance();
      final key = _deletedSessionsKey();
      var raw = prefs.getString(key) ?? '';
      expect(raw.contains('s-del-1'), isTrue);

      await service.loadSessions();
      await Future<void>.delayed(const Duration(milliseconds: 20));

      raw = prefs.getString(key) ?? '';
      expect(fakeSessionService.snapshotCalls, greaterThanOrEqualTo(1));
      expect(raw.contains('s-del-1'), isTrue);
      expect(service.sessions.where((s) => s.sessionId == 's-del-1'), isEmpty);

      final freshCreatedAt = DateTime.now().millisecondsSinceEpoch + 1000;
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'm-del-1',
            'session_id': 's-del-1',
            'session_type': 1,
            'sender_id': '2002',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'new msg',
            'created_at': freshCreatedAt,
            'inbox_seq': 9,
          },
        }),
      );

      raw = prefs.getString(key) ?? '';
      expect(raw.contains('s-del-1'), isFalse);
    },
  );

  test(
    'session_access_revoked preserves local history and restores after snapshot returns',
    () async {
      final fakeSessionService = _FakeSessionService();
      Get.put<SessionService>(fakeSessionService);

      final service = _makeImService();
      await LocalDb.setActiveUser(_testUserId);
      await LocalDb.upsertSession({
        'session_id': 's-revoke-restore-1',
        'title': 'Banned Group',
        'type': 'group',
        'updated_at': 1000,
        'is_pinned': false,
        'is_muted': false,
        'pinned_at': 0,
        'unread_count': 0,
      });
      await LocalDb.upsertMessage({
        'msg_id': 'm-revoke-restore-1',
        'session_id': 's-revoke-restore-1',
        'sender_id': '2002',
        'sender_type': 1,
        'msg_type': 1,
        'content': 'keep-history',
        'created_at': 2000,
        'inbox_seq': 1,
      });

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_access_revoked',
          'payload': {
            'session_id': 's-revoke-restore-1',
            'reason': 'group_banned',
          },
        }),
      );

      final prefs = await SharedPreferences.getInstance();
      final revokedKey = _revokedSessionsKey();
      expect(
        service.isSessionLocallyRevokedForTest('s-revoke-restore-1'),
        isTrue,
      );
      expect(
        service.getSessionAccessRevokedReason('s-revoke-restore-1'),
        'group_banned',
      );
      expect(
        (prefs.getString(revokedKey) ?? '').contains('s-revoke-restore-1'),
        isTrue,
      );

      final lastMessagesAfterRevoke = await LocalDb.getLastMessages();
      expect(lastMessagesAfterRevoke.containsKey('s-revoke-restore-1'), isTrue);

      await service.loadSessions(refreshFromServer: false);
      expect(
        service.sessions.where((s) => s.sessionId == 's-revoke-restore-1'),
        isEmpty,
      );

      fakeSessionService.snapshots = const [
        SessionSnapshot(
          sessionId: 's-revoke-restore-1',
          title: 'Recovered Group',
          type: 'group',
          peerId: '',
          peerType: 0,
          peerNickname: '',
          peerUsername: '',
          updatedAt: 9999999999999,
          unreadCount: 0,
          lastMessage: 'keep-history',
        ),
      ];

      await service.refreshSessionsNow();
      await Future<void>.delayed(const Duration(milliseconds: 20));

      expect(
        service.isSessionLocallyRevokedForTest('s-revoke-restore-1'),
        isFalse,
      );
      expect(
        service.getSessionAccessRevokedReason('s-revoke-restore-1'),
        isEmpty,
      );
      expect(
        service.sessions.where((s) => s.sessionId == 's-revoke-restore-1'),
        isNotEmpty,
      );
      expect(
        (prefs.getString(revokedKey) ?? '').contains('s-revoke-restore-1'),
        isFalse,
      );
    },
  );

  testWidgets('refreshSessionsNow: 列表摘要跟随本地可见消息，快照预览(time=0)仅在本地无消息时兜底', (
    tester,
  ) async {
    final fakeSessionService = _FakeSessionService();
    Get.put<SessionService>(fakeSessionService);
    final service = _makeImService();

    await tester.runAsync(() async {
      final userId = _nextTestUserId('session_snapshot_preview');
      await LocalDb.setActiveUser(userId);

      try {
        const localMessageTs = 1700000000000;
        const snapshotUpdatedAt = 1700000009000;

        await LocalDb.upsertSession({
          'session_id': 's-preview-refresh',
          'title': 'preview refresh',
          'type': 'group',
          'peer_id': '',
          'peer_type': 0,
          'peer_nickname': '',
          'peer_username': '',
          'updated_at': localMessageTs,
          'unread_count': 0,
          'last_message': 'older local preview',
          'last_message_time': localMessageTs,
        });
        await LocalDb.upsertMessage({
          'msg_id': 'm-preview-refresh-local',
          'session_id': 's-preview-refresh',
          'sender_id': 'u2',
          'msg_type': 1,
          'content': 'older local preview',
          'created_at': localMessageTs,
        });

        fakeSessionService.snapshots = const [
          SessionSnapshot(
            sessionId: 's-preview-refresh',
            title: 'preview refresh',
            type: 'group',
            peerId: '',
            peerType: 0,
            peerNickname: '',
            peerUsername: '',
            updatedAt: snapshotUpdatedAt,
            unreadCount: 3,
            lastMessage: 'server snapshot preview',
          ),
        ];

        await service.refreshSessionsNow();
        await service.loadSessions(refreshFromServer: false);

        expect(service.sessions, hasLength(1));
        expect(service.sessions.single.sessionId, 's-preview-refresh');
        // 快照预览(time=0)未套用聊天历史的 cutoff/visible_to 过滤，可能指向用户
        // 拉不到的消息；只要本地存在可见消息，列表摘要就以本地最新消息为准，保证与
        // 聊天页一致（快照里更新的内容会在其消息同步落库后自愈）。
        expect(service.sessions.single.lastMessage, 'older local preview');
        expect(service.sessions.single.lastMessageTime, 1700000000000);
        expect(service.sessions.single.updatedAt, 1700000009000);
        expect(service.sessions.single.unreadCount, 3);

        await LocalDb.upsertMessage({
          'msg_id': 'm-preview-refresh-newer',
          'session_id': 's-preview-refresh',
          'sender_id': 'u3',
          'msg_type': 1,
          'content': 'newer local message',
          'created_at': 1700000015000,
        });
        await LocalDb.updateSessionLastMsg(
          's-preview-refresh',
          'newer local message',
          1700000015000,
          type: 'group',
        );
        await service.loadSessions(refreshFromServer: false);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    });

    expect(service.sessions, hasLength(1));
    expect(service.sessions.single.sessionId, 's-preview-refresh');
    expect(service.sessions.single.lastMessage, 'newer local message');
    expect(service.sessions.single.lastMessageTime, 1700000015000);
    expect(service.sessions.single.updatedAt, 1700000015000);
    expect(service.sessions.single.unreadCount, 3);
  });

  testWidgets(
    'loadSessions sorts by latest message time when snapshot updatedAt is stale',
    (tester) async {
      late ImService service;

      await tester.runAsync(() async {
        final userId =
            'session_sort_test_${DateTime.now().millisecondsSinceEpoch}';
        await LocalDb.setActiveUser(userId);

        try {
          const staleSessionTs = 1700000000000;
          const newerSessionTs = 1700000005000;
          const latestMessageTs = 1700000010000;

          await LocalDb.upsertSession({
            'session_id': 's-stale',
            'title': 'stale',
            'type': 'group',
            'peer_id': '',
            'peer_type': 0,
            'peer_nickname': '',
            'peer_username': '',
            'updated_at': staleSessionTs,
            'unread_count': 0,
          });
          await LocalDb.upsertSession({
            'session_id': 's-mid',
            'title': 'mid',
            'type': 'group',
            'peer_id': '',
            'peer_type': 0,
            'peer_nickname': '',
            'peer_username': '',
            'updated_at': newerSessionTs,
            'unread_count': 0,
          });

          await LocalDb.upsertMessage({
            'msg_id': 'm-stale-latest',
            'session_id': 's-stale',
            'sender_id': 'u2',
            'msg_type': 1,
            'content': 'newest message',
            'created_at': latestMessageTs,
          });
          await LocalDb.upsertMessage({
            'msg_id': 'm-mid',
            'session_id': 's-mid',
            'sender_id': 'u3',
            'msg_type': 1,
            'content': 'older message',
            'created_at': newerSessionTs,
          });

          service = _makeImService();
          await service.loadSessions(refreshFromServer: false);
        } finally {
          await LocalDb.setActiveUser(null);
        }
      });

      await tester.pump();

      expect(service.sessions.map((e) => e.sessionId).toList(), [
        's-stale',
        's-mid',
      ]);
      expect(service.sessions.first.updatedAt, 1700000010000);
      expect(service.sessions.first.lastMessage, 'newest message');
      expect(service.sessions.first.lastMessageTime, 1700000010000);
    },
  );

  test(
    'pull_sync historical message does not revive locally deleted session',
    () async {
      final service = _makeImService();
      await service.deleteConversation('s-del-hist');

      final prefs = await SharedPreferences.getInstance();
      final key = _deletedSessionsKey();
      var raw = prefs.getString(key) ?? '';
      expect(raw.contains('s-del-hist'), isTrue);

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'pull_sync_resp',
          'payload': {
            'has_more': false,
            'messages': [
              {
                'inbox_seq': 7,
                'msg_id': 'm-del-hist-1',
                'session_id': 's-del-hist',
                'sender_id': '2002',
                'sender_type': 1,
                'msg_type': 1,
                'content': 'historical',
                'created_at': 2000,
              },
            ],
          },
        }),
      );
      await Future<void>.delayed(const Duration(milliseconds: 20));

      raw = prefs.getString(key) ?? '';
      expect(raw.contains('s-del-hist'), isTrue);
      expect(
        service.sessions.where((s) => s.sessionId == 's-del-hist'),
        isEmpty,
      );
    },
  );

  test(
    'auth_ack failed by transient reason should reconnect without logout',
    () async {
      final service = _makeImService();

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'auth_ack',
          'payload': {'code': 10001, 'msg': '鉴权失败'},
        }),
      );

      expect(authService.logoutCalls, 0);
      expect(service.connectionStage, ImConnectionStage.reconnecting);
    },
  );

  test(
    'pong refreshes heartbeat without waiting for downstream queue',
    () async {
      final service = _makeImService();
      final blocker = Completer<void>();
      service.blockDownstreamQueueForTest(blocker.future);

      expect(service.lastPongAtMsForTest, 0);

      service.handleSocketPayloadForTest(
        jsonEncode({'cmd': 'pong', 'payload': {}}),
      );

      expect(service.lastPongAtMsForTest, greaterThan(0));
      blocker.complete();
      await Future<void>.delayed(Duration.zero);
    },
  );

  test('call invite ack bypasses blocked downstream queue', () async {
    final service = _makeImService();
    final callController = Get.put<CallController>(_SpyCallController());
    final blocker = Completer<void>();
    service.blockDownstreamQueueForTest(blocker.future);

    service.handleSocketPayloadForTest(
      jsonEncode({
        'cmd': 'call:invite_ack',
        'payload': {
          'call_id': 'call-ios-safari',
          'room_url': 'wss://livekit.example.test',
          'room_token': 'token',
        },
      }),
    );

    expect(
      (callController as _SpyCallController).inviteAckPayload?['call_id'],
      'call-ios-safari',
    );
    blocker.complete();
    await Future<void>.delayed(Duration.zero);
  });

  test(
    'send ack timeout keeps outbound message pending and only reconnects after repeats',
    () async {
      final userId =
          'ack-timeout-${DateTime.now().millisecondsSinceEpoch.toString()}';
      await LocalDb.setActiveUser(userId);
      try {
        final service = _makeImService();
        service.seedRealtimeStateForTest(
          wsUrl: 'wss://example.test/ws',
          connected: true,
          authenticated: true,
          stage: ImConnectionStage.connected,
        );
        service.currentMessages.assignAll([
          _msg(
            msgId: 'temp-ack-timeout-1',
            clientMsgId: 'cid-ack-timeout-1',
            sessionId: 's-ack-timeout',
            content: 'pending send',
            createdAt: 1000,
            status: 'sending',
          ),
        ]);
        await LocalDb.insertLocalStub({
          'msg_id': 'temp-ack-timeout-1',
          'session_id': 's-ack-timeout',
          'sender_id': _testUserId,
          'sender_type': 1,
          'msg_type': 1,
          'content': 'pending send',
          'status': 'sending',
          'local_seq': 'cid-ack-timeout-1',
          'created_at': 1000,
        });

        await service.handleSendAckTimeoutForTest('cid-ack-timeout-1');

        final local = await LocalDb.getMessageByLocalSeq('cid-ack-timeout-1');
        expect(local?['status'], 'sending');
        expect(service.currentMessages.single.status, 'sending');
        expect(service.connectionStage, ImConnectionStage.connected);
        expect(service.hasReconnectTimerForTest, isFalse);
        expect(service.activeSendAckTimerCountForTest, 1);

        await service.handleSendAckTimeoutForTest('cid-ack-timeout-1');

        expect(service.connectionStage, ImConnectionStage.reconnecting);
        expect(service.hasReconnectTimerForTest, isTrue);
        expect(service.activeSendAckTimerCountForTest, 0);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'disconnect clears send ack timers and pending outbound message can resend',
    () async {
      final userId =
          'ack-disconnect-${DateTime.now().millisecondsSinceEpoch.toString()}';
      await LocalDb.setActiveUser(userId);
      try {
        final service = _makeSpyImService();
        service.seedRealtimeStateForTest(
          wsUrl: 'wss://example.test/ws',
          connected: true,
          authenticated: true,
          stage: ImConnectionStage.connected,
        );
        service.currentMessages.assignAll([
          _msg(
            msgId: 'temp-ack-disconnect-1',
            clientMsgId: 'cid-ack-disconnect-1',
            sessionId: 's-ack-disconnect',
            content: 'pending resend',
            createdAt: 1000,
            status: 'sending',
          ),
        ]);
        await LocalDb.insertLocalStub({
          'msg_id': 'temp-ack-disconnect-1',
          'session_id': 's-ack-disconnect',
          'sender_id': _testUserId,
          'sender_type': 1,
          'msg_type': 1,
          'content': 'pending resend',
          'status': 'sending',
          'local_seq': 'cid-ack-disconnect-1',
          'created_at': 1000,
        });

        service.startSendAckTimerForTest('cid-ack-disconnect-1');
        expect(service.activeSendAckTimerCountForTest, 1);

        service.handleDisconnectForTest(
          finalStage: ImConnectionStage.reconnecting,
        );

        final local = await LocalDb.getMessageByLocalSeq(
          'cid-ack-disconnect-1',
        );
        final pending = await LocalDb.getPendingOutboundMessages();
        expect(service.activeSendAckTimerCountForTest, 0);
        expect(local?['status'], 'sending');
        expect(
          pending.map((row) => row['local_seq']).toList(),
          contains('cid-ack-disconnect-1'),
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test('transient banner suppression hides short reconnect flashes', () {
    // 可注入时钟确定性推进，替代真实 sleep，避免 5s/抑制窗口边界 flaky。
    var fakeNowMs = 1700000000000;
    ImService.nowMsProvider = () => fakeNowMs;
    addTearDown(() {
      ImService.nowMsProvider = () => DateTime.now().millisecondsSinceEpoch;
    });

    final service = _makeImService();

    expect(service.shouldShowConnectionBanner, isFalse);

    fakeNowMs += 5100; // 越过 5s 丢失延迟，横幅显示
    expect(service.shouldShowConnectionBanner, isTrue);

    service.suppressConnectionBannerTemporarily(
      duration: const Duration(milliseconds: 40),
    );
    expect(service.shouldShowConnectionBanner, isFalse);

    fakeNowMs += 70; // 越过 40ms 抑制窗口
    expect(service.shouldShowConnectionBanner, isTrue);

    // 转入 connected 清理挂起定时器
    service.seedRealtimeStateForTest(stage: ImConnectionStage.connected);
  });

  test('reconnecting banner stays hidden until disconnect lasts 5 seconds', () {
    // 可注入时钟确定性推进，替代真实 sleep，避免 5s 边界 flaky。
    var fakeNowMs = 1700000000000;
    ImService.nowMsProvider = () => fakeNowMs;
    addTearDown(() {
      ImService.nowMsProvider = () => DateTime.now().millisecondsSinceEpoch;
    });

    final service = _makeImService();

    service.seedRealtimeStateForTest(stage: ImConnectionStage.reconnecting);
    expect(service.connectionStage, ImConnectionStage.reconnecting);
    expect(service.shouldShowConnectionBanner, isFalse);

    fakeNowMs += 4900;
    expect(service.shouldShowConnectionBanner, isFalse);

    fakeNowMs += 200; // 累计 5.1s，越过 5s 阈值
    expect(service.shouldShowConnectionBanner, isTrue);

    // 转入 connected 清理挂起的丢失横幅定时器
    service.seedRealtimeStateForTest(stage: ImConnectionStage.connected);
  });

  test(
    'initial connect banner stays hidden until connect flow lasts 6 seconds',
    () {
      // 用可注入时钟确定性推进时间，替代真实 sleep——避免在 6s 边界上
      // 因 CI 调度抖动让真实耗时漂过阈值而偶发失败（flaky）。
      var fakeNowMs = 1700000000000;
      ImService.nowMsProvider = () => fakeNowMs;
      addTearDown(() {
        ImService.nowMsProvider = () => DateTime.now().millisecondsSinceEpoch;
      });

      final service = _makeImService();

      service.seedRealtimeStateForTest(
        stage: ImConnectionStage.connecting,
        pendingInitialConnection: true,
      );
      expect(service.shouldShowConnectionBanner, isFalse);

      fakeNowMs += 3000;
      service.seedRealtimeStateForTest(
        stage: ImConnectionStage.authenticating,
        pendingInitialConnection: true,
      );
      expect(service.shouldShowConnectionBanner, isFalse);

      fakeNowMs += 2900; // 累计 5.9s，仍在 6s 延迟窗口内
      expect(service.shouldShowConnectionBanner, isFalse);

      fakeNowMs += 200; // 累计 6.1s，越过 6s 阈值
      expect(service.shouldShowConnectionBanner, isTrue);
      expect(service.connectionBannerTextKey, 'connection_authenticating');

      // 转入 connected 清理挂起的延迟定时器，避免遗留 Timer 警告
      service.seedRealtimeStateForTest(
        stage: ImConnectionStage.connected,
        pendingInitialConnection: false,
      );
    },
  );

  test('session_member_changed bumps per-session event version', () async {
    final service = _makeImService();

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'session_member_changed',
        'payload': {'session_id': 'group-1', 'action': 'add'},
      }),
    );
    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'session_member_changed',
        'payload': {'session_id': 'group-1', 'action': 'remove'},
      }),
    );

    expect(service.getSessionMemberEventVersion('group-1'), 2);
    expect(service.getSessionMemberEventVersion('group-2'), 0);
  });

  test(
    'session_member_changed remove with current user deletes local session',
    () async {
      final service = _makeSpyImService();
      service.sessions.assignAll([
        SessionModel(
          sessionId: 'group-rm-1',
          type: 'group',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_member_changed',
          'payload': {
            'session_id': 'group-rm-1',
            'action': 'remove',
            'removed_user_ids': [_testUserId],
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(service.deleteConversationCalls, 1);
      expect(service.deletedSessionID, 'group-rm-1');
      expect(service.sessions, isEmpty);
    },
  );

  test(
    'session_member_changed remove probes detail when removed list missing',
    () async {
      final fakeSessionService = _FakeSessionService()
        ..detailResult = const SessionDetailResult(
          code: 4003,
          message: 'permission denied',
        );
      Get.put<SessionService>(fakeSessionService);

      final service = _makeSpyImService();
      service.sessions.assignAll([
        SessionModel(
          sessionId: 'group-rm-2',
          type: 'group',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_member_changed',
          'payload': {'session_id': 'group-rm-2', 'action': 'remove'},
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(fakeSessionService.detailCalls, 1);
      expect(service.deleteConversationCalls, 1);
      expect(service.deletedSessionID, 'group-rm-2');
    },
  );

  test(
    'session_member_changed remove with current user deletes even without local session',
    () async {
      final service = _makeSpyImService();

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_member_changed',
          'payload': {
            'session_id': 'group-rm-3',
            'action': 'remove',
            'removed_user_ids': [_testUserId],
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(service.deleteConversationCalls, 1);
      expect(service.deletedSessionID, 'group-rm-3');
    },
  );

  test(
    'session_member_changed remove without local session and removed list skips probe',
    () async {
      final fakeSessionService = _FakeSessionService()
        ..detailResult = const SessionDetailResult(
          code: 4003,
          message: 'permission denied',
        );
      Get.put<SessionService>(fakeSessionService);

      final service = _makeSpyImService();

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_member_changed',
          'payload': {
            'session_id': 'group-rm-4',
            'action': 'remove',
            'removed_user_ids': ['1002'],
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(fakeSessionService.detailCalls, 0);
      expect(service.deleteConversationCalls, 0);
    },
  );

  test(
    'session_member_changed dissolve deletes local session immediately',
    () async {
      final fakeSessionService = _FakeSessionService()
        ..detailResult = const SessionDetailResult(
          code: 50001,
          message: 'should not probe',
        );
      Get.put<SessionService>(fakeSessionService);

      final service = _makeSpyImService();
      service.sessions.assignAll([
        SessionModel(
          sessionId: 'group-dis-1',
          type: 'group',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_member_changed',
          'payload': {
            'session_id': 'group-dis-1',
            'action': 'dissolve',
            'removed_user_ids': [_testUserId, '1002'],
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(service.deleteConversationCalls, 1);
      expect(service.deletedSessionID, 'group-dis-1');
      expect(fakeSessionService.detailCalls, 0);
    },
  );

  test(
    'session_member_changed dissolve ignores event when removed list excludes current user',
    () async {
      final service = _makeSpyImService();
      service.sessions.assignAll([
        SessionModel(
          sessionId: 'group-dis-2',
          type: 'group',
          updatedAt: 1,
          lastMessageTime: 1,
        ),
      ]);

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_member_changed',
          'payload': {
            'session_id': 'group-dis-2',
            'action': 'dissolve',
            'removed_user_ids': ['1002', '1003'],
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(service.deleteConversationCalls, 0);
    },
  );

  test('session_member_changed add appends local system message', () async {
    final service = _makeSpyImService();
    service.setCurrentSessionForTest('group-sys-1');

    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'session_member_changed',
        'payload': {
          'session_id': 'group-sys-1',
          'action': 'add',
          'updated_at': 1735689600,
        },
      }),
    );
    await Future<void>.delayed(Duration.zero);

    expect(service.currentMessages.length, 1);
    final msg = service.currentMessages.first;
    expect(msg.sessionId, 'group-sys-1');
    expect(msg.msgType, 3);
    expect(msg.content, isNotEmpty);
    expect(service.deleteConversationCalls, 0);
  });

  test(
    'session_member_changed add syncs missing session then appends system message',
    () async {
      final localUser =
          'member_add_sync_${DateTime.now().millisecondsSinceEpoch}';
      await LocalDb.setActiveUser(localUser);
      addTearDown(() async {
        await LocalDb.setActiveUser(null);
      });

      final fakeSessionService = _FakeSessionService()
        ..snapshots = const [
          SessionSnapshot(
            sessionId: 'group-sys-new-1',
            title: 'New Group',
            type: 'group',
            peerId: '',
            peerType: 0,
            peerNickname: '',
            peerUsername: '',
            updatedAt: 1735689600000,
            unreadCount: 0,
            lastMessage: '',
          ),
        ];
      Get.put<SessionService>(fakeSessionService);

      final service = _makeSpyImService();

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_member_changed',
          'payload': {
            'session_id': 'group-sys-new-1',
            'action': 'add',
            'updated_at': 1735689600,
          },
        }),
      );
      await Future<void>.delayed(const Duration(milliseconds: 20));

      expect(fakeSessionService.snapshotCalls, 1);
      final idx = service.sessions.indexWhere(
        (s) => s.sessionId == 'group-sys-new-1',
      );
      expect(idx, isNonNegative);
      expect(service.sessions[idx].type, 'group');
      expect(service.sessions[idx].unreadCount, greaterThan(0));

      final latestMessages = await LocalDb.getLatestMessages('group-sys-new-1');
      expect(latestMessages, isNotEmpty);
      expect(latestMessages.last['msg_type'], 3);
    },
  );

  test(
    'push_revoke removes the recalled message from the active chat page',
    () async {
      final service = _makeSpyImService();
      service.setCurrentSessionForTest('revoke-session-1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: '18889990001',
          sessionId: 'revoke-session-1',
          createdAt: 1700000001000,
          content: 'first',
        ),
      );
      service.upsertUIMessageForTest(
        _msg(
          msgId: '18889990002',
          sessionId: 'revoke-session-1',
          createdAt: 1700000002000,
          content: 'second',
        ),
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_revoke',
          'payload': {
            'session_id': 'revoke-session-1',
            'msg_id': '18889990001',
            'sender_id': '9001',
            'is_revoked': true,
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        '18889990002',
      ]);
    },
  );

  test(
    'push_revoke with throttled inbox gap preserves previous cursor for follow-up pull_sync',
    () async {
      final service = _makeSpyImService();
      service.observeInboxSeqForTest(20);
      service.setLastPullSyncRequestMsForTest(
        DateTime.now().millisecondsSinceEpoch,
      );

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_revoke',
          'payload': {
            'inbox_seq': 22,
            'session_id': 'revoke-gap-1',
            'msg_id': '38889990001',
            'sender_id': '9001',
            'is_revoked': true,
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(service.resolvePullSyncCursorForTest(0), 22);
      expect(service.pendingPullSyncCursorFloorForTest, 20);
    },
  );

  test(
    'push_edit updates current message content and session preview',
    () async {
      final service = _makeSpyImService();
      await LocalDb.setActiveUser(_testUserId);
      try {
        service.sessions.assignAll([
          SessionModel(
            sessionId: 'edit-session-1',
            title: 'Edit Session',
            type: 'private',
            updatedAt: 1700000002000,
            unreadCount: 0,
            lastMessage: 'before-edit',
            lastMessageTime: 1700000002000,
          ),
        ]);
        await LocalDb.upsertSession({
          'session_id': 'edit-session-1',
          'title': 'Edit Session',
          'type': 'private',
          'updated_at': 1700000002000,
          'is_pinned': 0,
          'is_muted': 0,
          'pinned_at': 0,
          'unread_count': 0,
        });
        await LocalDb.upsertMessage({
          'msg_id': '58889990001',
          'session_id': 'edit-session-1',
          'sender_id': '9002',
          'sender_type': 2,
          'msg_type': 1,
          'content': 'before-edit',
          'inbox_seq': 40,
          'created_at': 1700000002000,
        });
        service.setCurrentSessionForTest('edit-session-1');
        service.upsertUIMessageForTest(
          _msg(
            msgId: '58889990001',
            sessionId: 'edit-session-1',
            senderId: '9002',
            createdAt: 1700000002000,
            content: 'before-edit',
          ),
        );

        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'push_edit',
            'payload': {
              'inbox_seq': 41,
              'msg_id': '58889990001',
              'session_id': 'edit-session-1',
              'session_type': 1,
              'sender_id': '9002',
              'sender_type': 2,
              'msg_type': 1,
              'content': 'after-edit',
              'sync_event': 'edit',
              'created_at': 1700000002000,
            },
          }),
        );
        await Future<void>.delayed(Duration.zero);

        expect(service.currentMessages.single.content, 'after-edit');
        expect(
          service.sessions
              .firstWhere((s) => s.sessionId == 'edit-session-1')
              .lastMessage,
          'after-edit',
        );
        final latestMessages = await LocalDb.getLatestMessages(
          'edit-session-1',
        );
        expect(latestMessages.single['content'], 'after-edit');
        final dbSessions = await LocalDb.getSessions();
        final sessionRow = dbSessions.firstWhere(
          (s) => s['session_id'] == 'edit-session-1',
        );
        expect(sessionRow['last_message'], 'after-edit');
        expect(sessionRow['last_message_time'], 1700000002000);
        expect(service.resolvePullSyncCursorForTest(0), 41);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'push_edit DB failure preserves cursor and schedules pull_sync retry',
    () async {
      final service = _makeSpyImService();
      await LocalDb.setActiveUser(_testUserId);
      try {
        service.observeInboxSeqForTest(40);
        service.setLastPullSyncRequestMsForTest(
          DateTime.now().millisecondsSinceEpoch,
        );
        service.sessions.assignAll([
          SessionModel(
            sessionId: 'edit-fail-session-1',
            title: 'Edit Fail Session',
            type: 'private',
            updatedAt: 1700000002000,
            unreadCount: 0,
            lastMessage: 'before-edit',
            lastMessageTime: 1700000002000,
          ),
        ]);
        await LocalDb.upsertSession({
          'session_id': 'edit-fail-session-1',
          'title': 'Edit Fail Session',
          'type': 'private',
          'updated_at': 1700000002000,
          'is_pinned': 0,
          'is_muted': 0,
          'pinned_at': 0,
          'unread_count': 0,
        });
        await LocalDb.upsertMessage({
          'msg_id': '68889990001',
          'session_id': 'edit-fail-session-1',
          'sender_id': '9002',
          'sender_type': 2,
          'msg_type': 1,
          'content': 'before-edit',
          'inbox_seq': 40,
          'created_at': 1700000002000,
        });
        service.setCurrentSessionForTest('edit-fail-session-1');
        service.upsertUIMessageForTest(
          _msg(
            msgId: '68889990001',
            sessionId: 'edit-fail-session-1',
            senderId: '9002',
            createdAt: 1700000002000,
            content: 'before-edit',
          ),
        );
        ImService.failDbWriteOpForTest = (op) =>
            op == 'upsertMessage(push_edit)';

        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'push_edit',
            'payload': {
              'inbox_seq': 41,
              'msg_id': '68889990001',
              'session_id': 'edit-fail-session-1',
              'session_type': 1,
              'sender_id': '9002',
              'sender_type': 2,
              'msg_type': 1,
              'content': 'after-edit',
              'sync_event': 'edit',
              'created_at': 1700000002000,
            },
          }),
        );
        await Future<void>.delayed(Duration.zero);

        expect(service.currentMessages.single.content, 'before-edit');
        final latestMessages = await LocalDb.getLatestMessages(
          'edit-fail-session-1',
        );
        expect(latestMessages.single['content'], 'before-edit');
        expect(service.resolvePullSyncCursorForTest(0), 40);
        expect(service.pendingPullSyncCursorFloorForTest, 40);
        expect(service.pendingPersistFailPullSyncForTest, isTrue);
        expect(service.hasPullSyncThrottleTimerForTest, isTrue);

        // A second persist failure in the backoff window must not clear the
        // pending floor or fire an immediate pull; streak stays at 0 until flush.
        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'push_edit',
            'payload': {
              'inbox_seq': 42,
              'msg_id': '68889990001',
              'session_id': 'edit-fail-session-1',
              'session_type': 1,
              'sender_id': '9002',
              'sender_type': 2,
              'msg_type': 1,
              'content': 'after-edit-2',
              'sync_event': 'edit',
              'created_at': 1700000002000,
            },
          }),
        );
        await Future<void>.delayed(Duration.zero);
        expect(service.resolvePullSyncCursorForTest(0), 40);
        expect(service.pendingPullSyncCursorFloorForTest, 40);
        expect(service.persistFailPullSyncStreakForTest, 0);
        expect(service.pendingPersistFailPullSyncForTest, isTrue);
      } finally {
        ImService.failDbWriteOpForTest = null;
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'push_edit older message does not overwrite newer session preview',
    () async {
      final service = _makeSpyImService();
      await LocalDb.setActiveUser(_testUserId);
      try {
        service.sessions.assignAll([
          SessionModel(
            sessionId: 'edit-old-session-1',
            title: 'Edit Old Session',
            type: 'private',
            updatedAt: 1700000003000,
            unreadCount: 0,
            lastMessage: 'newer-message',
            lastMessageTime: 1700000003000,
          ),
        ]);
        await LocalDb.upsertSession({
          'session_id': 'edit-old-session-1',
          'title': 'Edit Old Session',
          'type': 'private',
          'updated_at': 1700000003000,
          'last_message': 'newer-message',
          'last_message_time': 1700000003000,
          'is_pinned': 0,
          'is_muted': 0,
          'pinned_at': 0,
          'unread_count': 0,
        });
        await LocalDb.upsertMessage({
          'msg_id': '78889990001',
          'session_id': 'edit-old-session-1',
          'sender_id': '9002',
          'sender_type': 2,
          'msg_type': 1,
          'content': 'old-message',
          'inbox_seq': 50,
          'created_at': 1700000002000,
        });
        await LocalDb.upsertMessage({
          'msg_id': '78889990002',
          'session_id': 'edit-old-session-1',
          'sender_id': '9002',
          'sender_type': 2,
          'msg_type': 1,
          'content': 'newer-message',
          'inbox_seq': 51,
          'created_at': 1700000003000,
        });

        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'push_edit',
            'payload': {
              'inbox_seq': 52,
              'msg_id': '78889990001',
              'session_id': 'edit-old-session-1',
              'session_type': 1,
              'sender_id': '9002',
              'sender_type': 2,
              'msg_type': 1,
              'content': 'old-message-edited',
              'sync_event': 'edit',
              'created_at': 1700000002000,
            },
          }),
        );
        await Future<void>.delayed(Duration.zero);

        final edited = await LocalDb.getMessageByMsgId('78889990001');
        expect(edited?['content'], 'old-message-edited');
        final dbSessions = await LocalDb.getSessions();
        final sessionRow = dbSessions.firstWhere(
          (s) => s['session_id'] == 'edit-old-session-1',
        );
        expect(sessionRow['last_message'], 'newer-message');
        expect(sessionRow['last_message_time'], 1700000003000);
        expect(
          service.sessions
              .firstWhere((s) => s.sessionId == 'edit-old-session-1')
              .lastMessage,
          'newer-message',
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test('push_edit updates latest preview after tool activity bump', () async {
    final service = _makeSpyImService();
    await LocalDb.setActiveUser(_testUserId);
    try {
      service.sessions.assignAll([
        SessionModel(
          sessionId: 'edit-tool-activity-1',
          title: 'Edit Tool Activity',
          type: 'private',
          updatedAt: 1700000003000,
          unreadCount: 0,
          lastMessage: 'before-edit',
          lastMessageTime: 1700000003000,
        ),
      ]);
      await LocalDb.upsertSession({
        'session_id': 'edit-tool-activity-1',
        'title': 'Edit Tool Activity',
        'type': 'private',
        'updated_at': 1700000003000,
        'last_message': 'before-edit',
        'last_message_time': 1700000003000,
        'is_pinned': 0,
        'is_muted': 0,
        'pinned_at': 0,
        'unread_count': 0,
      });
      await LocalDb.upsertMessage({
        'msg_id': '79889990001',
        'session_id': 'edit-tool-activity-1',
        'sender_id': '9002',
        'sender_type': 2,
        'msg_type': 1,
        'content': 'before-edit',
        'inbox_seq': 55,
        'created_at': 1700000002000,
      });
      await LocalDb.upsertMessage({
        'msg_id': '79889990002',
        'session_id': 'edit-tool-activity-1',
        'sender_id': '9002',
        'sender_type': 2,
        'msg_type': 4,
        'content': '',
        'inbox_seq': 56,
        'created_at': 1700000003000,
      });

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_edit',
          'payload': {
            'inbox_seq': 57,
            'msg_id': '79889990001',
            'session_id': 'edit-tool-activity-1',
            'session_type': 1,
            'sender_id': '9002',
            'sender_type': 2,
            'msg_type': 1,
            'content': 'after-edit',
            'sync_event': 'edit',
            'created_at': 1700000002000,
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      final dbSessions = await LocalDb.getSessions();
      final sessionRow = dbSessions.firstWhere(
        (s) => s['session_id'] == 'edit-tool-activity-1',
      );
      expect(sessionRow['last_message'], 'after-edit');
      expect(sessionRow['last_message_time'], 1700000003000);
      expect(sessionRow['updated_at'], 1700000003000);
      final inMemory = service.sessions.firstWhere(
        (s) => s.sessionId == 'edit-tool-activity-1',
      );
      expect(inMemory.lastMessage, 'after-edit');
      expect(inMemory.lastMessageTime, 1700000003000);
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });

  test('push_revoke DB failure preserves message, UI, and cursor', () async {
    final service = _makeSpyImService();
    await LocalDb.setActiveUser(_testUserId);
    try {
      service.observeInboxSeqForTest(60);
      service.setLastPullSyncRequestMsForTest(
        DateTime.now().millisecondsSinceEpoch,
      );
      await LocalDb.upsertMessage({
        'msg_id': '88889990001',
        'session_id': 'revoke-fail-session-1',
        'sender_id': '9002',
        'sender_type': 1,
        'msg_type': 1,
        'content': 'will-stay',
        'inbox_seq': 60,
        'created_at': 1700000002000,
      });
      service.setCurrentSessionForTest('revoke-fail-session-1');
      service.upsertUIMessageForTest(
        _msg(
          msgId: '88889990001',
          sessionId: 'revoke-fail-session-1',
          senderId: '9002',
          createdAt: 1700000002000,
          content: 'will-stay',
        ),
      );
      ImService.failDbWriteOpForTest = (op) =>
          op == 'deleteMessage(push_revoke)';

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_revoke',
          'payload': {
            'inbox_seq': 61,
            'session_id': 'revoke-fail-session-1',
            'msg_id': '88889990001',
            'sender_id': '9002',
            'is_revoked': true,
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(service.currentMessages.single.msgId, '88889990001');
      expect(await LocalDb.getMessageByMsgId('88889990001'), isNotNull);
      expect(service.resolvePullSyncCursorForTest(0), 60);
      expect(service.pendingPullSyncCursorFloorForTest, 60);
    } finally {
      ImService.failDbWriteOpForTest = null;
      await LocalDb.setActiveUser(null);
    }
  });

  test('revoked push_msg DB failure preserves cursor for retry', () async {
    final service = _makeSpyImService();
    await LocalDb.setActiveUser(_testUserId);
    try {
      service.observeInboxSeqForTest(70);
      service.setLastPullSyncRequestMsForTest(
        DateTime.now().millisecondsSinceEpoch,
      );
      await LocalDb.upsertMessage({
        'msg_id': '98889990001',
        'session_id': 'push-revoke-fail-session-1',
        'sender_id': '9002',
        'sender_type': 1,
        'msg_type': 1,
        'content': 'will-stay',
        'inbox_seq': 70,
        'created_at': 1700000002000,
      });
      ImService.failDbWriteOpForTest = (op) =>
          op == 'deleteMessage(push_msg_revoke)';

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'inbox_seq': 71,
            'session_id': 'push-revoke-fail-session-1',
            'msg_id': '98889990001',
            'sender_id': '9002',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'revoked-payload',
            'is_revoked': true,
            'created_at': 1700000002000,
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(await LocalDb.getMessageByMsgId('98889990001'), isNotNull);
      expect(service.resolvePullSyncCursorForTest(0), 70);
      expect(service.pendingPullSyncCursorFloorForTest, 70);
    } finally {
      ImService.failDbWriteOpForTest = null;
      await LocalDb.setActiveUser(null);
    }
  });

  test(
    'push_revoke applies authoritative session unread count for non-current session',
    () async {
      final service = _makeSpyImService();
      await LocalDb.setActiveUser(_testUserId);
      try {
        service.sessions.assignAll([
          SessionModel(
            sessionId: 'revoke-unread-1',
            title: 'Unread Session',
            type: 'private',
            updatedAt: 1700000002000,
            unreadCount: 3,
            lastMessage: 'will be revoked',
            lastMessageTime: 1700000002000,
          ),
        ]);
        await LocalDb.upsertSession({
          'session_id': 'revoke-unread-1',
          'title': 'Unread Session',
          'type': 'private',
          'peer_id': 'peer-1',
          'peer_type': 1,
          'peer_nickname': 'Peer',
          'peer_username': 'peer',
          'updated_at': 1700000002000,
          'is_pinned': 0,
          'is_muted': 0,
          'pinned_at': 0,
          'unread_count': 3,
        });
        await LocalDb.upsertMessage({
          'msg_id': '48889990001',
          'session_id': 'revoke-unread-1',
          'sender_id': '9002',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'will be revoked',
          'inbox_seq': 30,
          'created_at': 1700000002000,
        });

        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'push_revoke',
            'payload': {
              'inbox_seq': 31,
              'session_id': 'revoke-unread-1',
              'msg_id': '48889990001',
              'sender_id': '9002',
              'is_revoked': true,
              'session_unread_count': 2,
            },
          }),
        );
        await Future<void>.delayed(Duration.zero);

        expect(
          service.sessions
              .firstWhere((s) => s.sessionId == 'revoke-unread-1')
              .unreadCount,
          2,
        );
        final dbSessions = await LocalDb.getSessions();
        expect(
          dbSessions.firstWhere(
            (s) => s['session_id'] == 'revoke-unread-1',
          )['unread_count'],
          2,
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'pull_sync_resp revoked tombstone removes local message and advances cursor',
    () async {
      final service = _makeSpyImService();
      await LocalDb.setActiveUser(_testUserId);
      try {
        await LocalDb.upsertSession({
          'session_id': 'revoke-pull-sync-1',
          'title': 'Revoke Pull',
          'type': 'private',
          'updated_at': 1700000001000,
          'is_pinned': false,
          'is_muted': false,
          'pinned_at': 0,
          'unread_count': 0,
        });
        await LocalDb.upsertMessage({
          'msg_id': '18889990011',
          'session_id': 'revoke-pull-sync-1',
          'sender_id': '9001',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'cached-message',
          'created_at': 1700000001000,
          'inbox_seq': 5,
        });

        service.setCurrentSessionForTest('revoke-pull-sync-1');
        service.upsertUIMessageForTest(
          _msg(
            msgId: '18889990011',
            sessionId: 'revoke-pull-sync-1',
            createdAt: 1700000001000,
            content: 'cached-message',
          ),
        );

        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': false,
              'messages': [
                {
                  'msg_id': '18889990011',
                  'session_id': 'revoke-pull-sync-1',
                  'sender_id': '9001',
                  'sender_type': 1,
                  'msg_type': 1,
                  'content': '',
                  'created_at': 1700000002000,
                  'inbox_seq': 11,
                  'is_revoked': true,
                },
              ],
            },
          }),
        );
        await Future<void>.delayed(const Duration(milliseconds: 20));

        expect(service.currentMessages, isEmpty);
        final latestMessages = await LocalDb.getLatestMessages(
          'revoke-pull-sync-1',
        );
        expect(latestMessages, isEmpty);

        final localMaxInboxSeq = await LocalDb.getMaxInboxSeq();
        expect(
          service.resolvePullSyncCursorForTest(localMaxInboxSeq),
          greaterThanOrEqualTo(11),
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test('pull_sync_resp revoke DB failure preserves cursor for retry', () async {
    final service = _makeSpyImService();
    await LocalDb.setActiveUser(_testUserId);
    try {
      service.observeInboxSeqForTest(80);
      service.setLastPullSyncRequestMsForTest(
        DateTime.now().millisecondsSinceEpoch,
      );
      await LocalDb.upsertMessage({
        'msg_id': '18889990021',
        'session_id': 'revoke-pull-sync-fail-1',
        'sender_id': '9001',
        'sender_type': 1,
        'msg_type': 1,
        'content': 'cached-message',
        'created_at': 1700000001000,
        'inbox_seq': 80,
      });
      ImService.failDbWriteOpForTest = (op) =>
          op == 'deleteMessage(pull_sync_resp_revoke)';

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'pull_sync_resp',
          'payload': {
            'has_more': false,
            'messages': [
              {
                'msg_id': '18889990021',
                'session_id': 'revoke-pull-sync-fail-1',
                'sender_id': '9001',
                'sender_type': 1,
                'msg_type': 1,
                'content': '',
                'created_at': 1700000002000,
                'inbox_seq': 81,
                'is_revoked': true,
              },
            ],
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(await LocalDb.getMessageByMsgId('18889990021'), isNotNull);
      expect(service.resolvePullSyncCursorForTest(0), 80);
      expect(service.hasPullSyncThrottleTimerForTest, isTrue);
    } finally {
      ImService.failDbWriteOpForTest = null;
      await LocalDb.setActiveUser(null);
    }
  });

  test(
    'pull_sync_resp revoke failure retries from pre-batch cursor despite later inserts',
    () async {
      final service = _makeSpyImService();
      await LocalDb.setActiveUser(_testUserId);
      try {
        service.observeInboxSeqForTest(80);
        service.setLastPullSyncRequestMsForTest(
          DateTime.now().millisecondsSinceEpoch,
        );
        await LocalDb.upsertMessage({
          'msg_id': '18889990031',
          'session_id': 'revoke-mixed-fail-1',
          'sender_id': '9001',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'will-stay',
          'created_at': 1700000001000,
          'inbox_seq': 80,
        });
        ImService.failDbWriteOpForTest = (op) =>
            op == 'deleteMessage(pull_sync_resp_revoke)';

        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': false,
              'messages': [
                {
                  'msg_id': '18889990031',
                  'session_id': 'revoke-mixed-fail-1',
                  'sender_id': '9001',
                  'sender_type': 1,
                  'msg_type': 1,
                  'content': '',
                  'created_at': 1700000002000,
                  'inbox_seq': 81,
                  'is_revoked': true,
                },
                {
                  'msg_id': '18889990032',
                  'session_id': 'revoke-mixed-fail-1',
                  'sender_id': '9002',
                  'sender_type': 1,
                  'msg_type': 1,
                  'content': 'later-inserted',
                  'created_at': 1700000003000,
                  'inbox_seq': 82,
                },
              ],
            },
          }),
        );
        await Future<void>.delayed(Duration.zero);

        expect(await LocalDb.getMessageByMsgId('18889990031'), isNotNull);
        expect(await LocalDb.getMessageByMsgId('18889990032'), isNotNull);
        expect(await LocalDb.getMaxInboxSeq(), 82);
        expect(service.pendingPullSyncCursorFloorForTest, 80);
        expect(service.hasPullSyncThrottleTimerForTest, isTrue);
      } finally {
        ImService.failDbWriteOpForTest = null;
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'pull_sync_resp edit event updates old message without replacing latest session preview',
    () async {
      final service = _makeSpyImService();
      await LocalDb.setActiveUser(_testUserId);
      try {
        service.sessions.assignAll([
          SessionModel(
            sessionId: 'edit-pull-sync-1',
            title: 'Pull Edit',
            type: 'private',
            updatedAt: 1700000002000,
            unreadCount: 0,
            lastMessage: 'latest-message',
            lastMessageTime: 1700000002000,
          ),
        ]);
        await LocalDb.upsertSession({
          'session_id': 'edit-pull-sync-1',
          'title': 'Pull Edit',
          'type': 'private',
          'updated_at': 1700000002000,
          'is_pinned': 0,
          'is_muted': 0,
          'pinned_at': 0,
          'unread_count': 0,
        });
        await LocalDb.upsertMessage({
          'msg_id': '68889990001',
          'session_id': 'edit-pull-sync-1',
          'sender_id': '9001',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'old-message',
          'inbox_seq': 10,
          'created_at': 1700000001000,
        });
        await LocalDb.upsertMessage({
          'msg_id': '68889990002',
          'session_id': 'edit-pull-sync-1',
          'sender_id': '9001',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'latest-message',
          'inbox_seq': 11,
          'created_at': 1700000002000,
        });
        service.setCurrentSessionForTest('edit-pull-sync-1');
        service.upsertUIMessageForTest(
          _msg(
            msgId: '68889990001',
            sessionId: 'edit-pull-sync-1',
            createdAt: 1700000001000,
            content: 'old-message',
          ),
        );
        service.upsertUIMessageForTest(
          _msg(
            msgId: '68889990002',
            sessionId: 'edit-pull-sync-1',
            createdAt: 1700000002000,
            content: 'latest-message',
          ),
        );

        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': false,
              'messages': [
                {
                  'inbox_seq': 12,
                  'msg_id': '68889990001',
                  'session_id': 'edit-pull-sync-1',
                  'session_type': 1,
                  'sender_id': '9001',
                  'sender_type': 1,
                  'msg_type': 1,
                  'content': 'old-message-edited',
                  'sync_event': 'edit',
                  'created_at': 1700000001000,
                },
              ],
              'unread_snapshot': <String, int>{},
            },
          }),
        );
        await Future<void>.delayed(const Duration(milliseconds: 20));

        expect(
          service.currentMessages
              .firstWhere((m) => m.msgId == '68889990001')
              .content,
          'old-message-edited',
        );
        expect(
          service.sessions
              .firstWhere((s) => s.sessionId == 'edit-pull-sync-1')
              .lastMessage,
          'latest-message',
        );
        final latestMessages = await LocalDb.getLatestMessages(
          'edit-pull-sync-1',
        );
        expect(
          latestMessages.firstWhere(
            (m) => m['msg_id'] == '68889990001',
          )['content'],
          'old-message-edited',
        );
        final dbSessions = await LocalDb.getSessions();
        final sessionRow = dbSessions.firstWhere(
          (s) => s['session_id'] == 'edit-pull-sync-1',
        );
        expect(sessionRow['last_message'], 'latest-message');
        expect(sessionRow['last_message_time'], 1700000002000);
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'pull_sync_resp revoke event deletes a previously synced local message',
    () async {
      final service = _makeSpyImService();
      await LocalDb.setActiveUser(_testUserId);
      try {
        service.setCurrentSessionForTest('revoke-session-2');
        service.upsertUIMessageForTest(
          _msg(
            msgId: '28889990001',
            sessionId: 'revoke-session-2',
            createdAt: 1700000001000,
            content: 'synced-before',
          ),
        );
        await LocalDb.upsertMessage({
          'msg_id': '28889990001',
          'session_id': 'revoke-session-2',
          'sender_id': '9002',
          'sender_type': 1,
          'msg_type': 1,
          'content': 'synced-before',
          'inbox_seq': 20,
          'created_at': 1700000001000,
        });

        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': false,
              'messages': [
                {
                  'inbox_seq': 21,
                  'msg_id': '28889990001',
                  'session_id': 'revoke-session-2',
                  'session_type': 1,
                  'sender_id': '9002',
                  'sender_type': 1,
                  'msg_type': 1,
                  'content': 'synced-before',
                  'is_revoked': true,
                  'created_at': 1700000002000,
                },
              ],
              'unread_snapshot': <String, int>{},
            },
          }),
        );
        await Future<void>.delayed(Duration.zero);

        expect(
          service.currentMessages.where((m) => m.msgId == '28889990001'),
          isEmpty,
        );
        final latestMessages = await LocalDb.getLatestMessages(
          'revoke-session-2',
        );
        expect(
          latestMessages.where((m) => m['msg_id'] == '28889990001'),
          isEmpty,
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'pull_sync_resp with explicit empty unread snapshot clears local unread counts',
    () async {
      final service = _makeSpyImService();
      await LocalDb.setActiveUser(_testUserId);
      try {
        await LocalDb.upsertSession({
          'session_id': 'unread-reset-1',
          'title': 'Unread Reset',
          'type': 'private',
          'peer_id': 'peer-1',
          'peer_type': 1,
          'peer_nickname': 'Peer',
          'peer_username': 'peer',
          'updated_at': 1700000000000,
          'is_pinned': 0,
          'is_muted': 0,
          'pinned_at': 0,
          'unread_count': 3,
        });
        final beforeSessions = await LocalDb.getSessions();
        expect(
          beforeSessions.firstWhere(
            (s) => s['session_id'] == 'unread-reset-1',
          )['unread_count'],
          3,
        );

        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'pull_sync_resp',
            'payload': {
              'has_more': false,
              'messages': const [],
              'unread_snapshot': <String, int>{},
            },
          }),
        );
        await Future<void>.delayed(Duration.zero);

        final afterSessions = await LocalDb.getSessions();
        expect(
          afterSessions.firstWhere(
            (s) => s['session_id'] == 'unread-reset-1',
          )['unread_count'],
          0,
        );
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );

  test(
    'restoreCurrentSessionRealtimeState bumps current session member version',
    () {
      final service = _makeImService();
      service.setCurrentSessionForTest('restore-session-1');

      service.restoreCurrentSessionRealtimeStateForTest();

      expect(service.getSessionMemberEventVersion('restore-session-1'), 1);
    },
  );

  test(
    'session_member_changed remove for other users appends local system message',
    () async {
      final service = _makeSpyImService();
      service.setCurrentSessionForTest('group-sys-2');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_member_changed',
          'payload': {
            'session_id': 'group-sys-2',
            'action': 'remove',
            'removed_user_ids': ['1002'],
            'updated_at': 1735689600,
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(service.currentMessages.length, 1);
      final msg = service.currentMessages.first;
      expect(msg.sessionId, 'group-sys-2');
      expect(msg.msgType, 3);
      expect(msg.content, isNotEmpty);
      expect(service.deleteConversationCalls, 0);
    },
  );

  test(
    'session_member_changed transfer_owner appends local system message',
    () async {
      final service = _makeSpyImService();
      service.setCurrentSessionForTest('group-sys-3');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'session_member_changed',
          'payload': {
            'session_id': 'group-sys-3',
            'action': 'transfer_owner',
            'updated_at': 1735689600,
          },
        }),
      );
      await Future<void>.delayed(Duration.zero);

      expect(service.currentMessages.length, 1);
      final msg = service.currentMessages.first;
      expect(msg.sessionId, 'group-sys-3');
      expect(msg.msgType, 3);
      expect(msg.content, isNotEmpty);
      expect(service.deleteConversationCalls, 0);
    },
  );

  // ─── LocalDbChangeBus event-driven tests ──────────────────────────────

  group('LocalDbChangeBus event-driven window updates', () {
    test(
      'duplicate reconcile batch does not republish the message window',
      () async {
        final service = _makeImService();
        service.setCurrentSessionForTest('batch-s1');
        final rows = List<Map<String, dynamic>>.generate(100, (index) {
          return {
            'msg_id': 'batch-$index',
            'session_id': 'batch-s1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'message $index',
            'created_at': 1700000000000 + index,
          };
        });
        for (final row in rows) {
          service.upsertUIMessageForTest(MessageModel.fromJson(row));
        }

        var publications = 0;
        final subscription = service.currentMessages.listen((_) {
          publications++;
        });
        try {
          LocalDbChangeBus.instance.emitMessageChange(
            LocalMessagesInserted(
              sessionId: 'batch-s1',
              msgIds: rows.map((row) => row['msg_id']! as String).toList(),
              maxCreatedAt: 1700000000099,
              rows: rows,
            ),
          );

          expect(publications, 0);
          expect(service.currentMessages.length, 100);
        } finally {
          await subscription.cancel();
        }
      },
    );

    test(
      'changed reconcile batch republishes the message window only once',
      () async {
        final service = _makeImService();
        service.setCurrentSessionForTest('batch-s2');
        final existingRows = List<Map<String, dynamic>>.generate(100, (index) {
          return {
            'msg_id': 'batch-$index',
            'session_id': 'batch-s2',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'message $index',
            'created_at': 1700000000000 + index,
          };
        });
        for (final row in existingRows) {
          service.upsertUIMessageForTest(MessageModel.fromJson(row));
        }
        final reconciledRows =
            existingRows.map((row) => Map<String, dynamic>.from(row)).toList()
              ..[50]['content'] = 'updated message 50'
              ..add({
                'msg_id': 'batch-100',
                'session_id': 'batch-s2',
                'sender_id': 'u2',
                'sender_type': 1,
                'msg_type': 1,
                'content': 'message 100',
                'created_at': 1700000000100,
              });

        var publications = 0;
        final subscription = service.currentMessages.listen((_) {
          publications++;
        });
        try {
          LocalDbChangeBus.instance.emitMessageChange(
            LocalMessagesInserted(
              sessionId: 'batch-s2',
              msgIds: reconciledRows
                  .map((row) => row['msg_id']! as String)
                  .toList(),
              maxCreatedAt: 1700000000100,
              rows: reconciledRows,
            ),
          );

          expect(publications, 1);
          expect(service.currentMessages.length, 101);
          expect(
            service.currentMessages
                .firstWhere((message) => message.msgId == 'batch-50')
                .content,
            'updated message 50',
          );
        } finally {
          await subscription.cancel();
        }
      },
    );

    test('reconcile folds canonical msgId state before local client stub', () {
      final service = _makeImService();
      service.setCurrentSessionForTest('batch-dual-key');
      service.upsertUIMessageForTest(
        MessageModel(
          msgId: 'temp-local',
          sessionId: 'batch-dual-key',
          senderId: 'me',
          content: 'local content',
          extra: const {'source': 'local'},
          createdAt: 1700000000000,
          clientMsgId: 'client-1',
          agentDeliveryStatus: 'local-status',
        ),
      );
      service.upsertUIMessageForTest(
        MessageModel(
          msgId: 'server-1',
          sessionId: 'batch-dual-key',
          senderId: 'me',
          content: 'canonical content',
          extra: const {'source': 'canonical'},
          createdAt: 1700000001000,
          agentDeliveryStatus: 'canonical-status',
        ),
      );

      LocalDbChangeBus.instance.emitMessageChange(
        LocalMessagesInserted(
          sessionId: 'batch-dual-key',
          msgIds: const ['server-1'],
          maxCreatedAt: 1700000002000,
          rows: const [
            {
              'msg_id': 'server-1',
              'client_msg_id': 'client-1',
              'session_id': 'batch-dual-key',
              'sender_id': 'me',
              'sender_type': 1,
              'msg_type': 1,
              'content': '',
              'created_at': 1700000002000,
            },
          ],
        ),
      );

      expect(service.currentMessages, hasLength(1));
      final folded = service.currentMessages.single;
      expect(folded.msgId, 'server-1');
      expect(folded.clientMsgId, 'client-1');
      expect(folded.content, 'canonical content');
      expect(folded.extra, {'source': 'canonical'});
      expect(folded.agentDeliveryStatus, 'canonical-status');
    });

    test('reconcile preserves isThinking on an active type-4 placeholder', () {
      final service = _makeImService();
      service.setCurrentSessionForTest('batch-thinking');
      service.upsertUIMessageForTest(
        MessageModel(
          msgId: 'thinking-1',
          sessionId: 'batch-thinking',
          senderId: 'agent',
          senderType: 2,
          msgType: 4,
          content: 'thinking',
          createdAt: 1700000000000,
          isThinking: true,
        ),
      );
      var publications = 0;
      final subscription = service.currentMessages.listen((_) {
        publications++;
      });
      addTearDown(subscription.cancel);

      LocalDbChangeBus.instance.emitMessageChange(
        LocalMessagesInserted(
          sessionId: 'batch-thinking',
          msgIds: const ['thinking-1'],
          maxCreatedAt: 1700000000000,
          rows: const [
            {
              'msg_id': 'thinking-1',
              'session_id': 'batch-thinking',
              'sender_id': 'agent',
              'sender_type': 2,
              'msg_type': 4,
              'content': 'thinking',
              'created_at': 1700000000000,
            },
          ],
        ),
      );

      expect(publications, 0);
      expect(service.currentMessages.single.isThinking, isTrue);
    });

    test('push_msg inserts message via bus event with rows', () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'bus-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'via bus event',
            'created_at': 5000,
            'inbox_seq': 1,
          },
        }),
      );

      expect(service.currentMessages.length, 1);
      expect(service.currentMessages.first.msgId, 'bus-1');
      expect(service.currentMessages.first.content, 'via bus event');
    });

    test('bus subscriber ignores events for non-current session', () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'bus-other',
            'session_id': 's2',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'other session',
            'created_at': 5000,
            'inbox_seq': 1,
          },
        }),
      );

      expect(service.currentMessages.length, 0);
    });

    test('bus subscriber handles streaming placeholder via push_msg', () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'bus-stream-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 2,
            'msg_type': 4,
            'content': 'streaming placeholder',
            'created_at': 5000,
            'inbox_seq': 1,
          },
        }),
      );

      expect(service.currentMessages.length, 1);
      expect(service.currentMessages.first.msgType, 4);
      expect(service.currentMessages.first.msgId, 'bus-stream-1');
    });

    test('multiple push_msg arrive in correct order via bus', () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'bus-3',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'third',
            'created_at': 7000,
            'inbox_seq': 3,
          },
        }),
      );
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'bus-1',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'first',
            'created_at': 5000,
            'inbox_seq': 1,
          },
        }),
      );
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'bus-2',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'second',
            'created_at': 6000,
            'inbox_seq': 2,
          },
        }),
      );

      expect(service.currentMessages.map((e) => e.msgId).toList(), [
        'bus-1',
        'bus-2',
        'bus-3',
      ]);
    });

    test('subscription cancelled on session leave', () async {
      final service = _makeImService();
      service.setCurrentSessionForTest('s1');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'before-leave',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 'before',
            'created_at': 5000,
            'inbox_seq': 1,
          },
        }),
      );
      expect(service.currentMessages.length, 1);

      service.leaveSession();
      // currentMessages should be cleared after leave
      expect(service.currentMessages.length, 0);
    });

    test('session window cache is bounded and restores a lightweight tail', () {
      final service = _makeImService();
      for (var sessionIndex = 1; sessionIndex <= 4; sessionIndex++) {
        final sessionId = 'cache-s$sessionIndex';
        service.setCurrentSessionForTest(sessionId);
        for (var messageIndex = 0; messageIndex < 50; messageIndex++) {
          service.upsertUIMessageForTest(
            MessageModel(
              msgId: '$sessionId-$messageIndex',
              sessionId: sessionId,
              senderId: 'u2',
              content: 'message $messageIndex',
              createdAt: 1700000000000 + messageIndex,
            ),
          );
        }
        service.leaveSession(sessionId);
      }

      expect(service.cachedSessionWindowIdsForTest, [
        'cache-s2',
        'cache-s3',
        'cache-s4',
      ]);

      service.enterSession('cache-s4');
      expect(service.currentMessages.length, 30);
      expect(service.currentMessages.first.msgId, 'cache-s4-20');
      expect(service.currentMessages.last.msgId, 'cache-s4-49');
    });

    test('session window cache expires entries by TTL', () {
      final service = _makeImService();
      ImService.sessionWindowCacheNowMsForTest = 1000000;
      service.setCurrentSessionForTest('cache-expired');
      service.upsertUIMessageForTest(
        _msg(
          msgId: 'cached',
          sessionId: 'cache-expired',
          createdAt: 1700000000000,
        ),
      );
      service.leaveSession('cache-expired');
      expect(service.cachedSessionWindowIdsForTest, ['cache-expired']);

      ImService.sessionWindowCacheNowMsForTest = 1000000 + 300001;
      service.enterSession('cache-expired');

      expect(service.cachedSessionWindowIdsForTest, isEmpty);
      expect(service.currentMessages, isEmpty);
    });

    test(
      'session window cache skips messages with oversized extra payload',
      () {
        final service = _makeImService();
        service.setCurrentSessionForTest('cache-large-extra');
        service.upsertUIMessageForTest(
          MessageModel(
            msgId: 'large-extra',
            sessionId: 'cache-large-extra',
            senderId: 'agent',
            content: 'small content',
            extra: {'payload': List.filled(300000, 'x').join()},
            createdAt: 1700000000000,
          ),
        );

        service.leaveSession('cache-large-extra');

        expect(service.cachedSessionWindowIdsForTest, isEmpty);
      },
    );

    test('setCurrentSessionForTest starts new subscription', () async {
      final service = _makeImService();

      // First session
      service.setCurrentSessionForTest('s1');
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'msg-s1',
            'session_id': 's1',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 's1 message',
            'created_at': 5000,
            'inbox_seq': 1,
          },
        }),
      );
      expect(service.currentMessages.length, 1);

      // Switch to second session (setCurrentSessionForTest does not clear
      // currentMessages — that's leaveSession's job). Events for s2 should
      // now be accepted.
      service.setCurrentSessionForTest('s2');

      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': {
            'msg_id': 'msg-s2',
            'session_id': 's2',
            'sender_id': 'u2',
            'sender_type': 1,
            'msg_type': 1,
            'content': 's2 message',
            'created_at': 6000,
            'inbox_seq': 2,
          },
        }),
      );
      // s2 message should be in the window via bus event.
      final s2Msg = service.currentMessages.where((m) => m.msgId == 'msg-s2');
      expect(s2Msg.length, 1);
      expect(s2Msg.first.content, 's2 message');
    });
  });
}
