import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:grix/modules/profile/widgets/avatar_desktop_crop_dialog.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<Future<Uint8List?>> openDialog(WidgetTester tester) async {
    final byteData = await rootBundle.load('assets/icons/app_logo_cropped.png');
    final sampleImageBytes = byteData.buffer.asUint8List();

    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(1200, 1600);
    addTearDown(() {
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    });

    final resultCompleter = Completer<Uint8List?>();

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) {
            return Scaffold(
              body: Center(
                child: ElevatedButton(
                  onPressed: () {
                    showDialog<Uint8List>(
                      context: context,
                      barrierDismissible: false,
                      builder: (_) => AvatarDesktopCropDialog(
                        imageBytes: sampleImageBytes,
                        title: 'Crop Avatar',
                        hint: 'Drag and zoom image, then save',
                        zoomLabel: 'Zoom',
                        zoomOutLabel: 'Min',
                        zoomInLabel: 'Max',
                        cancelLabel: 'Cancel',
                        saveLabel: 'Save',
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
    await tester.pump();
    for (var i = 0; i < 30; i++) {
      if (find.text('Save').evaluate().isNotEmpty) {
        break;
      }
      await tester.pump(const Duration(milliseconds: 100));
    }
    expect(find.text('Save'), findsOneWidget);
    return resultCompleter.future;
  }

  testWidgets('returns null when cancel is tapped', (tester) async {
    final resultFuture = await openDialog(tester);
    Uint8List? result;
    var completed = false;
    resultFuture.then((value) {
      result = value;
      completed = true;
    });

    await tester.tap(find.text('Cancel'));
    for (var i = 0; i < 20; i++) {
      if (completed) {
        break;
      }
      await tester.pump(const Duration(milliseconds: 50));
    }

    expect(completed, isTrue);
    expect(result, isNull);
  });
}
