import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/call/voice_call_capability.dart';

void main() {
  tearDown(() => debugDefaultTargetPlatformOverride = null);

  group('VoiceCallCapability.isEnabled', () {
    test('桌面端（macOS/Windows）启用', () {
      for (final p in [
        TargetPlatform.macOS,
        TargetPlatform.windows,
      ]) {
        debugDefaultTargetPlatformOverride = p;
        expect(VoiceCallCapability.isEnabled, isTrue, reason: '$p 应启用');
      }
    });

    test('桌面端 Linux 启用', () {
      debugDefaultTargetPlatformOverride = TargetPlatform.linux;
      expect(VoiceCallCapability.isEnabled, isTrue, reason: 'Linux 应启用');
    });

    test('移动端（iOS/Android）禁用', () {
      for (final p in [
        TargetPlatform.iOS,
        TargetPlatform.android,
      ]) {
        debugDefaultTargetPlatformOverride = p;
        expect(VoiceCallCapability.isEnabled, isFalse, reason: '$p 应禁用');
      }
    });
  });
}
