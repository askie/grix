import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_flowchart_layout.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_parser.dart';

/// 回归：分组框之间的层级边与真实数据流反向时，展开出的隐形边会与骨架边成环，
/// Sugiyama 拆环把同组成员甩到不同层——分组框互相叠加、连线横穿全图、画布被撑
/// 到数倍宽（老郭反馈的 B806 主板架构图，画布一度算到 10671 × 2641）。
///
/// 这段源码是老郭原样给出的合法 mermaid：既有 `UART --> MB --> METER`
/// （驱动层→协议层的真实数据流），又有 `APP --> PROTO --> DRV --> BASE`
/// （协议层→驱动层的分组层级意图），两者方向相反。
void main() {
  const layoutEngine = ChatMermaidFlowchartLayoutEngine();
  const textStyle = TextStyle(fontSize: 12);

  const source = '''
flowchart TB
    subgraph FIELD["现场侧"]
        MTR["用电采集端<br/>Modbus RTU 9600"]
    end

    subgraph BOARD["B806 主板 · STM32F407VGT6 @168MHz"]
        direction TB

        subgraph APP["应用编排层"]
            FDLINK["fdlink.c 主流程<br/>冷启动→拨号→建链→采集→上报→掉线自愈"]
        end

        subgraph PROTO["协议逻辑层（纯C·可本机单测）"]
            METER["meter.c 采集引擎"]
            MAP["meter_map.c 映射表<br/>待按现场表落实"]
            HJ["hj212.c HJ212组帧<br/>CN=9014登录 / CN=2011数据"]
            CAL["caltime.c 软件日历<br/>QN / DataTime"]
            CFG["devcfg.c 设备配置"]
            RTC["rtc.c 硬件RTC<br/>尚不存在"]
            NOR["nor.c SPI+LittleFS+JSON<br/>尚不存在"]
        end

        subgraph DRV["驱动层"]
            MB["modbus.c<br/>CRC16 事务"]
            UART["uart.c 四路串口<br/>中断收+环形缓冲"]
            FRAM["fram.c 软件I2C"]
        end

        subgraph BASE["芯片基座"]
            CLK["clock.c 25M晶振→168MHz"]
            BRD["board.c GPIO/上电时序"]
            INIT["_init.c freestanding垫片<br/>CMSIS 无HAL"]
        end
    end

    subgraph CHIPS["板上外设"]
        F["MB85RC64 8KB FRAM<br/>序列号/APN/周期"]
        N["MX25L25645 32MB NOR<br/>本应存 MN/服务器地址"]
        R["RX-8025T 硬件RTC<br/>超容备电"]
        MODEM["Neoway N58 4G模块<br/>供电由 PD9 控制"]
    end

    CLOUD["数据平台服务器"]
    DBG["调试口 USB-TTL<br/>看全过程日志"]

    MTR -->|"RS485 · USART2 PA2/PA3 DE=PD1"| UART
    UART --> MB --> METER
    MAP -.->|"读取映射"| METER
    METER -->|"12个定点因子"| HJ
    CAL --> HJ
    CFG --> FDLINK
    HJ --> FDLINK
    METER --> FDLINK
    FDLINK -->|"AT指令 · USART3 PB10/PB11"| UART
    UART -->|"AT\$MYNETWRITE 裸帧"| MODEM
    MODEM -->|"4G / TCP"| CLOUD
    BRD -->|"PD9 供电使能<br/>PD8/PD10 控制脚"| MODEM
    FRAM <-->|"软件I2C PA8/PC9"| F
    CFG --> FRAM
    UART -->|"UART5 PC12 只发"| DBG

    NOR -.->|"应提供 MN/服务器地址<br/>现为硬编码"| FDLINK
    NOR -.->|"SPI 驱动缺失"| N
    RTC -.->|"应提供硬件时间<br/>现用软件日历"| CAL
    RTC -.->|"驱动缺失"| R

    APP --> PROTO --> DRV --> BASE

    style NOR stroke-dasharray: 5 5
    style RTC stroke-dasharray: 5 5
    style MAP stroke-dasharray: 5 5
    style N stroke-dasharray: 5 5
    style R stroke-dasharray: 5 5
''';

  ChatMermaidFlowchartLayout runLayout() {
    final result = const ChatMermaidParser().parse(source);
    final diagram = result.diagram;
    expect(
      diagram,
      isA<ChatMermaidFlowchart>(),
      reason: '解析失败: ${result.error}',
    );
    return layoutEngine.layout(
      diagram: diagram! as ChatMermaidFlowchart,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );
  }

  test('真实数据流方向压过矛盾的分组层级边', () {
    final layout = runLayout();
    final rects = layout.nodeRects;
    // 作者画的真实连线是 UART(驱动层) --> MB(驱动层) --> METER(协议层)，
    // 同时又写了 `PROTO --> DRV`（协议层压在驱动层之上）——两者矛盾。
    // 矛盾时必须以真实连线为准：TB 图里 UART、MB 应排在 METER 之上。
    // 修复前分组边胜出，METER 被抬到 UART 之上，整个骨架翻转、分组框被拉散。
    expect(
      rects['UART']!.top,
      lessThan(rects['METER']!.top),
      reason: 'UART 应排在 METER 之上（UART --> MB --> METER）',
    );
    expect(
      rects['MB']!.top,
      lessThan(rects['METER']!.top),
      reason: 'MB 应排在 METER 之上（MB --> METER）',
    );
    // 同理 HJ、FDLINK 在数据流下游，应排在 METER 之下。
    expect(
      rects['HJ']!.top,
      greaterThan(rects['METER']!.top),
      reason: 'HJ 应排在 METER 之下（METER --> HJ）',
    );
  });

  test('画布不被撑成大片空白（按节点实际范围锁，而非写死像素阈值）', () {
    // 上一版这里写死 width < 9000，而当时实测正好 8535——阈值是照着坏结果配的，
    // 余量只有 5%，而画布尺寸依赖 TextPainter 字体度量，CI 换字体就可能抖动。
    // 改成按「节点实际占用的范围」来锁：画布该被节点撑开，而不是被甩到最右侧的
    // 回边走线通道撑开。
    final layout = runLayout();
    var nodeRight = 0.0;
    var nodeBottom = 0.0;
    for (final rect in layout.nodeRects.values) {
      nodeRight = math.max(nodeRight, rect.right);
      nodeBottom = math.max(nodeBottom, rect.bottom);
    }
    expect(
      layout.canvasSize.width,
      lessThan(nodeRight * 1.6),
      reason:
          '画布宽 ${layout.canvasSize.width} 远超节点实际右边界 $nodeRight，'
          '多出来的是回边走线的空白通道',
    );
    expect(
      layout.canvasSize.height,
      lessThan(nodeBottom * 1.6),
      reason: '画布高 ${layout.canvasSize.height} 远超节点实际下边界 $nodeBottom',
    );
  });

  test('反向数据流写成「节点 --> 分组」时，矛盾同样拦得住', () {
    // 审查抓出的漏判：矛盾的反向数据流不一定写成「节点-->节点」，也可以写成
    // 「节点-->分组」（B1 属于 G2，`B1 --> G1` 实质就是 G2 → G1）。若把这类边
    // 误当成层级边而不是骨架，`G1 --> G2` 的矛盾就查不出来——同一个语义，换个
    // 写法就漏过去，同组的 B1/B2 被拆到不同层（实测层差 236px）。
    ChatMermaidFlowchartLayout run(String src) {
      final chart =
          const ChatMermaidParser().parse(src).diagram! as ChatMermaidFlowchart;
      return layoutEngine.layout(
        diagram: chart,
        textStyle: textStyle,
        labelStyle: textStyle,
        textDirection: TextDirection.ltr,
      );
    }

    const viaGroup = '''
flowchart TB
    subgraph G1["上游组"]
        A1["a1"]
        A2["a2"]
    end
    subgraph G2["下游组"]
        B1["b1"]
        B2["b2"]
    end
    B1 --> G1
    G1 --> G2
''';
    const viaNode = '''
flowchart TB
    subgraph G1["上游组"]
        A1["a1"]
        A2["a2"]
    end
    subgraph G2["下游组"]
        B1["b1"]
        B2["b2"]
    end
    B1 --> A1
    B1 --> A2
    G1 --> G2
''';

    for (final entry in <String, String>{
      '节点-->分组': viaGroup,
      '节点-->节点': viaNode,
    }.entries) {
      final rects = run(entry.value).nodeRects;
      expect(
        (rects['B1']!.top - rects['B2']!.top).abs(),
        lessThan(1),
        reason: '${entry.key} 写法下，同组的 B1/B2 被拆到了不同层',
      );
    }
  });

  test('反向数据流绕几跳才走回来时，矛盾同样拦得住', () {
    // 审查抓出的漏判：反向的数据流常常要绕几跳才回到源头（UART --> BUS --> MB，
    // 中间的 BUS 还不属于任何分组）。只看「有没有一条直接反向边」会漏判——中间
    // 隔一个节点，整个修复就被绕过去，同组成员照样被拆到不同层。判定必须用可达性。
    const viaBus = '''
flowchart TB
    subgraph PROTO["协议层"]
        METER["meter"]
        MAP["map"]
    end
    subgraph DRV["驱动层"]
        UART["uart"]
        MB["mb"]
    end
    UART --> BUS["bus"]
    BUS --> MB
    MB --> METER
    MAP --> METER
    PROTO --> DRV
''';
    final chart =
        const ChatMermaidParser().parse(viaBus).diagram!
            as ChatMermaidFlowchart;
    final rects = layoutEngine
        .layout(
          diagram: chart,
          textStyle: textStyle,
          labelStyle: textStyle,
          textDirection: TextDirection.ltr,
        )
        .nodeRects;
    // 真实数据流 UART -> BUS -> MB -> METER，UART 必须排在 METER 之上；
    // 若绕跳的反向层级边没被拦住，PROTO 会被抬到 DRV 之上、方向翻转。
    expect(
      rects['UART']!.top,
      lessThan(rects['METER']!.top),
      reason: '绕跳的反向层级边没拦住，UART 被压到了 METER 之下',
    );
    expect(
      rects['MB']!.top,
      lessThan(rects['METER']!.top),
      reason: 'MB 应排在 METER 之上（MB --> METER）',
    );
  });

  test('嵌套分组的标题不会压到子框边框（任意字号）', () {
    // 审查指出的隐患：嵌套时顶部让位量若写死，字号一变大就不够用，外层标题会压
    // 到子框的边框上。让位量必须由标题实际高度推出。
    const nested = '''
flowchart TB
    subgraph OUTER["外层主板 B806 · STM32F407VGT6"]
        subgraph INNER["协议逻辑层"]
            A["meter.c"]
            B["hj212.c"]
        end
        A --> B
    end
''';
    for (final fontSize in <double>[12, 16, 20, 26]) {
      final chart =
          const ChatMermaidParser().parse(nested).diagram!
              as ChatMermaidFlowchart;
      final layout = layoutEngine.layout(
        diagram: chart,
        textStyle: TextStyle(fontSize: fontSize),
        labelStyle: TextStyle(fontSize: fontSize),
        textDirection: TextDirection.ltr,
      );
      final boxes = <String, ChatMermaidFlowSubgraphLayout>{
        for (final s in layout.subgraphRects) s.subgraph.id: s,
      };
      expect(
        boxes['INNER']!.rect.top,
        greaterThan(boxes['OUTER']!.labelRect.bottom),
        reason: '字号 $fontSize 时外层标题压到了子框边框上',
      );
    }
  });

  // 层级边（`A --> B`，两端都是分组）必须用「每组一个虚拟节点」表达，不能展开成
  // 成员之间的笛卡尔积。笛卡尔积有两宗罪，这组测试把两宗都锁住：
  //
  //   1. 边数 |A|×|B| 爆炸 → Sugiyama 在「组内有链式边 + 大量跨组边」的形状上退
  //      化极快：256 对 4.6 秒、400 对 18 秒，界面直接冻死。
  //   2. 就算没爆炸，它也会把整组成员横向摊开：两个 8 成员的分组（才 64 对），
  //      展开后画布 6862px 宽，而虚拟节点方案只要 196px。
  //
  // 虚拟节点把边数降到 |A|+|B|+1，靠 `成员 → vA → vB → 成员` 的传递性表达层级。
  // 三个断言缺一不可：够快、够紧凑、层级约束真的生效（否则「快」可以靠不加约束
  // 作弊得到）。
  for (final size in <int>[8, 20, 40]) {
    test('每组 $size 成员的层级边：快、紧凑、约束生效', () {
      final buffer = StringBuffer('flowchart TB\n');
      buffer.writeln('    subgraph A["组A"]');
      for (var i = 0; i < size; i++) {
        buffer.writeln('        A$i["节点A$i"]');
      }
      buffer.writeln('    end');
      buffer.writeln('    subgraph B["组B"]');
      for (var i = 0; i < size; i++) {
        buffer.writeln('        B$i["节点B$i"]');
      }
      buffer.writeln('    end');
      // 组内链式边：制造深层次，这正是让笛卡尔积把 Sugiyama 拖垮的形状。
      for (var i = 0; i < size - 1; i++) {
        buffer.writeln('    A$i --> A${i + 1}');
        buffer.writeln('    B$i --> B${i + 1}');
      }
      buffer.writeln('    A --> B');

      final chart =
          const ChatMermaidParser().parse(buffer.toString()).diagram!
              as ChatMermaidFlowchart;
      final stopwatch = Stopwatch()..start();
      final layout = layoutEngine.layout(
        diagram: chart,
        textStyle: textStyle,
        labelStyle: textStyle,
        textDirection: TextDirection.ltr,
      );
      stopwatch.stop();

      // 够快。阈值对 CI 负载留足容忍度，但远低于笛卡尔积的 4.6s / 18s。
      expect(
        stopwatch.elapsedMilliseconds,
        lessThan(2000),
        reason:
            '${size * size} 对的层级边把布局拖死了'
            '（${stopwatch.elapsedMilliseconds}ms）——层级边可能又被展开成了'
            '成员笛卡尔积',
      );

      // 层级约束真的生效：A 组每个成员都排在 B 组每个成员之上。
      var maxABottom = 0.0;
      var minBTop = double.infinity;
      for (var i = 0; i < size; i++) {
        maxABottom = math.max(maxABottom, layout.nodeRects['A$i']!.bottom);
        minBTop = math.min(minBTop, layout.nodeRects['B$i']!.top);
      }
      expect(
        maxABottom,
        lessThanOrEqualTo(minBTop),
        reason: '`A --> B` 的层级约束没生效：A 组成员没有全部排在 B 组之上',
      );

      // 够紧凑：笛卡尔积会把整组横向摊开（8 成员那档能撑到 6862px）。
      expect(
        layout.canvasSize.width,
        lessThan(3000),
        reason:
            '画布被横向摊开到 ${layout.canvasSize.width}px——层级边可能又被'
            '展开成了成员笛卡尔积',
      );

      expect(layout.edges.length, chart.edges.length, reason: '不该吃掉任何一条连线');
    });
  }

  test('纯层级链 A-->B-->C：中间分组的约束不塌（含组内链）', () {
    // 审查抓出的真 bug：分组 B 同时是 `A --> B` 的目标和 `B --> C` 的源时，
    // 若入/出共用一个虚拟节点，B 的每个成员与 hubB 之间会同时存在两个方向的边
    // （2-环），Sugiyama 拆环随手丢一条后，`GB 压在 GC 之上` 的语义悄悄丢失——
    // 实测 GB 成员沉到 GC 成员之下（GB 底 602 > GC 顶 556）、分组框互相叠加。
    // 组内链式边（B1-->B2）是触发条件：它给了拆环算法把成员甩开的自由度。
    // 修法是按角色拆成出/入两个 hub，方向恒定、不可能成环。
    const src = '''
flowchart TB
    subgraph GA["应用层"]
        A1["a1"]
        A2["a2"]
    end
    subgraph GB["协议层"]
        B1["b1"]
        B2["b2"]
    end
    subgraph GC["驱动层"]
        C1["c1"]
        C2["c2"]
    end
    A1 --> A2
    B1 --> B2
    C1 --> C2
    GA --> GB --> GC
''';
    final chart =
        const ChatMermaidParser().parse(src).diagram! as ChatMermaidFlowchart;
    final rects = layoutEngine
        .layout(
          diagram: chart,
          textStyle: textStyle,
          labelStyle: textStyle,
          textDirection: TextDirection.ltr,
        )
        .nodeRects;
    double maxBottom(List<String> ids) =>
        ids.map((id) => rects[id]!.bottom).reduce(math.max);
    double minTop(List<String> ids) =>
        ids.map((id) => rects[id]!.top).reduce(math.min);
    expect(
      maxBottom(['A1', 'A2']),
      lessThanOrEqualTo(minTop(['B1', 'B2'])),
      reason: 'GA 应整体压在 GB 之上',
    );
    expect(
      maxBottom(['B1', 'B2']),
      lessThanOrEqualTo(minTop(['C1', 'C2'])),
      reason: 'GB 应整体压在 GC 之上——中间分组的层级约束塌了（2-环拆环丢约束）',
    );
  });

  test('组内伪可达不误杀不相干的层级边', () {
    // 审查抓出的另一个双角色后果：若分组 B 的入/出共用一个 hub，adjacency 里会
    // 出现 `b1 → vB → b2` 这条任何真实约束都不蕴含的组内伪可达。于是判定
    // `F --> G` 是否矛盾时，可达搜索沿 g1 → b1 → vB → b2 → f1 走通，这条与 B
    // 毫不相干、约束完全可满足的层级边被整条误杀（实测 F1 被排到 G1 之下）。
    // 出/入双 hub 拆分后组内伪可达消失。
    const src = '''
flowchart TB
    subgraph GA["a"]
        A1["a1"]
    end
    subgraph GB["b"]
        B1["b1"]
        B2["b2"]
    end
    subgraph GC["c"]
        C1["c1"]
    end
    subgraph GF["f"]
        F1["f1"]
    end
    subgraph GG["g"]
        G1["g1"]
    end
    G1 --> B1
    B2 --> F1
    GA --> GB
    GB --> GC
    GF --> GG
''';
    final chart =
        const ChatMermaidParser().parse(src).diagram! as ChatMermaidFlowchart;
    final rects = layoutEngine
        .layout(
          diagram: chart,
          textStyle: textStyle,
          labelStyle: textStyle,
          textDirection: TextDirection.ltr,
        )
        .nodeRects;
    expect(
      rects['F1']!.top,
      lessThan(rects['G1']!.top),
      reason: '`F --> G` 层级边被组内伪可达误杀了：F1 应排在 G1 之上',
    );
  });

  test('正交方向的大段空白被压缩（左右间隙不再松散）', () {
    // 老郭反馈「左右连线空间好大」：Sugiyama 坐标分配和分组消解会留下几百像素
    // 宽、没有任何节点的空白带。压缩 pass 把超过 levelSeparation 的空档压到该
    // 值。B806 图压缩前画布宽 4213、压缩后 3235——断言阈值 3700 落在两者之间的
    // 判别带上（距两侧各 >12%），字体度量的正常抖动不会跨带。
    final layout = runLayout();
    expect(
      layout.canvasSize.width,
      lessThan(3700),
      reason: '画布宽 ${layout.canvasSize.width}：横向空白带压缩没有生效',
    );
  });

  test('LR 方向同样压缩正交空白（压 y 轴）', () {
    // 压缩的轴选择必须跟着流向走：TB/BT 压 x，LR/RL 压 y。B806 图转成 LR 后，
    // 压缩前画布高 2514、压缩后 1699——阈值 2100 落在判别带中央。
    final lrSource = source.replaceFirst('flowchart TB', 'flowchart LR');
    final chart =
        const ChatMermaidParser().parse(lrSource).diagram!
            as ChatMermaidFlowchart;
    final layout = layoutEngine.layout(
      diagram: chart,
      textStyle: textStyle,
      labelStyle: textStyle,
      textDirection: TextDirection.ltr,
    );
    expect(
      layout.canvasSize.height,
      lessThan(2100),
      reason: '画布高 ${layout.canvasSize.height}：LR 方向的空白压缩没有生效',
    );
  });

  test('共用一条通道的回边，占用区间互不重叠', () {
    // 回边通道按区间贪心合并后，同一条通道（同一 x 的外侧竖线）上的线段必须
    // 两两错开，否则两条回边画在同一条线上分不清谁是谁。
    final layout = runLayout();
    var nodeRight = 0.0;
    for (final rect in layout.nodeRects.values) {
      nodeRight = math.max(nodeRight, rect.right);
    }
    // 收集图形内容右侧的竖直走线段，按所在 x（即通道）分组。
    final segmentsByLane = <double, List<(double, double)>>{};
    for (final routed in layout.edges) {
      for (var i = 0; i + 1 < routed.points.length; i++) {
        final a = routed.points[i];
        final b = routed.points[i + 1];
        if ((a.dx - b.dx).abs() < 0.1 && a.dx > nodeRight) {
          (segmentsByLane[a.dx] ??= []).add((
            math.min(a.dy, b.dy),
            math.max(a.dy, b.dy),
          ));
        }
      }
    }
    expect(segmentsByLane, isNotEmpty, reason: '本图应存在外侧回边通道');
    segmentsByLane.forEach((laneX, segments) {
      for (var i = 0; i < segments.length; i++) {
        for (var j = i + 1; j < segments.length; j++) {
          final overlap =
              math.min(segments[i].$2, segments[j].$2) -
              math.max(segments[i].$1, segments[j].$1);
          expect(
            overlap,
            lessThanOrEqualTo(0),
            reason: '通道 x=$laneX 上两条回边线段重叠 ${overlap}px',
          );
        }
      }
    });
  });

  test('虚拟节点不会泄漏到绘制结果里', () {
    // 虚拟节点只存在于分层图中，不在 diagram.nodes 里，因此不该出现在 nodeRects
    // （不绘制），也不该被算进分组框的包围盒。
    final layout = runLayout();
    final leaked = layout.nodeRects.keys.where((id) => id.contains('\x00'));
    expect(leaked, isEmpty, reason: '虚拟分组节点泄漏进了绘制结果: $leaked');
  });

  test('任意两个非父子的分组框都不叠加（含嵌套的兄弟分组）', () {
    // 上一版这条只盯顶层的 FIELD/BOARD/CHIPS，还在注释里把嵌套进 BOARD 的
    // APP/PROTO/DRV/BASE 明确排除——而恰恰是被排除的 PROTO 与 DRV 实打实叠着
    // 643×222px。断言范围正好绕开了真正的缺陷：测试全绿，图还是坏的。
    // 现在扫全部分组，只豁免真正的父子关系（父框本就该包住子框）。
    final layout = runLayout();
    final boxes = <String, Rect>{
      for (final s in layout.subgraphRects) s.subgraph.id: s.rect,
    };
    const parentChild = <String>{
      'BOARD|APP',
      'BOARD|PROTO',
      'BOARD|DRV',
      'BOARD|BASE',
    };
    final ids = boxes.keys.toList();
    for (var i = 0; i < ids.length; i++) {
      for (var j = i + 1; j < ids.length; j++) {
        final a = ids[i];
        final b = ids[j];
        if (parentChild.contains('$a|$b') || parentChild.contains('$b|$a')) {
          continue;
        }
        expect(
          boxes[a]!.overlaps(boxes[b]!),
          isFalse,
          reason: '分组框 $a 与 $b 叠加：${boxes[a]} / ${boxes[b]}',
        );
      }
    }
  });

  test('节点两两不重叠', () {
    final layout = runLayout();
    final entries = layout.nodeRects.entries.toList();
    for (var i = 0; i < entries.length; i++) {
      for (var j = i + 1; j < entries.length; j++) {
        expect(
          entries[i].value.overlaps(entries[j].value),
          isFalse,
          reason: '节点 ${entries[i].key} 与 ${entries[j].key} 叠加',
        );
      }
    }
  });

  test('嵌套分组：外层框严格包住子框，不与之边界重合', () {
    final layout = runLayout();
    final boxes = <String, Rect>{
      for (final s in layout.subgraphRects) s.subgraph.id: s.rect,
    };
    final board = boxes['BOARD'];
    expect(board, isNotNull, reason: 'BOARD 分组框缺失');
    for (final childId in <String>['APP', 'PROTO', 'DRV', 'BASE']) {
      final child = boxes[childId];
      expect(child, isNotNull, reason: '$childId 分组框缺失');
      // 严格包含：四边都要真正让出空间，边界重合就说明外层框没「包住」子框。
      expect(
        child!.left,
        greaterThan(board!.left),
        reason: 'BOARD 左边界与子框 $childId 重合/穿模',
      );
      expect(
        child.right,
        lessThan(board.right),
        reason: 'BOARD 右边界与子框 $childId 重合/穿模',
      );
      expect(
        child.top,
        greaterThan(board.top),
        reason: 'BOARD 上边界与子框 $childId 重合/穿模',
      );
      expect(
        child.bottom,
        lessThan(board.bottom),
        reason: 'BOARD 下边界与子框 $childId 重合/穿模',
      );
    }
  });
}
