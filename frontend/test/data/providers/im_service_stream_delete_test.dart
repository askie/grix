import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/models/session_activity_model.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';
}

class _FakeSessionService extends SessionService {
  @override
  bool get isInitialized => true;

  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) async {
    return const SessionMessageHistoryResult(
      code: 0,
      messages: [],
      hasMore: false,
    );
  }
}

Future<void> _waitUntil(
  bool Function() cond, {
  Duration timeout = const Duration(seconds: 5),
  String? description,
}) async {
  final deadline = DateTime.now().add(timeout);
  while (!cond()) {
    if (DateTime.now().isAfter(deadline)) {
      fail('Timed out waiting for: ${description ?? 'condition'}');
    }
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  final trackedServices = <ImService>[];

  ImService makeService() {
    final service = ImService();
    trackedServices.add(service);
    return service;
  }

  String packet(String cmd, Map<String, dynamic> payload) {
    return jsonEncode(<String, dynamic>{'cmd': cmd, 'payload': payload});
  }

  Future<void> sendChunk(
    ImService service,
    String msgId, {
    String sessionId = 's1',
    int chunkSeq = 1,
    String delta = ' ',
  }) {
    return service.handleDownstreamForTest(
      packet('stream_chunk', <String, dynamic>{
        'msg_id': msgId,
        'session_id': sessionId,
        'sender_id': '2002',
        'sender_type': 2,
        'chunk_seq': chunkSeq,
        'delta_content': delta,
      }),
    );
  }

  Future<void> sendDelete(
    ImService service,
    String msgId, {
    String sessionId = 's1',
  }) {
    return service.handleDownstreamForTest(
      packet('stream_delete', <String, dynamic>{
        'msg_id': msgId,
        'session_id': sessionId,
        'sender_id': '2002',
        'sender_type': 2,
      }),
    );
  }

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    MessageStreamController.resetForTest();
    SharedPreferences.setMockInitialValues(<String, Object>{});
    await LocalDb.setActiveUser('im-stream-delete-test');
  });

  tearDown(() async {
    for (final service in trackedServices.reversed) {
      service.onClose();
    }
    trackedServices.clear();
    await LocalDb.setActiveUser(null);
    MessageStreamController.resetForTest();
    Get.reset();
  });

  test('stream_delete 移除流式占位气泡并清掉流式状态', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');

    // 纯空白 chunk：服务端最终会删掉这条占位，但客户端已经渲染了占位气泡。
    await sendChunk(service, 'm-del');
    expect(service.currentMessages.any((m) => m.msgId == 'm-del'), isTrue);
    expect(service.isMessageStreaming('m-del'), isTrue);

    await sendDelete(service, 'm-del');

    expect(service.currentMessages.any((m) => m.msgId == 'm-del'), isFalse);
    expect(service.isMessageStreaming('m-del'), isFalse);
    expect(MessageStreamController.hasActiveProducer('m-del'), isFalse);

    await sendChunk(service, 'm-del', chunkSeq: 2, delta: ' \n');
    expect(service.currentMessages.any((m) => m.msgId == 'm-del'), isFalse);
    expect(MessageStreamController.hasActiveProducer('m-del'), isFalse);
  });

  test('stream_delete 不影响已 finalize 出正文的消息', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');
    service.sessions.add(
      SessionModel(
        sessionId: 's1',
        peerId: '2002',
        peerType: 2,
        updatedAt: 1000,
        lastMessageTime: 1000,
      ),
    );

    await sendChunk(service, 'm-finish', delta: 'hello');
    await service.handleDownstreamForTest(
      packet('stream_finish', <String, dynamic>{
        'msg_id': 'm-finish',
        'session_id': 's1',
        'sender_id': '2002',
        'sender_type': 2,
        'final_content': 'hello',
        'is_finish': true,
      }),
    );
    expect(
      service.currentMessages.any(
        (m) => m.msgId == 'm-finish' && m.content == 'hello',
      ),
      isTrue,
    );

    await sendDelete(service, 'm-finish');

    expect(service.currentMessages.any((m) => m.msgId == 'm-finish'), isTrue);
  });

  test('stream_delete 对未知 msgId 是 no-op', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');

    await sendDelete(service, 'm-unknown');

    expect(service.currentMessages, isEmpty);
  });

  test('空白 stream_finish 丢弃占位，不落 msgType=1 空消息', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');

    // 流式期只收到纯空白 chunk（stream_delete 丢失的场景）。
    await sendChunk(service, 'm-blank-finish');
    expect(
      service.currentMessages.any((m) => m.msgId == 'm-blank-finish'),
      isTrue,
    );

    await service.handleDownstreamForTest(
      packet('stream_finish', <String, dynamic>{
        'msg_id': 'm-blank-finish',
        'session_id': 's1',
        'sender_id': '2002',
        'sender_type': 2,
        'final_content': ' \n',
        'is_finish': true,
      }),
    );

    // 空占位被移除，且不会落库成 msgType=1 的空消息。
    expect(
      service.currentMessages.any((m) => m.msgId == 'm-blank-finish'),
      isFalse,
    );
    expect(service.isMessageStreaming('m-blank-finish'), isFalse);
    expect(
      MessageStreamController.hasActiveProducer('m-blank-finish'),
      isFalse,
    );
    expect(await LocalDb.getMessageByMsgId('m-blank-finish'), isNull);

    await sendChunk(service, 'm-blank-finish', chunkSeq: 2, delta: ' \n');
    expect(
      service.currentMessages.any((m) => m.msgId == 'm-blank-finish'),
      isFalse,
    );
    expect(
      MessageStreamController.hasActiveProducer('m-blank-finish'),
      isFalse,
    );
  });

  test('stream_finish 终稿空白但缓冲已有正文：保留正文正常封板', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');
    service.sessions.add(
      SessionModel(
        sessionId: 's1',
        peerId: '2002',
        peerType: 2,
        updatedAt: 1000,
        lastMessageTime: 1000,
      ),
    );

    await sendChunk(service, 'm-partial-finish', delta: 'hello');
    await service.handleDownstreamForTest(
      packet('stream_finish', <String, dynamic>{
        'msg_id': 'm-partial-finish',
        'session_id': 's1',
        'sender_id': '2002',
        'sender_type': 2,
        'final_content': ' \n',
        'is_finish': true,
      }),
    );

    // final_content 为空时回退到流式缓冲正文，正常封板，不丢正文。
    expect(
      service.currentMessages.any(
        (m) => m.msgId == 'm-partial-finish' && m.content == 'hello',
      ),
      isTrue,
    );
    expect(service.isMessageStreaming('m-partial-finish'), isFalse);
  });

  test('非当前会话已封板占位遇到空白终稿时保留可恢复正文', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');
    service.sessions.add(
      SessionModel(
        sessionId: 's2',
        peerId: '2002',
        peerType: 2,
        updatedAt: 1000,
        lastMessageTime: 1000,
      ),
    );

    await sendChunk(
      service,
      'm-noncurrent-partial',
      sessionId: 's2',
      delta: 'hello',
    );
    service.agentOutputStates['s2'] = {
      'session_id': 's2',
      'run_id': 'run-noncurrent',
      'stream_msg_id': 'm-noncurrent-partial',
      'agent_id': '2002',
      'state': 'streaming',
      'updated_at': 1000,
    };
    await service.handleDownstreamForTest(
      packet('agent_output_status', <String, dynamic>{
        'session_id': 's2',
        'run_id': 'run-noncurrent',
        'stream_msg_id': 'm-noncurrent-partial',
        'agent_id': '2002',
        'state': 'completed',
        'updated_at': 2000,
      }),
    );
    expect(
      MessageStreamController.hasActiveProducer('m-noncurrent-partial'),
      isFalse,
    );

    await service.handleDownstreamForTest(
      packet('stream_finish', <String, dynamic>{
        'msg_id': 'm-noncurrent-partial',
        'session_id': 's2',
        'sender_id': '2002',
        'sender_type': 2,
        'final_content': ' \n',
        'is_finish': true,
      }),
    );

    final stored = await LocalDb.getMessageByMsgId('m-noncurrent-partial');
    expect(stored?['content'], 'hello');
    expect(stored?['msg_type'], 1);
  });

  test('agent 终态阻止迟到 chunk 复活，但仍接受一次有效终稿', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');
    service.sessions.add(
      SessionModel(
        sessionId: 's1',
        peerId: '2002',
        peerType: 2,
        updatedAt: 1000,
        lastMessageTime: 1000,
      ),
    );

    await sendChunk(service, 'm-terminal-late', delta: ' \n');
    service.agentOutputStates['s1'] = {
      'session_id': 's1',
      'run_id': 'run-terminal-late',
      'stream_msg_id': 'm-terminal-late',
      'agent_id': '2002',
      'state': 'streaming',
      'updated_at': 1000,
    };
    await service.handleDownstreamForTest(
      packet('agent_output_status', <String, dynamic>{
        'session_id': 's1',
        'run_id': 'run-terminal-late',
        'stream_msg_id': 'm-terminal-late',
        'agent_id': '2002',
        'state': 'completed',
        'updated_at': 2000,
      }),
    );

    await sendChunk(service, 'm-terminal-late', chunkSeq: 2, delta: ' \n');
    expect(
      service.currentMessages.any((m) => m.msgId == 'm-terminal-late'),
      isFalse,
    );
    expect(
      MessageStreamController.hasActiveProducer('m-terminal-late'),
      isFalse,
    );

    await service.handleDownstreamForTest(
      packet('stream_finish', <String, dynamic>{
        'msg_id': 'm-terminal-late',
        'session_id': 's1',
        'sender_id': '2002',
        'sender_type': 2,
        'final_content': 'late final',
        'is_finish': true,
      }),
    );
    final matches = service.currentMessages
        .where((m) => m.msgId == 'm-terminal-late')
        .toList();
    expect(matches, hasLength(1));
    expect(matches.single.content, 'late final');

    await sendChunk(service, 'm-new-run', delta: 'new run');
    expect(service.currentMessages.any((m) => m.msgId == 'm-new-run'), isTrue);
  });

  test('untracked 有效 stream_finish 清理同消息 composing 状态', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');
    service.sessions.add(
      SessionModel(
        sessionId: 's1',
        peerId: '2002',
        peerType: 2,
        updatedAt: 1000,
        lastMessageTime: 1000,
      ),
    );
    service.sessionActivities['s1'] = <SessionActivityModel>[
      SessionActivityModel(
        sessionId: 's1',
        kind: 'composing',
        active: true,
        actorId: '2002',
        actorType: 'agent',
        executorId: '2002',
        executorType: 'agent',
        source: 'agent_api',
        refMsgId: 'm-untracked-final',
        refEventId: 'event-untracked-final',
        statusText: '',
        updatedAt: 1000,
        expiresAt: DateTime.now().millisecondsSinceEpoch + 30000,
      ),
    ];

    await service.handleDownstreamForTest(
      packet('stream_finish', <String, dynamic>{
        'msg_id': 'm-untracked-final',
        'session_id': 's1',
        'sender_id': '2002',
        'sender_type': 2,
        'final_content': 'done',
        'created_at': 2000,
        'is_finish': true,
      }),
    );

    expect(service.sessionActivities['s1'], isNull);
  });

  test('重新进入会话不复活已非活跃的空占位', () async {
    Get.put<AuthService>(_FakeAuthService());
    Get.put<SessionService>(_FakeSessionService());
    final service = makeService();
    Get.put<ImService>(service);

    service.enterSession('s1');
    await _waitUntil(
      () => service.currentSessionId == 's1',
      description: 'entered s1',
    );

    // 纯空白 chunk 产生占位；随后模拟并发事件只清掉活跃标记（占位残留）。
    await sendChunk(service, 'm-ghost');
    expect(service.currentMessages.any((m) => m.msgId == 'm-ghost'), isTrue);
    service.debugRemoveStreamingMessageForTest('m-ghost');

    // 切走再切回：初始窗口重建时不得复活非活跃的空占位。
    service.enterSession('other-session');
    await _waitUntil(
      () => service.currentSessionId == 'other-session',
      description: 'entered other-session',
    );
    service.enterSession('s1');
    await _waitUntil(
      () => service.isInitialHistoryReady,
      description: 's1 initial window reloaded',
    );

    expect(service.currentMessages.any((m) => m.msgId == 'm-ghost'), isFalse);
  });

  test('重新进入会话仍保留已非活跃但有正文的占位', () async {
    Get.put<AuthService>(_FakeAuthService());
    Get.put<SessionService>(_FakeSessionService());
    final service = makeService();
    Get.put<ImService>(service);

    service.enterSession('s1');
    await _waitUntil(
      () => service.currentSessionId == 's1',
      description: 'entered s1',
    );

    await sendChunk(service, 'm-partial', delta: 'hello');
    expect(service.currentMessages.any((m) => m.msgId == 'm-partial'), isTrue);
    final backdated =
        DateTime.now().millisecondsSinceEpoch -
        const Duration(minutes: 6).inMilliseconds;
    service.debugSetStreamingActivityAtForTest('m-partial', backdated);
    service.sweepStaleStreamingMessagesForTest();

    service.enterSession('other-session');
    await _waitUntil(
      () => service.currentSessionId == 'other-session',
      description: 'entered other-session',
    );
    service.enterSession('s1');
    await _waitUntil(
      () => service.isInitialHistoryReady,
      description: 's1 initial window reloaded',
    );

    // 有可恢复正文的占位必须保留，避免丢正文。
    final restored = service.currentMessages.singleWhere(
      (m) => m.msgId == 'm-partial',
    );
    expect(restored.content, 'hello');
    expect(MessageStreamController.hasActiveProducer('m-partial'), isFalse);
  });

  test('空占位被看门狗清除后，迟到的有效 stream_finish 重新插入且只一次', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');
    service.sessions.add(
      SessionModel(
        sessionId: 's1',
        peerId: '2002',
        peerType: 2,
        updatedAt: 1000,
        lastMessageTime: 1000,
      ),
    );

    await sendChunk(service, 'm-late');
    expect(service.currentMessages.any((m) => m.msgId == 'm-late'), isTrue);

    // 看门狗按空占位清掉气泡（模拟 stream_finish/stream_delete 一度丢失）。
    final backdated =
        DateTime.now().millisecondsSinceEpoch -
        const Duration(minutes: 6).inMilliseconds;
    service.debugSetStreamingActivityAtForTest('m-late', backdated);
    service.sweepStaleStreamingMessagesForTest();
    expect(service.currentMessages.any((m) => m.msgId == 'm-late'), isFalse);

    // 迟到的有效终稿进入 untracked 路径：仍要重新插入当前会话。
    await service.handleDownstreamForTest(
      packet('stream_finish', <String, dynamic>{
        'msg_id': 'm-late',
        'session_id': 's1',
        'sender_id': '2002',
        'sender_type': 2,
        'final_content': 'late final',
        'is_finish': true,
      }),
    );

    final matches = service.currentMessages
        .where((m) => m.msgId == 'm-late')
        .toList();
    expect(matches.length, 1);
    expect(matches.single.content, 'late final');
    expect(service.isMessageStreaming('m-late'), isFalse);
  });
}
