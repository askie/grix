import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_parser.dart';

void main() {
  const parser = ChatMermaidParser();

  test('parses flowchart nodes, shapes, directions and edge labels', () {
    final result = parser.parse(
      '''
flowchart TD
A[开始] --> B{是否登录?}
B -->|是| C[进入主页]
B -->|否| D[跳转登录页]
D --> E[输入账号密码]
E --> F{验证通过?}
F -->|是| C
F -->|否| G[显示错误]
G --> E
C --> H[结束]
''',
    );

    expect(result.isSupported, isTrue);
    final diagram = result.diagram as ChatMermaidFlowchart;
    expect(diagram.direction, ChatMermaidFlowDirection.topDown);
    expect(diagram.nodes, hasLength(8));
    expect(diagram.edges, hasLength(9));
    expect(
      diagram.nodes.firstWhere((node) => node.id == 'B').shape,
      ChatMermaidNodeShape.diamond,
    );
    expect(
      diagram.nodes.firstWhere((node) => node.id == 'E').label,
      '输入账号密码',
    );
    expect(
      diagram.edges.firstWhere((edge) => edge.targetId == 'C').label,
      '是',
    );
  });

  test('supports semicolon statements and standalone node declarations', () {
    final result = parser.parse(
      'graph LR; A[Start]; B(Review); A --> B; B --> C[Done]',
    );

    expect(result.isSupported, isTrue);
    final diagram = result.diagram as ChatMermaidFlowchart;
    expect(diagram.direction, ChatMermaidFlowDirection.leftRight);
    expect(diagram.nodes, hasLength(3));
    expect(
      diagram.nodes.firstWhere((node) => node.id == 'B').shape,
      ChatMermaidNodeShape.rounded,
    );
  });

  test('supports & operator for multiple source nodes', () {
    final result = parser.parse(
      '''
graph TB
    Claude -->|"session/update"| JsonRpc
    Cursor -->|"session/update"| JsonRpc
''',
    );

    expect(result.isSupported, isTrue);
    final diagram = result.diagram as ChatMermaidFlowchart;
    expect(diagram.nodes, hasLength(3));
    expect(diagram.edges, hasLength(2));
    expect(
      diagram.edges.every((e) => e.targetId == 'JsonRpc'),
      isTrue,
    );
  });

  test('supports & operator combining source nodes', () {
    final result = parser.parse(
      '''
graph TB
    Claude & Cursor -->|"session/update"| JsonRpc --> EventMapper --> Bridge
''',
    );

    expect(result.isSupported, isTrue);
    final diagram = result.diagram as ChatMermaidFlowchart;
    expect(diagram.nodes, hasLength(5));
    expect(diagram.edges, hasLength(4));
    // Claude and Cursor both connect to JsonRpc
    final toRpc = diagram.edges.where((e) => e.targetId == 'JsonRpc').toList();
    expect(toRpc, hasLength(2));
    expect(toRpc.every((e) => e.label == 'session/update'), isTrue);
    expect(
      toRpc.map((e) => e.sourceId).toSet(),
      {'Claude', 'Cursor'},
    );
    // Chain continues from JsonRpc
    final fromRpc =
        diagram.edges.where((e) => e.sourceId == 'JsonRpc').toList();
    expect(fromRpc, hasLength(1));
    expect(fromRpc.first.targetId, 'EventMapper');
  });

  test('supports & operator with three source nodes', () {
    final result = parser.parse(
      'graph LR\nA & B & C --> D',
    );

    expect(result.isSupported, isTrue);
    final diagram = result.diagram as ChatMermaidFlowchart;
    expect(diagram.nodes, hasLength(4));
    expect(diagram.edges, hasLength(3));
    expect(
      diagram.edges.every((e) => e.targetId == 'D'),
      isTrue,
    );
  });

  test('parses real-world & operator in complex flowchart', () {
    final result = parser.parse(
      '''
graph TB
    subgraph Grix["Grix AIBot 平台"]
        User["用户"]
        AIBotServer["AIBot Server"]
    end

    subgraph ACPBridge["grix-acp"]
        Bridge["AgentInstance"]
        AibotClient["AibotClient"]
        JsonRpc["JsonRpcTransport"]
    end

    subgraph Agents["AI Agent"]
        Claude["claude"]
        Cursor["cursor-agent"]
    end

    User -->|"发消息"| AIBotServer
    Claude & Cursor -->|"session/update"| JsonRpc --> Bridge
    Bridge -->|"StreamChunk"| AibotClient
''',
    );

    expect(result.isSupported, isTrue);
    final diagram = result.diagram as ChatMermaidFlowchart;
    // Claude and Cursor both connect to JsonRpc via &
    final toRpc = diagram.edges.where((e) => e.targetId == 'JsonRpc').toList();
    expect(toRpc, hasLength(2));
    expect(
      toRpc.map((e) => e.sourceId).toSet(),
      {'Claude', 'Cursor'},
    );
    // Chain continues: JsonRpc --> Bridge
    final fromRpc =
        diagram.edges.where((e) => e.sourceId == 'JsonRpc').toList();
    expect(fromRpc, hasLength(1));
    expect(fromRpc.first.targetId, 'Bridge');
  });

  test('parses flowchart subgraphs and subgraph references', () {
    final result = parser.parse(
      '''
flowchart TD
subgraph Client ["客户端层"]
AppUI[Flutter UI]
DB[Sqflite]
end
Client --> Gateway[API Gateway]
''',
    );

    expect(result.isSupported, isTrue);
    final diagram = result.diagram as ChatMermaidFlowchart;
    expect(diagram.subgraphs, hasLength(1));
    expect(diagram.subgraphs.single.label, '客户端层');
    expect(
        diagram.subgraphs.single.nodeIds, containsAll(<String>['AppUI', 'DB']));
    expect(diagram.edges.single.sourceId, 'Client');
    expect(diagram.edges.single.targetId, 'Gateway');
  });

  test('skips style directives and parses flowchart', () {
    // Style directives are now skipped instead of causing rejection
    final result = parser.parse(
      '''
flowchart TD
A --> B
style A fill:#f9f
''',
    );

    expect(result.isSupported, isTrue);
    expect(result.diagram, isA<ChatMermaidFlowchart>());
    final flowchart = result.diagram as ChatMermaidFlowchart;
    expect(flowchart.nodes.length, equals(2));
    expect(flowchart.edges.length, equals(1));
  });

  test('skips direction directives inside flowchart and subgraph', () {
    final result = parser.parse(
      '''
flowchart TD
direction TB
subgraph Client [客户端]
direction LR
A[开始] --> B[结束]
end
''',
    );

    expect(result.isSupported, isTrue);
    final flowchart = result.diagram as ChatMermaidFlowchart;
    expect(flowchart.nodes.length, equals(2));
    expect(flowchart.edges.length, equals(1));
    expect(flowchart.subgraphs.length, equals(1));
  });

  test('skips accessibility directives in flowchart', () {
    final result = parser.parse(
      '''
flowchart TD
accTitle: 登录流程
accDescr: 用户登录路径
A[开始] --> B[结束]
''',
    );

    expect(result.isSupported, isTrue);
    final flowchart = result.diagram as ChatMermaidFlowchart;
    expect(flowchart.nodes.length, equals(2));
    expect(flowchart.edges.length, equals(1));
  });

  test('parses first flowchart when multiple flowchart headers exist', () {
    final result = parser.parse(
      '''
flowchart TB
A[顶部开始] --> B[向下流动]

flowchart LR
C[左] --> D[右]

flowchart RL
E[右] --> F[左]

flowchart BT
G[底部] --> H[顶部]
''',
    );

    expect(result.isSupported, isTrue);
    final diagram = result.diagram as ChatMermaidFlowchart;
    expect(diagram.direction, ChatMermaidFlowDirection.topDown);
    expect(diagram.nodes, hasLength(2));
    expect(diagram.edges, hasLength(1));
    expect(
        diagram.nodes.map((node) => node.id), containsAll(<String>['A', 'B']));
  });

  test('parses basic sequence diagrams', () {
    final result = parser.parse(
      '''
sequenceDiagram
Alice->>Bob: hello
''',
    );

    expect(result.isSupported, isTrue);
    expect(result.diagram, isA<ChatMermaidSequenceDiagram>());
  });

  test('parses sequence diagrams with notes and groups', () {
    final result = parser.parse(
      '''
sequenceDiagram
participant Caller as 调用方
participant SS as StreamSession
Note over Caller, SS: 首个 chunk
Caller->>SS: NewStreamSession(config)
loop 每个 chunk
Caller->>SS: AppendChunk(delta)
SS-->>Caller: ok
end
alt 成功
SS->>SS: finish
else 失败
SS-->>Caller: error
end
''',
    );

    expect(result.isSupported, isTrue);
    final diagram = result.diagram as ChatMermaidSequenceDiagram;
    expect(diagram.participants, hasLength(2));
    expect(
      diagram.events.whereType<ChatMermaidSequenceNote>().single.text,
      '首个 chunk',
    );
    expect(diagram.events.whereType<ChatMermaidSequenceGroupStart>(),
        hasLength(2));
    expect(
      diagram.events.whereType<ChatMermaidSequenceGroupDivider>().single.label,
      '失败',
    );
    expect(
      diagram.events.whereType<ChatMermaidSequenceMessage>().first.style,
      ChatMermaidSequenceMessageStyle.solidArrow,
    );
    expect(
      diagram.events.whereType<ChatMermaidSequenceMessage>().last.style,
      ChatMermaidSequenceMessageStyle.dashedArrow,
    );
  });

  test('rejects unclosed sequence groups', () {
    final result = parser.parse(
      '''
sequenceDiagram
participant A
participant B
loop retry
A->>B: ping
''',
    );

    expect(result.isSupported, isFalse);
    expect(result.error, 'sequence group not closed');
  });

  test('parses state diagrams with start end and self transitions', () {
    final result = parser.parse(
      '''
stateDiagram-v2
    [*] --> CONNECTED
    CONNECTED --> AUTHED: recv auth + verify ok
    CONNECTED --> CLOSED: auth timeout / auth fail
    AUTHED --> AUTHED: ping/pong
    AUTHED --> AUTHED: event_msg / send_msg
    AUTHED --> CLOSED: kicked / network error / close
    CLOSED --> [*]
''',
    );

    expect(result.isSupported, isTrue);
    final diagram = result.diagram as ChatMermaidStateDiagram;
    expect(diagram.nodes, hasLength(5));
    expect(
      diagram.nodes.firstWhere((node) => node.id == '__state_start__').kind,
      ChatMermaidStateNodeKind.start,
    );
    expect(
      diagram.nodes.firstWhere((node) => node.id == '__state_end__').kind,
      ChatMermaidStateNodeKind.end,
    );
    expect(diagram.transitions, hasLength(7));
    expect(
      diagram.transitions.where((transition) => transition.isSelfTransition),
      hasLength(2),
    );
    expect(
      diagram.transitions
          .firstWhere(
            (transition) => transition.targetId == 'AUTHED',
          )
          .label,
      'recv auth + verify ok',
    );
  });

  test('parses gantt diagrams with sections and after dependencies', () {
    final result = parser.parse(
      '''
gantt
    title 实施路线图
    dateFormat YYYY-MM-DD
    axisFormat %m-%d

    section Phase 1 数据基础
    Agent 模型扩展 + 迁移 :p1a, 2026-03-03, 1d
    Agent CRUD API :p1b, after p1a, 1d

    section Phase 2 前端 AI 页面
    底部 4 Tab + Agent 列表页 :p2a, after p1b, 1d
''',
    );

    expect(result.isSupported, isTrue);
    final diagram = result.diagram as ChatMermaidGanttDiagram;
    expect(diagram.title, '实施路线图');
    expect(diagram.axisFormat, '%m-%d');
    expect(diagram.sections, hasLength(2));
    expect(diagram.sections.first.tasks, hasLength(2));
    expect(diagram.sections.first.tasks.first.id, 'p1a');
    expect(
      diagram.sections.first.tasks[1].startDate,
      DateTime.utc(2026, 3, 4),
    );
    expect(
      diagram.sections[1].tasks.single.startDate,
      DateTime.utc(2026, 3, 5),
    );
  });

  group('new diagram types', () {
    test('parses class diagrams with relations', () {
      final result = parser.parse(
        '''
classDiagram
    class Animal {
        +String name
        +int age
        +makeSound()
    }
    class Dog {
        +String breed
        +bark()
    }
    Animal <|-- Dog : extends
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidClassDiagram;
      expect(diagram.classes, hasLength(2));
      expect(diagram.relations, hasLength(1));
      expect(
        diagram.relations.first.relationType,
        ChatMermaidClassRelationType.inheritance,
      );
    });

    test('parses ER diagrams with cardinality', () {
      final result = parser.parse(
        '''
erDiagram
    CUSTOMER ||--o{ ORDER : places
    ORDER ||--|{ LINE_ITEM : contains
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidErDiagram;
      expect(diagram.entities, hasLength(3));
      expect(diagram.relations, hasLength(2));
      expect(
        diagram.relations.first.sourceCardinality,
        ChatMermaidErCardinality.exactlyOne,
      );
      expect(
        diagram.relations.first.targetCardinality,
        ChatMermaidErCardinality.zeroOrMore,
      );
    });

    test('parses pie charts', () {
      final result = parser.parse(
        '''
pie title Pets adopted
    "Dogs" : 386
    "Cats" : 85
    "Rats" : 15
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidPieDiagram;
      expect(diagram.title, 'Pets adopted');
      expect(diagram.slices, hasLength(3));
      expect(diagram.slices.first.label, 'Dogs');
      expect(diagram.slices.first.value, 386);
    });

    test('parses mindmap diagrams', () {
      final result = parser.parse(
        '''
mindmap
  root((mindmap))
    origins
      Long history
      ::icon(fa fa-book)
      Popularisation
        British popular psychology author Tony Buzan
    research
      On effectiveness<br/>and features
      On Automatic creation
        Uses
            Creative techniques
            Strategic planning
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidMindmapDiagram;
      expect(diagram.root.label, 'mindmap');
      expect(diagram.root.children, isNotEmpty);
    });

    test('parses journey diagrams', () {
      final result = parser.parse(
        '''
journey
    title My working day
    section Go to work
      Make tea: 5: Me
      Go upstairs: 3: Me
      Do work: 1: Me, Cat
    section Go home
      Go downstairs: 5: Me
      Sit down: 5: Me
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidJourneyDiagram;
      expect(diagram.title, 'My working day');
      expect(diagram.sections, hasLength(2));
      expect(diagram.sections.first.tasks, hasLength(3));
      expect(diagram.sections.first.tasks.first.score, 5);
    });

    test('parses gitGraph diagrams', () {
      final result = parser.parse(
        '''
gitGraph
    commit id: "initial"
    commit id: "add feature"
    branch develop
    checkout develop
    commit id: "wip"
    checkout main
    merge develop tag: "v1.0"
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidGitGraphDiagram;
      expect(diagram.branches, containsAll(['main', 'develop']));
      expect(diagram.commits, hasLength(4));
      expect(diagram.commits.last.mergeFrom, 'develop');
      expect(diagram.commits.last.tag, 'v1.0');
    });

    test('complex sequence diagram with actors', () {
      final result = parser.parse(
        '''
sequenceDiagram
    actor User
    actor System
    User->>System: Request
    System-->>User: Response
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidSequenceDiagram;
      expect(diagram.participants.length, equals(2));
      expect(diagram.participants[0].isActor, isTrue);
      expect(diagram.events.whereType<ChatMermaidSequenceMessage>().length,
          equals(2));
    });

    test('stateDiagram without v2 suffix', () {
      final result = parser.parse(
        '''
stateDiagram
    [*] --> Still
    Still --> [*]
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidStateDiagram;
      expect(diagram.nodes.length, greaterThanOrEqualTo(3));
    });
  });

  group('节点 <br> 换行兼容', () {
    test('将 <br/> 转换为换行', () {
      final result = parser.parse(
        '''
flowchart TD
A[第一行<br/>第二行] --> B[结束]
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(
        diagram.nodes.firstWhere((node) => node.id == 'A').label,
        '第一行\n第二行',
      );
    });

    test('兼容 <br>、<br /> 以及大小写写法', () {
      final result = parser.parse(
        '''
flowchart TD
A[一<br>二<BR />三] --> B[结束]
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(
        diagram.nodes.firstWhere((node) => node.id == 'A').label,
        '一\n二\n三',
      );
    });

    test('兼容被换行拆断的 <br/> 标签', () {
      final result = parser.parse(
        '''
flowchart TD
A[Agent 调用工具<br/
>grix_client_action] --> B[结束]
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(
        diagram.nodes.firstWhere((node) => node.id == 'A').label,
        'Agent 调用工具\ngrix_client_action',
      );
    });

    test('管道边标签同样支持 <br/> 换行', () {
      final result = parser.parse(
        '''
flowchart TD
A[开始] -->|第一段<br/>第二段| B[结束]
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(
        diagram.edges.first.label,
        '第一段\n第二段',
      );
    });

    test('内联边标签(-- 文本 -->)同样支持 <br/> 换行', () {
      final result = parser.parse(
        '''
flowchart TD
A[开始] -- 第一段<br/>第二段 --> B[结束]
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(
        diagram.edges.first.label,
        '第一段\n第二段',
      );
    });

    test('子图标题支持 <br/> 换行', () {
      final result = parser.parse(
        '''
flowchart TD
subgraph 分组<br/>标题
  A[节点] --> B[节点]
end
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(diagram.subgraphs.first.label, '分组\n标题');
    });

    test('连续 <br><br> 保留空白行(符合 mermaid 语义)', () {
      final result = parser.parse(
        '''
flowchart TD
A[第一行<br><br>第三行] --> B[结束]
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(
        diagram.nodes.firstWhere((node) => node.id == 'A').label,
        '第一行\n\n第三行',
      );
    });

    test('非法的 "< br>" 不被当作换行(保持原文)', () {
      final result = parser.parse(
        '''
flowchart TD
A[文本< br>仍是文本] --> B[结束]
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(
        diagram.nodes.firstWhere((node) => node.id == 'A').label,
        '文本< br>仍是文本',
      );
    });

    test('字面量 \\n 转换为换行', () {
      final result = parser.parse(
        r'''
flowchart TD
A[第一行\n第二行] --> B[结束]
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(
        diagram.nodes.firstWhere((node) => node.id == 'A').label,
        '第一行\n第二行',
      );
    });

    test('\\n 与 <br/> 混用均转换为换行', () {
      final result = parser.parse(
        r'''
flowchart TD
A[第一行\n第二行<br/>第三行] --> B[结束]
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(
        diagram.nodes.firstWhere((node) => node.id == 'A').label,
        '第一行\n第二行\n第三行',
      );
    });
  });

  group('subgraph 带引号且含空格的标签名', () {
    test('整体引号字符串 "emoji 中文" 正确提取标签，不把 emoji 当 id', () {
      final result = parser.parse(
        '''
flowchart TB
subgraph "📱 用户入口"
  G[首页]
end
subgraph "🌐 Tailscale Mesh"
  T1[100.1.1.1] --- T2[100.1.1.2]
end
G --> T1
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(diagram.subgraphs, hasLength(2));
      expect(diagram.subgraphs[0].label, '📱 用户入口');
      expect(diagram.subgraphs[1].label, '🌐 Tailscale Mesh');
    });

    test('节点在两个兄弟子图中都被引用时，布局不因共享成员而陷入无限循环', () {
      // 复现场景：D1/D2 在"设备"子图中声明，同时在"互调"子图中被边引用。
      // 旧代码会把 D1/D2 注册进两个子图，_separateSiblingSubgraphs 16 轮后
      // 节点被推出画布。
      final result = parser.parse(
        '''
flowchart TB
subgraph "💻 设备"
  D1["台式机"]
  D2["笔记本"]
end
subgraph "🤖 Agent 互调"
  D1 -.->|curl| D2
  D2 -.->|报告| D1
end
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(diagram.subgraphs[0].label, '💻 设备');
      expect(diagram.subgraphs[1].label, '🤖 Agent 互调');
      expect(diagram.nodes, hasLength(2));
    });
  });

  group('带文字的虚线/粗线边(对齐 mermaid 官方协议)', () {
    test('解析带文字的虚线箭头 -. text .->', () {
      final result = parser.parse(
        '''
flowchart TB
B -.授权数据.-> D
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      final edge = diagram.edges.single;
      expect(edge.sourceId, 'B');
      expect(edge.targetId, 'D');
      expect(edge.style, ChatMermaidEdgeStyle.dashedArrow);
      expect(edge.label, '授权数据');
    });

    test('解析带文字的粗线箭头 == text ==>', () {
      final result = parser.parse(
        '''
flowchart LR
A ==处理==> B
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      final edge = diagram.edges.single;
      expect(edge.sourceId, 'A');
      expect(edge.targetId, 'B');
      expect(edge.style, ChatMermaidEdgeStyle.thickArrow);
      expect(edge.label, '处理');
    });

    test('带文字虚线/粗线边支持两侧空格写法', () {
      final result = parser.parse(
        '''
flowchart LR
A -. 触发 .-> B
B == 完成 ==> C
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(diagram.edges, hasLength(2));
      expect(diagram.edges[0].style, ChatMermaidEdgeStyle.dashedArrow);
      expect(diagram.edges[0].label, '触发');
      expect(diagram.edges[1].style, ChatMermaidEdgeStyle.thickArrow);
      expect(diagram.edges[1].label, '完成');
    });

    test('带文字虚线边支持 pipe 标签写法 -.|text|.->', () {
      final result = parser.parse(
        '''
flowchart LR
A -.|备注|.-> B
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(diagram.edges.single.style, ChatMermaidEdgeStyle.dashedArrow);
      expect(diagram.edges.single.label, '备注');
    });

    test('不带文字的虚线/粗线箭头仍按原样解析', () {
      final result = parser.parse(
        '''
flowchart LR
A -.-> B
B ==> C
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      expect(diagram.edges[0].style, ChatMermaidEdgeStyle.dashedArrow);
      expect(diagram.edges[0].label, isNull);
      expect(diagram.edges[1].style, ChatMermaidEdgeStyle.thickArrow);
      expect(diagram.edges[1].label, isNull);
    });

    test('用户真实代码:含带文字虚线箭头与中文 subgraph 的完整流程图', () {
      final result = parser.parse(
        '''
flowchart TB
    A["owner 在 Agent 编辑页<br/>勾选授予该 agent 的工具子集"] --> B["后端 agent_client_tool_grant 表"]

    subgraph 闸1["闸1: owner 隔离 (已有)"]
        C["mcp_frame 只在 owner 本人连接<br/>与其 agent 间路由"]
    end

    subgraph 闸2["闸2: tools/list 可见性过滤"]
        D["APP 返回全部工具<br/>→ 后端按 grant 过滤<br/>→ Agent 只看到被授权工具"]
    end

    subgraph 闸3["闸3: tools/call 调用校验"]
        E["Agent 调用工具<br/>→ 后端查 grant<br/>→ 未授权直接回 error, 不下发 APP"]
    end

    B -.授权数据.-> D
    B -.授权数据.-> E
    C --> D --> E

    style 闸2 fill:#e8f5e9
    style 闸3 fill:#fff3e0
''',
      );

      expect(result.isSupported, isTrue);
      final diagram = result.diagram as ChatMermaidFlowchart;
      final dashedEdges = diagram.edges
          .where((edge) => edge.style == ChatMermaidEdgeStyle.dashedArrow)
          .toList();
      expect(dashedEdges, hasLength(2));
      expect(
        dashedEdges.every((edge) => edge.label == '授权数据'),
        isTrue,
      );
      expect(
        dashedEdges.map((edge) => edge.targetId).toSet(),
        {'D', 'E'},
      );
    });
  });

  group('第一批兼容:无空格紧凑箭头 / 更长链路 / 目标侧 &', () {
    ChatMermaidFlowchart flow(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidFlowchart;
    }

    String sig(ChatMermaidEdge e) =>
        '${e.sourceId}-${e.style.name}${e.label != null ? "(${e.label})" : ""}->${e.targetId}';

    test('无空格紧凑箭头 A-->B 及各种连接符', () {
      final d = flow('flowchart LR\nA-->B\nC-.->D\nE==>F\nG--oH\nI--xJ');
      expect(d.edges.map(sig).toList(), [
        'A-solidArrow->B',
        'C-dashedArrow->D',
        'E-thickArrow->F',
        'G-circle->H',
        'I-cross->J',
      ]);
    });

    test('无空格带管道标签 A-->|标签|B', () {
      final d = flow('flowchart LR\nA-->|是|B');
      expect(sig(d.edges.single), 'A-solidArrow(是)->B');
    });

    test('无空格链式 A-->B-->C 拆成两条边(不被误当成带标签单边)', () {
      final d = flow('flowchart LR\nA-->B-->C');
      expect(d.edges.map(sig).toList(), ['A-solidArrow->B', 'B-solidArrow->C']);
    });

    test('保留带连字符/点号的节点 id', () {
      final d = flow('flowchart LR\nmy-node-->n2\na.b --> c');
      expect(d.edges.map(sig).toList(), [
        'my-node-solidArrow->n2',
        'a.b-solidArrow->c',
      ]);
    });

    test('更长链路 ---> / ----> / ===> / -..-> 归一到对应样式', () {
      final d = flow(
        'flowchart LR\nA ---> B\nB ----> C\nC ===> D\nD -..-> E\nE ---- F',
      );
      expect(d.edges.map(sig).toList(), [
        'A-solidArrow->B',
        'B-solidArrow->C',
        'C-thickArrow->D',
        'D-dashedArrow->E',
        'E-solidLine->F',
      ]);
    });

    test('带文字的更长实线箭头 -- t --->', () {
      final d = flow('flowchart LR\nA -- 步骤 ---> B');
      expect(sig(d.edges.single), 'A-solidArrow(步骤)->B');
    });

    test('目标侧 & 链 A --> B & C', () {
      final d = flow('flowchart LR\nA --> B & C');
      expect(d.edges.map(sig).toList(), [
        'A-solidArrow->B',
        'A-solidArrow->C',
      ]);
    });

    test('源侧与目标侧 & 笛卡尔连接 A & B --> C & D', () {
      final d = flow('flowchart LR\nA & B --> C & D');
      expect(d.edges.map(sig).toList(), [
        'A-solidArrow->C',
        'A-solidArrow->D',
        'B-solidArrow->C',
        'B-solidArrow->D',
      ]);
    });

    test('目标侧 & 后继续链式 A --> B & C --> D', () {
      final d = flow('flowchart LR\nA --> B & C --> D');
      expect(d.edges.map(sig).toList(), [
        'A-solidArrow->B',
        'A-solidArrow->C',
        'B-solidArrow->D',
        'C-solidArrow->D',
      ]);
    });

    test('紧贴 o/x 连接符按 mermaid 语义生成圆/叉边 A---oB', () {
      final d = flow('flowchart LR\nA---oB\nC---xD');
      expect(d.edges.map(sig).toList(), [
        'A-circle->B',
        'C-cross->D',
      ]);
    });
  });

  group('第二批兼容:双向/双端箭头 + ::: 类简写', () {
    ChatMermaidFlowchart flow(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidFlowchart;
    }

    String sig(ChatMermaidEdge e) => '${e.sourceId}-${e.style.name}->${e.targetId}';

    test('双向箭头 <-->/<-.->/<==> 映射到对应单端样式', () {
      final d = flow('flowchart LR\nA <--> B\nB <-.-> C\nC <==> D');
      expect(d.edges.map(sig).toList(), [
        'A-solidArrow->B',
        'B-dashedArrow->C',
        'C-thickArrow->D',
      ]);
    });

    test('双向箭头无空格 A<-->B', () {
      final d = flow('flowchart LR\nA<-->B');
      expect(sig(d.edges.single), 'A-solidArrow->B');
    });

    test('双端 o--o / x--x 及单端 o--> / x-->', () {
      final d = flow('flowchart LR\nA o--o B\nC x--x D\nE o--> F\nG x--> H');
      expect(d.edges.map(sig).toList(), [
        'A-circle->B',
        'C-cross->D',
        'E-circle->F',
        'G-cross->H',
      ]);
    });

    test('::: 类简写在裸 id 上被解析并忽略', () {
      final d = flow('flowchart LR\nA:::foo --> B\nclassDef foo fill:#f9f');
      expect(d.nodes.map((n) => n.id).toList(), ['A', 'B']);
      expect(sig(d.edges.single), 'A-solidArrow->B');
    });

    test('::: 类简写在带形状节点及链式上被忽略', () {
      final d = flow('flowchart LR\nA[开始]:::foo --> B[结束]:::bar --> C');
      expect(d.nodes.firstWhere((n) => n.id == 'A').label, '开始');
      expect(d.nodes.firstWhere((n) => n.id == 'B').label, '结束');
      expect(d.edges.map(sig).toList(), [
        'A-solidArrow->B',
        'B-solidArrow->C',
      ]);
    });

    test('以 o/x 开头的节点 id 不被误判为双端边', () {
      final d = flow('flowchart LR\nox1 --> oxen');
      expect(d.nodes.map((n) => n.id).toList(), ['ox1', 'oxen']);
      expect(sig(d.edges.single), 'ox1-solidArrow->oxen');
    });
  });

  group('第三批兼容:双圆形等少见形状', () {
    ChatMermaidFlowchart flow(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidFlowchart;
    }

    test('双圆形 (((text))) 映射为 circle,且不破坏后续解析', () {
      final d = flow('flowchart LR\nA(((停止))) --> B');
      final a = d.nodes.firstWhere((n) => n.id == 'A');
      expect(a.shape, ChatMermaidNodeShape.circle);
      expect(a.label, '停止');
      expect(d.edges.single.targetId, 'B');
    });

    test('双圆形无空格 / 作为目标节点', () {
      final d = flow('flowchart LR\nA(((停止)))-->B(((结束)))');
      expect(d.nodes.firstWhere((n) => n.id == 'A').shape,
          ChatMermaidNodeShape.circle);
      final b = d.nodes.firstWhere((n) => n.id == 'B');
      expect(b.shape, ChatMermaidNodeShape.circle);
      expect(b.label, '结束');
    });

    test('单圆形 ((text)) 与圆角 (text) 不受影响', () {
      final d = flow('flowchart LR\nA((圆)) --> B(圆角) --> C([体育场])');
      expect(d.nodes.firstWhere((n) => n.id == 'A').shape,
          ChatMermaidNodeShape.circle);
      expect(d.nodes.firstWhere((n) => n.id == 'B').shape,
          ChatMermaidNodeShape.rounded);
      expect(d.nodes.firstWhere((n) => n.id == 'C').shape,
          ChatMermaidNodeShape.stadium);
    });
  });

  group('第二轮兼容:时序/状态/类/ER/甘特/流程图扩展', () {
    test('时序图:activate/deactivate 被消费,参与者 id 干净', () {
      final r = parser.parse(
          'sequenceDiagram\nA->>+B: hi\nactivate B\nB-->>-A: bye\ndeactivate B');
      expect(r.isSupported, isTrue);
      final d = r.diagram as ChatMermaidSequenceDiagram;
      expect(d.participants.map((p) => p.id).toList(), ['A', 'B']);
    });

    test('时序图:box 分组与 rect 高亮块及其 end 不破坏分组配平', () {
      final r = parser.parse(
        'sequenceDiagram\nbox 紫色 G\nparticipant A\nparticipant B\nend\n'
        'alt 条件\nrect rgb(0,0,0)\nA->>B: 1\nend\nelse 否则\nA->>B: 2\nend',
      );
      expect(r.isSupported, isTrue);
      final d = r.diagram as ChatMermaidSequenceDiagram;
      expect(d.participants.map((p) => p.id).toList(), ['A', 'B']);
    });

    test('时序图:create/destroy participant', () {
      final r = parser.parse(
          'sequenceDiagram\nA->>B: hi\ncreate participant C\nA->>C: hi\ndestroy C');
      expect(r.isSupported, isTrue);
      final d = r.diagram as ChatMermaidSequenceDiagram;
      expect(d.participants.map((p) => p.id).toSet(), {'A', 'B', 'C'});
    });

    test('时序图:participant "引号名" 正确去掉引号，不把引号留在 id 里', () {
      final r = parser.parse('''
sequenceDiagram
participant "Alice"
participant "Bob"
Alice ->> Bob: 你好
''');
      expect(r.isSupported, isTrue);
      final d = r.diagram as ChatMermaidSequenceDiagram;
      expect(d.participants.any((p) => p.id == 'Alice' && p.label == 'Alice'),
          isTrue);
      expect(d.participants.any((p) => p.id == 'Bob' && p.label == 'Bob'),
          isTrue);
    });

    test('时序图:participant "含空格名称" 正确解析，不因空格导致 match 失败', () {
      final r = parser.parse('''
sequenceDiagram
participant "Alice Smith" as AS
participant "Bob Jones" as BJ
AS ->> BJ: 请求
BJ -->> AS: 响应
''');
      expect(r.isSupported, isTrue);
      final d = r.diagram as ChatMermaidSequenceDiagram;
      final p = d.participants;
      expect(p.any((x) => x.id == 'AS' && x.label == 'Alice Smith'), isTrue);
      expect(p.any((x) => x.id == 'BJ' && x.label == 'Bob Jones'), isTrue);
    });

    test('状态图:fork/join/choice 节点与并发分隔 --', () {
      final r = parser.parse(
        'stateDiagram-v2\nstate fk <<fork>>\n[*] --> fk\nfk --> A\nfk --> B\n'
        'state P {\n[*] --> X\n--\n[*] --> Y\n}',
      );
      expect(r.isSupported, isTrue);
      final d = r.diagram as ChatMermaidStateDiagram;
      expect(d.nodes.any((n) => n.id == 'fk'), isTrue);
    });

    test('类图:泛型 ~T~ 解析为 <T> 标签且不破坏成员', () {
      final r = parser.parse('classDiagram\nclass Box~T~ {\n+items List~int~\n}');
      expect(r.isSupported, isTrue);
      final d = r.diagram as ChatMermaidClassDiagram;
      final box = d.classes.firstWhere((c) => c.id == 'Box');
      expect(box.label, 'Box<T>');
      expect(box.members.any((m) => m.contains('List<int>')), isTrue);
    });

    test('ER 图:属性块注册实体 + 非标识关系 ..', () {
      final r = parser.parse(
        'erDiagram\nCUSTOMER {\nstring name\nint id PK\n}\n'
        'CUSTOMER ||..o{ ORDER : places',
      );
      expect(r.isSupported, isTrue);
      final d = r.diagram as ChatMermaidErDiagram;
      expect(d.entities.map((e) => e.id).toSet(), {'CUSTOMER', 'ORDER'});
      expect(d.relations, hasLength(1));
    });

    test('甘特图:状态标签 done/active 与里程碑 0d', () {
      final r = parser.parse(
        'gantt\ndateFormat YYYY-MM-DD\nsection S\n'
        'A :done, a1, 2024-01-01, 3d\nB :active, after a1, 2d\n'
        'M :milestone, m1, 2024-01-10, 0d',
      );
      expect(r.isSupported, isTrue);
      final d = r.diagram as ChatMermaidGanttDiagram;
      final tasks = d.sections.single.tasks;
      expect(tasks.map((t) => t.label).toList(), ['A', 'B', 'M']);
      expect(tasks.firstWhere((t) => t.label == 'M').durationDays, 1);
    });

    test('流程图:@{ shape: } 新形状语法与标签', () {
      final r = parser
          .parse('flowchart TD\nA@{ shape: circle, label: "圆" } --> B@{ shape: diam }');
      expect(r.isSupported, isTrue);
      final d = r.diagram as ChatMermaidFlowchart;
      final a = d.nodes.firstWhere((n) => n.id == 'A');
      expect(a.shape, ChatMermaidNodeShape.circle);
      expect(a.label, '圆');
      expect(d.nodes.firstWhere((n) => n.id == 'B').shape,
          ChatMermaidNodeShape.diamond);
    });

    test('流程图:边 ID 前缀 e1@--> 被剥离', () {
      final r = parser.parse('flowchart LR\nA e1@--> B\nB e2@-.-> C');
      expect(r.isSupported, isTrue);
      final d = r.diagram as ChatMermaidFlowchart;
      expect(d.edges.map((e) => '${e.sourceId}-${e.style.name}->${e.targetId}')
          .toList(), [
        'A-solidArrow->B',
        'B-dashedArrow->C',
      ]);
    });
  });

  group('Timeline 时间线图', () {
    ChatMermaidTimelineDiagram timeline(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidTimelineDiagram;
    }

    test('解析标题与时间段、多事件(冒号分隔)', () {
      final d = timeline(
        'timeline\ntitle 社媒发展\n2002 : LinkedIn\n2004 : Facebook : Google',
      );
      expect(d.title, '社媒发展');
      expect(d.sections, hasLength(1));
      expect(d.sections.single.title, '');
      final periods = d.sections.single.periods;
      expect(periods.map((p) => p.label).toList(), ['2002', '2004']);
      expect(periods[1].events, ['Facebook', 'Google']);
    });

    test('解析多个分组', () {
      final d = timeline(
        'timeline\nsection 2000s\n2002 : A\n2004 : B\n'
        'section 2010s\n2010 : Instagram',
      );
      expect(d.sections.map((s) => s.title).toList(), ['2000s', '2010s']);
      expect(d.sections[0].periods, hasLength(2));
      expect(d.sections[1].periods.single.events, ['Instagram']);
    });

    test('以冒号开头的续行把事件追加到上一个时间段', () {
      final d = timeline('timeline\n2002 : A\n: B\n: C');
      expect(d.sections.single.periods.single.events, ['A', 'B', 'C']);
    });

    test('时间段可无事件', () {
      final d = timeline('timeline\n2002\n2004 : X');
      expect(d.sections.single.periods[0].events, isEmpty);
      expect(d.sections.single.periods[1].events, ['X']);
    });

    test('仅标题无时间段时不支持(回退原文)', () {
      final result = parser.parse('timeline\ntitle 只有标题');
      expect(result.isSupported, isFalse);
    });
  });

  group('QuadrantChart 象限图', () {
    ChatMermaidQuadrantDiagram quadrant(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidQuadrantDiagram;
    }

    test('解析标题、双轴、四象限标签与数据点', () {
      final d = quadrant(
        'quadrantChart\ntitle 营销活动\n'
        'x-axis 低触达 --> 高触达\ny-axis 低参与 --> 高参与\n'
        'quadrant-1 应扩大\nquadrant-2 需推广\nquadrant-3 重新评估\nquadrant-4 可改进\n'
        'A: [0.3, 0.6]\nB: [0.45, 0.23]',
      );
      expect(d.title, '营销活动');
      expect(d.xAxisLeft, '低触达');
      expect(d.xAxisRight, '高触达');
      expect(d.yAxisBottom, '低参与');
      expect(d.yAxisTop, '高参与');
      expect([d.quadrant1, d.quadrant2, d.quadrant3, d.quadrant4],
          ['应扩大', '需推广', '重新评估', '可改进']);
      expect(d.points.map((p) => p.label).toList(), ['A', 'B']);
      expect(d.points[0].x, closeTo(0.3, 1e-9));
      expect(d.points[0].y, closeTo(0.6, 1e-9));
    });

    test('轴标签去除引号', () {
      final d = quadrant(
        'quadrantChart\nx-axis "Low Reach" --> "High Reach"\nC: [0.5, 0.5]',
      );
      expect(d.xAxisLeft, 'Low Reach');
      expect(d.xAxisRight, 'High Reach');
    });

    test('仅数据点也可渲染', () {
      final d = quadrant('quadrantChart\nA: [0.1, 0.9]\nB: [0.8, 0.2]');
      expect(d.points, hasLength(2));
    });

    test('越界坐标被裁剪到 0..1', () {
      final d = quadrant('quadrantChart\nX: [1.5, -0.2]');
      expect(d.points.single.x, 1.0);
      expect(d.points.single.y, 0.0);
    });

    test('仅标题无内容时不支持(回退原文)', () {
      final result = parser.parse('quadrantChart\ntitle 只有标题');
      expect(result.isSupported, isFalse);
    });
  });

  group('Sankey 桑基流向图', () {
    ChatMermaidSankeyDiagram sankey(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidSankeyDiagram;
    }

    test('解析 CSV 三列、节点去重与连接顺序', () {
      final d = sankey('sankey-beta\nA,B,10\nB,C,6\nB,D,4');
      expect(d.nodes.map((n) => n.id).toList(), ['A', 'B', 'C', 'D']);
      expect(
        d.links.map((l) => '${l.sourceId}>${l.targetId}=${l.value}').toList(),
        ['A>B=10.0', 'B>C=6.0', 'B>D=4.0'],
      );
    });

    test('跳过空行与注释', () {
      final d = sankey('sankey-beta\n\n%% 说明\nA,B,5');
      expect(d.links, hasLength(1));
    });

    test('引号字段内的逗号不作分隔', () {
      final d = sankey('sankey-beta\n"农业废料, 原料",Bio,124.7');
      expect(d.nodes.first.id, '农业废料, 原料');
      expect(d.links.single.value, closeTo(124.7, 1e-9));
    });

    test('双引号转义 "" 表示字面引号', () {
      final d = sankey('sankey-beta\n"He said ""hi""",B,3');
      expect(d.nodes.first.id, 'He said "hi"');
    });

    test('跳过非法或非正数值', () {
      final d = sankey('sankey-beta\nA,B,foo\nA,C,0\nA,D,5');
      expect(d.links.map((l) => l.targetId).toList(), ['D']);
    });

    test('sankey 别名等同 sankey-beta', () {
      final d = sankey('sankey\nA,B,1');
      expect(d.links, hasLength(1));
    });

    test('无有效连接时不支持(回退原文)', () {
      final result = parser.parse('sankey-beta\n%% 仅注释');
      expect(result.isSupported, isFalse);
    });
  });

  group('Radar 雷达图', () {
    ChatMermaidRadarDiagram radar(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidRadarDiagram;
    }

    test('解析轴与位置曲线,默认 min=0 自动 max', () {
      final d = radar(
        'radar-beta\naxis A, B, C, D, E\ncurve c1{1,2,3,4,5}\ncurve c2{5,4,3,2,1}',
      );
      expect(d.axes.map((a) => a.id).toList(), ['A', 'B', 'C', 'D', 'E']);
      expect(d.minValue, 0);
      expect(d.maxValue, 5);
      expect(d.curves, hasLength(2));
      expect(d.curves[0].values, [1, 2, 3, 4, 5]);
    });

    test('解析标题、带标签轴/曲线与各项选项', () {
      final d = radar(
        'radar-beta\ntitle 成绩\n'
        'axis m["数学"], s["科学"], e["英语"]\n'
        'curve a["小明"]{85,90,80}\ncurve b["小红"]{70,75,85}\n'
        'max 100\nmin 0\ngraticule polygon\nticks 4\nshowLegend false',
      );
      expect(d.title, '成绩');
      expect(d.axes.map((a) => a.label).toList(), ['数学', '科学', '英语']);
      expect(d.curves.map((c) => c.label).toList(), ['小明', '小红']);
      expect(d.maxValue, 100);
      expect(d.ticks, 4);
      expect(d.graticule, ChatMermaidRadarGraticule.polygon);
      expect(d.showLegend, isFalse);
    });

    test('键值形式曲线按轴 id 对齐', () {
      final d = radar(
        'radar-beta\naxis a1, a2, a3\ncurve k{ a3: 30, a1: 20, a2: 10 }',
      );
      expect(d.curves.single.values, [20, 10, 30]);
      expect(d.maxValue, 30);
    });

    test('同一行多条曲线', () {
      final d = radar('radar-beta\naxis a1, a2, a3\ncurve c1{1,2,3}, c2{3,2,1}');
      expect(d.curves.map((c) => c.id).toList(), ['c1', 'c2']);
    });

    test('少于 3 个轴或无曲线时不支持(回退原文)', () {
      expect(parser.parse('radar-beta\naxis a1, a2\ncurve c{1,2}').isSupported,
          isFalse);
      expect(parser.parse('radar-beta\naxis a1,a2,a3').isSupported, isFalse);
    });
  });

  group('Kanban 看板图', () {
    ChatMermaidKanbanDiagram kanban(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidKanbanDiagram;
    }

    test('按缩进区分列与任务,解析 id[标题]', () {
      final d = kanban(
        'kanban\n  Todo\n    [创建文档]\n    docs[写博客]\n'
        '  [进行中]\n    id6[实现渲染]\n  id11[Done]\n    id5[定义 getData]',
      );
      expect(d.columns.map((c) => c.title).toList(),
          ['Todo', '进行中', 'Done']);
      expect(d.columns[0].items.map((i) => i.text).toList(), ['创建文档', '写博客']);
      expect(d.columns[0].items[1].id, 'docs');
      expect(d.columns[2].id, 'id11');
    });

    test('解析任务 @{} 元数据(ticket/assigned/priority)', () {
      final d = kanban(
        "kanban\n  id10[Ready]\n"
        "    id4[Create parsing tests]@{ ticket: MC-2038, assigned: 'K.S', priority: 'High' }\n"
        "    id66[last item]@{ priority: 'Very Low', assigned: knsv }",
      );
      final items = d.columns.single.items;
      expect(items[0].ticket, 'MC-2038');
      expect(items[0].assigned, 'K.S');
      expect(items[0].priority, 'High');
      expect(items[1].priority, 'Very Low');
      expect(items[1].assigned, 'knsv');
      expect(items[1].text, 'last item');
    });

    test('支持中文列名/任务名与中文元数据值', () {
      final d = kanban(
        "kanban\n  待办\n    需求评审\n    任务A@{ assigned: 张三, priority: 'High' }\n"
        '  进行中\n    开发登录页',
      );
      expect(d.columns.map((c) => c.title).toList(), ['待办', '进行中']);
      expect(d.columns[0].items[1].assigned, '张三');
    });

    test('允许无任务的空列', () {
      final d = kanban('kanban\n  Backlog\n  Active\n    work[做事]');
      expect(d.columns[0].items, isEmpty);
      expect(d.columns[1].items.single.text, '做事');
    });

    test('无列时不支持(回退原文)', () {
      expect(parser.parse('kanban\n%% 仅注释').isSupported, isFalse);
    });
  });

  group('Treemap 矩形树图', () {
    ChatMermaidTreemapDiagram treemap(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidTreemapDiagram;
    }

    test('按缩进建树,分组取值为后代之和', () {
      final d = treemap(
        'treemap-beta\n"Section 1"\n    "Leaf 1.1": 12\n    "Section 1.2"\n'
        '      "Leaf 1.2.1": 12\n"Section 2"\n    "Leaf 2.1": 20\n    "Leaf 2.2": 25',
      );
      expect(d.roots.map((n) => n.label).toList(), ['Section 1', 'Section 2']);
      expect(d.roots[0].value, 24); // 12 + 12
      expect(d.roots[0].isLeaf, isFalse);
      expect(d.roots[0].children[1].label, 'Section 1.2');
      expect(d.roots[0].children[1].value, 12);
      expect(d.roots[1].value, 45); // 20 + 25
      expect(d.roots[1].children[0].isLeaf, isTrue);
    });

    test('千分位与小数取值', () {
      final d = treemap('treemap-beta\n"A": 1,200\n"B": 3.5');
      expect(d.roots[0].value, 1200);
      expect(d.roots[1].value, closeTo(3.5, 1e-9));
    });

    test(':::class 被忽略且不影响层级', () {
      final d = treemap('treemap-beta\n"Root":::big\n  "X": 10');
      expect(d.roots.single.label, 'Root');
      expect(d.roots.single.value, 10);
      expect(d.roots.single.children.single.label, 'X');
    });

    test('兼容无引号的中文分组与叶', () {
      final d = treemap('treemap-beta\n预算\n  人力: 50\n  营销: 30');
      expect(d.roots.single.label, '预算');
      expect(d.roots.single.value, 80);
      expect(d.roots.single.children.map((n) => n.label).toList(),
          ['人力', '营销']);
    });

    test('全为零值或无节点时不支持(回退原文)', () {
      expect(parser.parse('treemap-beta\n"A"\n"B"').isSupported, isFalse);
      expect(parser.parse('treemap-beta\n%% c').isSupported, isFalse);
    });
  });

  group('Block 块图', () {
    ChatMermaidBlockDiagram block(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidBlockDiagram;
    }

    test('一行多块,默认列数为该行块数', () {
      final d = block('block-beta\n  a b c');
      expect(d.columns, 3);
      expect(d.items.map((i) => i.id).toList(), ['a', 'b', 'c']);
      expect(d.items.every((i) => !i.isComposite && !i.isSpace), isTrue);
    });

    test('columns 指令与换行', () {
      final d = block('block-beta\n  columns 3\n  a b c\n  d');
      expect(d.columns, 3);
      expect(d.items, hasLength(4));
    });

    test('块形状与列跨度 :width', () {
      final d = block('block-beta\n  columns 3\n  a["A"] b:2\n  '
          'id1(("circle")) id2{"rhombus"} db[("DB")]');
      expect(d.items.firstWhere((i) => i.id == 'b').width, 2);
      expect(d.items.firstWhere((i) => i.id == 'id1').shape,
          ChatMermaidNodeShape.circle);
      expect(d.items.firstWhere((i) => i.id == 'id2').shape,
          ChatMermaidNodeShape.diamond);
      expect(d.items.firstWhere((i) => i.id == 'db').shape,
          ChatMermaidNodeShape.cylindrical);
    });

    test('space 与 space:n 占位块', () {
      final d = block('block-beta\n  columns 3\n  a space:2 b');
      final space = d.items.firstWhere((i) => i.isSpace);
      expect(space.width, 2);
    });

    test('复合块默认/显式列数与嵌套子块', () {
      final d = block(
        'block-beta\n  columns 2\n  block:grp\n    e f\n  end\n  g',
      );
      final grp = d.items.firstWhere((i) => i.isComposite);
      expect(grp.compositeColumns, 2);
      expect(grp.children.map((c) => c.id).toList(), ['e', 'f']);
      final explicit = block('block-beta\n  block:g2:3\n    a b c\n  end');
      expect(explicit.items.single.compositeColumns, 3);
    });

    test('箭头边:无文字与带文字', () {
      final d = block('block-beta\n  a\n  b\n  a --> b\n  a -- "links" --> b');
      expect(d.edges, hasLength(2));
      expect(d.edges[0].label, isNull);
      expect(d.edges[1].label, 'links');
      expect(d.edges[1].sourceId, 'a');
      expect(d.edges[1].targetId, 'b');
    });

    test('无块时不支持(回退原文)', () {
      expect(parser.parse('block-beta\n%% c').isSupported, isFalse);
    });
  });

  group('Packet 数据包图', () {
    ChatMermaidPacketDiagram packet(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidPacketDiagram;
    }

    test('解析标题、单比特与范围字段', () {
      final d = packet(
        'packet-beta\ntitle UDP\n0-15: "Source Port"\n16-31: "Dest Port"\n'
        '32-63: "Length"',
      );
      expect(d.title, 'UDP');
      expect(d.fields.map((f) => f.label).toList(),
          ['Source Port', 'Dest Port', 'Length']);
      expect(d.fields[0].start, 0);
      expect(d.fields[0].end, 15);
      expect(d.fields[0].bitCount, 16);
    });

    test('单比特字段 start==end', () {
      final d = packet('packet\n0: "Flag"\n1-7: "Rest"');
      expect(d.fields[0].start, 0);
      expect(d.fields[0].end, 0);
      expect(d.fields[1].bitCount, 7);
    });

    test('+C 位计数从上一字段末尾自动起始', () {
      final d = packet('packet\n+1: "A"\n+8: "B"\n9-15: "Mixed"');
      expect(d.fields[0].start, 0);
      expect(d.fields[0].end, 0);
      expect(d.fields[1].start, 1);
      expect(d.fields[1].end, 8);
      expect(d.fields[2].start, 9);
      expect(d.fields[2].end, 15);
    });

    test('行内 %% 注释被剥离、标签去引号', () {
      final d = packet('packet\n0-7: "X" %% first byte\n8-15: Y');
      expect(d.fields[0].label, 'X');
      expect(d.fields[1].label, 'Y');
    });

    test('每行比特数:超 32 用 32,否则用总位数', () {
      expect(packet('packet\n0-31: "R1"\n32-63: "R2"').bitsPerRow, 32);
      expect(packet('packet\n0-3: "Nibble"').bitsPerRow, 4);
    });

    test('无字段时不支持(回退原文)', () {
      expect(parser.parse('packet\n%% c').isSupported, isFalse);
    });
  });

  group('RequirementDiagram 需求图', () {
    ChatMermaidRequirementDiagram req(String src) {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: src);
      return result.diagram as ChatMermaidRequirementDiagram;
    }

    test('解析需求块(各字段)与元素块,及正向关系', () {
      final d = req(
        'requirementDiagram\nrequirement test_req {\nid: 1\ntext: the test text.\n'
        'risk: high\nverifymethod: test\n}\nelement test_entity {\ntype: simulation\n'
        'docref: reqs/doc1\n}\ntest_entity - satisfies -> test_req',
      );
      expect(d.requirements.single.name, 'test_req');
      expect(d.requirements.single.id, '1');
      expect(d.requirements.single.risk, 'high');
      expect(d.requirements.single.verifyMethod, 'test');
      expect(d.elements.single.name, 'test_entity');
      expect(d.elements.single.elementType, 'simulation');
      expect(d.elements.single.docref, 'reqs/doc1');
      expect(d.relations.single.sourceName, 'test_entity');
      expect(d.relations.single.type, 'satisfies');
      expect(d.relations.single.targetName, 'test_req');
    });

    test('解析多种需求类型', () {
      final d = req(
        'requirementDiagram\nfunctionalRequirement FR {\nid: 1\ntext: t\n}\n'
        'designConstraint DC {\nid: 2\ntext: t\n}',
      );
      expect(d.requirements[0].kind,
          ChatMermaidRequirementKind.functionalRequirement);
      expect(d.requirements[1].kind,
          ChatMermaidRequirementKind.designConstraint);
    });

    test('反向关系 <- type -', () {
      final d = req(
        'requirementDiagram\nrequirement A {\nid: a\ntext: t\n}\n'
        'requirement B {\nid: b\ntext: t\n}\nB <- derives - A',
      );
      expect(d.relations.single.sourceName, 'A');
      expect(d.relations.single.targetName, 'B');
      expect(d.relations.single.type, 'derives');
    });

    test('direction/style 忽略、引号名称', () {
      final d = req(
        'requirementDiagram\ndirection LR\nrequirement "My Req" {\n'
        'id: 1\ntext: "quoted"\nrisk: high\nverifymethod: test\n}',
      );
      expect(d.requirements.single.name, 'My Req');
      expect(d.requirements.single.text, 'quoted');
    });

    test('无内容时不支持(回退原文)', () {
      expect(parser.parse('requirementDiagram\n%% c').isSupported, isFalse);
    });
  });

  group('Frontmatter 剥离', () {
    test('带 YAML frontmatter 的 flowchart 正常解析', () {
      final result = parser.parse('''
---
config:
  theme: base
---
flowchart LR
A --> B
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidFlowchart;
      expect(d.nodes, hasLength(2));
    });

    test('带 frontmatter 的 xychart-beta 正常解析', () {
      final result = parser.parse('''
---
config:
  theme: base
  themeVariables:
    primaryColor: "#002FA7"
---
xychart-beta
    title "核心速度指标"
    x-axis ["总提交", "日均", "单日最高", "连续天数"]
    y-axis "次数" 0 --> 7000
    bar [6219, 53, 145, 118]
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidXyChartDiagram;
      expect(d.barSeries, hasLength(1));
      expect(d.barSeries.first, hasLength(4));
    });

    test('只有 frontmatter 无图表内容时不支持', () {
      final result = parser.parse('''
---
config:
  theme: base
---
''');
      expect(result.isSupported, isFalse);
    });
  });

  group('XyChart 完整规范', () {
    test('xychart 关键字(无 -beta)', () {
      final result = parser.parse('''
xychart
    title "Sales"
    x-axis ["Q1", "Q2", "Q3"]
    bar [100, 200, 300]
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidXyChartDiagram;
      expect(d.barSeries.first, [100, 200, 300]);
      expect(d.horizontal, isFalse);
    });

    test('xychart horizontal', () {
      final result = parser.parse('''
xychart horizontal
    title "Sales"
    x-axis ["Q1", "Q2"]
    bar [10, 20]
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidXyChartDiagram;
      expect(d.horizontal, isTrue);
    });

    test('xychart-beta horizontal', () {
      final result = parser.parse('''
xychart-beta horizontal
    title "Sales"
    x-axis ["Q1", "Q2"]
    bar [10, 20]
''');
      expect(result.isSupported, isTrue);
      expect((result.diagram as ChatMermaidXyChartDiagram).horizontal, isTrue);
    });

    test('y-axis min --> max 范围', () {
      final result = parser.parse('''
xychart-beta
    x-axis ["A", "B"]
    y-axis "次数" 0 --> 7000
    bar [100, 200]
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidXyChartDiagram;
      expect(d.yAxisTitle, '次数');
      expect(d.yAxisMin, 0);
      expect(d.yAxisMax, 7000);
    });

    test('x-axis 标签去引号', () {
      final result = parser.parse('''
xychart-beta
    x-axis ["总提交", "日均", "单日最高"]
    bar [100, 200, 300]
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidXyChartDiagram;
      expect(d.xAxisLabels, ['总提交', '日均', '单日最高']);
    });

    test('多组 bar 数据', () {
      final result = parser.parse('''
xychart-beta
    x-axis ["A", "B"]
    bar [10, 20]
    bar [30, 40]
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidXyChartDiagram;
      expect(d.barSeries, hasLength(2));
      expect(d.barSeries[0], [10, 20]);
      expect(d.barSeries[1], [30, 40]);
    });

    test('多组 line 数据', () {
      final result = parser.parse('''
xychart-beta
    x-axis ["A", "B"]
    line [10, 20]
    line [30, 40]
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidXyChartDiagram;
      expect(d.lineSeries, hasLength(2));
    });

    test('x-axis 数值范围 min --> max', () {
      final result = parser.parse('''
xychart-beta
    x-axis "Year" 2020 --> 2024
    bar [10, 20, 30, 40, 50]
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidXyChartDiagram;
      expect(d.xAxisTitle, 'Year');
      expect(d.xAxisLabels, isNotEmpty);
    });

    test('无数据时不支持', () {
      expect(parser.parse('xychart-beta\n%% empty').isSupported, isFalse);
    });
  });

  group('Gantt 增强', () {
    test('非 YYYY-MM-DD dateFormat 不再拒绝', () {
      final result = parser.parse('''
gantt
    dateFormat DD-MM-YYYY
    title A Project
    section Phase 1
    Task A: 2024-01-01, 5d
''');
      expect(result.isSupported, isTrue);
    });

    test('任意 axisFormat 接受', () {
      final result = parser.parse('''
gantt
    axisFormat %d/%m
    title A Project
    section Phase 1
    Task A: 2024-01-01, 5d
''');
      expect(result.isSupported, isTrue);
    });

    test('无 section 的任务自动分组', () {
      final result = parser.parse('''
gantt
    Task A: 2024-01-01, 5d
    Task B: 2024-01-06, 3d
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidGanttDiagram;
      expect(d.sections, hasLength(1));
      expect(d.sections.first.title, isEmpty);
    });

    test('excludes/todayMarker 等指令被忽略', () {
      final result = parser.parse('''
gantt
    excludes weekends
    todayMarker off
    tickInterval 1day
    section Phase 1
    Task A: 2024-01-01, 5d
''');
      expect(result.isSupported, isTrue);
    });

    test('小时/月/年等时长单位', () {
      final result = parser.parse('''
gantt
    section Phase 1
    Hourly task: 2024-01-01, 12h
    Monthly task: 2024-02-01, 2M
    Yearly task: 2024-03-01, 1y
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidGanttDiagram;
      final tasks = d.sections.first.tasks;
      expect(tasks[0].durationDays, 1);
      expect(tasks[1].durationDays, 60);
      expect(tasks[2].durationDays, 365);
    });

    test('结束日期代替时长', () {
      final result = parser.parse('''
gantt
    section Phase 1
    Task A: 2024-01-01, 2024-01-15
''');
      expect(result.isSupported, isTrue);
      final d = result.diagram as ChatMermaidGanttDiagram;
      expect(d.sections.first.tasks.first.durationDays, 14);
    });
  });
}
