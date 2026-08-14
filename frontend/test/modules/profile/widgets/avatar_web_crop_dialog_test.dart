import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image_cropper/image_cropper.dart';

import 'package:grix/modules/profile/widgets/avatar_web_crop_dialog.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<Future<String?>> openDialog(
    WidgetTester tester, {
    required Future<String?> Function() crop,
    required Duration cropTimeout,
    void Function(RotationAngle angle)? onRotate,
    VoidCallback? onInit,
  }) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(1200, 1600);
    addTearDown(() {
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    });

    final resultCompleter = Completer<String?>();

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) {
            return Scaffold(
              body: Center(
                child: ElevatedButton(
                  onPressed: () {
                    showDialog<String?>(
                      context: context,
                      barrierDismissible: false,
                      builder: (_) => AvatarWebCropDialog(
                        cropper: Container(key: const ValueKey('cropper')),
                        initCropper: onInit ?? () {},
                        crop: crop,
                        rotate: onRotate ?? (_) {},
                        sourcePath: 'blob:source',
                        cropTimeout: cropTimeout,
                        translations: const WebTranslations.en(),
                      ),
                    ).then(resultCompleter.complete);
                  },
                  child: const Text('open'),
                ),
              ),
            );
          },
        ),
      ),
    );

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('cropper')), findsOneWidget);
    return resultCompleter.future;
  }

  testWidgets('crop success returns cropped image path', (tester) async {
    int initCalls = 0;
    final resultFuture = await openDialog(
      tester,
      crop: () async => 'blob:cropped',
      cropTimeout: const Duration(milliseconds: 150),
      onInit: () {
        initCalls++;
      },
    );

    expect(initCalls, 1);
    await tester.tap(find.text('Crop'));
    await tester.pumpAndSettle();

    expect(await resultFuture, 'blob:cropped');
  });

  testWidgets('crop failure falls back to source image path', (tester) async {
    final resultFuture = await openDialog(
      tester,
      crop: () async {
        throw Exception('crop failed');
      },
      cropTimeout: const Duration(milliseconds: 150),
    );

    await tester.tap(find.text('Crop'));
    await tester.pumpAndSettle();

    expect(await resultFuture, 'blob:source');
  });

  testWidgets('crop timeout falls back to source image path', (tester) async {
    final pendingCrop = Completer<String?>();
    final resultFuture = await openDialog(
      tester,
      crop: () => pendingCrop.future,
      cropTimeout: const Duration(milliseconds: 80),
    );

    await tester.tap(find.text('Crop'));
    await tester.pump(const Duration(milliseconds: 120));
    await tester.pumpAndSettle();

    expect(await resultFuture, 'blob:source');
  });
}
