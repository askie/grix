import 'dart:io' show File, Platform;
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Tolerates minor antialiasing diffs across platforms when matching goldens.
class _TolerantComparator extends GoldenFileComparator {
  _TolerantComparator(this.basedir, {this.threshold = 0.02});

  final Uri basedir;
  final double threshold;

  @override
  Future<bool> compare(Uint8List imageBytes, Uri golden) async {
    final goldenFile = File.fromUri(basedir.resolve(golden.path));
    if (!goldenFile.existsSync()) {
      throw TestFailure('Golden file not found: ${goldenFile.path}');
    }
    final goldenBytes = goldenFile.readAsBytesSync();
    final diff = await _diffRatio(imageBytes, goldenBytes);
    return diff <= threshold;
  }

  @override
  Future<void> update(Uri golden, Uint8List imageBytes) async {
    final goldenFile = File.fromUri(basedir.resolve(golden.path));
    goldenFile.parent.createSync(recursive: true);
    goldenFile.writeAsBytesSync(imageBytes);
  }

  Future<double> _diffRatio(Uint8List a, Uint8List b) async {
    final imgA = await _decode(a);
    final imgB = await _decode(b);
    if (imgA.width != imgB.width || imgA.height != imgB.height) {
      return 1.0;
    }
    final bdA = await imgA.toByteData(format: ui.ImageByteFormat.rawRgba);
    final bdB = await imgB.toByteData(format: ui.ImageByteFormat.rawRgba);
    if (bdA == null || bdB == null) {
      return 1.0;
    }
    final total = bdA.lengthInBytes ~/ 4;
    var diffCount = 0;
    for (var i = 0; i < bdA.lengthInBytes; i += 4) {
      if (bdA.getUint32(i) != bdB.getUint32(i)) {
        diffCount++;
      }
    }
    imgA.dispose();
    imgB.dispose();
    return diffCount / total;
  }

  Future<ui.Image> _decode(Uint8List bytes) async {
    final codec = await ui.instantiateImageCodec(bytes);
    final frame = await codec.getNextFrame();
    codec.dispose();
    return frame.image;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUpAll(() {
    if (Platform.isLinux || Platform.isMacOS) {
      goldenFileComparator = _TolerantComparator(
        Uri.directory('test/modules/chat/'),
        threshold: 0.03,
      );
    }
  });

  setUp(() {
    SharedPreferences.setMockInitialValues(<String, Object>{});
  });

  const phone = Size(390, 844);
  const fullBody =
      '这是排队任务的完整正文，用来截取队列全文弹窗的 golden。'
      '内容比列表 48 字预览更长，确认弹窗展示全文并可滚动。';

  Future<void> pumpDialog(WidgetTester tester, {required ThemeData theme}) async {
    final item = EventLifecycleQueueItem(
      eventId: 'evt-golden',
      sessionId: 'sess-g',
      messageId: '',
      clientMsgId: '',
      contentPreview: '${fullBody.substring(0, 48)}...',
      state: 'queued',
      queuePosition: 1,
      actions: const <String>['cancel'],
      updatedAt: 1,
      content: fullBody,
    );
    await tester.binding.setSurfaceSize(phone);
    addTearDown(() => tester.binding.setSurfaceSize(null));
    await tester.pumpWidget(
      MaterialApp(
        theme: theme,
        home: Scaffold(
          body: Builder(
            builder: (context) => Center(
              child: ElevatedButton(
                onPressed: () => showQueueTaskContentDialog(context, item: item),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
  }

  testWidgets('queue task content dialog light golden', (tester) async {
    await pumpDialog(tester, theme: AppTheme.lightTheme);
    await expectLater(
      find.byType(MaterialApp),
      matchesGoldenFile('goldens/queue_task_content_light.png'),
    );
  });

  testWidgets('queue task content dialog dark golden', (tester) async {
    await pumpDialog(tester, theme: AppTheme.darkTheme);
    await expectLater(
      find.byType(MaterialApp),
      matchesGoldenFile('goldens/queue_task_content_dark.png'),
    );
  });
}
