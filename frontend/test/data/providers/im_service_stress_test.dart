import 'dart:convert';

import 'package:flutter/foundation.dart' as foundation;
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/message_model.dart';
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
}

String _testUserId = 'im-stress-bootstrap';

String _nextTestUserId() {
  return 'im-stress-${DateTime.now().microsecondsSinceEpoch}';
}

Map<String, dynamic> _buildPushPayload({
  required int inboxSeq,
  required int msgId,
  required String sessionId,
  required int createdAt,
}) {
  return {
    'inbox_seq': inboxSeq,
    'msg_id': msgId.toString(),
    'session_id': sessionId,
    'sender_id': '2002',
    'sender_type': 1,
    'msg_type': 1,
    'content': 'msg_$msgId',
    'created_at': createdAt,
  };
}

void main() {
  late foundation.DebugPrintCallback originalDebugPrint;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues({});
    _testUserId = _nextTestUserId();
    Get.put<AuthService>(_FakeAuthService(_testUserId));
    originalDebugPrint = foundation.debugPrint;
    foundation.debugPrint = (String? _, {int? wrapWidth}) {};
  });

  tearDown(() async {
    await LocalDb.setActiveUser(null);
    foundation.debugPrint = originalDebugPrint;
    Get.reset();
  });

  test('stress: sequential push_msg throughput', () async {
    final service = ImService();
    service.setCurrentSessionForTest('s1');

    const total = 20000;
    final sw = Stopwatch()..start();
    for (var i = 1; i <= total; i++) {
      await service.handleDownstreamForTest(
        jsonEncode({
          'cmd': 'push_msg',
          'payload': _buildPushPayload(
            inboxSeq: i,
            msgId: i,
            sessionId: 's1',
            createdAt: i * 10,
          ),
        }),
      );
    }
    sw.stop();

    final elapsedMs = sw.elapsedMilliseconds;
    final throughput = elapsedMs == 0 ? 0.0 : total * 1000 / elapsedMs;
    // ignore: avoid_print
    print(
      'BENCH push_msg total=$total elapsed_ms=$elapsedMs throughput_msg_per_s=${throughput.toStringAsFixed(2)}',
    );

    expect(service.currentMessages.length, service.residentMessageCapForTest);
    // Wide guardrail, mainly to catch pathological regressions.
    expect(elapsedMs, lessThan(30000));
  });

  test('stress: large pull_sync_resp incremental merge', () async {
    final service = ImService();
    service.setCurrentSessionForTest('s1');

    const existing = 5000;
    for (var i = 1; i <= existing; i++) {
      service.upsertUIMessageForTest(
        MessageModel(
          msgId: 'e$i',
          sessionId: 's1',
          senderId: '2002',
          content: 'existing_$i',
          createdAt: i * 10,
        ),
      );
    }

    const currentSessionIncoming = 10000;
    const otherSessionIncoming = 10000;
    final messages = <Map<String, dynamic>>[];

    for (var i = 1; i <= currentSessionIncoming; i++) {
      final seq = i;
      messages.add(
        _buildPushPayload(
          inboxSeq: seq,
          msgId: 100000 + i,
          sessionId: 's1',
          createdAt: 100000 + i,
        ),
      );
    }
    for (var i = 1; i <= otherSessionIncoming; i++) {
      final seq = currentSessionIncoming + i;
      messages.add(
        _buildPushPayload(
          inboxSeq: seq,
          msgId: 200000 + i,
          sessionId: 's2',
          createdAt: 200000 + i,
        ),
      );
    }

    final sw = Stopwatch()..start();
    await service.handleDownstreamForTest(
      jsonEncode({
        'cmd': 'pull_sync_resp',
        'payload': {'has_more': false, 'messages': messages},
      }),
    );
    sw.stop();

    final elapsedMs = sw.elapsedMilliseconds;
    const totalIncoming = currentSessionIncoming + otherSessionIncoming;
    final throughput = elapsedMs == 0 ? 0.0 : totalIncoming * 1000 / elapsedMs;
    // ignore: avoid_print
    print(
      'BENCH pull_sync_resp total_incoming=$totalIncoming elapsed_ms=$elapsedMs throughput_msg_per_s=${throughput.toStringAsFixed(2)}',
    );

    expect(service.currentMessages.length, service.residentMessageCapForTest);
    expect(elapsedMs, lessThan(30000));
  });

  test(
    'stress: sequential push_msg throughput with LocalDb persistence',
    () async {
      try {
        await LocalDb.setActiveUser(_testUserId);
      } catch (e) {
        // ignore: avoid_print
        print(
          'BENCH push_msg(LocalDb) skipped: LocalDb unavailable in this env: $e',
        );
        return;
      }

      final service = ImService();
      service.setCurrentSessionForTest('s1');

      // Each push_msg performs real LocalDb transactions (message insert +
      // session preview update). Per-message commit cost on slow disk-backed
      // sqflite (e.g. Windows sqflite_common_ffi) is far higher than CI's
      // in-memory store, so the absolute count is sized to stay within the
      // wide 30s guardrail on those hosts while still being large enough
      // (>> the 200 resident cap) to surface pathological super-linear
      // regressions in the sequential push_msg path.
      const total = 1500;
      final sw = Stopwatch()..start();
      for (var i = 1; i <= total; i++) {
        await service.handleDownstreamForTest(
          jsonEncode({
            'cmd': 'push_msg',
            'payload': _buildPushPayload(
              inboxSeq: i,
              msgId: 300000 + i,
              sessionId: 's1',
              createdAt: i * 10,
            ),
          }),
        );
      }
      sw.stop();

      final elapsedMs = sw.elapsedMilliseconds;
      final throughput = elapsedMs == 0 ? 0.0 : total * 1000 / elapsedMs;
      // ignore: avoid_print
      print(
        'BENCH push_msg(LocalDb) total=$total elapsed_ms=$elapsedMs throughput_msg_per_s=${throughput.toStringAsFixed(2)}',
      );

      expect(service.currentMessages.length, service.residentMessageCapForTest);
      expect(elapsedMs, lessThan(30000));
    },
  );
}
