import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:shared_preferences/shared_preferences.dart';

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
}
