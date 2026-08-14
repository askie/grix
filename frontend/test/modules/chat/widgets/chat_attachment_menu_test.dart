import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/widgets/chat_attachment_menu.dart';

void main() {
  testWidgets('attachment menu no overflow at large text scale',
      (tester) async {
    tester.view.physicalSize = const Size(390, 600);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      MaterialApp(
        home: MediaQuery(
          data: const MediaQueryData(textScaler: TextScaler.linear(1.3)),
          child: Scaffold(
            body: ChatAttachmentMenu(
              enabled: true,
              onImageTap: () {},
              onVideoTap: () {},
              onFileTap: () {},
              showHideSendAction: true,
              onHideSendTap: () {},
              onVoiceBrainTap: () {},
              onBrowseFilesTap: () {},
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
  });
}
