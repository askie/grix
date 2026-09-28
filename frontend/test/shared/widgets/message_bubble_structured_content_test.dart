import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/shared/utils/chat_message_preview.dart';
import 'package:grix/shared/widgets/message_bubble.dart';

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

  Widget buildBubbles(List<Widget> bubbles) {
    return MaterialApp(
      home: Scaffold(
        body: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: bubbles,
        ),
      ),
    );
  }

  testWidgets(
    'human json object stays verbatim for sent and received bubbles',
    (tester) async {
      const raw = '''{
  "content": "content value",
  "text": "text value"
}''';

      await tester.pumpWidget(
        buildBubbles(const [
          MessageBubble(
            msgId: 'human-json-sent',
            initialContent: raw,
            senderType: 1,
            isMine: true,
          ),
          MessageBubble(
            msgId: 'human-json-received',
            initialContent: raw,
            senderType: 1,
            isMine: false,
          ),
        ]),
      );
      await tester.pumpAndSettle();

      expect(find.text(raw), findsNWidgets(2));
      expect(find.text('text value'), findsNothing);
      expect(find.text('content value'), findsNothing);
    },
  );

  testWidgets('human json arrays and nested objects stay verbatim', (
    tester,
  ) async {
    const raw =
        '[{"content":{"text":"nested"}},{"type":"text","text":"block"}]';

    await tester.pumpWidget(
      buildBubbles(const [
        MessageBubble(
          msgId: 'human-json-array',
          initialContent: raw,
          senderType: 1,
        ),
      ]),
    );
    await tester.pumpAndSettle();

    expect(find.text(raw), findsOneWidget);
    expect(find.text('nested\nblock'), findsNothing);
  });

  testWidgets('agent content envelopes and text block arrays still unwrap', (
    tester,
  ) async {
    const envelope = '{"type":"assistant","content":"agent content"}';
    const blocks =
        '[{"type":"text","text":"block one"},{"type":"text","text":"block two"}]';

    await tester.pumpWidget(
      buildBubbles(const [
        MessageBubble(
          msgId: 'agent-json-envelope',
          initialContent: envelope,
          senderType: 2,
        ),
        MessageBubble(
          msgId: 'agent-json-blocks',
          initialContent: blocks,
          senderType: 2,
        ),
      ]),
    );
    await tester.pumpAndSettle();

    expect(find.text('agent content'), findsOneWidget);
    expect(find.text('block one\nblock two'), findsOneWidget);
    expect(find.text(envelope), findsNothing);
    expect(find.text(blocks), findsNothing);
  });

  testWidgets('reply preview keeps quoted human json verbatim', (tester) async {
    const quotedJson = '{"content":"quoted content","text":"quoted text"}';
    expect(ChatMessagePreview.summarize(quotedJson), 'quoted text');
    final repliedMessage = MessageModel(
      msgId: 'quoted-human-json',
      sessionId: 'session',
      senderId: 'human',
      senderType: 1,
      content: quotedJson,
      createdAt: 1,
    );

    await tester.pumpWidget(
      buildBubbles([
        MessageBubble(
          msgId: 'replying-agent-message',
          initialContent: 'reply body',
          senderType: 2,
          quotedMessageId: repliedMessage.msgId,
          repliedMsg: repliedMessage,
        ),
      ]),
    );
    await tester.pumpAndSettle();

    expect(find.text(quotedJson), findsOneWidget);
    expect(find.text('quoted text'), findsNothing);
    expect(find.text('quoted content'), findsNothing);
  });
}
