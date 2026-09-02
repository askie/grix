import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_sequence_layout.dart';

void main() {
  test(
    'sequence layout keeps space between participant and first message row',
    () {
      const diagram = ChatMermaidSequenceDiagram(
        participants: <ChatMermaidSequenceParticipant>[
          ChatMermaidSequenceParticipant(id: 'A', label: '用户', order: 0),
          ChatMermaidSequenceParticipant(id: 'B', label: '前端', order: 1),
        ],
        events: <ChatMermaidSequenceEvent>[
          ChatMermaidSequenceMessage(
            order: 0,
            fromId: 'A',
            toId: 'B',
            label: '点击登录',
            style: ChatMermaidSequenceMessageStyle.solidArrow,
          ),
        ],
      );

      final layout = const ChatMermaidSequenceLayoutEngine().layout(
        diagram: diagram,
        participantStyle: const TextStyle(fontSize: 12),
        messageStyle: const TextStyle(fontSize: 12),
        noteStyle: const TextStyle(fontSize: 12),
        groupStyle: const TextStyle(fontSize: 12),
        textDirection: TextDirection.ltr,
      );
      final participantBottom = layout.participants
          .map((participant) => participant.rect.bottom)
          .reduce(math.max);

      expect(layout.headerBottom - participantBottom, 34);

      final firstMessage = layout.events
          .whereType<ChatMermaidSequenceMessageLayout>()
          .first;
      expect(firstMessage.top, layout.headerBottom);
      expect(firstMessage.centerY - participantBottom, 56);
    },
  );
}
