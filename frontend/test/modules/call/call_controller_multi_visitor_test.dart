import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import 'package:grix/modules/call/call_controller.dart';
import 'package:grix/modules/call/call_state.dart';
import 'package:grix/data/providers/feature_flag_service.dart';

void main() {
  setUp(() {
    debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
    Get.testMode = true;
    final ffs = FeatureFlagService();
    ffs.features.addAll(['voice_call', 'agent_voice_llm']);
    Get.put<FeatureFlagService>(ffs);
    Get.put<CallController>(CallController());
  });

  tearDown(() {
    debugDefaultTargetPlatformOverride = null;
    Get.reset();
  });

  group('多访客 AI 代接 - _delegatedCalls', () {
    test('onCallAiDelegated 只登记元数据，不改 _session（不连房）', () {
      final ctrl = Get.find<CallController>();

      ctrl.onCallAiDelegated({
        'call_id': 'call-001',
        'session_id': 'sess-v1',
        'peer_name': 'Visitor1',
        'room_token': 'tok1',
        'room_url': 'wss://lk.test',
      });

      expect(ctrl.session, isNull, reason: '收到 ai_delegated 不应自动建 _session（不连房）');
      expect(ctrl.hasVoiceCallForSession('sess-v1'), isTrue);
      expect(ctrl.delegatedCalls['sess-v1']?.callId, 'call-001');
      expect(ctrl.delegatedCalls['sess-v1']?.peerName, 'Visitor1');
    });

    test('多通 AI 代接并存', () {
      final ctrl = Get.find<CallController>();

      ctrl.onCallAiDelegated({
        'call_id': 'call-001',
        'session_id': 'sess-v1',
        'peer_name': 'Visitor1',
        'room_token': 'tok1',
        'room_url': 'wss://lk.test',
      });
      ctrl.onCallAiDelegated({
        'call_id': 'call-002',
        'session_id': 'sess-v2',
        'peer_name': 'Visitor2',
        'room_token': 'tok2',
        'room_url': 'wss://lk.test',
      });

      expect(ctrl.delegatedCalls.length, 2);
      expect(ctrl.hasVoiceCallForSession('sess-v1'), isTrue);
      expect(ctrl.hasVoiceCallForSession('sess-v2'), isTrue);
      expect(ctrl.session, isNull, reason: '无活跃通话时 _session 应为 null');
    });

    test('onCallVoiceStatusEnd 清除对应会话徽标', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallAiDelegated({
        'call_id': 'call-001',
        'session_id': 'sess-v1',
        'peer_name': 'V1',
        'room_token': 'tok1',
        'room_url': 'wss://lk.test',
      });
      ctrl.onCallAiDelegated({
        'call_id': 'call-002',
        'session_id': 'sess-v2',
        'peer_name': 'V2',
        'room_token': 'tok2',
        'room_url': 'wss://lk.test',
      });
      expect(ctrl.delegatedCalls.length, 2);

      ctrl.onCallVoiceStatusEnd({'call_id': 'call-001', 'session_id': 'sess-v1'});
      expect(ctrl.hasVoiceCallForSession('sess-v1'), isFalse);
      expect(ctrl.hasVoiceCallForSession('sess-v2'), isTrue, reason: '其它通话徽标不受影响');
    });
  });

  group('懒连接旁听 - onCallListenAck', () {
    test('onCallListenAck 设置 aiDelegated 状态（旁听进房），不报错', () {
      final ctrl = Get.find<CallController>();

      // 先登记一通 AI 代接
      ctrl.onCallAiDelegated({
        'call_id': 'call-010',
        'session_id': 'sess-w1',
        'peer_name': 'Visitor',
        'room_token': 'tok',
        'room_url': 'wss://lk.test',
      });

      // 模拟发出 listen，创建 connecting session
      ctrl.listenToDelegatedCall('sess-w1', (_) {});
      expect(ctrl.session?.state, CallState.connecting);
      expect(ctrl.session?.callId, 'call-010');

      // 服务端回 listen_ack
      ctrl.onCallListenAck({
        'call_id': 'call-010',
        'room_token': 'tok-callee',
        'room_url': 'wss://lk.test',
      });

      expect(ctrl.session?.state, CallState.aiDelegated);
      expect(ctrl.isMuted.value, isTrue, reason: '旁听应静音');
    });
  });

  group('切换通话 - 自动交回 AI', () {
    test('已在旁听通话A时 listenToDelegatedCall 通话B → 自动 leave A + 进入 connecting B', () {
      final ctrl = Get.find<CallController>();
      final sent = <Map<String, dynamic>>[];

      ctrl.onCallAiDelegated({
        'call_id': 'call-A',
        'session_id': 'sess-A',
        'peer_name': 'VisitorA',
        'room_token': 'tA',
        'room_url': 'wss://lk.test',
      });
      ctrl.onCallAiDelegated({
        'call_id': 'call-B',
        'session_id': 'sess-B',
        'peer_name': 'VisitorB',
        'room_token': 'tB',
        'room_url': 'wss://lk.test',
      });

      // 旁听 A（注：_connectRoom 在测试中无 LiveKit，不会真正连房，但状态正常切换）
      ctrl.listenToDelegatedCall('sess-A', (pkt) => sent.add(pkt));
      ctrl.onCallListenAck({
        'call_id': 'call-A',
        'room_token': 'tok-A',
        'room_url': 'wss://lk.test',
      });
      expect(ctrl.session?.callId, 'call-A');

      // 切换到 B
      sent.clear();
      ctrl.listenToDelegatedCall('sess-B', (pkt) => sent.add(pkt));

      final leaveCmd = sent.where((p) => p['cmd'] == 'call:leave').toList();
      final listenCmd = sent.where((p) => p['cmd'] == 'call:listen').toList();
      expect(leaveCmd.isNotEmpty, isTrue, reason: '应自动对 A 发 call:leave');
      expect(leaveCmd.first['payload']['call_id'], 'call-A');
      expect(listenCmd.isNotEmpty, isTrue, reason: '应对 B 发 call:listen');
      expect(ctrl.session?.callId, 'call-B');
    });
  });

  group('多设备被拒', () {
    test('feature flag 禁用时 listenToDelegatedCall 无操作', () {
      Get.find<FeatureFlagService>().features.remove('voice_call');
      final ctrl = Get.find<CallController>();
      final sent = <Map<String, dynamic>>[];
      ctrl.onCallAiDelegated({
        'call_id': 'call-X',
        'session_id': 'sess-X',
        'peer_name': 'V',
        'room_token': 'tok',
        'room_url': 'wss://lk.test',
      });
      ctrl.listenToDelegatedCall('sess-X', (pkt) => sent.add(pkt));
      expect(sent, isEmpty, reason: 'feature flag 禁用时不发 listen');
    });
  });
}
