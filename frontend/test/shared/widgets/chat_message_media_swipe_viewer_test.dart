import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/chat_markdown_zoomable_image_viewport.dart';
import 'package:grix/shared/widgets/chat_message_media_swipe_viewer.dart';
import 'package:grix/shared/widgets/message_bubble.dart';

const _threeImageAttachments = <Map<String, dynamic>>[
  <String, dynamic>{
    'media_url': 'https://cdn.example.com/0.png',
    'attachment_type': 'image',
    'file_name': '0.png',
    'content_type': 'image/png',
  },
  <String, dynamic>{
    'media_url': 'https://cdn.example.com/1.png',
    'attachment_type': 'image',
    'file_name': '1.png',
    'content_type': 'image/png',
  },
  <String, dynamic>{
    'media_url': 'https://cdn.example.com/2.png',
    'attachment_type': 'image',
    'file_name': '2.png',
    'content_type': 'image/png',
  },
];

// Network images never resolve in the test environment (HttpClient calls are
// stubbed to fail), which leaves an indeterminate loading spinner animating
// forever inside the fullscreen preview. pumpAndSettle would spin until its
// internal timeout, so settle with a couple of fixed-duration pumps instead —
// same pattern already used by the existing markdown image preview tests.
Future<void> _settleDialog(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 200));
}

Future<void> _performDoubleTap(WidgetTester tester, Finder finder) async {
  final position = tester.getCenter(finder);
  await tester.tapAt(position);
  await tester.pump(const Duration(milliseconds: 80));
  await tester.tapAt(position);
  await _settleDialog(tester);
}

void main() {
  testWidgets('tapping an attachment tile opens swipe viewer at the tapped index', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-swipe-index',
            initialContent: '',
            messageExtra: <String, dynamic>{
              'attachments': _threeImageAttachments,
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('chat_message_attachment_tile_1')));
    await _settleDialog(tester);

    expect(find.byType(ChatMessageMediaSwipeViewer), findsOneWidget);
    expect(find.text('2/3'), findsOneWidget);
  });

  testWidgets('swiping left advances the page indicator to the next attachment', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-swipe-drag',
            initialContent: '',
            messageExtra: <String, dynamic>{
              'attachments': _threeImageAttachments,
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('chat_message_attachment_tile_0')));
    await _settleDialog(tester);
    expect(find.text('1/3'), findsOneWidget);

    await tester.drag(
      find.byKey(const Key('chat_message_media_swipe_viewer_pages')),
      const Offset(-600, 0),
    );
    await _settleDialog(tester);

    expect(find.text('2/3'), findsOneWidget);
  });

  testWidgets('single attachment does not show a page indicator', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-swipe-single',
            initialContent: '',
            messageExtra: <String, dynamic>{
              'attachments': <Map<String, dynamic>>[
                <String, dynamic>{
                  'media_url': 'https://cdn.example.com/only.png',
                  'attachment_type': 'image',
                  'file_name': 'only.png',
                  'content_type': 'image/png',
                },
              ],
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('chat_message_attachment_tile_0')));
    await _settleDialog(tester);

    expect(find.byType(ChatMessageMediaSwipeViewer), findsOneWidget);
    expect(
      find.byKey(const Key('chat_message_media_swipe_viewer_index')),
      findsNothing,
    );
  });

  testWidgets('plain file attachments are excluded from the swipe viewer data source', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-swipe-mixed-file',
            initialContent: '',
            messageExtra: <String, dynamic>{
              'attachments': <Map<String, dynamic>>[
                <String, dynamic>{
                  'media_url': 'https://cdn.example.com/0.png',
                  'attachment_type': 'image',
                  'file_name': '0.png',
                  'content_type': 'image/png',
                },
                <String, dynamic>{
                  'media_url': 'https://cdn.example.com/demo.pdf',
                  'attachment_type': 'file',
                  'file_name': 'demo.pdf',
                  'content_type': 'application/pdf',
                },
                <String, dynamic>{
                  'media_url': 'https://cdn.example.com/1.png',
                  'attachment_type': 'image',
                  'file_name': '1.png',
                  'content_type': 'image/png',
                },
              ],
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // Tapping the second image (grid index 2) should land on media index 1
    // (the plain file in between is skipped in the swipe data source).
    await tester.tap(find.byKey(const Key('chat_message_attachment_tile_2')));
    await _settleDialog(tester);

    expect(find.text('2/2'), findsOneWidget);
  });

  testWidgets('zooming into an image locks paging until zoom resets', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-swipe-zoom-lock',
            initialContent: '',
            messageExtra: <String, dynamic>{
              'attachments': _threeImageAttachments,
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('chat_message_attachment_tile_0')));
    await _settleDialog(tester);
    expect(find.text('1/3'), findsOneWidget);

    final viewportFinder = find.byType(ChatMarkdownZoomableImageViewport);
    expect(viewportFinder, findsOneWidget);

    // Zoom in via double tap: paging must lock, so a horizontal drag pans the
    // zoomed image instead of advancing to the next attachment.
    await _performDoubleTap(tester, viewportFinder);
    await tester.drag(
      find.byKey(const Key('chat_message_media_swipe_viewer_pages')),
      const Offset(-600, 0),
    );
    await _settleDialog(tester);
    expect(find.text('1/3'), findsOneWidget);

    // Reset zoom (second double tap): paging must unlock again.
    await _performDoubleTap(tester, viewportFinder);
    await tester.drag(
      find.byKey(const Key('chat_message_media_swipe_viewer_pages')),
      const Offset(-600, 0),
    );
    await _settleDialog(tester);
    expect(find.text('2/3'), findsOneWidget);
  });
}
