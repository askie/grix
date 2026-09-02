import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:grix/modules/chat/message_cards/models/chat_message_card_type.dart';
import 'package:grix/modules/chat/message_cards/models/chat_progress_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/modules/chat/message_cards/widgets/chat_progress_card_view.dart';

/// 从 Markdown 链接中抽取 grix:// URI。
String? _extractGrixUri(String content) {
  final match = RegExp(r'\((grix://card/[^)]+)\)').firstMatch(content);
  return match?.group(1);
}

void main() {
  group('ChatProgressCardData model', () {
    test('determinate payload carries label and percent', () {
      const card = ChatProgressCardData(label: '正在编译', percent: 60);
      expect(card.type, ChatMessageCardType.progress);
      expect(card.isIndeterminate, isFalse);
      expect(card.clampedPercent, 60);
      expect(card.fraction, closeTo(0.6, 1e-9));
      expect(card.toPayload(), {'label': '正在编译', 'percent': 60});
    });

    test('indeterminate when percent is null', () {
      const card = ChatProgressCardData(label: '处理中');
      expect(card.isIndeterminate, isTrue);
      expect(card.clampedPercent, isNull);
      expect(card.fraction, isNull);
    });

    test('percent is clamped into 0..100', () {
      expect(
        const ChatProgressCardData(label: 'x', percent: 150).clampedPercent,
        100,
      );
      expect(
        const ChatProgressCardData(label: 'x', percent: -5).clampedPercent,
        0,
      );
    });
  });

  group('ChatProgressCard codec roundtrip', () {
    test('determinate card encodes and decodes via grix:// roundtrip', () {
      final envelope = ChatMessageCardCodec.encode(
        const ChatProgressCardData(label: 'Downloading model', percent: 42),
      );
      final uri = _extractGrixUri(envelope.content);
      expect(uri, isNotNull);

      final decoded = ChatMessageCardCodec.decodeGrixUriCard(uri!);
      expect(decoded, isA<ChatProgressCardData>());
      final card = decoded as ChatProgressCardData;
      expect(card.label, 'Downloading model');
      expect(card.percent, 42);
      expect(card.isIndeterminate, isFalse);
    });

    test('indeterminate card omits percent and decodes as indeterminate', () {
      final envelope = ChatMessageCardCodec.encode(
        const ChatProgressCardData(label: '准备中'),
      );
      final uri = _extractGrixUri(envelope.content)!;
      expect(uri.contains('percent'), isFalse);

      final card =
          ChatMessageCardCodec.decodeGrixUriCard(uri) as ChatProgressCardData;
      expect(card.label, '准备中');
      expect(card.percent, isNull);
      expect(card.isIndeterminate, isTrue);
    });

    test('decodes from a standalone grix markdown message', () {
      final envelope = ChatMessageCardCodec.encode(
        const ChatProgressCardData(label: 'Step 1', percent: 10),
      );
      final decoded = ChatMessageCardCodec.decodeFromMessage(
        content: envelope.content,
      );
      expect(decoded, isA<ChatProgressCardData>());
      expect((decoded as ChatProgressCardData).percent, 10);
    });

    test('out-of-range percent survives roundtrip and clamps on render', () {
      final envelope = ChatMessageCardCodec.encode(
        const ChatProgressCardData(label: 'x', percent: 150),
      );
      final uri = _extractGrixUri(envelope.content)!;
      final card =
          ChatMessageCardCodec.decodeGrixUriCard(uri) as ChatProgressCardData;
      expect(card.clampedPercent, 100);
      expect(card.fraction, 1.0);
    });

    test('rejects empty label', () {
      final decoded = ChatMessageCardCodec.decodeGrixUriCard(
        'grix://card/progress?label=',
      );
      expect(decoded, isNull);
    });
  });

  group('ChatProgressCardView widget', () {
    testWidgets('determinate renders label, percent and bar value', (
      tester,
    ) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: ChatProgressCardView(
              card: ChatProgressCardData(label: '正在编译', percent: 60),
              isMine: false,
              fontScale: 1.0,
            ),
          ),
        ),
      );

      expect(find.text('正在编译'), findsOneWidget);
      expect(find.text('60%'), findsOneWidget);

      final bar = tester.widget<LinearProgressIndicator>(
        find.byKey(const Key('chat_message_card_progress_bar')),
      );
      expect(bar.value, closeTo(0.6, 1e-9));
    });

    testWidgets('indeterminate hides percent and bar has null value', (
      tester,
    ) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: ChatProgressCardView(
              card: ChatProgressCardData(label: '处理中'),
              isMine: false,
              fontScale: 1.0,
            ),
          ),
        ),
      );

      expect(find.text('处理中'), findsOneWidget);
      expect(
        find.byKey(const Key('chat_message_card_progress_percent')),
        findsNothing,
      );

      final bar = tester.widget<LinearProgressIndicator>(
        find.byKey(const Key('chat_message_card_progress_bar')),
      );
      expect(bar.value, isNull);
    });
  });
}
