import 'dart:io' show File, Platform;
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/themes/app_theme.dart';

// Tolerates up to 2% pixel difference to absorb sub-pixel font rendering
// variance across Linux CI image versions (same layout, different antialiasing).
class _TolerantComparator extends GoldenFileComparator {
  final Uri basedir;
  final double threshold;

  _TolerantComparator(this.basedir, {this.threshold = 0.02});

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
    if (imgA.width != imgB.width || imgA.height != imgB.height) return 1.0;
    final bdA = await imgA.toByteData(format: ui.ImageByteFormat.rawRgba);
    final bdB = await imgB.toByteData(format: ui.ImageByteFormat.rawRgba);
    if (bdA == null || bdB == null) return 1.0;
    final total = bdA.lengthInBytes ~/ 4;
    int diffCount = 0;
    for (var i = 0; i < bdA.lengthInBytes; i += 4) {
      if (bdA.getUint32(i) != bdB.getUint32(i)) diffCount++;
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

// Regression for the "invisible action buttons" bug: the global elevated-button
// theme uses Size.fromHeight (infinite min width); placed in a Row next to a
// Spacer (which forces an unbounded-width measurement) it threw
// "BoxConstraints forces an infinite width", collapsing the detail sheet's
// action row. This mirrors the FIXED _showSiteDetail action row (ElevatedButton
// pinned to a content-sized min width) and confirms it renders through a real
// isScrollControlled modal sheet with the real app theme.
void main() {
  setUpAll(() {
    if (Platform.isLinux) {
      goldenFileComparator = _TolerantComparator(
        Uri.directory('test/modules/profile/'),
        threshold: 0.02,
      );
    }
  });

  const phone = Size(390, 844);

  // Faithful mirror of the FIXED _showSiteDetail body.
  Widget detailBody(BuildContext ctx) => SafeArea(
    child: Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text('演示站点', style: Theme.of(ctx).textTheme.titleMedium),
              ),
              IconButton(onPressed: () {}, icon: const Icon(Icons.close)),
            ],
          ),
          const SizedBox(height: 10),
          const Text('Site Key: sk_demo_1234567890'),
          const SizedBox(height: 10),
          const Text('嵌入脚本'),
          const SizedBox(height: 6),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(10),
            decoration: BoxDecoration(
              color: Theme.of(ctx).colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(8),
            ),
            child: const SelectableText(
              '<script src="https://grix.dhf.pub/w.js" data-site-key="sk_demo"></script>',
            ),
          ),
          const SizedBox(height: 12),
          Row(
            children: [
              ElevatedButton(
                style: ElevatedButton.styleFrom(
                  minimumSize: const Size(0, AppTheme.btnHeightMedium),
                ),
                onPressed: () {},
                child: const Text('复制脚本'),
              ),
              const SizedBox(width: 8),
              TextButton(onPressed: () {}, child: const Text('轮换密钥')),
              const Spacer(),
              TextButton(onPressed: () {}, child: const Text('编辑')),
              TextButton(
                style: TextButton.styleFrom(
                  foregroundColor: Theme.of(ctx).colorScheme.error,
                ),
                onPressed: () {},
                child: const Text('删除'),
              ),
            ],
          ),
          const SizedBox(height: 6),
        ],
      ),
    ),
  );

  Future<void> shoot(WidgetTester tester, ThemeData theme, String file) async {
    tester.view.devicePixelRatio = 1.0;
    tester.view.physicalSize = phone;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      GetMaterialApp(
        theme: theme,
        home: Scaffold(
          body: Builder(
            builder: (ctx) {
              return Center(
                child: ElevatedButton(
                  onPressed: () => showModalBottomSheet<void>(
                    context: ctx,
                    isScrollControlled: true,
                    builder: detailBody,
                  ),
                  child: const Text('open'),
                ),
              );
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    expect(
      tester.takeException(),
      isNull,
      reason: 'detail sheet action row still crashes',
    );
    expect(find.text('复制脚本'), findsOneWidget);
    expect(find.text('轮换密钥'), findsOneWidget);
    expect(find.text('编辑'), findsOneWidget);
    expect(find.text('删除'), findsOneWidget);
    // golden 像素比对只在生成基准的规范平台（Linux CI）上执行。
    // 字形描边/抗锯齿在 Windows/macOS 与 Linux 之间存在亚像素差异，
    // 会导致非规范平台稳定误报（布局零位移、纯渲染差异）。
    // 上面的结构性断言（不崩溃 + 4 个按钮存在）在所有平台都会运行。
    if (Platform.isLinux) {
      await expectLater(
        find.byType(MaterialApp),
        matchesGoldenFile('goldens/$file'),
      );
    }
  }

  testWidgets('fixed detail sheet renders action buttons - light', (t) async {
    await shoot(t, AppTheme.lightTheme, 'ws_detail_fixed_light.png');
  });
  testWidgets('fixed detail sheet renders action buttons - dark', (t) async {
    await shoot(t, AppTheme.darkTheme, 'ws_detail_fixed_dark.png');
  });
}
