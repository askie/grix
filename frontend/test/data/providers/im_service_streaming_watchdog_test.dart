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
    // 有可恢复的非空白正文：消息气泡保留并封板，正文写回窗口消息，
    // 不再视为"正在流式"。
    final zombie = service.currentMessages.firstWhere(
      (m) => m.msgId == 'm-zombie',
    );
    expect(zombie.content, 'chunk');
    // 集合清空后看门狗计时器自动取消。
    expect(service.hasStreamingWatchdogTimerForTest, isFalse);
  });

  test('纯空白 chunk 的僵尸流被看门狗连占位气泡一起清除', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');

    await sendChunk(service, 'm-blank', delta: '  \n');
    expect(service.currentMessages.any((m) => m.msgId == 'm-blank'), isTrue);
    expect(service.isMessageStreaming('m-blank'), isTrue);

    // 模拟终态包与 stream_delete 同时丢失：超过空闲阈值被清扫。
    final backdated =
        DateTime.now().millisecondsSinceEpoch -
        const Duration(minutes: 6).inMilliseconds;
    service.debugSetStreamingActivityAtForTest('m-blank', backdated);
    service.sweepStaleStreamingMessagesForTest();

    expect(service.isMessageStreaming('m-blank'), isFalse);
    expect(service.hasStreamingAgentOutputForSession('s1'), isFalse);
    // 无可恢复正文：空占位气泡一并移除，不留空气泡。
    expect(service.currentMessages.any((m) => m.msgId == 'm-blank'), isFalse);
    expect(MessageStreamController.hasActiveProducer('m-blank'), isFalse);
  });

  test('看门狗不是权威终态，同 msgId 后续 chunk 仍可恢复流式输出', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');

    await sendChunk(service, 'm-watchdog-resume', delta: ' \n');
    final backdated =
        DateTime.now().millisecondsSinceEpoch -
        const Duration(minutes: 6).inMilliseconds;
    service.debugSetStreamingActivityAtForTest('m-watchdog-resume', backdated);
    service.sweepStaleStreamingMessagesForTest();
    expect(
      service.currentMessages.any((m) => m.msgId == 'm-watchdog-resume'),
      isFalse,
    );

    await sendChunk(
      service,
      'm-watchdog-resume',
      chunkSeq: 1,
      delta: 'resumed',
    );

    expect(service.isMessageStreaming('m-watchdog-resume'), isTrue);
    expect(
      service.currentMessages.any((m) => m.msgId == 'm-watchdog-resume'),
      isTrue,
    );
    expect(
      MessageStreamController.peekRecoverableContent('m-watchdog-resume'),
      'resumed',
    );
  });

  test('agent 终态到达时清除无可恢复正文的空占位', () async {
    final service = makeService();
    service.setCurrentSessionForTest('s1');

    await sendChunk(service, 'm-term', delta: ' ');
    final baseUpdatedAt = DateTime.now().millisecondsSinceEpoch;
    await service.handleDownstreamForTest(
      packet('agent_output_status', <String, dynamic>{
        'session_id': 's1',
        'run_id': 'r-term',
        'agent_id': 'agent-1',
        'state': 'running',
        'stream_msg_id': 'm-term',
        'updated_at': baseUpdatedAt,
      }),
    );
    expect(service.currentMessages.any((m) => m.msgId == 'm-term'), isTrue);
    expect(service.isMessageStreaming('m-term'), isTrue);

    // 终态到达但 stream_finish / stream_delete 均丢失：空占位必须被移除。
    await service.handleDownstreamForTest(
      packet('agent_output_status', <String, dynamic>{
        'session_id': 's1',
        'run_id': 'r-term',
        'agent_id': 'agent-1',
        'state': 'completed',
        'stream_msg_id': 'm-term',
        'updated_at': baseUpdatedAt + 1000,
      }),
    );

    expect(service.isMessageStreaming('m-term'), isFalse);
    expect(service.currentMessages.any((m) => m.msgId == 'm-term'), isFalse);
    expect(MessageStreamController.hasActiveProducer('m-term'), isFalse);
    expect(service.agentOutputStateFor('s1'), isNull);
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

    // 等看门狗清扫把流式态清掉（勿用固定 200ms，CI 抖动会漏清扫周期）。
    final deadline = DateTime.now().add(const Duration(seconds: 5));
    while (service.isMessageStreaming('m-auto')) {
      if (DateTime.now().isAfter(deadline)) {
        fail('Timed out waiting for streaming watchdog to clear m-auto');
      }
      await Future<void>.delayed(const Duration(milliseconds: 20));
    }

    expect(service.isMessageStreaming('m-auto'), isFalse);
    expect(service.hasStreamingAgentOutputForSession('s1'), isFalse);
  });
}
