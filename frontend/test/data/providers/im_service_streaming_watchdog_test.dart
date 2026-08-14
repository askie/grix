import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/im_service.dart';
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
    String delta = 'chunk',
  }) {
    return service.handleDownstreamForTest(
      packet('stream_chunk', <String, dynamic>{
        'msg_id': msgId,
        'session_id': sessionId,
        'sender_id': 'agent-1',
        'sender_type': 2,
        'chunk_seq': chunkSeq,
        'delta_content': delta,
      }),
    );
  }

  int staleUpdatedAt() =>
      DateTime.now().millisecondsSinceEpoch -
      const Duration(minutes: 5).inMilliseconds;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    MessageStreamController.resetForTest();
    SharedPreferences.setMockInitialValues(<String, Object>{});
  });

  tearDown(() {
    for (final service in trackedServices.reversed) {
      service.onClose();
    }
    trackedServices.clear();
    ImService.streamingIdleTimeoutForTest = null;
    ImService.streamingWatchdogIntervalForTest = null;
    MessageStreamController.resetForTest();
    Get.reset();
  });

  test('僵尸流被看门狗清除，被顶住的 agentOutputStates 随之被 stale 清理', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');

    await sendChunk(service, 'm-zombie');
    await service.handleDownstreamForTest(
      packet('agent_output_status', <String, dynamic>{
        'session_id': 's1',
        'run_id': 'r-zombie',
        'agent_id': 'agent-1',
        'state': 'running',
        'stream_msg_id': 'm-zombie',
        'updated_at': staleUpdatedAt(),
      }),
    );

    expect(service.isMessageStreaming('m-zombie'), isTrue);
    expect(service.hasStreamingAgentOutputForSession('s1'), isTrue);
    expect(service.agentOutputStateFor('s1'), isNotNull);
    expect(service.hasStreamingWatchdogTimerForTest, isTrue);

    // 模拟终态包丢失：活动时间停在 6 分钟前，超过 5 分钟空闲阈值。
    final backdated =
        DateTime.now().millisecondsSinceEpoch -
        const Duration(minutes: 6).inMilliseconds;
    service.debugSetStreamingActivityAtForTest('m-zombie', backdated);

    service.sweepStaleStreamingMessagesForTest();

    expect(service.isMessageStreaming('m-zombie'), isFalse);
    expect(service.hasStreamingAgentOutputForSession('s1'), isFalse);
    // 被僵尸流顶住的 stale 胶囊状态被补刀清掉。
    expect(service.agentOutputStateFor('s1'), isNull);
    // 消息气泡保留，只是不再视为"正在流式"。
    expect(service.currentMessages.any((m) => m.msgId == 'm-zombie'), isTrue);
    // 集合清空后看门狗计时器自动取消。
    expect(service.hasStreamingWatchdogTimerForTest, isFalse);
  });

  test('活跃流不会被看门狗误清', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');

    await sendChunk(service, 'm-active');
    service.sweepStaleStreamingMessagesForTest();

    expect(service.isMessageStreaming('m-active'), isTrue);
    expect(service.hasStreamingAgentOutputForSession('s1'), isTrue);
  });

  test('chunk 到达会刷新活动时间，避免长流被误判为僵尸流', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');

    await sendChunk(service, 'm-refresh', chunkSeq: 1);

    // 把活动时间钉到 6 分钟前，随后再来一个 chunk，活动时间应被刷新。
    final backdated =
        DateTime.now().millisecondsSinceEpoch -
        const Duration(minutes: 6).inMilliseconds;
    service.debugSetStreamingActivityAtForTest('m-refresh', backdated);
    await sendChunk(service, 'm-refresh', chunkSeq: 2, delta: 'more');

    service.sweepStaleStreamingMessagesForTest();

    expect(service.isMessageStreaming('m-refresh'), isTrue);
    expect(service.hasStreamingAgentOutputForSession('s1'), isTrue);
  });

  test('test override 阈值生效：超过自定义空闲阈值即被清扫', () async {
    ImService.streamingIdleTimeoutForTest = const Duration(milliseconds: 50);
    final service = makeService();
    service.setCurrentSessionForTest('s1');

    await sendChunk(service, 'm-override');
    expect(service.isMessageStreaming('m-override'), isTrue);

    await Future<void>.delayed(const Duration(milliseconds: 80));
    service.sweepStaleStreamingMessagesForTest();

    expect(service.isMessageStreaming('m-override'), isFalse);
    expect(service.hasStreamingAgentOutputForSession('s1'), isFalse);
  });

  test('看门狗周期计时器自动触发清扫', () async {
    ImService.streamingIdleTimeoutForTest = const Duration(milliseconds: 50);
    ImService.streamingWatchdogIntervalForTest = const Duration(
      milliseconds: 30,
    );
    final service = makeService();
    service.setCurrentSessionForTest('s1');

    await sendChunk(service, 'm-auto');
    expect(service.isMessageStreaming('m-auto'), isTrue);

    // 等待至少一个清扫周期 + 空闲阈值。
    await Future<void>.delayed(const Duration(milliseconds: 200));

    expect(service.isMessageStreaming('m-auto'), isFalse);
    expect(service.hasStreamingAgentOutputForSession('s1'), isFalse);
  });
}
