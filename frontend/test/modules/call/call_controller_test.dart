import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import 'package:grix/modules/call/call_controller.dart';
import 'package:grix/modules/call/call_state.dart';
import 'package:grix/data/providers/feature_flag_service.dart';

void main() {
  setUp(() {
    // 语音通话仅在 Web/桌面启用；测试默认平台为 android（禁用），此处置为 macOS 以验证启用态逻辑。
    debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
    Get.testMode = true;
    // 注册 FeatureFlagService 并启用语音通话相关 feature flag
    final ffs = FeatureFlagService();
    ffs.features.addAll(['voice_call', 'agent_voice_llm']);
    Get.put<FeatureFlagService>(ffs);
    Get.put<CallController>(CallController());
  });

  tearDown(() {
    debugDefaultTargetPlatformOverride = null;
    Get.reset();
  });

  group('CallController', () {
    test('Feature flag 禁用忽略来电，不进入 ringing', () {
      // 清除 voice_call flag 模拟禁用状态
      Get.find<FeatureFlagService>().features.clear();
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-x',
        'caller_id': '100',
        'caller_name': 'Alice',
        'call_mode': 1,
      });
      expect(ctrl.session, isNull);
      expect(ctrl.isInCall, isFalse);
    });

    test('初始状态为 idle', () {
      final ctrl = Get.find<CallController>();
      expect(ctrl.session, isNull);
      expect(ctrl.isInCall, isFalse);
    });

    test('onCallRing 设置 ringing 状态', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-1',
        'caller_id': '100',
        'caller_name': 'Alice',
        'call_mode': 1,
      });

      expect(ctrl.session, isNotNull);
      expect(ctrl.session!.state, CallState.ringing);
      expect(ctrl.session!.callId, 'call-1');
      expect(ctrl.session!.peerName, 'Alice');
      expect(ctrl.isInCall, isTrue);
    });

    test('onCallPeerAnswered 设置 active 状态', () {
      final ctrl = Get.find<CallController>();
      // 先设置 ringing 状态
      ctrl.onCallRing({
        'call_id': 'call-2',
        'caller_id': '200',
        'caller_name': 'Bob',
        'call_mode': 1,
      });

      ctrl.onCallPeerAnswered({'call_id': 'call-2', 'mode': 'human'});

      expect(ctrl.session!.state, CallState.active);
    });

    test('onCallState ENDED 清理状态', () async {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-3',
        'caller_id': '300',
        'caller_name': 'Carol',
        'call_mode': 1,
      });

      ctrl.onCallState({'call_id': 'call-3', 'state': 2}); // 2=ended

      expect(ctrl.session!.state, CallState.ended);
    });

    test('onCallState 忽略不匹配的 call_id', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-4',
        'caller_id': '400',
        'caller_name': 'Dave',
        'call_mode': 1,
      });

      ctrl.onCallState({'call_id': 'other-call', 'state': 2});

      // 状态不变
      expect(ctrl.session!.state, CallState.ringing);
    });

    test('reject 发送 call:reject 并清理状态', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-5',
        'caller_id': '500',
        'caller_name': 'Eve',
        'call_mode': 1,
      });
      expect(ctrl.isInCall, isTrue);

      final sent = <Map<String, dynamic>>[];
      ctrl.reject((pkt) => sent.add(pkt));

      expect(sent, hasLength(1));
      expect(sent.first['cmd'], 'call:reject');
      expect((sent.first['payload'] as Map)['call_id'], 'call-5');
      expect(ctrl.session!.state, CallState.ended);
    });

    test('hangup 发送 call:hangup 并清理状态', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-6',
        'caller_id': '600',
        'caller_name': 'Frank',
        'call_mode': 1,
      });

      final sent = <Map<String, dynamic>>[];
      ctrl.hangup((pkt) => sent.add(pkt));

      expect(sent, hasLength(1));
      expect(sent.first['cmd'], 'call:hangup');
      expect((sent.first['payload'] as Map)['call_id'], 'call-6');
      expect(ctrl.session!.state, CallState.ended);
    });

    test('reject 在 idle 状态不发送任何包', () {
      final ctrl = Get.find<CallController>();
      expect(ctrl.session, isNull);

      final sent = <Map<String, dynamic>>[];
      ctrl.reject((pkt) => sent.add(pkt));

      expect(sent, isEmpty);
    });

    test('dismissIncoming 只关闭本机来电，不发送拒接信令', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-local-dismiss',
        'caller_id': '501',
        'caller_name': 'Local',
        'call_mode': 1,
      });

      ctrl.dismissIncoming();

      expect(ctrl.session!.state, CallState.ended);
    });

    test('hangup 在 idle 状态不发送任何包', () {
      final ctrl = Get.find<CallController>();
      final sent = <Map<String, dynamic>>[];
      ctrl.hangup((pkt) => sent.add(pkt));
      expect(sent, isEmpty);
    });

    // --- Phase 2 测试 ---

    test('onCallPeerAnswered mode=ai_delegated 设置 aiDelegated 状态', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-ai-1',
        'caller_id': '10',
        'caller_name': 'A',
        'call_mode': 1,
      });

      ctrl.onCallPeerAnswered({'call_id': 'call-ai-1', 'mode': 'ai_delegated'});

      expect(ctrl.session!.state, CallState.aiDelegated);
      expect(ctrl.session!.delegationMode, DelegationMode.aiDelegated);
    });

    test('answerWithAI 发送 call:answer_with_ai 并等待服务端确认', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-ai-2',
        'caller_id': '20',
        'caller_name': 'B',
        'call_mode': 1,
      });

      final sent = <Map<String, dynamic>>[];
      ctrl.answerWithAI('agent-42', 'AI 助手', (pkt) => sent.add(pkt));

      expect(sent, hasLength(1));
      expect(sent.first['cmd'], 'call:answer_with_ai');
      expect((sent.first['payload'] as Map)['agent_id'], 'agent-42');
      expect(ctrl.session!.state, CallState.connecting);
      expect(ctrl.session!.agentId, 'agent-42');
      expect(ctrl.session!.agentName, 'AI 助手');
    });

    test('takeover 发送 call:takeover 并切换为 humanActive', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-ai-3',
        'caller_id': '30',
        'caller_name': 'C',
        'call_mode': 1,
      });
      ctrl.answerWithAI('agent-1', 'Bot', (pkt) {});
      ctrl.onCallPeerAnswered({'call_id': 'call-ai-3', 'mode': 'ai_delegated'});

      final sent = <Map<String, dynamic>>[];
      ctrl.takeover((pkt) => sent.add(pkt));

      expect(sent, hasLength(1));
      expect(sent.first['cmd'], 'call:takeover');
      expect(ctrl.session!.state, CallState.humanActive);
      expect(ctrl.session!.delegationMode, DelegationMode.mixed);
    });

    test('handBack 发送 call:hand_back 并切换为 aiDelegated', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-ai-4',
        'caller_id': '40',
        'caller_name': 'D',
        'call_mode': 1,
      });
      ctrl.answerWithAI('agent-1', 'Bot', (pkt) {});
      ctrl.onCallPeerAnswered({'call_id': 'call-ai-4', 'mode': 'ai_delegated'});
      ctrl.takeover((pkt) {});

      final sent = <Map<String, dynamic>>[];
      ctrl.handBack((pkt) => sent.add(pkt));

      expect(sent, hasLength(1));
      expect(sent.first['cmd'], 'call:hand_back');
      expect(ctrl.session!.state, CallState.aiDelegated);
    });

    test('takeover 在非 aiDelegated 状态不发送任何包', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-ai-5',
        'caller_id': '50',
        'caller_name': 'E',
        'call_mode': 1,
      });
      // 真人接听（非 AI 托管）
      ctrl.answer((pkt) {});

      final sent = <Map<String, dynamic>>[];
      ctrl.takeover((pkt) => sent.add(pkt));
      expect(sent, isEmpty);
    });

    test('onCallState answered_elsewhere 清理本设备来电', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-multi-1',
        'caller_id': '70',
        'caller_name': 'G',
        'call_mode': 1,
      });

      ctrl.onCallState({
        'call_id': 'call-multi-1',
        'state': 1,
        'reason': 'answered_elsewhere',
        'answered_device_id': 'device-winner',
      });

      expect(ctrl.session!.state, CallState.ended);
    });

    test('handBack 在非 humanActive 状态不发送任何包', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-ai-6',
        'caller_id': '60',
        'caller_name': 'F',
        'call_mode': 1,
      });
      ctrl.answerWithAI('agent-1', 'Bot', (pkt) {});
      // 此时是 aiDelegated，不是 humanActive

      final sent = <Map<String, dynamic>>[];
      ctrl.handBack((pkt) => sent.add(pkt));
      expect(sent, isEmpty);
    });

    test('onCallState mode=human_active 切换为 humanActive', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-ai-7',
        'caller_id': '70',
        'caller_name': 'G',
        'call_mode': 1,
      });
      ctrl.answerWithAI('agent-1', 'Bot', (pkt) {});

      ctrl.onCallState({'call_id': 'call-ai-7', 'mode': 'human_active'});
      expect(ctrl.session!.state, CallState.humanActive);
    });

    test('onCallState mode=ai_delegated 切换为 aiDelegated', () {
      final ctrl = Get.find<CallController>();
      ctrl.onCallRing({
        'call_id': 'call-ai-8',
        'caller_id': '80',
        'caller_name': 'H',
        'call_mode': 1,
      });
      ctrl.answerWithAI('agent-1', 'Bot', (pkt) {});
      ctrl.takeover((pkt) {});

      ctrl.onCallState({'call_id': 'call-ai-8', 'mode': 'ai_delegated'});
      expect(ctrl.session!.state, CallState.aiDelegated);
    });

    test('CallSession.isAIInvolved 在 aiDelegated/humanActive 时为 true', () {
      const s1 = CallSession(
        callId: '1',
        peerId: '1',
        peerName: '',
        callMode: 1,
        state: CallState.aiDelegated,
      );
      const s2 = CallSession(
        callId: '2',
        peerId: '1',
        peerName: '',
        callMode: 1,
        state: CallState.humanActive,
      );
      const s3 = CallSession(
        callId: '3',
        peerId: '1',
        peerName: '',
        callMode: 1,
        state: CallState.active,
      );
      expect(s1.isAIInvolved, isTrue);
      expect(s2.isAIInvolved, isTrue);
      expect(s3.isAIInvolved, isFalse);
    });

    test('directCallAgent 发送 call:direct_ai 并进入 connecting', () async {
      final ctrl = Get.find<CallController>();
      final sent = <Map<String, dynamic>>[];
      await ctrl.directCallAgent('42', 'Bot', sent.add);
      expect(sent, hasLength(1));
      expect(sent.first['cmd'], 'call:direct_ai');
      expect((sent.first['payload'] as Map)['agent_id'], '42');
      expect(ctrl.session!.state, CallState.connecting);
      expect(ctrl.session!.agentId, '42');
    });

    test('directCallAgent 超时未收到 invite_ack 则收尾', () {
      fakeAsync((async) {
        final ctrl = Get.find<CallController>();
        ctrl.directCallAgent('42', 'Bot', (_) {});
        async.flushMicrotasks();
        expect(ctrl.isInCall, isTrue);
        async.elapse(const Duration(seconds: 46));
        expect(ctrl.isInCall, isFalse, reason: '看门狗应收尾未连入的测试拨打');
      });
    });

    test('directCallAgent 收到 invite_ack 取消看门狗', () {
      fakeAsync((async) {
        final ctrl = Get.find<CallController>();
        ctrl.directCallAgent('42', 'Bot', (_) {});
        async.flushMicrotasks();
        ctrl.onCallInviteAck({
          'call_id': 'c1',
          'room_token': '',
          'room_url': '',
        });
        async.elapse(const Duration(seconds: 13));
        expect(ctrl.isInCall, isTrue);
        expect(ctrl.session!.callId, 'c1');
      });
    });

    // --- connecting 状态机测试（直拨 AI 通话）---

    test('directCallAgent 初始状态为 connecting，不在通话中计时', () async {
      final ctrl = Get.find<CallController>();
      await ctrl.directCallAgent('42', 'Bot', (_) {});
      expect(ctrl.session!.state, CallState.connecting);
      expect(ctrl.session!.agentId, '42');
      expect(ctrl.session!.agentName, 'Bot');
      expect(
        ctrl.callStopwatch.isRunning,
        isFalse,
        reason: 'connecting 阶段不应启动计时',
      );
    });

    test(
      'directCallAgent isActiveCallOverlayVisible 在 connecting 为 true',
      () async {
        final ctrl = Get.find<CallController>();
        expect(ctrl.isActiveCallOverlayVisible, isFalse);
        await ctrl.directCallAgent('42', 'Bot', (_) {});
        expect(ctrl.isActiveCallOverlayVisible, isTrue);
      },
    );

    test('inviteAck 不直接切换 connecting 为 aiDelegated', () async {
      final ctrl = Get.find<CallController>();
      await ctrl.directCallAgent('42', 'Bot', (_) {});
      expect(ctrl.session!.state, CallState.connecting);

      // invite_ack 更新 callId 但不切换状态（需要 AI participant 加入房间）
      ctrl.onCallInviteAck({
        'call_id': 'c100',
        'room_token': '',
        'room_url': '',
      });

      expect(ctrl.session!.callId, 'c100');
      expect(
        ctrl.session!.state,
        CallState.connecting,
        reason: 'invite_ack 不应改变 connecting 状态',
      );
      expect(ctrl.callStopwatch.isRunning, isFalse);
    });

    test('inviteAck 收到后看门狗被取消', () {
      fakeAsync((async) {
        final ctrl = Get.find<CallController>();
        ctrl.directCallAgent('42', 'Bot', (_) {});
        async.flushMicrotasks();

        ctrl.onCallInviteAck({
          'call_id': 'c100',
          'room_token': '',
          'room_url': '',
        });

        // 超过看门狗时间但已被取消
        async.elapse(const Duration(seconds: 46));
        expect(ctrl.isInCall, isTrue, reason: 'invite_ack 取消看门狗后不应超时');
      });
    });

    test('hangup 在 connecting 状态正常结束通话', () async {
      final ctrl = Get.find<CallController>();
      await ctrl.directCallAgent('42', 'Bot', (_) {});
      expect(ctrl.session!.state, CallState.connecting);

      final sent = <Map<String, dynamic>>[];
      ctrl.hangup(sent.add);
      expect(sent, hasLength(1));
      expect(sent.first['cmd'], 'call:hangup');
      expect(ctrl.session!.state, CallState.ended);
    });

    test('inviteCall 仍使用 ringing（不受 connecting 影响）', () async {
      final ctrl = Get.find<CallController>();
      await ctrl.inviteCall('100', 'Alice', (_) {});
      expect(ctrl.session!.state, CallState.ringing);
      expect(ctrl.callStopwatch.isRunning, isFalse);
    });

    test('onCallPeerAnswered 正常切换 ringing→active（不受 connecting 影响）', () async {
      final ctrl = Get.find<CallController>();
      await ctrl.inviteCall('100', 'Alice', (_) {});

      ctrl.onCallInviteAck({
        'call_id': 'c200',
        'room_token': '',
        'room_url': '',
      });
      // inviteCall 路径：ringing 不变（不触发 connecting 逻辑）
      expect(ctrl.session!.state, CallState.ringing);

      ctrl.onCallPeerAnswered({'call_id': 'c200', 'mode': 'human'});
      expect(ctrl.session!.state, CallState.active);
      expect(ctrl.callStopwatch.isRunning, isTrue);
    });

    test('CallSession.isAIInvolved 在 connecting 时为 false', () {
      const s = CallSession(
        callId: '1',
        peerId: '1',
        peerName: '',
        callMode: 1,
        state: CallState.connecting,
      );
      expect(s.isAIInvolved, isFalse);
    });
  });
}
