import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_flowchart_layout.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_parser.dart';
import 'package:grix/shared/mermaid/chat_mermaid_sequence_layout.dart';

/// 回归集：线上会话 5040ec36-270b-4148-961a-9664df0ac0c3 的消息气泡里出现过
/// 「流程图无法渲染 / 几个分组框压在一块」的全部图表，逐张固化下来防止回归。
///
/// 失败历史：
/// - 「边直接连分组框」的图（如 O / E / Q）旧实现整体塌缩成一行。
/// - 「一个中心节点同时连多个分组」的图（C）三个分组框并排时横向叠加。
void main() {
  const parser = ChatMermaidParser();
  const flowEngine = ChatMermaidFlowchartLayoutEngine();
  const seqEngine = ChatMermaidSequenceLayoutEngine();
  const textStyle = TextStyle(fontSize: 12);

  // 会话里出现过的全部不同图表（A–R，含 2 张时序图）。
  const cases = <String, String>{
    'A_封面_TD链': '''
flowchart TD
    T["GRIX"]
    S1["AI 优先 · 移动端优先"]
    S2["即时通讯平台"]
    S3["与 Agent 对话 · 调度 Agent 工作"]
    T --> S1 --> S2 --> S3
''',
    'B_Slide2_Old-Gap': '''
flowchart LR
    subgraph Old["传统聊天 App"]
        O1["只能跟人聊"]
        O2["AI 只是内嵌 Bot"]
        O3["场景有限<br/>问个天气可以<br/>做复杂工作不行"]
    end
    subgraph Gap["缺什么"]
        G1["❌ Agent 不是一等公民"]
        G2["❌ 不能调度 Agent 干活"]
        G3["❌ 移动端体验弱"]
    end
    Old --> Gap
''',
    'C_Slide3_TB_三分组': '''
flowchart TB
    G["GRIX<br/>即时通讯平台"]
    subgraph People["👤 人"]
        User["你跟 Agent 对话<br/>就像跟朋友聊天"]
    end
    subgraph Agent["🤖 Agent"]
        A1["Claude"]
        A2["DeepSeek"]
        A3["更多..."]
    end
    subgraph Work["⚡ 调度工作"]
        W1["写代码"]
        W2["做分析"]
        W3["生成文档"]
    end
    G --- People
    G --- Agent
    G --- Work
    style G fill:#4a6cf7,color:#fff
''',
    'D_Slide4_seq': '''
sequenceDiagram
    participant U as 👤 你（手机端）
    participant G as GRIX 平台
    participant C as 🧠 Claude Agent
    U->>G: "帮我分析这份财报"
    Note over G: 识别意图，调度 Claude
    G->>C: 启动分析任务
    C-->>G: 返回分析报告
    G-->>U: 对话里直接展示结果
    Note over U,G: 你换到电脑端，会话还在
    U->>G: "画一张趋势图"
    G->>C: 继续同一任务
    C-->>G: 返回图表
    G-->>U: 展示图表
    Note over U,C: 全程像聊天一样自然<br/>设备切换无感知
''',
    'E_Slide5_LR_三分组': '''
flowchart LR
    subgraph Mobile["📱 手机端 Grix"]
        G["即时通讯<br/>对话即调度"]
    end
    subgraph Agents["可对话 / 可调度的 Agent"]
        C["Claude"]
        D["DeepSeek"]
        K["Kiro"]
        X["..."]
    end
    subgraph Devices["跨设备同步"]
        P["电脑端"]
        T["平板端"]
    end
    Mobile --> Agents
    Agents <--> Devices
    G <-->|"同一会话"| P
    G <-->|"同一会话"| T
''',
    'F_Slide6_收尾TD': '''
flowchart TD
    T["跟 Agent 对话<br/>调度 Agent 工作<br/>从手机开始"]
    S["Grix · AI 优先的即时通讯平台"]
    Logo["[ GRIX ]"]
    CTA["📲 扫码下载 / 进入虾塘"]
    T --> S --> Logo --> CTA
    style T fill:#4a6cf7,color:#fff
''',
    'G_封面2_TD': '''
flowchart TD
    G["GRIX"]
    tag["智能体调度 Agent"]
    subtitle["以 Claude 为例 · 移动端随时调度"]
    G --> tag
    tag --> subtitle
''',
    'H_痛点_LR': '''
flowchart LR
    U["👤 你"] -->|"想用 Claude"| Prob["问题"]
    Prob --> P1["❌ 官方 APP -> 仅限官方订阅"]
    Prob --> P2["❌ 第三方 API -> 锁在电脑"]
    Prob --> P3["❌ 换设备 -> 重新加载"]
    P1 --> Pain["😰 Agent 被绑死在固定设备上"]
    P2 --> Pain
    P3 --> Pain
''',
    'I_Slide3_LR_GrixAgent': '''
flowchart LR
    subgraph Grix_Agent["Grix 智能体"]
        G["调度中枢"]
    end
    U["👤 你"] -->|"下达指令"| G
    G -->|"调度 →"| A1["Claude<br/>📱 手机端"]
    G -->|"调度 →"| A2["Claude<br/>💻 电脑端"]
    G -->|"调度 →"| A3["Claude<br/>📟 平板端"]
    A1 <-->|"同一会话<br/>实时同步"| A2
    A2 <-->|"同一会话<br/>实时同步"| A3
''',
    'J_Slide4_seq2': '''
sequenceDiagram
    participant U as 👤 你
    participant G as GRIX 智能体
    participant C as 🧠 Claude Agent
    U->>G: "帮我写一份周报"
    Note over G: 识别意图 → 调度 Claude
    G->>C: 执行写作任务
    C-->>G: 返回周报草稿
    G-->>U: 展示结果
    Note over U,C: 📱 你在手机端发起 → <br/>💻 切换到电脑端继续编辑
    U->>G: "再润色一下"
    G->>C: 继续同一会话调度
    C-->>G: 返回润色版
    G-->>U: 完成
''',
    'K_Slide5_TD_AgentPool': '''
flowchart TD
    G["GRIX 智能体"] -->|"统一调度"| Pool["Agent 池"]
    subgraph Pool["你可以接入的 Agent"]
        C["Claude"]
        D["DeepSeek"]
        K["Kiro"]
        X["...更多"]
    end
    Pool -->|"手机端"| M["📱"]
    Pool -->|"电脑端"| P["💻"]
    Pool -->|"平板端"| T["📟"]
    style G fill:#4a6cf7,color:#fff
''',
    'L_Slide6_收尾TD2': '''
flowchart TD
    Tagline["调度 Agent<br/>从手机开始"]
    Sub["Grix 智能体 · 让你随时随地调度"]
    Logo["[ GRIX ]"]
    CTA["📲 扫码下载 / 进入虾塘"]
    Tagline --> Sub
    Sub --> Logo
    Logo --> CTA
    style Tagline fill:#4a6cf7,color:#fff,font-weight:bold
''',
    'M_封面3_TD': '''
flowchart TD
    title["GRIX · 你的 AI 随身走"]
    subtitle["跨设备无缝同步 × 虾塘生态"]
    devices["📱 手机端  ↔  💻 电脑端  ↔  📟 平板端"]
    sync["↓ 实时同步 ↑"]
    title --> subtitle
    subtitle --> devices
    devices --> sync
''',
    'N_痛点2_LR_三分组': '''
flowchart LR
    subgraph User["用户类型"]
        A["接第三方 API<br/>(DeepSeek等)"]
        B["Claude 官方订阅<br/>但接第三方 API"]
    end
    subgraph Result["结果"]
        C["❌ 无官方移动 APP"]
        D["❌ 只能电脑端使用"]
    end
    subgraph Conclusion["结论"]
        E["😰 离开电脑 = AI 断联"]
    end
    A --> C
    B --> D
    C --> E
    D --> E
''',
    'O_Slide3_TD_三分组': '''
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
''',
    'P_Slide4_对比_LR': '''
flowchart LR
    subgraph Traditional["传统方案"]
        T1["❌ 官方APP仅限官方订阅"]
        T2["❌ 第三方API无移动端"]
        T3["❌ 设备间需手动传输"]
        T4["❌ 换模型＝换工具"]
        T5["❌ 离开电脑＝断联"]
    end
    subgraph Grix_Solution["Grix"]
        G1["✅ 任意API全支持"]
        G2["✅ 第三方API也有移动端"]
        G3["✅ 设备间实时同步"]
        G4["✅ 统一入口任意模型"]
        G5["✅ 随时随地随身走"]
    end
    Traditional -->|VS| Grix_Solution
''',
    'Q_Slide5_TB_GrixPond': '''
flowchart TB
    Grix["Grix 跨设备同步"] --> Pond["🦐 虾塘生态"]
    subgraph Pond_Features["虾塘能力"]
        Agent["🤖 多 Agent 协作"]
        Voice["🎙️ 语音通话"]
        Task["📋 任务审批"]
        Monitor["👀 实时监控"]
    end
    Pond --> Pond_Features
    Pond_Features --> Value["从'能用'→'好用'<br/>真正移动 AI 工作站"]
''',
    'R_Slide6_收尾TD3': '''
flowchart TD
    Tagline["告别电脑前的一亩三分地"]
    Sub["Grix，让你的 AI 随身走"]
    Logo["[ GRIX LOGO ]"]
    CTA["📲 扫码下载 / 进入虾塘"]
    Tagline --> Sub
    Sub --> Logo
    Logo --> CTA
''',
  };

  cases.forEach((name, src) {
    test('会话5040 回归：$name 能解析且布局不塌缩/不重叠', () {
      final result = parser.parse(src);
      expect(result.isSupported, isTrue, reason: '解析失败: ${result.error}');
      final diagram = result.diagram;

      if (diagram is ChatMermaidFlowchart) {
        final layout = flowEngine.layout(
          diagram: diagram,
          textStyle: textStyle,
          labelStyle: textStyle,
          textDirection: TextDirection.ltr,
        );

        // 画布没有塌缩。
        expect(layout.canvasSize.width, greaterThan(1));
        expect(layout.canvasSize.height, greaterThan(1));

        // 节点两两不重叠。
        final nodes = layout.nodeRects.entries.toList();
        for (var i = 0; i < nodes.length; i++) {
          for (var j = i + 1; j < nodes.length; j++) {
            expect(
              nodes[i].value.overlaps(nodes[j].value),
              isFalse,
              reason: '节点 ${nodes[i].key} 与 ${nodes[j].key} 叠加',
            );
          }
        }

        // 分组框两两不重叠（C/O 这类历史故障的核心断言）。
        final boxes = layout.subgraphRects;
        for (var i = 0; i < boxes.length; i++) {
          for (var j = i + 1; j < boxes.length; j++) {
            expect(
              boxes[i].rect.overlaps(boxes[j].rect),
              isFalse,
              reason:
                  '分组框 ${boxes[i].subgraph.id} 与 ${boxes[j].subgraph.id} 叠加',
            );
          }
        }
      } else if (diagram is ChatMermaidSequenceDiagram) {
        final layout = seqEngine.layout(
          diagram: diagram,
          participantStyle: textStyle,
          messageStyle: textStyle,
          noteStyle: textStyle,
          groupStyle: textStyle,
          textDirection: TextDirection.ltr,
        );
        expect(layout.canvasSize.width, greaterThan(1));
        expect(layout.canvasSize.height, greaterThan(1));
      } else {
        fail('非预期图类型: ${diagram.runtimeType}');
      }
    });
  });
}
