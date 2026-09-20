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

  ChatImageEditorPageState editorState(WidgetTester tester) {
    return tester.state<ChatImageEditorPageState>(
      find.byType(ChatImageEditorPage),
    );
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

  testWidgets('defaults to crop tool on first frame', (tester) async {
    await pumpEditor(tester, physicalSize: const Size(430, 900));

    final state = editorState(tester);
    expect(state.debugIsCropToolSelected, isTrue);
    expect(find.text('裁剪工具：拖动四边或四角调整裁剪范围'), findsOneWidget);
    expect(find.byKey(const Key('chat_image_editor_undo_button')), findsOneWidget);

    // Crop tool chip is highlighted (blue fill).
    final cropLabel = find.text('裁剪').first;
    final cropContainer = tester.widget<Container>(
      find
          .ancestor(of: cropLabel, matching: find.byType(Container))
          .first,
    );
    final decoration = cropContainer.decoration! as BoxDecoration;
    expect(decoration.color, const Color(0xFF2D79F3));
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

    final ChatImageEditorPageState state = editorState(tester);

    // 注入确定性位图，避免依赖异步 asset decode 时序。
    final ui.Image seeded = await createSolidImage(width: 200, height: 120);
    state.debugReplaceDecodedImage(seeded);
    await tester.pump();

    final Size original = state.debugDecodedImageSize!;
    expect(original, const Size(200, 120));

    // Default tool is already crop; keep an explicit select for clarity.
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

  testWidgets('undo restores previous crop rect after drag change', (
    tester,
  ) async {
    await pumpEditor(tester, physicalSize: const Size(430, 900));
    final state = editorState(tester);
    final ui.Image seeded = await createSolidImage(width: 200, height: 120);
    state.debugReplaceDecodedImage(seeded);
    await tester.pump();

    const Rect first = Rect.fromLTWH(10, 10, 120, 80);
    const Rect second = Rect.fromLTWH(20, 20, 100, 60);
    state.debugPushCropChange(first);
    await tester.pump();
    expect(state.debugCropRect, first);

    state.debugPushCropChange(second);
    await tester.pump();
    expect(state.debugCropRect, second);

    state.debugUndo();
    await tester.pump();
    expect(state.debugCropRect, first);

    state.debugUndo();
    await tester.pump();
    expect(state.debugCropRect, isNull);
  });

  testWidgets('undo restores baked image and annotations', (tester) async {
    await pumpEditor(tester, physicalSize: const Size(430, 900));
    final state = editorState(tester);
    final ui.Image seeded = await createSolidImage(width: 200, height: 120);
    state.debugReplaceDecodedImage(seeded);
    await tester.pump();

    state.debugAddPenStroke(const <Offset>[
      Offset(10, 10),
      Offset(40, 40),
    ]);
    await tester.pump();
    expect(state.debugAnnotationCount, 1);

    state.debugSetCropRect(const Rect.fromLTWH(0, 0, 100, 60));
    await tester.pump();

    final Future<void> selectPen = state.debugSelectPenTool();
    for (var i = 0; i < 40; i++) {
      await tester.pump(const Duration(milliseconds: 20));
      if (state.debugDecodedImageSize == const Size(100, 60)) {
        break;
      }
    }
    await selectPen;

    expect(state.debugDecodedImageSize, const Size(100, 60));
    expect(state.debugAnnotationCount, 1);

    state.debugUndo();
    await tester.pump();

    expect(state.debugDecodedImageSize, const Size(200, 120));
    expect(state.debugCropRect, const Rect.fromLTWH(0, 0, 100, 60));
    expect(state.debugAnnotationCount, 1);
  });

  testWidgets('undo removes annotations one by one for each draw tool', (
    tester,
  ) async {
    await pumpEditor(tester, physicalSize: const Size(430, 900));
    final state = editorState(tester);
    final ui.Image seeded = await createSolidImage(width: 200, height: 120);
    state.debugReplaceDecodedImage(seeded);
    await tester.pump();

    await state.debugSelectPenTool();
    await tester.pump();

    state.debugAddPenStroke(const <Offset>[Offset(5, 5), Offset(15, 15)]);
    state.debugAddArrow(const Offset(20, 20), const Offset(60, 40));
    state.debugAddCircle(const Rect.fromLTWH(30, 30, 40, 40));
    state.debugAddRectangle(const Rect.fromLTWH(50, 10, 40, 30));
    state.debugAddText(const Offset(70, 70), 'hi');
    await tester.pump();
    expect(state.debugAnnotationCount, 5);

    for (var remaining = 4; remaining >= 0; remaining--) {
      state.debugUndo();
      await tester.pump();
      expect(state.debugAnnotationCount, remaining);
    }
  });

  testWidgets('undo restores clear and reset-crop', (tester) async {
    await pumpEditor(tester, physicalSize: const Size(430, 900));
    final state = editorState(tester);
    final ui.Image seeded = await createSolidImage(width: 200, height: 120);
    state.debugReplaceDecodedImage(seeded);
    await tester.pump();

    state.debugAddPenStroke(const <Offset>[Offset(5, 5), Offset(15, 15)]);
    state.debugAddArrow(const Offset(20, 20), const Offset(60, 40));
    state.debugPushCropChange(const Rect.fromLTWH(10, 10, 100, 60));
    await tester.pump();
    expect(state.debugAnnotationCount, 2);
    expect(state.debugCropRect, const Rect.fromLTWH(10, 10, 100, 60));

    state.debugClearAnnotations();
    await tester.pump();
    expect(state.debugAnnotationCount, 0);

    state.debugUndo();
    await tester.pump();
    expect(state.debugAnnotationCount, 2);

    state.debugResetCrop();
    await tester.pump();
    expect(state.debugCropRect, isNull);

    state.debugUndo();
    await tester.pump();
    expect(state.debugCropRect, const Rect.fromLTWH(10, 10, 100, 60));
  });

  testWidgets(
    'at max zoom crop edges stay inset by handle hit radius',
    (tester) async {
      await pumpEditor(tester, physicalSize: const Size(430, 900));
      final state = editorState(tester);
      final ui.Image seeded = await createSolidImage(width: 400, height: 240);
      state.debugReplaceDecodedImage(seeded);
      await tester.pump();

      final double hitRadius =
          ChatImageEditorPageState.debugCropHandleHitRadiusScreen;

      Future<void> assertAllHandlesInset(String label) async {
        await tester.pump();
        final Size canvas = state.debugLastCanvasSize;
        final Rect? cropDisplay = state.debugCropDisplayRect();
        expect(cropDisplay, isNotNull, reason: label);
        expect(
          cropDisplay!.left,
          greaterThanOrEqualTo(hitRadius - 0.5),
          reason: '$label left=${cropDisplay.left} canvas=$canvas '
              'scale=${state.debugViewportScale} offset=${state.debugViewportOffset}',
        );
        expect(
          cropDisplay.top,
          greaterThanOrEqualTo(hitRadius - 0.5),
          reason: '$label top',
        );
        expect(
          canvas.width - cropDisplay.right,
          greaterThanOrEqualTo(hitRadius - 0.5),
          reason: '$label right',
        );
        expect(
          canvas.height - cropDisplay.bottom,
          greaterThanOrEqualTo(hitRadius - 0.5),
          reason: '$label bottom',
        );
      }

      // Manual max zoom: near-side image edges stay padded when panned hard.
      state.debugSetViewportScale(4);
      await tester.pump();
      expect(state.debugViewportScale, 4);

      state.debugPanViewport(const Offset(9999, 9999));
      await tester.pump();
      final Rect topLeftImage = state.debugLastDisplayRect!;
      expect(topLeftImage.left, greaterThanOrEqualTo(hitRadius - 0.5));
      expect(topLeftImage.top, greaterThanOrEqualTo(hitRadius - 0.5));

      state.debugPanViewport(const Offset(-9999, -9999));
      await tester.pump();
      final Size canvas = state.debugLastCanvasSize;
      final Rect bottomRightImage = state.debugLastDisplayRect!;
      expect(
        canvas.width - bottomRightImage.right,
        greaterThanOrEqualTo(hitRadius - 0.5),
        reason:
            'right margin: display=$bottomRightImage canvas=$canvas '
            'offset=${state.debugViewportOffset}',
      );
      expect(
        canvas.height - bottomRightImage.bottom,
        greaterThanOrEqualTo(hitRadius - 0.5),
        reason: 'bottom margin',
      );

      // Small crop at image corners + auto-fit: all four handles visible.
      state.debugSetCropRect(const Rect.fromLTWH(0, 0, 100, 60));
      state.debugFitViewportToCropRect();
      await assertAllHandlesInset('crop top-left auto-fit');
      expect(state.debugViewportScale, greaterThan(1));

      state.debugSetCropRect(const Rect.fromLTWH(300, 180, 100, 60));
      state.debugFitViewportToCropRect();
      await assertAllHandlesInset('crop bottom-right auto-fit');
    },
  );

  testWidgets(
    'crop handle margins stay inset at 100%, 142%, 400%, fit corners',
    (tester) async {
      await pumpEditor(tester, physicalSize: const Size(430, 900));
      final state = editorState(tester);
      final ui.Image seeded = await createSolidImage(width: 400, height: 240);
      state.debugReplaceDecodedImage(seeded);
      // debugReplaceDecodedImage does not setState; force a rebuild + layout.
      state.debugSetViewportScale(1);
      await tester.pump();
      await tester.pump();

      const double minInset = 33.5;

      void assertMargins(String label) {
        final Size canvas = state.debugLastCanvasSize;
        final Rect? cropDisplay = state.debugCropDisplayRect();
        expect(cropDisplay, isNotNull, reason: label);
        final Rect crop = cropDisplay!;
        final double left = crop.left;
        final double top = crop.top;
        final double right = canvas.width - crop.right;
        final double bottom = canvas.height - crop.bottom;
        final String reason =
            '$label L=${left.toStringAsFixed(2)} T=${top.toStringAsFixed(2)} '
            'R=${right.toStringAsFixed(2)} B=${bottom.toStringAsFixed(2)} '
            'scale=${state.debugViewportScale}';
        expect(left, greaterThanOrEqualTo(minInset), reason: reason);
        expect(top, greaterThanOrEqualTo(minInset), reason: reason);
        expect(right, greaterThanOrEqualTo(minInset), reason: reason);
        expect(bottom, greaterThanOrEqualTo(minInset), reason: reason);
      }

      // First frame: default full-image crop, tool is crop.
      expect(state.debugIsCropToolSelected, isTrue);
      assertMargins('100% first-frame full crop');

      state.debugSetCropRect(const Rect.fromLTWH(40, 30, 200, 120));
      state.debugSetViewportScale(1);
      await tester.pump();
      assertMargins('100% mid crop');

      state.debugSetViewportScale(1.42);
      await tester.pump();
      assertMargins('142% mid crop');

      // 400%: pan to each corner — near-side image edges (full-crop handles)
      // must stay inset; far sides overflow by design.
      state.debugSetViewportScale(4);
      await tester.pump();
      expect(state.debugViewportScale, 4);

      state.debugPanViewport(const Offset(9999, 9999));
      await tester.pump();
      final Rect tl = state.debugLastDisplayRect!;
      expect(
        tl.left,
        greaterThanOrEqualTo(minInset),
        reason: '400% pan TL L=${tl.left.toStringAsFixed(2)} '
            'T=${tl.top.toStringAsFixed(2)}',
      );
      expect(
        tl.top,
        greaterThanOrEqualTo(minInset),
        reason: '400% pan TL L=${tl.left.toStringAsFixed(2)} '
            'T=${tl.top.toStringAsFixed(2)}',
      );

      state.debugPanViewport(const Offset(-9999, -9999));
      await tester.pump();
      final Size canvas = state.debugLastCanvasSize;
      final Rect br = state.debugLastDisplayRect!;
      final double right = canvas.width - br.right;
      final double bottom = canvas.height - br.bottom;
      expect(
        right,
        greaterThanOrEqualTo(minInset),
        reason: '400% pan BR R=${right.toStringAsFixed(2)} '
            'B=${bottom.toStringAsFixed(2)}',
      );
      expect(
        bottom,
        greaterThanOrEqualTo(minInset),
        reason: '400% pan BR R=${right.toStringAsFixed(2)} '
            'B=${bottom.toStringAsFixed(2)}',
      );

      state.debugSetCropRect(const Rect.fromLTWH(0, 0, 120, 80));
      state.debugFitViewportToCropRect();
      await tester.pump();
      expect(state.debugViewportScale, greaterThan(1));
      assertMargins('fit crop top-left');

      state.debugSetCropRect(const Rect.fromLTWH(280, 160, 120, 80));
      state.debugFitViewportToCropRect();
      await tester.pump();
      expect(state.debugViewportScale, greaterThan(1));
      assertMargins('fit crop bottom-right');
    },
  );
}
