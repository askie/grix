import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_state_layout.dart';

void main() {
  const layoutEngine = ChatMermaidStateLayoutEngine();
  const textStyle = TextStyle(fontSize: 12);

  test('top-down aligned states use a straight forward transition', () {
    const diagram = ChatMermaidStateDiagram(
      nodes: <ChatMermaidStateNode>[
        ChatMermaidStateNode(
          id: '__state_start__',
          label: '',
          kind: ChatMermaidStateNodeKind.start,
          order: 0,
        ),
        ChatMermaidStateNode(
          id: 'PENDING',
          label: '待支付',
          kind: ChatMermaidStateNodeKind.regular,
          order: 1,
        ),
      ],
      transitions: <ChatMermaidStateTransition>[
        ChatMermaidStateTransition(
          sourceId: '__state_start__',
          targetId: 'PENDING',
          order: 0,
        ),
      ],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      stateStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    final transition = layout.transitions.single;
    expect(transition.points, hasLength(2));
    expect(
      transition.points.first.dx,
      closeTo(transition.points.last.dx, 0.001),
    );
  });

  test('state transition label keeps a visible gap from vertical line', () {
    const diagram = ChatMermaidStateDiagram(
      nodes: <ChatMermaidStateNode>[
        ChatMermaidStateNode(
          id: '__state_start__',
          label: '',
          kind: ChatMermaidStateNodeKind.start,
          order: 0,
        ),
        ChatMermaidStateNode(
          id: 'PENDING',
          label: '待支付',
          kind: ChatMermaidStateNodeKind.regular,
          order: 1,
        ),
      ],
      transitions: <ChatMermaidStateTransition>[
        ChatMermaidStateTransition(
          sourceId: '__state_start__',
          targetId: 'PENDING',
          order: 0,
          label: '条件较长标签',
        ),
      ],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      stateStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    final transition = layout.transitions.single;
    final lineX = transition.points.first.dx;
    final labelLeft =
        transition.labelAnchor.dx - (transition.labelSize.width / 2);
    expect(
      labelLeft - lineX,
      greaterThanOrEqualTo(layoutEngine.edgeLabelGap),
    );
  });

  test('self-transition label keeps a visible gap from loop top line', () {
    const diagram = ChatMermaidStateDiagram(
      nodes: <ChatMermaidStateNode>[
        ChatMermaidStateNode(
          id: 'A',
          label: '待支付',
          kind: ChatMermaidStateNodeKind.regular,
          order: 0,
        ),
      ],
      transitions: <ChatMermaidStateTransition>[
        ChatMermaidStateTransition(
          sourceId: 'A',
          targetId: 'A',
          order: 0,
          label: '触发',
        ),
      ],
    );

    final layout = layoutEngine.layout(
      diagram: diagram,
      stateStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );

    final transition = layout.transitions.single;
    final lineY = transition.points[2].dy;
    final labelBottom =
        transition.labelAnchor.dy + (transition.labelSize.height / 2);
    expect(
      lineY - labelBottom,
      greaterThanOrEqualTo(layoutEngine.edgeLabelGap),
    );
  });
}
