import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';
}

class _PagedHistorySessionService extends SessionService {
  _PagedHistorySessionService(this.messages);

  final List<Map<String, dynamic>> messages;
  final requests = <String>[];
  Completer<SessionMessageHistoryResult>? historyCompleter;

  @override
  bool get isInitialized => true;

  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) async {
    final before = beforeMsgId?.trim() ?? '';
    requests.add('${before.isEmpty ? 'latest' : before}:$limit');
    final completer = historyCompleter;
    if (completer != null) return completer.future;
    final eligible = messages.where((message) {
      return before.isEmpty ||
          before == '0' ||
          int.parse(message['msg_id'] as String) < int.parse(before);
    }).toList()
      ..sort(
        (left, right) => int.parse(
          right['msg_id'] as String,
        ).compareTo(int.parse(left['msg_id'] as String)),
      );
    final page = eligible.take(limit).toList(growable: false);
    return SessionMessageHistoryResult(
      messages: page,
      hasMore: eligible.length > page.length,
      nextBeforeMsgId: page.isEmpty ? '' : page.last['msg_id'].toString(),
    );
  }
}

Map<String, dynamic> _messageRow({
  required String sessionId,
  required int id,
  required int senderType,
  required String content,
}) {
  return {
    'msg_id': '$id',
    'session_id': sessionId,
    'sender_id': senderType == 1 ? 'user-1' : 'agent-1',
    'sender_type': senderType,
    'msg_type': 1,
    'content': content,
    'created_at': 1700000000000 + id * 1000,
    'status': 'sent',
    'state_version': '1',
  };
}

List<Map<String, dynamic>> _history(String sessionId, int count) {
  return List.generate(count, (index) {
    final id = index + 1;
    final senderType = id == 1 || id % 2 == 1 ? 1 : 2;
    return _messageRow(
      sessionId: sessionId,
      id: id,
      senderType: senderType,
      content: id == 1 ? 'user original' : 'message $id',
    );
  });
}

