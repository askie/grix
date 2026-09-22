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

class _HistorySessionService extends SessionService {
  int historyCalls = 0;
  List<Map<String, dynamic>> historyMessages = const <Map<String, dynamic>>[];

  @override
  bool get isInitialized => true;

  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) async {
    historyCalls++;
    return SessionMessageHistoryResult(
      code: 0,
      messages: historyMessages,
      hasMore: false,
    );
  }
}

Future<Map<String, dynamic>> _readPendingReadStates() async {
  final prefs = await SharedPreferences.getInstance();
  final raw = prefs.getString('pending_read_states_1001');
  if (raw == null || raw.trim().isEmpty) {
    return <String, dynamic>{};
  }
  final decoded = jsonDecode(raw);
  if (decoded is Map<String, dynamic>) {
    return decoded;
  }
  return Map<String, dynamic>.from(decoded as Map);
}

Future<void> _expectPendingReadEventually(
  String sessionId,
  String lastReadMsgId,
) async {
  for (var i = 0; i < 20; i++) {
    final pending = await _readPendingReadStates();
    if (pending[sessionId] == lastReadMsgId) return;
    await Future<void>.delayed(const Duration(milliseconds: 20));
  }
  final pending = await _readPendingReadStates();
  expect(pending[sessionId], lastReadMsgId);
}

/// Production hole: bootstrap sync_head skips message.upsert bodies while the
/// session snapshot lands tip + unread. Local window still has older rows.
Future<void> _seedBootstrapHole({
  required String sid,
  required String oldMsg,
  required String newMsg,
  required int unread,
}) async {
  await LocalDb.upsertSession({
    'session_id': sid,
    'title': 'Watermark hole',
    'type': 'group',
    'unread_count': unread,
    'updated_at': 1774500920000,
    'last_message': 'skipped upsert tip',
    'last_message_time': 1774500920000,
  });
  await LocalDb.batchInsertMessages([
    {
      'msg_id': oldMsg,
      'session_id': sid,
      'sender_id': '1001',
      'sender_type': 1,
      'msg_type': 1,
      'content': 'pre-bootstrap local tip',
      'created_at': 1774500000000,
      'status': 'sent',
    },
  ]);
}

List<Map<String, dynamic>> _archiveTip({
  required String sid,
  required String oldMsg,
  required String newMsg,
}) {
  return [
    {
      'msg_id': newMsg,
      'session_id': sid,
      'sender_id': '2001',
      'sender_type': 2,
      'msg_type': 1,
      'content': 'skipped upsert tip',
      'created_at': 1774500920000,
      'status': 'sent',
      'state_version': '1',
    },
    {
      'msg_id': oldMsg,
      'session_id': sid,
      'sender_id': '1001',
      'sender_type': 1,
      'msg_type': 1,
      'content': 'pre-bootstrap local tip',
      'created_at': 1774500000000,
      'status': 'sent',
      'state_version': '1',
    },
  ];
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late String userId;
  late ImService imService;
  late _HistorySessionService sessionService;

  const sid = '49dc128a-1c7c-4750-b739-d0d4076ea1b5';
  const oldMsg = '2102377089335820288';
  const newMsg = '2102398496081973248';

  setUp(() async {
    Get.testMode = true;
    Get.reset();
    userId = 'v2_bootstrap_vis_${DateTime.now().microsecondsSinceEpoch}';
    SharedPreferences.setMockInitialValues({});
    Get.put<AuthService>(_FakeAuthService());
    Get.put<SessionService>(_HistorySessionService());
    sessionService = Get.find<SessionService>() as _HistorySessionService;
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(userId);
    imService = ImService();
    Get.put<ImService>(imService);
    imService.setActiveSyncModeForTest('v2');
  });

  tearDown(() async {
    imService.onClose();
    await LocalDb.setActiveUser(null);
    Get.reset();
  });

  for (final unread in const [3, 0]) {
    group('bootstrap tip lag unread=$unread', () {
      test(
        'open chat catch-up shows tip and reports latest read boundary',
        () async {
          await _seedBootstrapHole(
            sid: sid,
            oldMsg: oldMsg,
            newMsg: newMsg,
            unread: unread,
          );
          sessionService.historyMessages = _archiveTip(
            sid: sid,
            oldMsg: oldMsg,
            newMsg: newMsg,
          );

          await imService.loadInitialWindowForTest(sid);

          expect(sessionService.historyCalls, 1);
          expect(
            imService.currentMessages.map((m) => m.msgId),
            containsAll(<String>[oldMsg, newMsg]),
          );
          expect(await LocalDb.getLatestServerMessageId(sid), newMsg);
          await _expectPendingReadEventually(sid, newMsg);
        },
      );

      test(
        'left chat then re-enter catch-up shows tip once',
        () async {
          // Opened before bootstrap: local tip matches session tip.
          await LocalDb.upsertSession({
            'session_id': sid,
            'title': 'Watermark hole',
            'type': 'group',
            'unread_count': unread,
            'updated_at': 1774500000000,
            'last_message': 'pre-bootstrap local tip',
            'last_message_time': 1774500000000,
          });
          await LocalDb.batchInsertMessages([
            {
              'msg_id': oldMsg,
              'session_id': sid,
              'sender_id': '1001',
              'sender_type': 1,
              'msg_type': 1,
              'content': 'pre-bootstrap local tip',
              'created_at': 1774500000000,
              'status': 'sent',
            },
          ]);
          sessionService.historyMessages = _archiveTip(
            sid: sid,
            oldMsg: oldMsg,
            newMsg: newMsg,
          );

          await imService.loadInitialWindowForTest(sid);
          expect(sessionService.historyCalls, 0);
          expect(
            imService.currentMessages.map((m) => m.msgId),
            isNot(contains(newMsg)),
          );

          imService.leaveSession();

          // Bootstrap snapshot advances session tip past skipped upserts.
          await LocalDb.upsertSession({
            'session_id': sid,
            'title': 'Watermark hole',
            'type': 'group',
            'unread_count': unread,
            'updated_at': 1774500920000,
            'last_message': 'skipped upsert tip',
            'last_message_time': 1774500920000,
          });

          await imService.loadInitialWindowForTest(sid);
          expect(sessionService.historyCalls, 1);
          expect(
            imService.currentMessages.map((m) => m.msgId),
            containsAll(<String>[oldMsg, newMsg]),
          );
          expect(await LocalDb.getLatestServerMessageId(sid), newMsg);
          await _expectPendingReadEventually(sid, newMsg);

          // Third enter must not re-hit archive.
          imService.leaveSession();
          await imService.loadInitialWindowForTest(sid);
          expect(sessionService.historyCalls, 1);
        },
      );

      test(
        'never-opened chat catch-up shows tip on first enter',
        () async {
          await _seedBootstrapHole(
            sid: sid,
            oldMsg: oldMsg,
            newMsg: newMsg,
            unread: unread,
          );
          sessionService.historyMessages = _archiveTip(
            sid: sid,
            oldMsg: oldMsg,
            newMsg: newMsg,
          );

          expect(imService.currentSessionId, isNull);
          expect(imService.cachedSessionWindowIdsForTest, isEmpty);

          await imService.loadInitialWindowForTest(sid);

          expect(sessionService.historyCalls, 1);
          expect(
            imService.currentMessages.map((m) => m.msgId),
            containsAll(<String>[oldMsg, newMsg]),
          );
          expect(await LocalDb.getLatestServerMessageId(sid), newMsg);
          await _expectPendingReadEventually(sid, newMsg);
        },
      );
    });
  }
}
