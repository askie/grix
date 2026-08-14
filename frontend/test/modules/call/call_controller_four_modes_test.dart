import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import 'package:grix/modules/call/call_controller.dart';
import 'package:grix/modules/call/call_state.dart';
import 'package:grix/data/providers/feature_flag_service.dart';

/// 四档参与模型（待命/旁听/加入/接管）的切档逻辑测试。
/// 验证 callMode 推导 + setCallMode 各档切换发出的信令与最终静音/状态。
/// 注：测试无 LiveKit，_connectRoom 不会真正连房，但同步状态切换正常。
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

  CallController registerDelegated(String sid, String cid) {
    final ctrl = Get.find<CallController>();
    ctrl.onCallAiDelegated({
      'call_id': cid,
      'session_id': sid,
      'peer_name': 'V',
      'room_token': 't',
      'room_url': 'wss://lk.test',
    });
    return ctrl;
  }

  // 进入「旁听」态（aiDelegated + 静音）
  void enterListening(
    CallController ctrl,
    String sid,
    String cid,
    List<Map<String, dynamic>> sent,
  ) {
    ctrl.listenToDelegatedCall(sid, (p) => sent.add(p));
    ctrl.onCallListenAck(
        {'call_id': cid, 'room_token': 'tc', 'room_url': 'wss://lk.test'});
  }

  group('callMode 推导', () {
    test('待命：openStandby 后 callMode=standby、不连房、不发信令', () {
      final ctrl = registerDelegated('s1', 'c1');
      final sent = <Map<String, dynamic>>[];
      ctrl.openStandby('s1', (p) => sent.add(p));
      expect(ctrl.callMode, CallMode.standby);
      expect(ctrl.isStandby.value, isTrue);
      expect(ctrl.session?.callId, 'c1');
      expect(sent, isEmpty, reason: '待命不发任何信令、不连房');
    });

    test('旁听：listen_ack 后 callMode=listening（静音）', () {
      final ctrl = registerDelegated('s1', 'c1');
      final sent = <Map<String, dynamic>>[];
      enterListening(ctrl, 's1', 'c1', sent);
      expect(ctrl.session?.state, CallState.aiDelegated);
      expect(ctrl.isMuted.value, isTrue);
      expect(ctrl.callMode, CallMode.listening);
    });
  });

  group('四档切换', () {
    test('旁听→加入：开麦、不发接管信令、AI 仍代接（joined）', () {
      final ctrl = registerDelegated('s1', 'c1');
      final sent = <Map<String, dynamic>>[];
      enterListening(ctrl, 's1', 'c1', sent);
      sent.clear();
      ctrl.setCallMode(CallMode.joined, (p) => sent.add(p));
      expect(ctrl.isMuted.value, isFalse, reason: '加入应开麦');
      expect(ctrl.session?.state, CallState.aiDelegated,
          reason: '加入时 AI 仍代接（不静音）');
      expect(sent.where((p) => p['cmd'] == 'call:takeover'), isEmpty,
          reason: '加入不接管');
      expect(ctrl.callMode, CallMode.joined);
    });

    test('加入→接管：发 call:takeover、AI 静音（humanActive）、开麦', () {
      final ctrl = registerDelegated('s1', 'c1');
      final sent = <Map<String, dynamic>>[];
      enterListening(ctrl, 's1', 'c1', sent);
      ctrl.setCallMode(CallMode.joined, (_) {});
      sent.clear();
      ctrl.setCallMode(CallMode.takeover, (p) => sent.add(p));
      expect(sent.where((p) => p['cmd'] == 'call:takeover').isNotEmpty, isTrue);
      expect(ctrl.session?.state, CallState.humanActive);
      expect(ctrl.isMuted.value, isFalse);
      expect(ctrl.callMode, CallMode.takeover);
    });

    test('接管→旁听：发 call:hand_back、AI 恢复、关麦（listening）', () {
      final ctrl = registerDelegated('s1', 'c1');
      final sent = <Map<String, dynamic>>[];
      enterListening(ctrl, 's1', 'c1', sent);
      ctrl.setCallMode(CallMode.takeover, (_) {});
      expect(ctrl.session?.state, CallState.humanActive);
      sent.clear();
      ctrl.setCallMode(CallMode.listening, (p) => sent.add(p));
      expect(sent.where((p) => p['cmd'] == 'call:hand_back').isNotEmpty, isTrue);
      expect(ctrl.session?.state, CallState.aiDelegated);
      expect(ctrl.isMuted.value, isTrue);
      expect(ctrl.callMode, CallMode.listening);
    });

    test('接管→待命：先交回 AI 再离开（hand_back，避免访客被静音 AI 晾着）', () {
      final ctrl = registerDelegated('s1', 'c1');
      final sent = <Map<String, dynamic>>[];
      enterListening(ctrl, 's1', 'c1', sent);
      ctrl.setCallMode(CallMode.takeover, (_) {});
      sent.clear();
      ctrl.setCallMode(CallMode.standby, (p) => sent.add(p));
      expect(sent.where((p) => p['cmd'] == 'call:hand_back').isNotEmpty, isTrue,
          reason: '接管中切待命应先交回 AI');
      expect(ctrl.isStandby.value, isTrue);
      expect(ctrl.callMode, CallMode.standby);
    });
  });
}
