import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/chat_message_video_preview_dialog.dart';
import 'package:video_player/video_player.dart';
// ignore: depend_on_referenced_packages
import 'package:video_player_platform_interface/video_player_platform_interface.dart';

const Key _bottomControlsKey = Key('video_preview_bottom_controls');
const Key _centerPlayKey = Key('video_preview_center_play');

/// 假的视频平台实现：创建播放器后立刻投递一个 initialized 事件，
/// 让 `VideoPlayerController.initialize()` 在 widget 测试里能正常完成。
class _FakeVideoPlayerPlatform extends VideoPlayerPlatform {
  final Map<int, StreamController<VideoEvent>> _events =
      <int, StreamController<VideoEvent>>{};
  int _nextPlayerId = 1;
  Duration _position = Duration.zero;

  @override
  Future<void> init() async {}

  @override
  Future<int?> createWithOptions(VideoCreationOptions options) async {
    final int playerId = _nextPlayerId++;
    final controller = StreamController<VideoEvent>();
    _events[playerId] = controller;
    controller.add(
      VideoEvent(
        eventType: VideoEventType.initialized,
        duration: const Duration(seconds: 30),
        size: const Size(640, 360),
      ),
    );
    return playerId;
  }

  @override
  Stream<VideoEvent> videoEventsFor(int playerId) => _events[playerId]!.stream;

  @override
  Future<void> dispose(int playerId) async {
    await _events.remove(playerId)?.close();
  }

  @override
  Future<void> setLooping(int playerId, bool looping) async {}

  @override
  Future<void> play(int playerId) async {}

  @override
  Future<void> pause(int playerId) async {}

  @override
  Future<void> setVolume(int playerId, double volume) async {}

  @override
  Future<void> setPlaybackSpeed(int playerId, double speed) async {}

  @override
  Future<void> seekTo(int playerId, Duration position) async {
    _position = position;
  }

  @override
  Future<Duration> getPosition(int playerId) async => _position;

  @override
  Future<void> setMixWithOthers(bool mixWithOthers) async {}

  /// 模拟播放到结尾：平台侧上报 completed，controller 会自动暂停并 seek 到结尾。
  void emitCompleted() {
    for (final controller in _events.values) {
      controller.add(VideoEvent(eventType: VideoEventType.completed));
    }
  }

  @override
  Widget buildViewWithOptions(VideoViewOptions options) =>
      const SizedBox.expand();
}

/// 控制条 3 秒后自动隐藏；播放器初始化本身也要几次 pump 才落定。
Future<void> _settle(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 100));
  await tester.pump(const Duration(milliseconds: 300));
}

Future<void> _pumpDialog(WidgetTester tester) async {
  // 竖屏尺寸：视频按 16:9 居中后，画面上下留出足够大的黑边可供点击。
  tester.view.physicalSize = const Size(800, 1600);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  // 直接挂内层播放器：外层 dialog 只负责查磁盘缓存/反代改写，
  // 那条链路在 widget 测试里没有 path_provider 支撑，跑不完。
  await tester.pumpWidget(
    MaterialApp(
      home: VideoPreviewPlayer(
        playbackUri: Uri.parse('https://cdn.example.com/demo.mp4'),
        originalUri: Uri.parse('https://cdn.example.com/demo.mp4'),
        cachedPath: null,
        title: 'demo.mp4',
        autoPlay: true,
        onScrubbingChanged: null,
      ),
    ),
  );
  await _settle(tester);
}

bool _isPlaying(WidgetTester tester) =>
    tester.widget<VideoPlayer>(find.byType(VideoPlayer)).controller.value.isPlaying;

double _opacityOf(WidgetTester tester, Key key) =>
    tester.widget<AnimatedOpacity>(find.byKey(key)).opacity;

/// 控制条自动隐藏后画面上剩下的可见落点：视频画面正中。
Offset _videoCenter(WidgetTester tester) =>
    tester.getCenter(find.byType(VideoPlayer));

void main() {
  late _FakeVideoPlayerPlatform fakePlatform;

  setUp(() {
    fakePlatform = _FakeVideoPlayerPlatform();
    VideoPlayerPlatform.instance = fakePlatform;
  });

  testWidgets('tapping the video while playing re-shows the hidden controls', (
    WidgetTester tester,
  ) async {
    await _pumpDialog(tester);
    expect(_isPlaying(tester), isTrue);

    // 播放 3 秒后控制条自动隐藏，此时整屏只剩顶部按钮。
    await tester.pump(const Duration(seconds: 4));
    expect(_opacityOf(tester, _bottomControlsKey), 0);

    await tester.tapAt(_videoCenter(tester));
    await _settle(tester);

    expect(_opacityOf(tester, _bottomControlsKey), 1);
    expect(_isPlaying(tester), isTrue);
  });

  testWidgets('tapping the video while controls are visible pauses playback', (
    WidgetTester tester,
  ) async {
    await _pumpDialog(tester);
    expect(_isPlaying(tester), isTrue);
    expect(_opacityOf(tester, _bottomControlsKey), 1);

    await tester.tapAt(_videoCenter(tester));
    await _settle(tester);

    expect(_isPlaying(tester), isFalse);
    expect(_opacityOf(tester, _centerPlayKey), 1);
    expect(_opacityOf(tester, _bottomControlsKey), 1);
  });

  testWidgets('tapping the center play button resumes playback', (
    WidgetTester tester,
  ) async {
    await _pumpDialog(tester);

    await tester.tapAt(_videoCenter(tester));
    await _settle(tester);
    expect(_isPlaying(tester), isFalse);

    await tester.tap(find.byKey(_centerPlayKey));
    await _settle(tester);

    expect(_isPlaying(tester), isTrue);
    expect(_opacityOf(tester, _centerPlayKey), 0);
  });

  testWidgets('the center play button comes back when playback completes', (
    WidgetTester tester,
  ) async {
    await _pumpDialog(tester);
    expect(_isPlaying(tester), isTrue);
    expect(_opacityOf(tester, _centerPlayKey), 0);

    fakePlatform.emitCompleted();
    await _settle(tester);

    expect(_isPlaying(tester), isFalse);
    expect(_opacityOf(tester, _centerPlayKey), 1);
  });

  testWidgets('tapping the black bar above the video also toggles controls', (
    WidgetTester tester,
  ) async {
    await _pumpDialog(tester);
    await tester.pump(const Duration(seconds: 4));
    expect(_opacityOf(tester, _bottomControlsKey), 0);

    // 视频画面上方的黑边：在顶部栏下方，仍属于视频区域。
    final Rect videoRect = tester.getRect(find.byType(VideoPlayer));
    final Offset blackBar = Offset(videoRect.center.dx, videoRect.top - 120);
    await tester.tapAt(blackBar);
    await _settle(tester);

    expect(_opacityOf(tester, _bottomControlsKey), 1);
    expect(_isPlaying(tester), isTrue);
  });
}
