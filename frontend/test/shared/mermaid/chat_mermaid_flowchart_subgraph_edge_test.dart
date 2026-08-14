import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_flowchart_layout.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_parser.dart';

/// 回归：当流程图的边连接的是 subgraph（分组框）而非框内节点时，
/// 旧实现会在建图阶段把整条边丢弃，导致分层算法拿不到骨架、节点全部塌缩、
/// 分组框互相重叠（老郭反馈“压在一块了，乱套了”）。
void main() {
  const layoutEngine = ChatMermaidFlowchartLayoutEngine();
  const textStyle = TextStyle(fontSize: 12);

  // 老郭反馈的原始图：所有 7 条边都连在 subgraph 上。
  const source = '''
flowchart TD
    subgraph Mobile["📱 手机端"]
        M1["开始对话"]
        M2["语音输入"]
        M3["随时查看"]
    end

    subgraph Sync["实时同步"]
        S["同一会话<br/>自动同步"]
    end

    subgraph Desktop["💻 电脑端"]
        D1["无缝继续"]
        D2["详细编辑"]
        D3["文件处理"]
    end

    Mobile --> S
    S --> Desktop
    Desktop --> S
    S --> Mobile

    Sync -.-> note1["无需手动传输"]
    Sync -.-> note2["无需重新加载"]
    Sync -.-> note3["打破设备×账号×模型限制"]
''';

  ChatMermaidFlowchart parse(String text) {
    final result = const ChatMermaidParser().parse(text);
    final diagram = result.diagram;
    expect(diagram, isA<ChatMermaidFlowchart>(),
        reason: '解析失败: ${result.error}');
    return diagram! as ChatMermaidFlowchart;
  }

  ChatMermaidFlowchartLayout runLayout(ChatMermaidFlowchart diagram) {
    return layoutEngine.layout(
      diagram: diagram,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );
  }

  test('subgraph-anchored edges：节点不重叠', () {
    final layout = runLayout(parse(source));
    final entries = layout.nodeRects.entries.toList();
    for (var i = 0; i < entries.length; i++) {
      for (var j = i + 1; j < entries.length; j++) {
        final a = entries[i];
        final b = entries[j];
        expect(
          a.value.overlaps(b.value),
          isFalse,
          reason: '节点 ${a.key} 与 ${b.key} 叠加: ${a.value} / ${b.value}',
        );
      }
    }
  });

  test('subgraph-anchored edges：分组框互不重叠', () {
    final layout = runLayout(parse(source));
    final boxes = layout.subgraphRects;
    for (var i = 0; i < boxes.length; i++) {
      for (var j = i + 1; j < boxes.length; j++) {
        final a = boxes[i];
        final b = boxes[j];
        expect(
          a.rect.overlaps(b.rect),
          isFalse,
          reason:
              '分组框 ${a.subgraph.id} 与 ${b.subgraph.id} 叠加: ${a.rect} / ${b.rect}',
        );
      }
    }
  });

  test('subgraph-anchored edges：恢复纵向分层（手机在上→同步→电脑在下）', () {
    final layout = runLayout(parse(source));
    Rect boxOf(String id) =>
        layout.subgraphRects.firstWhere((b) => b.subgraph.id == id).rect;
    final mobile = boxOf('Mobile');
    final sync = boxOf('Sync');
    final desktop = boxOf('Desktop');
    // 分层结构：上一组的底边应高于下一组的顶边。
    expect(mobile.bottom, lessThanOrEqualTo(sync.top),
        reason: '手机端分组未排在同步分组之上');
    expect(sync.bottom, lessThanOrEqualTo(desktop.top),
        reason: '同步分组未排在电脑端分组之上');
    // 画布不应退化成一条扁平的长行（旧实现高度仅 ~142）。
    expect(layout.canvasSize.height, greaterThan(300),
        reason: '布局塌缩成单行，未形成纵向层级');
  });

  test('subgraph-anchored edges：分组框不圈住非成员节点', () {
    final layout = runLayout(parse(source));
    for (final box in layout.subgraphRects) {
      final members = box.subgraph.nodeIds.toSet();
      for (final entry in layout.nodeRects.entries) {
        if (members.contains(entry.key)) continue;
        final center = entry.value.center;
        final inside = box.rect.contains(center);
        expect(
          inside,
          isFalse,
          reason: '分组框 ${box.subgraph.id} 圈住了非成员节点 ${entry.key}',
        );
      }
    }
  });
}
