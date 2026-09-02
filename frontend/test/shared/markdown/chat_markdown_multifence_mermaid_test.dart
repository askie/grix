import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_ast.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';

/// 回归：一条消息里有「多个 mermaid 围栏 + 中间夹着 markdown 表格」时，
/// 全部 mermaid 块都必须被识别出来。
///
/// 失败历史（线上会话 5040）：`_unwrapAccidentalTableFenceBlocks` 把前一个
/// mermaid 块的收尾裸 ``` 误当成「accidental table fence」的开头，向后一直吞到
/// 下一个真实围栏的收尾，把中间的表格/标题/第二张图整段解开，导致后面的流程图
/// 在气泡里渲染不出来。组件级（parser/layout）测试喂的是单张干净图，测不到这条。
void main() {
  final pipeline = ChatMarkdownPipeline(
    normalizer: const ChatMarkdownNormalizer(),
    parser: ChatMarkdownDialect.buildParserAdapter(),
  );

  List<ChatMarkdownNode> collectMermaid(ChatMarkdownNode node) {
    final out = <ChatMarkdownNode>[
      if (node.type == ChatMarkdownNodeType.mermaidBlock) node,
    ];
    for (final child in node.children) {
      out.addAll(collectMermaid(child));
    }
    return out;
  }

  int countMermaid(String raw) {
    final doc = pipeline.prepareFinalRender(raw).document;
    return doc == null ? 0 : collectMermaid(doc).length;
  }

  test('最小复现：mermaid + 表格 + mermaid 必须得到 2 个独立 mermaid 块', () {
    const raw = '''```mermaid
flowchart TD
    A --> B
```

| 左 | 右 |
|---|---|
| a | b |

```mermaid
sequenceDiagram
    participant U as 你
    U->>S: hi
```''';
    final doc = pipeline.prepareFinalRender(raw).document!;
    final blocks = collectMermaid(doc);
    expect(blocks, hasLength(2), reason: '两个 mermaid 围栏都应被识别');
    // 第二个块必须是序列图原文，而不是被前一个块吞掉。
    expect(blocks[0].attrs['text'].toString(), contains('flowchart'));
    expect(blocks[1].attrs['text'].toString(), contains('sequenceDiagram'));
    expect(
      blocks[1].attrs['text'].toString(),
      isNot(contains('| 左 |')),
      reason: '表格不应被并进 mermaid 块',
    );
  });

  test('泛化：非 mermaid 代码块 + 表格 + mermaid 也互不干扰', () {
    const raw = '''```python
print("hi")
```

| 左 | 右 |
|---|---|
| a | b |

```mermaid
flowchart TD
    A --> B
```''';
    final doc = pipeline.prepareFinalRender(raw).document!;
    final mermaid = collectMermaid(doc);
    expect(mermaid, hasLength(1), reason: 'mermaid 块应独立保留');
    expect(mermaid.single.attrs['text'].toString(), contains('flowchart'));
    expect(
      mermaid.single.attrs['text'].toString(),
      isNot(contains('print')),
      reason: '前面的 python 代码块不应被并进 mermaid 块',
    );
  });

  // 线上会话 5040 的真实气泡原文（图 + 表格 + --- + 口播 + (x/4) 混排）。
  final realMessages = <String, ({String raw, int expect})>{
    'msg_2of4_flowchartTB+sequence': (
      expect: 2,
      raw: '''**口播：** 传统的聊天 App 也能加 Bot。

---

### 📄 Slide 3 — Grix：Agent 是一等公民

```mermaid
flowchart TB
    G["GRIX<br/>即时通讯平台"]

    subgraph People["👤 人"]
        User["你跟 Agent 对话<br/>就像跟朋友聊天"]
    end

    subgraph Agent["🤖 Agent"]
        A1["Claude"]
        A2["DeepSeek"]
    end

    G --- People
    G --- Agent

    style G fill:#4a6cf7,color:#fff
```

| 左 · 概念 | 右 · 录屏 |
|---------|---------|
| Grix 三角 | 录屏：打开 Grix |

**口播：** Grix 把 Agent 当成聊天里的一等公民。

---

### 📄 Slide 4 — 人是怎么用 Grix 调度 Agent 的

```mermaid
sequenceDiagram
    participant U as 👤 你（手机端）
    participant G as GRIX 平台
    U->>G: "帮我分析这份财报"
    Note over G: 识别意图，调度 Claude
    G-->>U: 对话里直接展示结果
    Note over U,G: 全程像聊天一样自然<br/>设备切换无感知
```
 (2/4)''',
    ),
    'msg_4of4_flowchartTD+双表格': (
      expect: 1,
      raw: '''```mermaid
flowchart TD
    T["跟 Agent 对话<br/>从手机开始"]
    S["Grix · AI 优先的即时通讯平台"]
    T --> S
    style T fill:#4a6cf7,color:#fff
```

| 左 · 概念 | 右 · 录屏 |
|---------|---------|
| 收尾 slogan | Grix 手机端全貌 |

**口播：** 跟 Agent 对话，从手机开始。

---

### 附录：核心校正总结

| 之前错误 | 现在正确 |
|---------|---------|
| Grix 是智能体 | Grix 是即时通讯平台 |

这样对吧？ (4/4)''',
    ),
  };

  realMessages.forEach((name, m) {
    test('真实消息渲染：$name 得到 ${m.expect} 个 mermaid 块', () {
      expect(countMermaid(m.raw), m.expect);
    });
  });
}