Future<void> _waitForMessageCount(ImService service, int count) async {
  final deadline = DateTime.now().add(const Duration(seconds: 3));
  while (service.currentMessages.length != count &&
      DateTime.now().isBefore(deadline)) {
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  expect(service.currentMessages.length, count);
}

Future<String?> _readPendingReadBoundary(String sessionId) async {
  final prefs = await SharedPreferences.getInstance();
  final raw = prefs.getString('pending_read_states_1001');
  if (raw == null || raw.trim().isEmpty) return null;
  final decoded = jsonDecode(raw);
  if (decoded is! Map) return null;
  return decoded[sessionId]?.toString();
}

Future<void> _waitForPendingReadBoundary(
  String sessionId,
  String expected,
) async {
  final deadline = DateTime.now().add(const Duration(seconds: 3));
  while (await _readPendingReadBoundary(sessionId) != expected &&
      DateTime.now().isBefore(deadline)) {
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  expect(await _readPendingReadBoundary(sessionId), expected);
}

Future<void> _waitForOlderState(ImService service, bool expected) async {
  final deadline = DateTime.now().add(const Duration(seconds: 3));
  while (service.hasOlderMessages != expected &&
      DateTime.now().isBefore(deadline)) {
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  expect(service.hasOlderMessages, expected);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late ImService imService;

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    Get.put<AuthService>(_FakeAuthService());
    await LocalDb.setActiveUser(
      'initial_history_${DateTime.now().microsecondsSinceEpoch}',
    );
    imService = ImService();
    Get.put<ImService>(imService);
  });

  tearDown(() async {
    imService.onClose();
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  test(
    'underfilled short tail is reconciled in order without repeat fetches',
    () async {
      const sessionId = 'short-session';
      final rows = _history(sessionId, 3);
      final historyService = _PagedHistorySessionService(rows);
      Get.put<SessionService>(historyService);
      imService.setActiveSyncModeForTest('v2');

      await LocalDb.upsertSession({
        'session_id': sessionId,
        'title': 'Short session',
        'type': 'group',
        'unread_count': 0,
        'last_message': 'message 3',
        'last_message_time': rows.last['created_at'],
      });
      await LocalDb.upsertMessage(rows.last);

      await imService.loadInitialWindowForTest(sessionId);

      expect(
        imService.currentMessages.map((message) => message.msgId).toList(),
        ['1', '2', '3'],
      );
      expect(imService.currentMessages.first.content, 'user original');
      expect(imService.currentMessages.last.senderType, 1);
      expect(imService.hasOlderMessages, isFalse);
      expect(historyService.requests, ['latest:30']);

      await imService.forceReloadSessionWindow(
        sessionId,
        triggerPullSync: false,
      );
      expect(
        imService.currentMessages.map((message) => message.msgId).toList(),
        ['1', '2', '3'],
      );
      expect(historyService.requests, ['latest:30', 'latest:40']);

      await imService.loadOlderForCurrentSession();
      imService.leaveSession(sessionId);
      await imService.loadInitialWindowForTest(sessionId);

      expect(
        imService.currentMessages.map((message) => message.msgId).toList(),
        ['1', '2', '3'],
      );
      expect(historyService.requests, ['latest:30', 'latest:40']);
    },
  );

  test('local snapshot renders before first-page archive completes', () async {
    const sessionId = 'async-short-session';
    final rows = _history(sessionId, 3);
    final historyService = _PagedHistorySessionService(rows)
      ..historyCompleter = Completer<SessionMessageHistoryResult>();
    Get.put<SessionService>(historyService);
    await LocalDb.upsertMessage(rows.last);

    await imService.loadInitialWindowForTest(
      sessionId,
      waitForBackfill: false,
    );

    expect(
      imService.currentMessages.map((message) => message.msgId).toList(),
      ['3'],
    );
    expect(historyService.requests, ['latest:30']);

    historyService.historyCompleter!.complete(
      SessionMessageHistoryResult(messages: rows),
    );
    await _waitForMessageCount(imService, 3);
    expect(
      imService.currentMessages.map((message) => message.msgId).toList(),
      ['1', '2', '3'],
    );
  });

  test(
    'archive repair advances read boundary only through messages in the window',
    () async {
      const sessionId = 'read-boundary-backfill-session';
      final rows = _history(sessionId, 3);
      final historyService = _PagedHistorySessionService(rows)
        ..historyCompleter = Completer<SessionMessageHistoryResult>();
      Get.put<SessionService>(historyService);
      await LocalDb.upsertMessage(rows.first);

      await imService.loadInitialWindowForTest(
        sessionId,
        waitForBackfill: false,
      );
      await _waitForPendingReadBoundary(sessionId, '1');

      // This message is in local storage but is not in the current window.
      // The read receipt must follow the messages actually merged into UI.
      await LocalDb.applyArchiveMessages([
        _messageRow(
          sessionId: sessionId,
          id: 4,
          senderType: 2,
          content: 'newer unseen message',
        ),
      ]);
      historyService.historyCompleter!.complete(
        SessionMessageHistoryResult(messages: rows),
      );

      await _waitForMessageCount(imService, 3);
      await _waitForPendingReadBoundary(sessionId, '3');
      expect(
        imService.currentMessages.map((message) => message.msgId).toList(),
        ['1', '2', '3'],
      );
      expect(
        (await LocalDb.getLatestMessages(sessionId, limit: 10)).map(
          (message) => message['msg_id'],
        ),
        contains('4'),
      );
      expect(historyService.requests, ['latest:30']);

      imService.leaveSession(sessionId);
      await imService.loadInitialWindowForTest(sessionId);
      await _waitForMessageCount(imService, 4);
      await _waitForPendingReadBoundary(sessionId, '4');
      expect(historyService.requests, ['latest:30']);
      expect(
        imService.currentMessages.map((message) => message.msgId).toSet(),
        hasLength(4),
      );
    },
  );

  test(
    'long partial tail fills the latest page then pages older rows once',
    () async {
      const sessionId = 'long-partial-session';
      final rows = _history(sessionId, 100);
      final historyService = _PagedHistorySessionService(rows);
      Get.put<SessionService>(historyService);
      await LocalDb.upsertMessage(rows.last);

      await imService.loadInitialWindowForTest(sessionId);

      expect(
        imService.currentMessages.map((message) => message.msgId).toList(),
        List.generate(30, (index) => '${index + 71}'),
      );
      expect(imService.hasOlderMessages, isTrue);
      expect(historyService.requests, ['latest:30']);

      await imService.loadOlderForCurrentSession();
      await _waitForMessageCount(imService, 70);
      expect(
        imService.currentMessages.map((message) => message.msgId).toList(),
        List.generate(70, (index) => '${index + 31}'),
      );
      expect(imService.hasOlderMessages, isTrue);

      await imService.loadOlderForCurrentSession();
      await _waitForMessageCount(imService, 100);
      expect(
        imService.currentMessages.map((message) => message.msgId).toList(),
        List.generate(100, (index) => '${index + 1}'),
      );
      expect(
        imService.currentMessages.map((message) => message.msgId).toSet(),
        hasLength(100),
      );
      expect(imService.hasOlderMessages, isFalse);
      expect(historyService.requests, ['latest:30', '71:40', '31:40']);

      await imService.loadOlderForCurrentSession();
      expect(historyService.requests, ['latest:30', '71:40', '31:40']);
    },
  );

  test('full long tail repairs an interior hole before older paging', () async {
    const sessionId = 'long-gapped-session';
    final rows = _history(sessionId, 100);
    final historyService = _PagedHistorySessionService(rows);
    Get.put<SessionService>(historyService);
    final localTail = <Map<String, dynamic>>[
      rows[69],
      ...rows.skip(70).where((row) => row['msg_id'] != '85'),
    ];
    await LocalDb.batchInsertMessages(localTail);

    await imService.loadInitialWindowForTest(sessionId);

    expect(
      imService.currentMessages.map((message) => message.msgId).toList(),
      List.generate(31, (index) => '${index + 70}'),
    );
    expect(
      imService.currentMessages.map((message) => message.msgId).toSet(),
      hasLength(31),
    );
    expect(imService.hasOlderMessages, isTrue);
    expect(historyService.requests, ['latest:30']);

    await imService.loadOlderForCurrentSession();
    await _waitForMessageCount(imService, 71);
    await imService.loadOlderForCurrentSession();
    await _waitForMessageCount(imService, 100);
    expect(
      imService.currentMessages.map((message) => message.msgId).toList(),
      List.generate(100, (index) => '${index + 1}'),
    );
    expect(imService.hasOlderMessages, isFalse);
    expect(historyService.requests, ['latest:30', '70:40', '30:40']);
  });

  test('empty long window keeps the server hasMore paging boundary', () async {
    const sessionId = 'long-empty-session';
    final historyService = _PagedHistorySessionService(
      _history(sessionId, 100),
    );
    Get.put<SessionService>(historyService);

    await imService.loadInitialWindowForTest(sessionId);

    expect(
      imService.currentMessages.map((message) => message.msgId).toList(),
      List.generate(30, (index) => '${index + 71}'),
    );
    expect(imService.hasOlderMessages, isTrue);
    expect(historyService.requests, ['latest:30']);
  });

  test(
    'local overflow remains pageable when latest archive page is final',
    () async {
      const sessionId = 'local-overflow-session';
      final rows = List.generate(30, (index) {
        final id = 1001 + index;
        return _messageRow(
          sessionId: sessionId,
          id: id,
          senderType: id.isOdd ? 1 : 2,
          content: 'archive $id',
        );
      });
      final historyService = _PagedHistorySessionService(rows);
      Get.put<SessionService>(historyService);
      await LocalDb.batchInsertMessages([
        _messageRow(
          sessionId: sessionId,
          id: 1000,
          senderType: 1,
          content: 'older cached message',
        ),
        ...rows,
      ]);

      await imService.loadInitialWindowForTest(sessionId);

      expect(
        imService.currentMessages.map((message) => message.msgId).toList(),
        List.generate(30, (index) => '${index + 1001}'),
      );
      expect(imService.hasOlderMessages, isTrue);
      expect(historyService.requests, ['latest:30']);

      await imService.loadOlderForCurrentSession();
      await _waitForOlderState(imService, false);
      expect(
        imService.currentMessages.map((message) => message.msgId).toList(),
        ['1000', ...List.generate(30, (index) => '${index + 1001}')],
      );
      expect(imService.hasOlderMessages, isFalse);
      expect(historyService.requests, ['latest:30', '1000:40']);
    },
  );
}
