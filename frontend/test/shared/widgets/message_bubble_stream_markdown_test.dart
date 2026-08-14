import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:markdown_widget/markdown_widget.dart';
import 'package:grix/shared/widgets/chat_markdown_ast_view.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:grix/shared/widgets/stream_pending_indicator.dart';

class _BubbleSpec {
  const _BubbleSpec({
    required this.msgId,
    required this.isStreaming,
    required this.content,
  });

  final String msgId;
  final bool isStreaming;
  final String content;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    MessageStreamController.resetForTest();
    MessageBubble.resetFinalRenderCacheForTest();
  });

  tearDown(() {
    MessageStreamController.resetForTest();
    MessageBubble.resetFinalRenderCacheForTest();
  });

  Widget buildBubble({
    required String msgId,
    required bool isStreaming,
    required String content,
    double? width,
  }) {
    return MaterialApp(
      home: Scaffold(
        body: width == null
            ? MessageBubble(
                msgId: msgId,
                initialContent: content,
                isStreaming: isStreaming,
                isMine: false,
              )
            : SizedBox(
                width: width,
                child: MessageBubble(
                  msgId: msgId,
                  initialContent: content,
                  isStreaming: isStreaming,
                  isMine: false,
                ),
              ),
      ),
    );
  }

  Widget buildBubbleList(List<_BubbleSpec> bubbles) {
    return MaterialApp(
      home: Scaffold(
        body: Column(
          children: [
            for (final bubble in bubbles)
              MessageBubble(
                key: ValueKey('bubble:${bubble.msgId}'),
                msgId: bubble.msgId,
                initialContent: bubble.content,
                isStreaming: bubble.isStreaming,
                isMine: false,
              ),
          ],
        ),
      ),
    );
  }

  Finder findBubbleContainer() {
    return find.byWidgetPredicate((widget) {
      if (widget is! Container) {
        return false;
      }
      final decoration = widget.decoration;
      return decoration is BoxDecoration &&
          decoration.color == Colors.white &&
          decoration.borderRadius == BorderRadius.circular(12);
    });
  }

  testWidgets(
    'streaming markdown preview uses lightweight text path before final rerender',
    (WidgetTester tester) async {
      const msgId = 'stream_markdown_preview';

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: true, content: ''),
      );

      expect(find.byType(StreamPendingIndicator), findsOneWidget);

      MessageStreamController.addChunk(msgId, '# Title');
      await tester.pump(const Duration(milliseconds: 100));

      expect(find.byType(StreamPendingIndicator), findsNothing);
      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(Text), findsWidgets);

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: false, content: '# Title'),
      );
      await tester.pumpAndSettle();

      expect(find.byType(MarkdownWidget), findsNothing);
      expect(find.byType(ChatMarkdownAstView), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('streaming text does not apply markdown repair before finish', (
    WidgetTester tester,
  ) async {
    const msgId = 'stream_raw_text_before_finish';
    const rawChunk = '```go\nfmt.Println(1)';

    await tester.pumpWidget(
      buildBubble(msgId: msgId, isStreaming: true, content: ''),
    );

    MessageStreamController.addChunk(msgId, rawChunk);
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.text(rawChunk), findsOneWidget);
    expect(find.text('```go\nfmt.Println(1)\n```'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'stream finish trusts backend final text without frontend repair',
    (WidgetTester tester) async {
      const msgId = 'stream_finish_trusted_backend_final';
      const finalContent =
          '好，那用流程图：```mermaid\nflowchart TD\nA[开始] --> B[结束]\n```';
      const normalizedContent =
          '好，那用流程图：\n```mermaid\nflowchart TD\nA[开始] --> B[结束]\n```';

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: true, content: ''),
      );

      MessageStreamController.addChunk(msgId, '好，那用流程图：');
      await tester.pump(const Duration(milliseconds: 100));

      MessageStreamController.finish(msgId, finalContent);
      // finish 通过 rxdart 异步投递 data/onDone 微任务；tester.pump() 只在已有
      // 帧被调度时才 flush 微任务。若上一帧后无 ticker 调度新帧（指示器已改为
      // Timer 驱动），需要显式 idle() 冲刷微任务，再渲帧断言最终文本。
      await tester.idle();
      await tester.pump();

      expect(find.text(finalContent), findsOneWidget);
      expect(find.text(normalizedContent), findsNothing);

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: false, content: finalContent),
      );
      await tester.pumpAndSettle();

      expect(find.text(finalContent), findsOneWidget);
      expect(find.text(normalizedContent), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets(
    'stream finish full markdown payload triggers final markdown render',
    (WidgetTester tester) async {
      const msgId = 'stream_finish_full_markdown_render';
      const finalContent = '```go\nfmt.Println(1)\n```';

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: true, content: ''),
      );

      MessageStreamController.addChunk(msgId, '```go\nfmt.Println(1)');
      await tester.pump(const Duration(milliseconds: 100));

      expect(find.text('```go\nfmt.Println(1)'), findsOneWidget);
      expect(find.byType(ChatMarkdownAstView), findsNothing);

      MessageStreamController.finish(msgId, finalContent);
      await tester.pump();

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: false, content: finalContent),
      );
      await tester.pumpAndSettle();

      expect(find.byType(ChatMarkdownAstView), findsOneWidget);
      expect(find.text(finalContent), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('final plain text render keeps selection locked by default', (
    WidgetTester tester,
  ) async {
    const msgId = 'stream_plain_final';

    await tester.pumpWidget(
      buildBubble(msgId: msgId, isStreaming: true, content: ''),
    );

    expect(find.byType(StreamPendingIndicator), findsOneWidget);

    MessageStreamController.addChunk(msgId, 'plain text only');
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.byType(StreamPendingIndicator), findsNothing);
    expect(find.byType(MarkdownWidget), findsNothing);

    await tester.pumpWidget(
      buildBubble(msgId: msgId, isStreaming: false, content: 'plain text only'),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(SelectionArea), findsNothing);
    expect(find.text('plain text only'), findsOneWidget);

    final text = tester.widget<Text>(find.text('plain text only'));
    expect(text.strutStyle?.forceStrutHeight, isTrue);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'stream finish with empty terminal payload does not blank previously rendered text',
    (WidgetTester tester) async {
      const msgId = 'stream_empty_finish_guard';

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: true, content: ''),
      );

      MessageStreamController.addChunk(msgId, 'visible before finish');
      await tester.pump(const Duration(milliseconds: 100));

      expect(find.text('visible before finish'), findsOneWidget);

      MessageStreamController.finish(msgId, '');
      await tester.pump();

      expect(find.text('visible before finish'), findsOneWidget);
      expect(find.byType(StreamPendingIndicator), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets(
    'stream to final transition keeps visible text when parent final content is temporarily empty',
    (WidgetTester tester) async {
      const msgId = 'stream_transition_empty_final';

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: true, content: ''),
      );

      MessageStreamController.addChunk(msgId, 'kept across transition');
      await tester.pump(const Duration(milliseconds: 100));

      expect(find.text('kept across transition'), findsOneWidget);

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: false, content: ''),
      );
      await tester.pump();

      expect(find.text('kept across transition'), findsOneWidget);

      await tester.pumpWidget(
        buildBubble(
          msgId: msgId,
          isStreaming: false,
          content: 'kept across transition',
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('kept across transition'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets(
    'stream state flicker does not reset an already visible bubble to empty',
    (WidgetTester tester) async {
      const msgId = 'stream_state_flicker_guard';

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: true, content: ''),
      );

      MessageStreamController.addChunk(msgId, 'keep me visible');
      await tester.pump(const Duration(milliseconds: 100));
      expect(find.text('keep me visible'), findsOneWidget);

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: false, content: ''),
      );
      await tester.pump();
      expect(find.text('keep me visible'), findsOneWidget);

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: true, content: ''),
      );
      await tester.pump();
      expect(find.text('keep me visible'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('streaming plain text keeps bubble height stable before wrap', (
    WidgetTester tester,
  ) async {
    const msgId = 'stream_plain_height_stable';

    await tester.pumpWidget(
      buildBubble(msgId: msgId, isStreaming: true, content: '', width: 360),
    );

    MessageStreamController.addChunk(msgId, 'alpha');
    await tester.pump(const Duration(milliseconds: 100));

    final beforeHeight = tester.getSize(findBubbleContainer()).height;

    MessageStreamController.addChunk(msgId, '🙂');
    await tester.pump(const Duration(milliseconds: 100));

    final afterHeight = tester.getSize(findBubbleContainer()).height;
    expect(afterHeight, beforeHeight);

    final text = tester.widget<Text>(find.byType(Text).first);
    expect(text.strutStyle?.forceStrutHeight, isTrue);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('ordered stream chunks wait for missing leading sequence', (
    WidgetTester tester,
  ) async {
    const msgId = 'stream_missing_leading_seq';

    await tester.pumpWidget(
      buildBubble(msgId: msgId, isStreaming: true, content: ''),
    );

    MessageStreamController.addChunk(msgId, 'B', chunkSeq: 2);
    MessageStreamController.addChunk(msgId, 'C', chunkSeq: 3);
    await tester.pump(const Duration(milliseconds: 460));

    expect(find.text('BC'), findsNothing);
    expect(find.byType(StreamPendingIndicator), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets('final rerender applies normalization before markdown render', (
    WidgetTester tester,
  ) async {
    const msgId = 'stream_latex_final';

    await tester.pumpWidget(
      buildBubble(msgId: msgId, isStreaming: true, content: ''),
    );

    expect(find.byType(StreamPendingIndicator), findsOneWidget);

    MessageStreamController.addChunk(msgId, '```latex\nx = \\frac{1}{2}\n```');
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.byType(StreamPendingIndicator), findsNothing);
    expect(find.byType(MarkdownWidget), findsNothing);

    await tester.pumpWidget(
      buildBubble(
        msgId: msgId,
        isStreaming: false,
        content: '```latex\nx = \\frac{1}{2}\n```',
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(MarkdownWidget), findsNothing);
    expect(find.byType(ChatMarkdownAstView), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'bubble recovers buffered content when created after chunks arrive',
    (WidgetTester tester) async {
      const msgId = 'recovery_buffered';

      // Chunks arrive BEFORE the widget subscribes (e.g. scroll recycling).
      MessageStreamController.addChunk(msgId, 'Hello ');
      MessageStreamController.addChunk(msgId, 'world');
      await tester.pump(const Duration(milliseconds: 100));

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: true, content: ''),
      );

      expect(find.byType(StreamPendingIndicator), findsNothing);
      expect(find.textContaining('Hello world'), findsOneWidget);

      MessageStreamController.finish(msgId, 'Hello world');
      await tester.pumpAndSettle();
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets('bubble recovers content from buffer before flush timer fires', (
    WidgetTester tester,
  ) async {
    const msgId = 'recovery_unflushed';

    // Add chunks but do NOT let the flush timer fire.
    MessageStreamController.addChunk(msgId, 'Buffered text');

    // Create the bubble immediately (before 80ms flush).
    await tester.pumpWidget(
      buildBubble(msgId: msgId, isStreaming: true, content: ''),
    );

    // peekContent reads _buffers directly, so the bubble should recover.
    expect(find.byType(StreamPendingIndicator), findsNothing);
    expect(find.textContaining('Buffered text'), findsOneWidget);

    await tester.pump(const Duration(milliseconds: 100));
    expect(find.textContaining('Buffered text'), findsOneWidget);

    MessageStreamController.finish(msgId, 'Buffered text');
    await tester.pumpAndSettle();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump(const Duration(milliseconds: 600));
  });

  testWidgets(
    'bubble preserves content during stream finish to widget rebuild gap',
    (WidgetTester tester) async {
      const msgId = 'recovery_finish_gap';

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: true, content: ''),
      );

      MessageStreamController.addChunk(msgId, 'Some content');
      await tester.pump(const Duration(milliseconds: 100));
      expect(find.textContaining('Some content'), findsOneWidget);

      // Stream finishes, then widget is rebuilt with isStreaming=false and
      // empty content (simulates the async gap before msg.content arrives).
      MessageStreamController.finish(msgId, 'Some content');
      await tester.pump();

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: false, content: ''),
      );

      expect(find.textContaining('Some content'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets(
    'multiple bubbles keep isolated content across dispose and stream finish gap',
    (WidgetTester tester) async {
      const alphaMsgId = 'multi_alpha';
      const betaMsgId = 'multi_beta';

      await tester.pumpWidget(
        buildBubbleList(const [
          _BubbleSpec(msgId: alphaMsgId, isStreaming: true, content: ''),
          _BubbleSpec(msgId: betaMsgId, isStreaming: true, content: ''),
        ]),
      );

      MessageStreamController.addChunk(alphaMsgId, 'Alpha');
      MessageStreamController.addChunk(betaMsgId, 'Beta');
      await tester.pump(const Duration(milliseconds: 100));

      expect(find.text('Alpha'), findsOneWidget);
      expect(find.text('Beta'), findsOneWidget);

      MessageStreamController.finish(alphaMsgId, '');
      await tester.pump();

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();

      await tester.pumpWidget(
        buildBubbleList(const [
          _BubbleSpec(msgId: alphaMsgId, isStreaming: false, content: ''),
          _BubbleSpec(msgId: betaMsgId, isStreaming: true, content: ''),
        ]),
      );
      await tester.pump();

      expect(find.text('Alpha'), findsOneWidget);
      expect(find.text('Beta'), findsOneWidget);

      MessageStreamController.addChunk(betaMsgId, ' plus');
      await tester.pump(const Duration(milliseconds: 100));

      expect(find.text('Alpha'), findsOneWidget);
      expect(find.text('Beta plus'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );

  testWidgets(
    'recoverable fallback yields to later authoritative final content',
    (WidgetTester tester) async {
      const msgId = 'recoverable_authoritative_override';

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: true, content: ''),
      );

      MessageStreamController.addChunk(msgId, 'temporary streamed text');
      await tester.pump(const Duration(milliseconds: 100));
      expect(find.text('temporary streamed text'), findsOneWidget);

      MessageStreamController.finish(msgId, '');
      await tester.pump();

      await tester.pumpWidget(
        buildBubble(msgId: msgId, isStreaming: false, content: ''),
      );
      await tester.pump();
      expect(find.text('temporary streamed text'), findsOneWidget);

      await tester.pumpWidget(
        buildBubble(
          msgId: msgId,
          isStreaming: false,
          content: 'authoritative final text',
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('authoritative final text'), findsOneWidget);
      expect(find.text('temporary streamed text'), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(milliseconds: 600));
    },
  );
}
