import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/shared/widgets/message_bubble.dart';

void main() {
  setUpAll(() {
    Get.addTranslations(AppTranslations().keys);
    Get.locale = const Locale('zh', 'CN');
  });

  tearDownAll(Get.reset);

  testWidgets(
    'message bubble renders attachments grid and hides generated markdown',
    (WidgetTester tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: MessageBubble(
              msgId: 'msg-attachments-grid',
              initialContent:
                  '![image](<https://cdn.example.com/a.png>)\n[demo.pdf](<https://cdn.example.com/demo.pdf>)',
              messageExtra: <String, dynamic>{
                'attachments': <Map<String, dynamic>>[
                  <String, dynamic>{
                    'media_url': 'https://cdn.example.com/a.png',
                    'attachment_type': 'image',
                    'file_name': 'a.png',
                    'content_type': 'image/png',
                  },
                  <String, dynamic>{
                    'media_url': 'https://cdn.example.com/demo.pdf',
                    'attachment_type': 'file',
                    'file_name': 'demo.pdf',
                    'content_type': 'application/pdf',
                  },
                ],
              },
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('chat_message_attachment_grid')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('chat_message_attachment_tile_0')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('chat_message_attachment_tile_1')),
        findsOneWidget,
      );
      expect(find.text('demo.pdf'), findsOneWidget);
      expect(
        find.text('![image](<https://cdn.example.com/a.png>)'),
        findsNothing,
      );
    },
  );

  testWidgets(
    'message bubble with text and attachments hides attachment markdown from text area',
    (WidgetTester tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: MessageBubble(
              msgId: 'msg-text-attachments',
              initialContent:
                  'Check these out\n![image](<https://cdn.example.com/a.png>)',
              messageExtra: <String, dynamic>{
                'attachments': <Map<String, dynamic>>[
                  <String, dynamic>{
                    'media_url': 'https://cdn.example.com/a.png',
                    'attachment_type': 'image',
                    'file_name': 'a.png',
                    'content_type': 'image/png',
                  },
                ],
              },
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Grid should render
      expect(
        find.byKey(const Key('chat_message_attachment_grid')),
        findsOneWidget,
      );

      // User text should be visible
      expect(find.text('Check these out'), findsOneWidget);

      // Raw markdown syntax should NOT appear as text
      expect(
        find.text('![image](<https://cdn.example.com/a.png>)'),
        findsNothing,
      );
    },
  );

  testWidgets(
    'message bubble exposes overflow attachments in dialog when count exceeds nine',
    (WidgetTester tester) async {
      final attachments = List<Map<String, dynamic>>.generate(10, (index) {
        return <String, dynamic>{
          'media_url': 'https://cdn.example.com/$index.png',
          'attachment_type': 'image',
          'file_name': '$index.png',
          'content_type': 'image/png',
        };
      });

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: MessageBubble(
              msgId: 'msg-attachments-overflow',
              initialContent: List<String>.generate(
                10,
                (index) => '![image](<https://cdn.example.com/$index.png>)',
              ).join('\n'),
              messageExtra: <String, dynamic>{'attachments': attachments},
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('chat_message_attachment_tile_8')),
        findsOneWidget,
      );
      expect(find.text('+1'), findsOneWidget);
      expect(
        find.byKey(const Key('chat_message_attachment_tile_9')),
        findsNothing,
      );

      await tester.tap(find.byKey(const Key('chat_message_attachment_tile_8')));
      await tester.pumpAndSettle();

      expect(find.text('全部附件'), findsOneWidget);
      expect(
        find.byKey(const Key('chat_message_attachment_tile_9')),
        findsOneWidget,
      );
    },
  );

  testWidgets(
    'single video attachment renders as a tall poster tile, not a flat bar',
    (WidgetTester tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: MessageBubble(
              msgId: 'msg-single-video',
              initialContent: '[clip.mp4](<https://cdn.example.com/clip.mp4>)',
              messageExtra: <String, dynamic>{
                'attachments': <Map<String, dynamic>>[
                  <String, dynamic>{
                    'media_url': 'https://cdn.example.com/clip.mp4',
                    'attachment_type': 'video',
                    'file_name': 'clip.mp4',
                    'content_type': 'video/mp4',
                  },
                ],
              },
            ),
          ),
        ),
      );
      // First frame is enough: tile geometry is set at build time and does not
      // depend on the (mocked-out) video player finishing initialization.
      await tester.pump();

      final tile = find.byKey(const Key('chat_message_attachment_tile_0'));
      expect(tile, findsOneWidget);

      // The old behaviour collapsed single non-image files to an 80px-tall bar.
      // A video must now get a proper poster-sized tile instead.
      final size = tester.getSize(tile);
      expect(size.height, greaterThan(120));
    },
  );
}
