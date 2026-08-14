import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/audio_session_util.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const channel = MethodChannel('pub.dhf.grix/audio_session');
  final messenger =
      TestWidgetsFlutterBinding.instance.defaultBinaryMessenger;

  final List<String> calls = <String>[];

  setUp(() {
    calls.clear();
    messenger.setMockMethodCallHandler(channel, (call) async {
      calls.add(call.method);
      return null;
    });
  });

  tearDown(() {
    messenger.setMockMethodCallHandler(channel, null);
    debugDefaultTargetPlatformOverride = null;
  });

  test('iOS 上 release 会通过原生通道归还音频会话', () async {
    debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
    await AudioSessionReleaser.release();
    expect(calls, <String>['releaseAudioSession']);
  });

  test('非 iOS 平台 release 不触碰原生通道（空操作）', () async {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    await AudioSessionReleaser.release();
    expect(calls, isEmpty);
  });
}
