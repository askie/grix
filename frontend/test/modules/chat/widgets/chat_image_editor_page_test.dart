import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/widgets/chat_image_editor_page.dart';
import 'package:get/get.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<void> pumpEditor(
    WidgetTester tester, {
    Size physicalSize = const Size(320, 800),
  }) async {
    final byteData = await rootBundle.load('assets/icons/app_logo_cropped.png');
    final imageBytes = byteData.buffer.asUint8List();

    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = physicalSize;
    addTearDown(() {
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
      Get.reset();
    });

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: ChatImageEditorPage(
          imageBytes: imageBytes,
          fileName: 'image.png',
          contentType: 'image/png',
        ),
      ),
    );

    await tester.pump();
    for (var i = 0; i < 20; i++) {
      if (find.byType(CircularProgressIndicator).evaluate().isEmpty) {
        break;
      }
      await tester.pump(const Duration(milliseconds: 50));
    }
  }

  Future<ui.Image> createSolidImage({
    required int width,
    required int height,
  }) async {
    final ui.PictureRecorder recorder = ui.PictureRecorder();
    final Canvas canvas = Canvas(recorder);
    canvas.drawRect(
      Rect.fromLTWH(0, 0, width.toDouble(), height.toDouble()),
      Paint()..color = const Color(0xFF2288FF),
    );
    final ui.Picture picture = recorder.endRecording();
    return picture.toImage(width, height);
  }

  testWidgets('does not overflow bottom bar on narrow screens', (tester) async {
    await pumpEditor(tester);

    expect(find.text('原图上传（忽略编辑）'), findsOneWidget);
    expect(find.text('撤销'), findsOneWidget);
    expect(find.text('清空'), findsOneWidget);
    expect(find.text('重置裁剪'), findsOneWidget);
    expect(
      find.byKey(const Key('chat_image_editor_zoom_in_button')),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('supports zoom controls in image editor', (tester) async {
    await pumpEditor(tester, physicalSize: const Size(430, 900));

    final zoomInFinder = find.byKey(
      const Key('chat_image_editor_zoom_in_button'),
    );
    final zoomResetFinder = find.byKey(
      const Key('chat_image_editor_zoom_reset_button'),
    );

    expect(find.text('100%'), findsOneWidget);

    final zoomInButton = tester.widget<IconButton>(zoomInFinder);
    zoomInButton.onPressed!.call();
    await tester.pump();

    expect(find.text('125%'), findsOneWidget);

    final zoomResetButton = tester.widget<TextButton>(zoomResetFinder);
    zoomResetButton.onPressed!.call();
    await tester.pump();

    expect(find.text('100%'), findsOneWidget);
  });

  testWidgets('clips canvas above bottom toolbar when zoomed', (tester) async {
    await pumpEditor(tester, physicalSize: const Size(430, 900));

    final canvasClipFinder = find.byKey(
      const Key('chat_image_editor_canvas_clip'),
    );
    expect(canvasClipFinder, findsOneWidget);

    final zoomInButton = tester.widget<IconButton>(
      find.byKey(const Key('chat_image_editor_zoom_in_button')),
    );
    // Zoom past 200% so the painted image would overflow the canvas bounds
    // without ClipRect, bleeding under the opaque bottom toolbar.
    for (var i = 0; i < 5; i++) {
      zoomInButton.onPressed!.call();
      await tester.pump();
    }
    expect(find.text('225%'), findsOneWidget);

    final clipRect = tester.getRect(canvasClipFinder);
    final toolBarLabel = tester.getRect(find.text('画笔').first);
    expect(clipRect.bottom, lessThanOrEqualTo(toolBarLabel.top));
    expect(tester.takeException(), isNull);
  });

  testWidgets('leaving crop tool bakes crop and refits at 100%', (
    tester,
  ) async {
    await pumpEditor(tester, physicalSize: const Size(430, 900));

    final ChatImageEditorPageState state = tester
        .state<ChatImageEditorPageState>(find.byType(ChatImageEditorPage));

    // 注入确定性位图，避免依赖异步 asset decode 时序。
    final ui.Image seeded = await createSolidImage(width: 200, height: 120);
    state.debugReplaceDecodedImage(seeded);
    await tester.pump();

    final Size original = state.debugDecodedImageSize!;
    expect(original, const Size(200, 120));

    final Future<void> selectCrop = state.debugSelectCropTool();
    await tester.pump();
    await selectCrop;

    state.debugSetCropRect(const Rect.fromLTWH(0, 0, 100, 60));
    await tester.pump();

    final Future<void> selectPen = state.debugSelectPenTool();
    for (var i = 0; i < 40; i++) {
      await tester.pump(const Duration(milliseconds: 20));
      final Size? size = state.debugDecodedImageSize;
      if (size == const Size(100, 60)) {
        break;
      }
    }
    await selectPen;

    expect(find.text('100%'), findsOneWidget);
    expect(state.debugViewportScale, 1);
    expect(state.debugDecodedImageSize, const Size(100, 60));
    expect(tester.takeException(), isNull);
  });
}
