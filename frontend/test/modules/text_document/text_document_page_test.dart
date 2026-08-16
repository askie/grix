import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/text_document/models/text_document_descriptor.dart';
import 'package:grix/modules/text_document/text_document_page.dart';

void main() {
  tearDown(Get.reset);

  Widget wrap(Widget home) {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      fallbackLocale: const Locale('en', 'US'),
      home: home,
    );
  }

  testWidgets(
    'Markdown document renders rich preview even when it contains HTML',
    (tester) async {
      const source = '# Rendered heading\n\n<div>embedded html</div>';
      await tester.pumpWidget(
        wrap(
          TextDocumentPage(
            descriptor: const TextDocumentDescriptor(
              handle: 'markdown-test',
              displayName: 'PLAN.md',
              mimeType: 'text/markdown',
              canWrite: false,
              source: TextDocumentSource.remoteFileBrowser,
            ),
            bytes: Uint8List.fromList(utf8.encode(source)),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Rendered heading'), findsOneWidget);
      expect(find.text(source), findsNothing, reason: '不应把整篇 Markdown 当源码显示');
      expect(find.byTooltip('View source'), findsOneWidget);
    },
  );

  testWidgets(
    'plain text document can enter edit mode and guards dirty close',
    (tester) async {
      await tester.pumpWidget(
        wrap(
          TextDocumentPage(
            descriptor: const TextDocumentDescriptor(
              handle: 'test',
              displayName: 'main.go',
              mimeType: 'text/plain',
              canWrite: false,
              source: TextDocumentSource.importedCopy,
            ),
            bytes: Uint8List.fromList(utf8.encode('package main\n')),
          ),
        ),
      );

      expect(find.text('package main\n'), findsOneWidget);
      await tester.tap(find.byIcon(Icons.edit_outlined));
      await tester.pump();
      await tester.enterText(find.byType(TextField), 'package changed\n');
      await tester.pump();

      expect(find.byIcon(Icons.save_outlined), findsOneWidget);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();

      expect(find.text('Unsaved changes'), findsOneWidget);
      expect(find.text('Discard'), findsOneWidget);
    },
  );
}
