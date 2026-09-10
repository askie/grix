import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/data/models/local_search_result.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/account_info/controllers/account_info_controller.dart';

class _FakeImService extends ImService {
  @override
  bool get isConnected => true;
}

SessionModel _session(
  String id, {
  required String title,
  String peerId = 'peer-1',
  int peerType = 1,
  int updatedAt = 1000,
  String lastMessage = '',
}) {
  return SessionModel(
    sessionId: id,
    title: title,
    type: 'private',
    peerId: peerId,
    peerType: peerType,
    updatedAt: updatedAt,
    lastMessage: lastMessage,
    lastMessageTime: updatedAt,
  );
}

/// 搜索去抖 200ms，留出余量让派发落地。
Future<void> _settle() =>
    Future<void>.delayed(const Duration(milliseconds: 320));

/// 短暂等待，让某一段搜索结果的 Completer 落地生效。
Future<void> _tick() => Future<void>.delayed(const Duration(milliseconds: 20));

void main() {
  late _FakeImService imService;
  late AccountInfoController controller;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    imService = _FakeImService();
    controller = AccountInfoController(
      initialArguments: const {'peer_id': 'peer-1', 'peer_type': '1'},
      imService: imService,
    );
    controller.onInit();
  });

  tearDown(() {
    controller.onClose();
    Get.reset();
  });

  test('消息命中的会话进入结果并带命中摘要', () async {
    final hitSession = _session(
      's-hit',
      title: '未命中标题的会话',
      lastMessage: '别的内容',
    );
    controller.searchSessionRecordsOverrideForTest = (_, {scope}) async => [];
    controller.searchMessagesOverrideForTest = (_, {scope}) async => [
      const MatchedMessage(
        msgId: 'm-1',
        sessionId: 's-hit',
        content: '装修报价单发你了',
        createdAt: 1000,
      ),
    ];
    controller.resolveSessionForIdOverrideForTest = (sessionId) async {
      expect(sessionId, 's-hit');
      return hitSession;
    };

    controller.searchQuery.value = '装修';
    await _settle();

    final results = controller.conversationSessions;
    expect(results.map((s) => s.sessionId).toList(), ['s-hit']);
    expect(controller.sessionThreadPreview(hitSession), '装修报价单发你了');
  });

  test('会话段先于消息段落地：会话立即返回、消息段延迟返回后再并入', () async {
    final sessionA = _session('s-a', title: '装修群A');
    final messageHitSession = _session(
      's-b',
      title: '不相关标题',
      lastMessage: '',
    );
    controller.searchSessionRecordsOverrideForTest = (_, {scope}) async => [
      sessionA.toJson(),
    ];
    final messagesGate = Completer<List<MatchedMessage>>();
    controller.searchMessagesOverrideForTest = (_, {scope}) =>
        messagesGate.future;
    controller.resolveSessionForIdOverrideForTest = (sessionId) async {
      expect(sessionId, 's-b');
      return messageHitSession;
    };

    controller.searchQuery.value = '装修';
    await _settle();

    expect(
      controller.conversationSessions.map((s) => s.sessionId).toList(),
      ['s-a'],
      reason: '会话段应该已经先落地',
    );
    expect(controller.searchInFlight.value, isTrue, reason: '消息段还没回来');

    messagesGate.complete([
      const MatchedMessage(
        msgId: 'm-b',
        sessionId: 's-b',
        content: '装修进度',
        createdAt: 2000,
      ),
    ]);
    await _tick();

    expect(
      controller.conversationSessions.map((s) => s.sessionId).toSet(),
      {'s-a', 's-b'},
      reason: '消息段落地后应该并入结果，不覆盖已经展示的会话段结果',
    );
    expect(controller.searchInFlight.value, isFalse);
  });

  test(
    'in-flight 在派发时为 true，两段都落地后为 false；清空关键词不等去抖立即收起并清空结果',
    () async {
      final sessionsGate = Completer<List<Map<String, dynamic>>>();
      final messagesGate = Completer<List<MatchedMessage>>();
      controller.searchSessionRecordsOverrideForTest = (_, {scope}) =>
          sessionsGate.future;
      controller.searchMessagesOverrideForTest = (_, {scope}) =>
          messagesGate.future;

      expect(controller.searchInFlight.value, isFalse);

      controller.searchQuery.value = '装修';
      await _settle();
      expect(controller.searchInFlight.value, isTrue, reason: '派发搜索后应立即进入 in-flight');

      sessionsGate.complete(const []);
      await _tick();
      expect(
        controller.searchInFlight.value,
        isTrue,
        reason: '消息段还没回来，不能提前收起 in-flight',
      );

      messagesGate.complete(const []);
      await _tick();
      expect(controller.searchInFlight.value, isFalse, reason: '两段都落地后应该收起 in-flight');

      // 派发一版还没有落地的新搜索，验证清空不用等它、也不用等 200ms 去抖。
      final sessionsGate2 = Completer<List<Map<String, dynamic>>>();
      final messagesGate2 = Completer<List<MatchedMessage>>();
      controller.searchSessionRecordsOverrideForTest = (_, {scope}) =>
          sessionsGate2.future;
      controller.searchMessagesOverrideForTest = (_, {scope}) =>
          messagesGate2.future;
      controller.searchQuery.value = '装修工';
      await _settle();
      expect(controller.searchInFlight.value, isTrue);

      controller.searchQuery.value = '';
      await _tick();
      expect(
        controller.searchInFlight.value,
        isFalse,
        reason: '清空关键词应立即收起 in-flight，不等 200ms 去抖也不等上一版查询落地',
      );
      expect(controller.conversationSessions, isEmpty);

      // 上一版查询这时才姗姗来迟，不该把已经清空的状态又改回去。
      sessionsGate2.complete([_session('s-late', title: '迟到会话').toJson()]);
      messagesGate2.complete(const []);
      await _tick();
      expect(controller.searchInFlight.value, isFalse);
      expect(controller.conversationSessions, isEmpty);
    },
  );

  test('过期版本的搜索结果不会覆盖最新版本已经展示的结果', () async {
    final sessionsGate1 = Completer<List<Map<String, dynamic>>>();
    final messagesGate1 = Completer<List<MatchedMessage>>();
    controller.searchSessionRecordsOverrideForTest = (_, {scope}) =>
        sessionsGate1.future;
    controller.searchMessagesOverrideForTest = (_, {scope}) =>
        messagesGate1.future;

    controller.searchQuery.value = '装修';
    await _settle();

    // 改词派发 v2，换新的 gate；v1 还挂着没完成。
    final sessionV2 = _session('s-v2', title: '装修工队');
    controller.searchSessionRecordsOverrideForTest = (_, {scope}) async => [
      sessionV2.toJson(),
    ];
    controller.searchMessagesOverrideForTest = (_, {scope}) async =>
        const <MatchedMessage>[];
    controller.searchQuery.value = '装修工';
    await _settle();

    expect(
      controller.conversationSessions.map((s) => s.sessionId).toList(),
      ['s-v2'],
      reason: 'v2 应该已经落地',
    );

    // v1 这时才姗姗来迟：不该覆盖 v2 已经展示的结果。
    sessionsGate1.complete([_session('s-v1', title: '过期结果').toJson()]);
    messagesGate1.complete(const []);
    await _tick();

    expect(
      controller.conversationSessions.map((s) => s.sessionId).toList(),
      ['s-v2'],
      reason: '过期版本(v1)的迟到完成不该覆盖当前版本(v2)的结果',
    );
  });

  test('打下第一个字立即进入 in-flight，不等 200ms 去抖就不会被判成"无匹配"', () async {
    final sessionsGate = Completer<List<Map<String, dynamic>>>();
    final messagesGate = Completer<List<MatchedMessage>>();
    controller.searchSessionRecordsOverrideForTest = (_, {scope}) =>
        sessionsGate.future;
    controller.searchMessagesOverrideForTest = (_, {scope}) =>
        messagesGate.future;

    expect(controller.searchInFlight.value, isFalse);

    // 不 await _settle：只让赋值同步执行完，去抖回调根本还没到点，
    // 验证的正是去抖触发前这一刻的状态——此时 _dbSearchResults 必然还是
    // 空，只有 searchInFlight 同步跟上才不会被 `_HistoryEmptyCard` 误判。
    controller.searchQuery.value = '装';

    expect(
      controller.searchInFlight.value,
      isTrue,
      reason: '打下第一个字应立即进入 in-flight，不用等 200ms 去抖',
    );
    expect(
      controller.conversationSessions,
      isEmpty,
      reason: '结果还没回来，conversationSessions 应该仍是空',
    );

    sessionsGate.complete(const []);
    messagesGate.complete(const []);
    await _tick();
  });
}
