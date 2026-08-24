import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:graphview/GraphView.dart';

import 'chat_mermaid_flowchart_edge_router.dart';
import 'chat_mermaid_layout_tokens.dart';
import 'chat_mermaid_model.dart';
import 'chat_mermaid_node_style_tokens.dart';

class ChatMermaidFlowchartLayout {
  const ChatMermaidFlowchartLayout({
    required this.canvasSize,
    required this.nodeRects,
    required this.subgraphRects,
    required this.edges,
  });

  final Size canvasSize;
  final Map<String, Rect> nodeRects;
  final List<ChatMermaidFlowSubgraphLayout> subgraphRects;
  final List<ChatMermaidRoutedEdge> edges;
}

class ChatMermaidFlowSubgraphLayout {
  const ChatMermaidFlowSubgraphLayout({
    required this.subgraph,
    required this.rect,
    required this.labelRect,
  });

  final ChatMermaidFlowSubgraph subgraph;
  final Rect rect;
  final Rect labelRect;
}

class ChatMermaidRoutedEdge {
  const ChatMermaidRoutedEdge({
    required this.edge,
    required this.points,
    required this.labelAnchor,
    this.labelSize = Size.zero,
  });

  final ChatMermaidEdge edge;
  final List<Offset> points;
  final Offset labelAnchor;
  final Size labelSize;
}

class ChatMermaidFlowchartLayoutEngine {
  const ChatMermaidFlowchartLayoutEngine({
    // 左右留白比上下更大：可缩放视口会按整张画布（含 padding）做适配缩放，
    // 宽流程图被缩小后，原本 20px 的留白会被压到几像素，导致最左/右节点的
    // 描边贴边甚至被裁切。这里加大水平 padding，保证缩放后左右仍有可见留白。
    this.padding = const EdgeInsets.symmetric(horizontal: 40, vertical: 20),
    this.maxNodeTextWidth = 200,
    this.alignmentTolerance = 1,
    this.edgeLabelGap = 6,
    this.maxLabelTextWidth = 220,
    this.maxSubgraphLabelWidth = 280,
    this.levelSeparation = ChatMermaidLayoutTokens.flowchartLevelSeparation,
    this.nodeSeparation = ChatMermaidLayoutTokens.flowchartNodeSeparation,
  });

  final EdgeInsets padding;
  final double maxNodeTextWidth;
  final double alignmentTolerance;
  final double edgeLabelGap;
  final double maxLabelTextWidth;
  final double maxSubgraphLabelWidth;
  final int levelSeparation;
  final int nodeSeparation;

  ChatMermaidFlowchartLayout layout({
    required ChatMermaidFlowchart diagram,
    required TextStyle textStyle,
    required TextStyle labelStyle,
    required TextDirection textDirection,
  }) {
    final graph = Graph();
    final graphNodes = <String, Node>{};
    final nodeRects = <String, Rect>{};
    final anchorRects = <String, Rect>{};

    for (final node in diagram.nodes) {
      final measuredSize = _measureNode(
        node: node,
        textStyle: textStyle,
        textDirection: textDirection,
      );
      final graphNode = Node.Id(node.id)..size = measuredSize;
      graphNodes[node.id] = graphNode;
      graph.addNode(graphNode);
    }

    // 把 subgraph id 映射到其成员节点 id（仅保留已建图的真实节点）。
    // 边可以直接连接 subgraph（如 `Mobile --> S`），但 Sugiyama 分层算法只认识
    // 节点、不认识分组框。若直接按 subgraph id 取节点会得到 null 而丢弃整条边，
    // 当全部边都连在分组框上时（本场景），算法会拿到一堆互不相连的孤立节点、
    // 失去层级骨架，所有节点塌缩到同一层并排成一行。这里把分组框端点展开成
    // 框内每个成员节点，让分层约束作用到整个分组上。
    final subgraphMembers = <String, List<String>>{
      for (final subgraph in diagram.subgraphs)
        subgraph.id: subgraph.nodeIds
            .where(graphNodes.containsKey)
            .toList(growable: false),
    };
    List<String> expandEndpoint(String id) {
      if (graphNodes.containsKey(id)) {
        return <String>[id];
      }
      return subgraphMembers[id] ?? const <String>[];
    }

    // 按「端点是节点还是分组框」把边分成三类，依次喂给 Sugiyama。
    //
    // 分类的依据是这条边有没有具体的数据流语义：
    //   · 两端都是节点（`UART --> MB`）——作者画的具体连线，是分层骨架。
    //   · 一端节点一端分组（`B1 --> G1`、`Sync -.-> note1`）——同样是作者画的
    //     具体连线，只是另一头落在框上；展开成「该节点 ↔ 框内每个成员」后，
    //     依然是真实的数据流约束，属于骨架。
    //   · 两端都是分组（`APP --> PROTO`）——不对应任何具体连线，纯粹是「整组压
    //     在整组之上」的层级示意。展开后会膨胀成 N×M 条隐形边。
    //
    // 只有第三类需要与数据流做矛盾判定。前两类都是骨架，先入图；这样第三类判定
    // 时能看到全部真实连线，与作者的书写顺序无关。
    //
    // 这个划分是必须的：`B1 --> G1` 若被误当成层级边，它表达的反向数据流就不在
    // 骨架里，`G1 --> G2` 的矛盾便查不出来——同一个矛盾，写成「节点-->节点」拦得
    // 住、写成「节点-->分组」就漏过去，同组成员照样被拆到不同层。
    bool isGroup(String id) => !graphNodes.containsKey(id);

    final skeletonEdges = <ChatMermaidEdge>[];
    final groupLevelEdges = <ChatMermaidEdge>[];
    for (final edge in diagram.edges) {
      if (edge.sourceId == edge.targetId) {
        continue; // skip self-loops – Sugiyama can't handle them
      }
      if (isGroup(edge.sourceId) && isGroup(edge.targetId)) {
        groupLevelEdges.add(edge);
      } else {
        skeletonEdges.add(edge);
      }
    }

    // 已入图边的邻接表，供层级边的可达性判定使用；键为源节点 id。
    final adjacency = <String, Set<String>>{};

    // 按「无向对」去重：两个节点之间只向分层算法提供一条边，方向取先声明者。
    // 双向边（如 `Mobile --> S` 同时 `S --> Mobile`）展开后会在每个成员与目标
    // 之间形成 2-环，Sugiyama 逐节点拆环会把同一分组的成员甩到不同层、撑爆分组
    // 框。去掉反向重复后，整组成员获得一致的层级约束、聚拢在同一层；先声明的
    // 方向胜出也符合书写意图（先写的分组在上/在前）。可视化连线仍按原始 edges
    // 双向绘制，不受影响。
    //
    // 这里不再逐条做环检测：层级边在整条边这一层已用可达性判定挡掉了矛盾的那些
    // （见 contradictsDataFlow），层级边之间互相成环（`A --> B` 与 `B --> A`）
    // 也被它覆盖——B→A 判定时，A→B 展开出的边已在 adjacency 里，可达即作废。
    // 骨架边只靠这里的无向去重就够：双向连线不会撞出 2-环；作者自己画的循环数据
    // 流是合法的，交给 Sugiyama 拆环，不该由我们越俎代庖。
    final addedEdgePairs = <String>{};
    void addLayoutEdge(String sourceId, String targetId) {
      if (sourceId == targetId) {
        return; // 框与其自身成员、或同框成员间的展开自环
      }
      // 去重键用 NUL 作分隔符：合法节点 id 不含 NUL，避免不同 id 组合
      // 拼出同一字符串造成碰撞、误跳过合法边。
      final pairKey = sourceId.compareTo(targetId) <= 0
          ? '$sourceId\x00$targetId'
          : '$targetId\x00$sourceId';
      if (addedEdgePairs.contains(pairKey)) {
        return;
      }
      addedEdgePairs.add(pairKey);
      (adjacency[sourceId] ??= <String>{}).add(targetId);
      graph.addEdge(graphNodes[sourceId]!, graphNodes[targetId]!);
    }

    // 一条纯层级边（`A --> B`，两端都是分组）是否与真实数据流逆行：
    // 沿骨架已铺好的边，能不能从 B 侧的任一成员走回 A 侧的任一成员。
    //
    // 判定必须落在「整条层级边」这一层，而不是展开后的单条边上。逐条挑会挡住直
    // 接成环的那几条、放行其余的：`PROTO --> DRV` 展开出的 METER→UART 因与
    // UART→MB→METER 成环被挡，MAP→UART 却因 MAP 没有入边而放行——于是 MAP 被压
    // 到 UART 之上、METER 被压到 UART 之下，同属 PROTO 的两个成员被拆到不同层，
    // 分组框照样被拉长撑爆。只要作者已用真实连线表达了 B→A 的流向，那么 A→B 的
    // 层级意图就是矛盾的，整条作废。
    //
    // 用可达性而非「有没有直接反向边」：反向的数据流常常要绕几跳才走回来
    // （METER 经 HJ、FDLINK 才回到 UART），只看一跳会漏判。
    bool contradictsDataFlow(ChatMermaidEdge edge) {
      final sourceMembers = expandEndpoint(edge.sourceId).toSet();
      final targetMembers = expandEndpoint(edge.targetId).toSet();
      // 成员集相交（嵌套的祖先/子孙分组）时，「谁压在谁上面」无从谈起，同样作废。
      if (sourceMembers.any(targetMembers.contains)) {
        return true;
      }
      final visited = <String>{...targetMembers};
      final stack = <String>[...targetMembers];
      while (stack.isNotEmpty) {
        for (final next in adjacency[stack.removeLast()] ?? const <String>{}) {
          if (sourceMembers.contains(next)) {
            return true;
          }
          if (visited.add(next)) {
            stack.add(next);
          }
        }
      }
      return false;
    }

    // 骨架先铺满：这样纯层级边判定时看得到全部真实连线，与书写顺序无关。
    for (final edge in skeletonEdges) {
      for (final sourceId in expandEndpoint(edge.sourceId)) {
        for (final targetId in expandEndpoint(edge.targetId)) {
          addLayoutEdge(sourceId, targetId);
        }
      }
    }
    // 层级边用「分组虚拟节点」表达，而不是成员之间的笛卡尔积。
    //
    // 笛卡尔积（旧做法）有两宗罪，且第二宗比第一宗更隐蔽：
    //   1. 边数 |A|×|B| 爆炸。Sugiyama 在「组内有链式边 + 大量跨组边」的形状上退
    //      化极快：256 对要 4.6 秒、400 对要 18 秒，界面直接冻住。
    //   2. 就算没爆炸，它也会把整组成员横向摊开、把布局拉坏。实测两个 8 成员的分
    //      组（64 对，规模并不大）：展开后画布 6862px 宽，而完全不加这条约束只要
    //      328px——「展开」比「不展开」还难看 21 倍。
    //
    // 虚拟节点把边数降到 |A|+|B|+1，并且只施加纵向的层级约束、不牵扯成员之间的横
    // 向排布：
    //
    //     A 的每个成员 --> outA --> inB --> B 的每个成员
    //
    // 靠传递性就能保证「A 的所有成员都排在 B 的所有成员之上」，这正是层级边想说
    // 的意思。边数线性之后，那个「超过多少对就放弃约束」的上限也不再需要了。
    //
    // 虚拟节点只存在于分层图里：它们不在 diagram.nodes 中，因此不会进 nodeRects、
    // 不会被绘制，也不会被算进分组框的包围盒。
    // 虚拟节点 id 用 NUL 前缀，与任何合法的 mermaid 节点 id 都不会撞。
    //
    // 每个分组按角色拆成两个虚拟节点——「出 hub」（该分组作为层级边的源）与
    // 「入 hub」（作为目标）。不能共用一个：当分组 B 同时是 `A --> B` 的目标和
    // `B --> C` 的源时，共用会让 B 的每个成员与 hubB 之间同时存在
    // `hubB → 成员`（来自 A-->B）和 `成员 → hubB`（来自 B-->C）——2-环。
    // Sugiyama 拆环随手丢掉一条后，hubC 便不再受 B 成员约束，`GB 压在 GC 之上`
    // 的语义悄悄丢失（实测 GB 的成员沉到了 GC 成员之下、分组框互相叠加）。
    // 拆成两个后方向恒定——成员只会指向出 hub、入 hub 只会指向成员：
    //
    //     A成员 → outA → inB → B成员 → outB → inC → C成员
    //
    // 链上不存在任何反向边，传递性完整保留。
    final hubNodes = <String, Node>{};
    Node hubNodeOf(String hubId) {
      return hubNodes.putIfAbsent(hubId, () {
        // 尺寸取 1×1 而非 0：Sugiyama 对零尺寸节点的处理不稳，1px 视觉上等同于不
        // 存在（本来也不绘制），但能让分层算法正常给它排位。
        final node = Node.Id(hubId)..size = const Size(1, 1);
        graph.addNode(node);
        return node;
      });
    }

    String outHubIdOf(String groupId) => '\x00group-out\x00$groupId';
    String inHubIdOf(String groupId) => '\x00group-in\x00$groupId';

    // 虚拟边是有向链（成员→outA→inB→成员），不能复用骨架边的无向去重键，否则
    // 「成员→outA」会和「inA→成员」这类组合被误并。这里单独按有向对去重。
    final addedHubPairs = <String>{};
    void addHubEdge(Node from, String fromId, Node to, String toId) {
      if (!addedHubPairs.add('$fromId\x00$toId')) {
        return;
      }
      (adjacency[fromId] ??= <String>{}).add(toId);
      graph.addEdge(from, to);
    }

    for (final edge in groupLevelEdges) {
      if (contradictsDataFlow(edge)) {
        continue;
      }
      final sourceMembers = expandEndpoint(edge.sourceId);
      final targetMembers = expandEndpoint(edge.targetId);
      if (sourceMembers.isEmpty || targetMembers.isEmpty) {
        continue;
      }
      final sourceHubId = outHubIdOf(edge.sourceId);
      final targetHubId = inHubIdOf(edge.targetId);
      final sourceHub = hubNodeOf(sourceHubId);
      final targetHub = hubNodeOf(targetHubId);

      for (final memberId in sourceMembers) {
        addHubEdge(graphNodes[memberId]!, memberId, sourceHub, sourceHubId);
      }
      addHubEdge(sourceHub, sourceHubId, targetHub, targetHubId);
      for (final memberId in targetMembers) {
        addHubEdge(targetHub, targetHubId, graphNodes[memberId]!, memberId);
      }
    }

    final configuration = SugiyamaConfiguration()
      ..orientation = _mapOrientation(diagram.direction)
      ..levelSeparation = levelSeparation
      ..nodeSeparation = nodeSeparation
      // DFS 按加边顺序反转回边：骨架边先入图、分组层级边后入图，保证真实
      // 数据流方向优先。greedy 策略会按度数挑选反转边，打破这一顺序约束。
      ..cycleRemovalStrategy = CycleRemovalStrategy.dfs;
    final algorithm = SugiyamaAlgorithm(configuration);
    algorithm.run(graph, padding.left, padding.top);

    var maxRight = 0.0;
    var maxBottom = 0.0;
    for (final node in diagram.nodes) {
      final graphNode = graphNodes[node.id]!;
      nodeRects[node.id] = Rect.fromLTWH(
        graphNode.x,
        graphNode.y,
        graphNode.width,
        graphNode.height,
      );
    }
    final normalizedNodeRects = _normalizeNodeRectsForDirection(
      nodeRects: nodeRects,
      direction: diagram.direction,
    );
    final resolvedNodeRects = _resolveNodeRectCollisionsForDirection(
      nodeRects: normalizedNodeRects,
      direction: diagram.direction,
    );
    // 兜底：lane 内消解只处理单一方向，无法覆盖居中对齐与分层算法
    // 在正交方向上残留的叠加，这里对任意仍重叠的节点对做全局分离。
    final separatedNodeRects = _separateOverlappingNodes(
      nodeRects: resolvedNodeRects,
    );
    nodeRects
      ..clear()
      ..addAll(separatedNodeRects);
    // 节点级分离只保证节点矩形不重叠，但分组框是成员包围盒再外扩 padding 画出，
    // 相邻兄弟分组的成员间距可能小于两侧 padding 之和，导致分组框互相叠加。
    // 这里在边路由前对分组框再做一次重叠消解，把位移施加到框内成员节点上。
    // 嵌套顶部让位量按标题实际高度算一次，绘制与重叠消解共用，保证两处算出的框
    // 完全一致。
    final nestedTopStep = _measureNestedTopStep(
      diagram: diagram,
      textStyle: textStyle,
      textDirection: textDirection,
    );
    final subgraphSeparated = _separateSiblingSubgraphs(
      diagram: diagram,
      nodeRects: nodeRects,
      nestedTopStep: nestedTopStep,
    );
    // 分组消解只检查「兄弟分组框之间」是否重叠，把一整组成员按同一位移平移开——
    // 但平移目的地是否安全，从没有对照过不属于任何分组的自由节点：一个自由节点
    // 此前离得很开、没跟谁重叠，分组被推过去之后就会撞上它，而这里从不回头检查。
    // 200 张随机图的模糊测试测出 19 张中招（老郭反馈的「老毛病」）。
    // 用既有的全局节点级去重兜底：它本就不关心谁属于哪个分组，纯按矩形是否
    // 重叠处理，正好补上分组位移之后缺失的这一次复查。
    final freeNodeRechecked = _separateOverlappingNodes(
      nodeRects: subgraphSeparated,
    );
    nodeRects
      ..clear()
      ..addAll(freeNodeRechecked);
    // 压掉与流向正交方向上的大段空白（TB 图压横向）：Sugiyama 的坐标分配、
    // 分组消解的推移都会留下几百像素宽、没有任何节点占用的空带，连线被拉得
    // 老长、图松散（老郭反馈「左右间隙好大」）。压缩是保序平移，不产生新重叠；
    // 但分组框带 padding，空带压窄后相邻框可能重新贴上，所以压完再跑一遍兄弟
    // 消解兜底（无重叠时它是空操作）。
    final compressed = _compressOrthogonalGaps(
      nodeRects: nodeRects,
      direction: diagram.direction,
    );
    final recheckedAfterCompress = _separateSiblingSubgraphs(
      diagram: diagram,
      nodeRects: compressed,
      nestedTopStep: nestedTopStep,
    );
    // 同上：第二轮分组消解（响应压缩留下的新贴边）同样可能把成员推到自由节点
    // 身上，压缩本身也是保序平移、不会主动制造重叠，但消解这一步会，因此紧跟
    // 着再兜底一次。
    final freeNodeRecheckedAfterCompress = _separateOverlappingNodes(
      nodeRects: recheckedAfterCompress,
    );
    nodeRects
      ..clear()
      ..addAll(freeNodeRecheckedAfterCompress);
    anchorRects.addAll(nodeRects);
    for (final rect in nodeRects.values) {
      maxRight = math.max(maxRight, rect.right);
      maxBottom = math.max(maxBottom, rect.bottom);
    }

    final subgraphRects = _buildSubgraphLayouts(
      diagram: diagram,
      nodeRects: nodeRects,
      textStyle: textStyle,
      textDirection: textDirection,
      nestedTopStep: nestedTopStep,
    );
    for (final subgraphLayout in subgraphRects) {
      anchorRects[subgraphLayout.subgraph.id] = subgraphLayout.rect;
      maxRight = math.max(maxRight, subgraphLayout.rect.right);
      maxBottom = math.max(maxBottom, subgraphLayout.rect.bottom);
    }

    // 回边走线通道的基准：节点与分组框的右/下边界，在路由开始前一次性固定。
    //
    // 不能直接用循环里的 maxRight/maxBottom——它们会被回边自己的走线点撑大，于是
    // 第二条回边在第一条的外侧再开一条通道、第三条又在第二条外侧……画布被一路往
    // 外推出大片空白（老郭那张 B806 图 46% 的宽度都是这么来的空走线通道）。通道
    // 只应相对「图形内容」排布，彼此之间靠 backEdgeIndex 拉开即可。
    final router = ChatMermaidFlowchartEdgeRouter(
      levelSeparation: levelSeparation.toDouble(),
    );
    final routes = router.route(
      direction: diagram.direction,
      edges: diagram.edges,
      anchorRects: anchorRects,
      obstacleRects: nodeRects.values,
      corridorObstacleRects: subgraphRects.map((layout) => layout.rect),
      fixedPortIds: <String>{
        for (final node in diagram.nodes)
          if (node.shape != ChatMermaidNodeShape.rectangle &&
              node.shape != ChatMermaidNodeShape.rounded &&
              node.shape != ChatMermaidNodeShape.subroutine)
            node.id,
      },
    );

    final routedEdges = <ChatMermaidRoutedEdge>[];
    for (var i = 0; i < diagram.edges.length; i++) {
      final edge = diagram.edges[i];
      final sourceRect = anchorRects[edge.sourceId];
      final targetRect = anchorRects[edge.targetId];
      if (sourceRect == null || targetRect == null) {
        continue;
      }
      final labelSize = _measureEdgeLabel(
        label: edge.label,
        style: labelStyle,
        textDirection: textDirection,
      );

      final ChatMermaidRoutedEdge routedEdge;
      if (edge.sourceId == edge.targetId) {
        routedEdge = _routeSelfLoop(
          edge: edge,
          rect: sourceRect,
          labelSize: labelSize,
        );
      } else {
        final points = routes[i];
        if (points.length < 2) {
          continue;
        }
        routedEdge = ChatMermaidRoutedEdge(
          edge: edge,
          points: points,
          labelAnchor: _labelAnchorForPolyline(
            points: points,
            labelSize: labelSize,
          ),
          labelSize: labelSize,
        );
      }
      maxRight = math.max(
        maxRight,
        _maxPointDx(routedEdge.points) + padding.right,
      );
      maxBottom = math.max(
        maxBottom,
        _maxPointDy(routedEdge.points) + padding.bottom,
      );
      if (labelSize != Size.zero) {
        maxRight = math.max(
          maxRight,
          routedEdge.labelAnchor.dx + labelSize.width + padding.right,
        );
        maxBottom = math.max(
          maxBottom,
          routedEdge.labelAnchor.dy + labelSize.height + padding.bottom,
        );
      }
      routedEdges.add(routedEdge);
    }

    final resolvedEdges = _resolveEdgeLabelCollisions(
      edges: routedEdges,
      obstacleRects: <Rect>[
        ...nodeRects.values,
        ...subgraphRects.map((layout) => layout.rect),
      ],
    );

    // 统一归一化：把所有可见元素整体平移，确保画布四周都留出 padding 留白。
    // 避免顶部/左侧元素（如 subgraph 标题、回边走线）落在画布原点之外被裁剪，
    // 这也是导出图片时顶部被截断的根因。
    return _composeLayout(
      nodeRects: nodeRects,
      subgraphRects: subgraphRects,
      edges: resolvedEdges,
    );
  }

  /// 收集全部可见几何的实际边界，整体平移使内容左上角对齐到
  /// `(padding.left, padding.top)`，并按内容尺寸加四周 padding 重算画布大小。
  ChatMermaidFlowchartLayout _composeLayout({
    required Map<String, Rect> nodeRects,
    required List<ChatMermaidFlowSubgraphLayout> subgraphRects,
    required List<ChatMermaidRoutedEdge> edges,
  }) {
    var minLeft = double.infinity;
    var minTop = double.infinity;
    var maxRight = double.negativeInfinity;
    var maxBottom = double.negativeInfinity;

    void includeRect(Rect rect) {
      minLeft = math.min(minLeft, rect.left);
      minTop = math.min(minTop, rect.top);
      maxRight = math.max(maxRight, rect.right);
      maxBottom = math.max(maxBottom, rect.bottom);
    }

    void includePoint(Offset point) {
      minLeft = math.min(minLeft, point.dx);
      minTop = math.min(minTop, point.dy);
      maxRight = math.max(maxRight, point.dx);
      maxBottom = math.max(maxBottom, point.dy);
    }

    for (final rect in nodeRects.values) {
      includeRect(rect);
    }
    for (final subgraph in subgraphRects) {
      includeRect(subgraph.rect);
      includeRect(subgraph.labelRect);
    }
    for (final edge in edges) {
      for (final point in edge.points) {
        includePoint(point);
      }
      if (edge.labelSize != Size.zero) {
        includeRect(
          Rect.fromCenter(
            center: edge.labelAnchor,
            width: edge.labelSize.width,
            height: edge.labelSize.height,
          ),
        );
      }
    }

    if (minLeft == double.infinity) {
      return ChatMermaidFlowchartLayout(
        canvasSize: Size(padding.horizontal, padding.vertical),
        nodeRects: Map.unmodifiable(nodeRects),
        subgraphRects: List.unmodifiable(subgraphRects),
        edges: List.unmodifiable(edges),
      );
    }

    // 预留安全余量，吸收节点描边、箭头等超出几何矩形的绘制溢出。
    const strokeMargin = 2.0;
    minLeft -= strokeMargin;
    minTop -= strokeMargin;
    maxRight += strokeMargin;
    maxBottom += strokeMargin;

    final shift = Offset(padding.left - minLeft, padding.top - minTop);

    final shiftedNodeRects = <String, Rect>{
      for (final entry in nodeRects.entries)
        entry.key: entry.value.shift(shift),
    };
    final shiftedSubgraphRects = <ChatMermaidFlowSubgraphLayout>[
      for (final subgraph in subgraphRects)
        ChatMermaidFlowSubgraphLayout(
          subgraph: subgraph.subgraph,
          rect: subgraph.rect.shift(shift),
          labelRect: subgraph.labelRect.shift(shift),
        ),
    ];
    final shiftedEdges = <ChatMermaidRoutedEdge>[
      for (final edge in edges)
        ChatMermaidRoutedEdge(
          edge: edge.edge,
          points: <Offset>[for (final point in edge.points) point + shift],
          labelAnchor: edge.labelAnchor + shift,
          labelSize: edge.labelSize,
        ),
    ];

    final canvasSize = Size(
      (maxRight - minLeft) + padding.horizontal,
      (maxBottom - minTop) + padding.vertical,
    );

    return ChatMermaidFlowchartLayout(
      canvasSize: canvasSize,
      nodeRects: Map.unmodifiable(shiftedNodeRects),
      subgraphRects: List.unmodifiable(shiftedSubgraphRects),
      edges: List.unmodifiable(shiftedEdges),
    );
  }

  /// 每个分组「向内还套了几层子分组」。
  ///
  /// 嵌套分组的成员会同时算进所有祖先分组（见 parser：节点加入全部活动分组），
  /// 因此祖先与子孙的成员包围盒常常同边。绘制时按这个层数逐级放大外扩量，外层
  /// 框才包得住子框而不是与之重合。返回值以分组 id 为键，叶子分组为 0。
  static Map<String, int> _computeNestingHeights(ChatMermaidFlowchart diagram) {
    final memberSets = <String, Set<String>>{
      for (final subgraph in diagram.subgraphs)
        subgraph.id: subgraph.nodeIds.toSet(),
    };
    final heights = <String, int>{};
    for (final subgraph in diagram.subgraphs) {
      final members = memberSets[subgraph.id]!;
      var deepest = subgraph.depth;
      for (final other in diagram.subgraphs) {
        if (other.id == subgraph.id || other.depth <= subgraph.depth) {
          continue;
        }
        final otherMembers = memberSets[other.id]!;
        // 后代判定用成员包含而非声明顺序：depth 更深且成员被完全包住，才是子孙。
        if (otherMembers.isNotEmpty && otherMembers.every(members.contains)) {
          deepest = math.max(deepest, other.depth);
        }
      }
      heights[subgraph.id] = deepest - subgraph.depth;
    }
    return heights;
  }

  /// 分组框相对成员包围盒的外扩量，随嵌套层数逐级放大。
  ///
  /// 「绘制」（[_buildSubgraphLayouts]）与「重叠消解」（[_separateSiblingSubgraphs]）
  /// 必须共用此函数，否则消解时算出的框与实际画出的框不一致，消解完照样重叠。
  ///
  /// [nestedTopStep] 由标题实际高度推出（见 [_measureNestedTopStep]），不能写死：
  /// 外层每嵌套一层要在顶部让出的空间正好用来放自己的标题，字号一变大，写死的
  /// 数值就会不够，标题压到子框的边框上。
  static EdgeInsets _subgraphPadding(int nestingHeight, double nestedTopStep) {
    return EdgeInsets.fromLTRB(
      ChatMermaidLayoutTokens.subgraphPaddingLeft +
          nestingHeight * ChatMermaidLayoutTokens.subgraphNestedPaddingStep,
      ChatMermaidLayoutTokens.subgraphPaddingTop +
          nestingHeight * nestedTopStep,
      ChatMermaidLayoutTokens.subgraphPaddingRight +
          nestingHeight * ChatMermaidLayoutTokens.subgraphNestedPaddingStep,
      ChatMermaidLayoutTokens.subgraphPaddingBottom +
          nestingHeight * ChatMermaidLayoutTokens.subgraphNestedPaddingStep,
    );
  }

  /// 嵌套一层需要在顶部额外让出的高度：标题距框顶的偏移 + 标题实际高度 + 间隙。
  ///
  /// 与 [_buildSubgraphLayouts] 里 labelRect 的算法保持一致（偏移 10、高度至少
  /// 20），取全部分组标题里最高的那个，保证任何一层的标题都不会压到子框边框。
  double _measureNestedTopStep({
    required ChatMermaidFlowchart diagram,
    required TextStyle textStyle,
    required TextDirection textDirection,
  }) {
    var maxLabelHeight = 20.0;
    for (final subgraph in diagram.subgraphs) {
      final painter = TextPainter(
        text: TextSpan(
          text: subgraph.label,
          style: textStyle.copyWith(
            fontWeight: FontWeight.w700,
            fontSize: (textStyle.fontSize ?? 13) - 1,
          ),
        ),
        textDirection: textDirection,
      )..layout(maxWidth: maxSubgraphLabelWidth);
      maxLabelHeight = math.max(
        maxLabelHeight,
        math.max(20, painter.height + 6),
      );
    }
    return ChatMermaidLayoutTokens.subgraphLabelTopOffset +
        maxLabelHeight +
        ChatMermaidLayoutTokens.subgraphLabelBottomGap;
  }

  List<ChatMermaidFlowSubgraphLayout> _buildSubgraphLayouts({
    required ChatMermaidFlowchart diagram,
    required Map<String, Rect> nodeRects,
    required TextStyle textStyle,
    required TextDirection textDirection,
    required double nestedTopStep,
  }) {
    final layouts = <ChatMermaidFlowSubgraphLayout>[];
    final nestingHeights = _computeNestingHeights(diagram);
    final sorted = diagram.subgraphs.toList()
      ..sort((left, right) => right.depth.compareTo(left.depth));
    for (final subgraph in sorted) {
      final rects = subgraph.nodeIds
          .map((id) => nodeRects[id])
          .whereType<Rect>()
          .toList(growable: false);
      if (rects.isEmpty) {
        continue;
      }
      final padding = _subgraphPadding(
        nestingHeights[subgraph.id] ?? 0,
        nestedTopStep,
      );

      var left = rects.first.left;
      var top = rects.first.top;
      var right = rects.first.right;
      var bottom = rects.first.bottom;
      for (final rect in rects.skip(1)) {
        left = math.min(left, rect.left);
        top = math.min(top, rect.top);
        right = math.max(right, rect.right);
        bottom = math.max(bottom, rect.bottom);
      }

      final labelPainter = TextPainter(
        text: TextSpan(
          text: subgraph.label,
          style: textStyle.copyWith(
            fontWeight: FontWeight.w700,
            fontSize: (textStyle.fontSize ?? 13) - 1,
          ),
        ),
        textDirection: textDirection,
      )..layout(maxWidth: maxSubgraphLabelWidth);

      final rect = Rect.fromLTRB(
        left - padding.left,
        top - padding.top,
        right + padding.right,
        bottom + padding.bottom,
      );
      // 标题顶部偏移与 _measureNestedTopStep 取自同一 token：嵌套顶部让出的空间
      // 正是按这个偏移 + 标题高度算出来的，两处必须同源，否则标题会压到子框。
      final labelRect = Rect.fromLTWH(
        rect.left + 14,
        rect.top + ChatMermaidLayoutTokens.subgraphLabelTopOffset,
        math.min(maxSubgraphLabelWidth + 18, labelPainter.width + 14),
        math.max(20, labelPainter.height + 6),
      );
      layouts.add(
        ChatMermaidFlowSubgraphLayout(
          subgraph: subgraph,
          rect: rect,
          labelRect: labelRect,
        ),
      );
    }
    layouts.sort(
      (left, right) => left.subgraph.order.compareTo(right.subgraph.order),
    );
    return layouts;
  }

  Size _measureNode({
    required ChatMermaidNode node,
    required TextStyle textStyle,
    required TextDirection textDirection,
  }) {
    final painter = TextPainter(
      text: TextSpan(text: node.label, style: textStyle),
      textDirection: textDirection,
    )..layout(maxWidth: maxNodeTextWidth);

    final contentPadding = ChatMermaidNodeStyleTokens.flowchartPaddingForShape(
      node.shape,
    );
    var width = painter.width + contentPadding.horizontal;
    var height = painter.height + contentPadding.vertical;
    switch (node.shape) {
      case ChatMermaidNodeShape.rectangle:
        break;
      case ChatMermaidNodeShape.rounded:
        width += 8;
        break;
      case ChatMermaidNodeShape.diamond:
        // 文字只能落在菱形的内切矩形内（约为外接框的一半）。把外接框放大到
        // 文字块（含内边距）的约 2 倍，保证多行文字完整落在菱形内不超出斜边；
        // 同时保留宽高比下限，避免菱形过于瘦高。
        width *= 2;
        height *= 2;
        width = math.max(width, height * 1.4);
        break;
      case ChatMermaidNodeShape.circle:
        // 圆能容纳文字块的条件是直径 ≥ 文字块对角线，否则四角会超出圆周。
        final diameter = math.sqrt((width * width) + (height * height)) + 4;
        width = diameter;
        height = diameter;
        break;
      case ChatMermaidNodeShape.stadium:
        width += 8;
        break;
      case ChatMermaidNodeShape.subroutine:
        width += 10;
        break;
      case ChatMermaidNodeShape.cylindrical:
        width += 8;
        height += 8;
        break;
      case ChatMermaidNodeShape.hexagon:
        // 六边形左右各有约 0.15 宽的斜切，文字块上下边缘处可用宽度收窄。
        // 适当放大横向尺寸、并增高以压低文字高度占比，使文字落在可用区内。
        width = math.max(width * 1.5, height * 1.5);
        height *= 1.25;
        break;
    }

    // 宽度：文字宽度已由 maxNodeTextWidth 在测量时限制并强制换行，这里只保留
    // 下限避免节点过窄；不再设会把文字区压窄的上限，否则渲染端按更窄宽度重新
    // 换行会多出行数，导致文字超出按测量高度算出的节点而被遮挡。
    // 高度：随文字行数自适应增长，只保留下限，让文字多的节点能完整容纳。
    final finalWidth = math.max(width, 76).toDouble();

    // diamond/circle/hexagon 的节点尺寸已被几何放大（2×、对角线、1.5×），
    // 渲染时文字可用区远比测量宽度宽，不会触发额外换行，无需二次验证。
    // 其余形状的宽度只比文字宽度多出 padding，渲染可用宽度与测量宽度接近，
    // 用实际渲染宽度重新 layout 一次，取最大高度，消除亚像素差异导致的遮挡。
    var finalHeight = math.max(height, 46).toDouble();
    final needsHeightVerification =
        node.shape != ChatMermaidNodeShape.diamond &&
        node.shape != ChatMermaidNodeShape.circle &&
        node.shape != ChatMermaidNodeShape.hexagon;
    if (needsHeightVerification) {
      final renderTextWidth = finalWidth - contentPadding.horizontal;
      final verifyPainter = TextPainter(
        text: TextSpan(text: node.label, style: textStyle),
        textDirection: textDirection,
      )..layout(maxWidth: renderTextWidth);
      final verifiedHeight = verifyPainter.height + contentPadding.vertical;
      finalHeight = math.max(finalHeight, verifiedHeight);
    }

    return Size(finalWidth, finalHeight);
  }

  int _mapOrientation(ChatMermaidFlowDirection direction) {
    switch (direction) {
      case ChatMermaidFlowDirection.topDown:
        return SugiyamaConfiguration.ORIENTATION_TOP_BOTTOM;
      case ChatMermaidFlowDirection.bottomTop:
        return SugiyamaConfiguration.ORIENTATION_BOTTOM_TOP;
      case ChatMermaidFlowDirection.leftRight:
        return SugiyamaConfiguration.ORIENTATION_LEFT_RIGHT;
      case ChatMermaidFlowDirection.rightLeft:
        return SugiyamaConfiguration.ORIENTATION_RIGHT_LEFT;
    }
  }

  /// 压掉与流向正交方向上超过阈值的空白带（TB/BT 图压 x 轴，LR/RL 图压 y 轴）。
  ///
  /// 把所有节点矩形投影到该轴上，找出没有任何节点覆盖的空档；超过
  /// [levelSeparation]（引擎可配字段，默认取
  /// [ChatMermaidLayoutTokens.flowchartLevelSeparation]）的空档压缩到该值，
  /// 空档右（下）侧的节点整体平移。保序、保留最小间距，因此不会产生新的节点
  /// 重叠；分组框与 padding 的问题由调用方压缩后再跑一遍兄弟消解兜底。
  Map<String, Rect> _compressOrthogonalGaps({
    required Map<String, Rect> nodeRects,
    required ChatMermaidFlowDirection direction,
  }) {
    if (nodeRects.length < 2) {
      return Map<String, Rect>.from(nodeRects);
    }
    final vertical =
        direction == ChatMermaidFlowDirection.topDown ||
        direction == ChatMermaidFlowDirection.bottomTop;
    double startOf(Rect r) => vertical ? r.left : r.top;
    double endOf(Rect r) => vertical ? r.right : r.bottom;

    final sorted = nodeRects.entries.toList()
      ..sort((a, b) => startOf(a.value).compareTo(startOf(b.value)));

    // 与层间距同源：调用方若自定义了 levelSeparation，压缩阈值跟着走，
    // 不能写死 token（引擎字段与 token 不一致时会压得比层间距还紧/还松）。
    final keep = levelSeparation.toDouble();
    final shifts = <String, double>{};
    var covered = endOf(sorted.first.value);
    var accumulatedShift = 0.0;
    shifts[sorted.first.key] = 0;
    for (final entry in sorted.skip(1)) {
      final start = startOf(entry.value);
      final gap = start - covered;
      if (gap > keep) {
        accumulatedShift += gap - keep;
      }
      shifts[entry.key] = accumulatedShift;
      covered = math.max(covered, endOf(entry.value));
    }
    if (accumulatedShift == 0) {
      return Map<String, Rect>.from(nodeRects);
    }
    return <String, Rect>{
      for (final entry in nodeRects.entries)
        entry.key: entry.value.shift(
          vertical
              ? Offset(-shifts[entry.key]!, 0)
              : Offset(0, -shifts[entry.key]!),
        ),
    };
  }

  /// 给每条回边分配走线通道：占用区间重叠的才各占一条，错开的共用。
  ///
  /// 回边在图外侧沿通道方向走一段长直线（TB/BT 图是竖线、LR/RL 图是横线），
  /// 它在通道上的占用区间就是两端锚点之间的跨度。经典区间着色：按区间起点排序，
  /// 贪心放进第一条「最后占用位置 + 间隙」不冲突的通道。区间之间留间隙，避免
  /// 两条共道回边的端点贴在一起分不清。
  /// 标签挂在折线最长的一段上：水平段放在线上方，竖直段放在线右侧。
  Offset _labelAnchorForPolyline({
    required List<Offset> points,
    required Size labelSize,
  }) {
    var bestIndex = 0;
    var bestLength = -1.0;
    for (var i = 0; i + 1 < points.length; i++) {
      final length = (points[i + 1] - points[i]).distance;
      if (length > bestLength) {
        bestLength = length;
        bestIndex = i;
      }
    }
    final a = points[bestIndex];
    final b = points[bestIndex + 1];
    final mid = Offset((a.dx + b.dx) / 2, (a.dy + b.dy) / 2);
    if ((a.dy - b.dy).abs() <= (a.dx - b.dx).abs()) {
      return _labelAnchorAboveHorizontal(
        midX: mid.dx,
        lineY: mid.dy,
        labelSize: labelSize,
      );
    }
    return _labelAnchorRightOfVertical(
      lineX: mid.dx,
      midY: mid.dy,
      labelSize: labelSize,
    );
  }

  ChatMermaidRoutedEdge _routeSelfLoop({
    required ChatMermaidEdge edge,
    required Rect rect,
    required Size labelSize,
  }) {
    const loopOffset = 30.0;
    final start = Offset(rect.right, rect.center.dy - 8);
    final points = <Offset>[
      start,
      Offset(rect.right + loopOffset, rect.center.dy - 8),
      Offset(rect.right + loopOffset, rect.center.dy + 8),
      Offset(rect.right, rect.center.dy + 8),
    ];
    return ChatMermaidRoutedEdge(
      edge: edge,
      points: points,
      labelAnchor: _labelAnchorRightOfVertical(
        lineX: rect.right + loopOffset,
        midY: rect.center.dy,
        labelSize: labelSize,
        avoidRects: <Rect>[rect],
      ),
      labelSize: labelSize,
    );
  }

  Size _measureEdgeLabel({
    required String? label,
    required TextStyle style,
    required TextDirection textDirection,
  }) {
    if (label == null || label.isEmpty) {
      return Size.zero;
    }
    final painter = TextPainter(
      text: TextSpan(text: label, style: style),
      textDirection: textDirection,
    )..layout(maxWidth: maxLabelTextWidth);
    return Size(painter.width + 10, painter.height + 6);
  }

  double _maxPointDx(List<Offset> points) =>
      points.fold<double>(0, (current, point) => math.max(current, point.dx));

  double _maxPointDy(List<Offset> points) =>
      points.fold<double>(0, (current, point) => math.max(current, point.dy));

  Offset _labelAnchorAboveHorizontal({
    required double midX,
    required double lineY,
    required Size labelSize,
    List<Rect> avoidRects = const <Rect>[],
  }) {
    if (labelSize == Size.zero) {
      return Offset(midX, lineY - edgeLabelGap);
    }
    final left = midX - (labelSize.width / 2);
    final right = midX + (labelSize.width / 2);
    var bottom = lineY - edgeLabelGap;
    for (final rect in avoidRects) {
      if (_rangesOverlap(
        left,
        right,
        rect.left - edgeLabelGap,
        rect.right + edgeLabelGap,
      )) {
        bottom = math.min(bottom, rect.top - edgeLabelGap);
      }
    }
    return Offset(midX, bottom - (labelSize.height / 2));
  }

  Offset _labelAnchorRightOfVertical({
    required double lineX,
    required double midY,
    required Size labelSize,
    List<Rect> avoidRects = const <Rect>[],
  }) {
    if (labelSize == Size.zero) {
      return Offset(lineX + edgeLabelGap, midY);
    }
    final top = midY - (labelSize.height / 2);
    final bottom = midY + (labelSize.height / 2);
    var left = lineX + edgeLabelGap;
    for (final rect in avoidRects) {
      if (_rangesOverlap(
        top,
        bottom,
        rect.top - edgeLabelGap,
        rect.bottom + edgeLabelGap,
      )) {
        left = math.max(left, rect.right + edgeLabelGap);
      }
    }
    return Offset(left + (labelSize.width / 2), midY);
  }

  bool _rangesOverlap(double startA, double endA, double startB, double endB) {
    return startA < endB && endA > startB;
  }

  List<ChatMermaidRoutedEdge> _resolveEdgeLabelCollisions({
    required List<ChatMermaidRoutedEdge> edges,
    required List<Rect> obstacleRects,
  }) {
    if (edges.isEmpty) {
      return const <ChatMermaidRoutedEdge>[];
    }
    final placedLabelRects = <Rect>[];
    final resolved = <ChatMermaidRoutedEdge>[];

    for (final edge in edges) {
      if (edge.labelSize == Size.zero) {
        resolved.add(edge);
        continue;
      }
      final baseAnchor = edge.labelAnchor;
      final candidateAnchors = _buildLabelAnchorCandidates(
        baseAnchor: baseAnchor,
        labelSize: edge.labelSize,
      );
      var bestAnchor = baseAnchor;
      var bestScore = double.infinity;

      for (final candidate in candidateAnchors) {
        final rect = Rect.fromCenter(
          center: candidate,
          width: edge.labelSize.width,
          height: edge.labelSize.height,
        );
        final obstaclePenalty = _sumOverlapArea(
          rect,
          obstacleRects,
          inflation: edgeLabelGap,
        );
        final labelPenalty = _sumOverlapArea(
          rect,
          placedLabelRects,
          inflation: edgeLabelGap / 2,
        );
        final distancePenalty = (candidate - baseAnchor).distance * 0.01;
        final score = obstaclePenalty + labelPenalty + distancePenalty;
        if (score < bestScore) {
          bestScore = score;
          bestAnchor = candidate;
        }
        if (score == 0) {
          break;
        }
      }

      final resolvedEdge = ChatMermaidRoutedEdge(
        edge: edge.edge,
        points: edge.points,
        labelAnchor: bestAnchor,
        labelSize: edge.labelSize,
      );
      resolved.add(resolvedEdge);
      placedLabelRects.add(
        Rect.fromCenter(
          center: bestAnchor,
          width: edge.labelSize.width,
          height: edge.labelSize.height,
        ),
      );
    }

    return resolved;
  }

  List<Offset> _buildLabelAnchorCandidates({
    required Offset baseAnchor,
    required Size labelSize,
  }) {
    final horizontalStep = labelSize.width + edgeLabelGap;
    final verticalStep = labelSize.height + edgeLabelGap;
    final candidates = <Offset>[];

    void addCandidate(double dx, double dy) {
      candidates.add(Offset(baseAnchor.dx + dx, baseAnchor.dy + dy));
    }

    addCandidate(0, 0);
    addCandidate(horizontalStep, 0);
    addCandidate(-horizontalStep, 0);
    addCandidate(0, -verticalStep);
    addCandidate(0, verticalStep);
    addCandidate(horizontalStep, -verticalStep);
    addCandidate(horizontalStep, verticalStep);
    addCandidate(-horizontalStep, -verticalStep);
    addCandidate(-horizontalStep, verticalStep);
    addCandidate(horizontalStep * 2, 0);
    addCandidate(-horizontalStep * 2, 0);
    addCandidate(0, -verticalStep * 2);
    addCandidate(0, verticalStep * 2);
    addCandidate(horizontalStep * 2, -verticalStep);
    addCandidate(horizontalStep * 2, verticalStep);
    addCandidate(-horizontalStep * 2, -verticalStep);
    addCandidate(-horizontalStep * 2, verticalStep);

    return candidates;
  }

  double _sumOverlapArea(
    Rect rect,
    List<Rect> obstacles, {
    required double inflation,
  }) {
    var total = 0.0;
    for (final obstacle in obstacles) {
      final overlap = rect.intersect(obstacle.inflate(inflation));
      if (!overlap.isEmpty) {
        total += overlap.width * overlap.height;
      }
    }
    return total;
  }

  Map<String, Rect> _normalizeNodeRectsForDirection({
    required Map<String, Rect> nodeRects,
    required ChatMermaidFlowDirection direction,
  }) {
    switch (direction) {
      case ChatMermaidFlowDirection.topDown:
      case ChatMermaidFlowDirection.bottomTop:
        return _centerNodesWithinVerticalLanes(nodeRects);
      case ChatMermaidFlowDirection.leftRight:
      case ChatMermaidFlowDirection.rightLeft:
        return _centerNodesWithinHorizontalLanes(nodeRects);
    }
  }

  Map<String, Rect> _resolveNodeRectCollisionsForDirection({
    required Map<String, Rect> nodeRects,
    required ChatMermaidFlowDirection direction,
  }) {
    switch (direction) {
      case ChatMermaidFlowDirection.topDown:
      case ChatMermaidFlowDirection.bottomTop:
        return _resolveHorizontalNodeCollisions(nodeRects);
      case ChatMermaidFlowDirection.leftRight:
      case ChatMermaidFlowDirection.rightLeft:
        return _resolveVerticalNodeCollisions(nodeRects);
    }
  }

  Map<String, Rect> _centerNodesWithinVerticalLanes(
    Map<String, Rect> nodeRects,
  ) {
    final entries = nodeRects.entries.toList()
      ..sort(
        (left, right) => left.value.center.dx.compareTo(right.value.center.dx),
      );
    final normalized = <String, Rect>{};

    for (final lane in _groupVerticalLaneEntries(entries)) {
      final laneCenterX =
          lane
              .map((entry) => entry.value.center.dx)
              .reduce((sum, value) => sum + value) /
          lane.length;
      for (final entry in lane) {
        normalized[entry.key] = Rect.fromCenter(
          center: Offset(laneCenterX, entry.value.center.dy),
          width: entry.value.width,
          height: entry.value.height,
        );
      }
    }

    return normalized;
  }

  Map<String, Rect> _centerNodesWithinHorizontalLanes(
    Map<String, Rect> nodeRects,
  ) {
    final entries = nodeRects.entries.toList()
      ..sort(
        (left, right) => left.value.center.dy.compareTo(right.value.center.dy),
      );
    final normalized = <String, Rect>{};

    for (final lane in _groupHorizontalLaneEntries(entries)) {
      final laneCenterY =
          lane
              .map((entry) => entry.value.center.dy)
              .reduce((sum, value) => sum + value) /
          lane.length;
      for (final entry in lane) {
        normalized[entry.key] = Rect.fromCenter(
          center: Offset(entry.value.center.dx, laneCenterY),
          width: entry.value.width,
          height: entry.value.height,
        );
      }
    }

    return normalized;
  }

  Map<String, Rect> _resolveHorizontalNodeCollisions(
    Map<String, Rect> nodeRects,
  ) {
    final entries = nodeRects.entries.toList()
      ..sort((left, right) {
        final topCompare = left.value.top.compareTo(right.value.top);
        if (topCompare != 0) {
          return topCompare;
        }
        return left.value.left.compareTo(right.value.left);
      });
    final resolved = Map<String, Rect>.from(nodeRects);
    final gap = _minimumNodeGap;

    for (final row in _groupHorizontalLaneEntries(entries)) {
      final orderedRow = row.toList()
        ..sort((left, right) => left.value.left.compareTo(right.value.left));
      Rect? previousRect;
      for (final entry in orderedRow) {
        var rect = resolved[entry.key]!;
        if (previousRect != null) {
          final minimumLeft = previousRect.right + gap;
          if (rect.left < minimumLeft) {
            rect = rect.shift(Offset(minimumLeft - rect.left, 0));
            resolved[entry.key] = rect;
          }
        }
        previousRect = rect;
      }
    }

    return resolved;
  }

  Map<String, Rect> _resolveVerticalNodeCollisions(
    Map<String, Rect> nodeRects,
  ) {
    final entries = nodeRects.entries.toList()
      ..sort((left, right) {
        final leftCompare = left.value.left.compareTo(right.value.left);
        if (leftCompare != 0) {
          return leftCompare;
        }
        return left.value.top.compareTo(right.value.top);
      });
    final resolved = Map<String, Rect>.from(nodeRects);
    final gap = _minimumNodeGap;

    for (final column in _groupVerticalLaneEntries(entries)) {
      final orderedColumn = column.toList()
        ..sort((left, right) => left.value.top.compareTo(right.value.top));
      Rect? previousRect;
      for (final entry in orderedColumn) {
        var rect = resolved[entry.key]!;
        if (previousRect != null) {
          final minimumTop = previousRect.bottom + gap;
          if (rect.top < minimumTop) {
            rect = rect.shift(Offset(0, minimumTop - rect.top));
            resolved[entry.key] = rect;
          }
        }
        previousRect = rect;
      }
    }

    return resolved;
  }

  double get _minimumNodeGap => math.max(8, nodeSeparation).toDouble();

  /// 全局兜底：消除任意两节点矩形之间的真实叠加。
  ///
  /// lane 内消解只在单一方向推开节点，无法处理居中对齐与分层算法在正交方向
  /// 残留的重叠。这里遍历所有节点对，对真正重叠者按"最小平移向量"沿重叠更小
  /// 的轴推开最小距离，仅在确有叠加时移动节点，对已正确的布局零影响。
  Map<String, Rect> _separateOverlappingNodes({
    required Map<String, Rect> nodeRects,
  }) {
    if (nodeRects.length < 2) {
      return nodeRects;
    }
    final gap = _minimumNodeGap;
    final resolved = Map<String, Rect>.from(nodeRects);
    final ids = resolved.keys.toList();
    const maxPasses = 16;

    for (var pass = 0; pass < maxPasses; pass++) {
      // 稳定排序：先按横向中心，再按纵向中心，保证多次 pass 的推移方向一致、可收敛。
      ids.sort((left, right) {
        final leftRect = resolved[left]!;
        final rightRect = resolved[right]!;
        final dxCompare = leftRect.center.dx.compareTo(rightRect.center.dx);
        if (dxCompare != 0) {
          return dxCompare;
        }
        return leftRect.center.dy.compareTo(rightRect.center.dy);
      });

      var moved = false;
      for (var i = 0; i < ids.length; i++) {
        for (var j = i + 1; j < ids.length; j++) {
          final a = resolved[ids[i]]!;
          final b = resolved[ids[j]]!;
          final overlapX =
              math.min(a.right, b.right) - math.max(a.left, b.left);
          final overlapY =
              math.min(a.bottom, b.bottom) - math.max(a.top, b.top);
          if (overlapX <= 0 || overlapY <= 0) {
            continue; // 仅在两轴都重叠（真实叠加）时才分离。
          }

          if (overlapX <= overlapY) {
            // 沿水平方向分离：把横向中心更靠后的节点向右推，保持单调收敛。
            final push = overlapX + gap;
            if (a.center.dx <= b.center.dx) {
              resolved[ids[j]] = b.shift(Offset(push, 0));
            } else {
              resolved[ids[i]] = a.shift(Offset(push, 0));
            }
          } else {
            // 沿垂直方向分离：把纵向中心更靠后的节点向下推。
            final push = overlapY + gap;
            if (a.center.dy <= b.center.dy) {
              resolved[ids[j]] = b.shift(Offset(0, push));
            } else {
              resolved[ids[i]] = a.shift(Offset(0, push));
            }
          }
          moved = true;
        }
      }

      if (!moved) {
        break;
      }
    }

    return resolved;
  }

  /// 消除「分组框（subgraph）相互叠加」。
  ///
  /// [_separateOverlappingNodes] 只保证节点矩形不重叠，但分组框是把成员节点的
  /// 包围盒再向外扩 padding（[ChatMermaidLayoutTokens.subgraphPadding*]，与
  /// [_buildSubgraphLayouts] 同源）画出来的。当一个中心节点同时连多个分组、
  /// 被分层算法并排到同一秩时，
  /// 相邻分组的成员间距可能小于两侧 padding 之和，分组框就会横向叠在一起
  /// （老郭反馈「三分组压在一块」）。这里直接对分组框做重叠消解，并把位移施加到
  /// 框内成员节点上。
  ///
  /// 只处理「顶层分组」：成员集不是任何其它分组真子集的分组。嵌套子分组的成员
  /// 同时属于父分组，会随父分组整体平移，无需单独参与，从而不会被推出父框。
  /// 合法 mermaid 的分组构成森林，顶层分组之间成员两两互斥。
  Map<String, Rect> _separateSiblingSubgraphs({
    required ChatMermaidFlowchart diagram,
    required Map<String, Rect> nodeRects,
    required double nestedTopStep,
  }) {
    // 与 _buildSubgraphLayouts 共用同一个外扩量函数（含嵌套层级放大）和同一个
    // nestedTopStep，保证「算出的框」与「画出的框」完全一致。
    final nestingHeights = _computeNestingHeights(diagram);
    final paddings = <String, EdgeInsets>{
      for (final subgraph in diagram.subgraphs)
        subgraph.id: _subgraphPadding(
          nestingHeights[subgraph.id] ?? 0,
          nestedTopStep,
        ),
    };

    final memberSets = <String, Set<String>>{};
    for (final subgraph in diagram.subgraphs) {
      final ids = subgraph.nodeIds.where(nodeRects.containsKey).toSet();
      if (ids.isNotEmpty) {
        memberSets[subgraph.id] = ids;
      }
    }
    // 消解必须按「兄弟组」分层做，不能只做顶层。
    //
    // 旧实现只取顶层分组（成员集不是任何其它分组真子集的分组），于是嵌套进
    // BOARD 的 APP/PROTO/DRV/BASE——它们的成员集都是 BOARD 的真子集——被整体排
    // 除在外，**嵌套的兄弟分组之间从来不做重叠消解**。老郭那张 B806 图里 PROTO
    // 与 DRV 的框就这么实打实叠了 643×222px，而顶层的 FIELD/BOARD/CHIPS 却是好
    // 的，测试只盯顶层就会全绿放行。
    //
    // 只有同一个父分组下的分组才互为兄弟、需要分离；父与子本就该包含，不算重叠。
    final parents = _computeSubgraphParents(memberSets);
    final siblingSets = <String?, List<String>>{};
    for (final id in memberSets.keys) {
      (siblingSets[parents[id]] ??= <String>[]).add(id);
    }
    // 返回副本而非入参本身：调用方会对返回值做 clear()+addAll() 回填，若返回
    // 入参同一引用会先清空再从空 map 回填，导致节点全部丢失。
    final resolved = Map<String, Rect>.from(nodeRects);

    Rect boxOf(String subgraphId) {
      final ids = memberSets[subgraphId]!;
      var rect = resolved[ids.first]!;
      for (final id in ids.skip(1)) {
        rect = rect.expandToInclude(resolved[id]!);
      }
      final padding = paddings[subgraphId] ?? EdgeInsets.zero;
      return Rect.fromLTRB(
        rect.left - padding.left,
        rect.top - padding.top,
        rect.right + padding.right,
        rect.bottom + padding.bottom,
      );
    }

    // 先分内层、再分外层：内层兄弟被推开会撑大祖先的包围盒，外层必须看到撑大之
    // 后的尺寸才分得准。按父的嵌套深度倒序处理，顶层（父为 null）最后。
    int nestingDepthOf(String? id) {
      var depth = 0;
      var cursor = id;
      while (cursor != null) {
        depth++;
        cursor = parents[cursor];
      }
      return depth;
    }

    final orderedParents = siblingSets.keys.toList()
      ..sort((a, b) => nestingDepthOf(b).compareTo(nestingDepthOf(a)));

    for (final parent in orderedParents) {
      final siblings = siblingSets[parent]!;
      if (siblings.length < 2) {
        continue;
      }
      _pushSiblingsApart(
        siblings: siblings,
        resolved: resolved,
        memberSets: memberSets,
        boxOf: boxOf,
      );
    }

    return resolved;
  }

  /// 每个分组的最近祖先分组：成员集真包含它、且成员最少的那个。顶层分组为 null。
  ///
  /// 用成员包含关系而非声明顺序判定：parser 会把节点加进所有活动分组，嵌套子分
  /// 组的成员必然是祖先成员的真子集。取「最小的真超集」即最近的那层祖先。
  static Map<String, String?> _computeSubgraphParents(
    Map<String, Set<String>> memberSets,
  ) {
    final parents = <String, String?>{};
    for (final entry in memberSets.entries) {
      final self = entry.value;
      String? closest;
      int? closestSize;
      for (final other in memberSets.entries) {
        if (other.key == entry.key) {
          continue;
        }
        final candidate = other.value;
        // 真超集：包含自己的全部成员，且严格更大。成员集完全相同的两个分组互
        // 不为父子，会落在同一层当兄弟处理（共享成员会在推移时被跳过）。
        if (candidate.length > self.length && self.every(candidate.contains)) {
          if (closestSize == null || candidate.length < closestSize) {
            closestSize = candidate.length;
            closest = other.key;
          }
        }
      }
      parents[entry.key] = closest;
    }
    return parents;
  }

  /// 把一组互为兄弟的分组框两两推开，位移施加到框内成员节点上。
  void _pushSiblingsApart({
    required List<String> siblings,
    required Map<String, Rect> resolved,
    required Map<String, Set<String>> memberSets,
    required Rect Function(String) boxOf,
  }) {
    final gap = _minimumNodeGap;
    const maxPasses = 16;

    for (var pass = 0; pass < maxPasses; pass++) {
      final boxes = <String, Rect>{for (final id in siblings) id: boxOf(id)};
      // 稳定排序：按框中心，保证每轮推移方向一致、可收敛。
      siblings.sort((a, b) {
        final dx = boxes[a]!.center.dx.compareTo(boxes[b]!.center.dx);
        if (dx != 0) {
          return dx;
        }
        return boxes[a]!.center.dy.compareTo(boxes[b]!.center.dy);
      });

      var moved = false;
      for (var i = 0; i < siblings.length; i++) {
        for (var j = i + 1; j < siblings.length; j++) {
          // 若两个子图共享成员节点，移动任意一方都会同步移动另一方的成员，
          // 导致两个框同步偏移、永远无法分离（16 轮后节点被推出画布）。跳过。
          final membersA = memberSets[siblings[i]]!;
          final membersB = memberSets[siblings[j]]!;
          if (membersA.any(membersB.contains)) {
            continue;
          }

          final ra = boxes[siblings[i]]!;
          final rb = boxes[siblings[j]]!;
          final overlapX =
              math.min(ra.right, rb.right) - math.max(ra.left, rb.left);
          final overlapY =
              math.min(ra.bottom, rb.bottom) - math.max(ra.top, rb.top);
          if (overlapX <= 0 || overlapY <= 0) {
            continue; // 仅在两轴都重叠（真实叠加）时才分离。
          }

          final String target;
          final Offset push;
          if (overlapX <= overlapY) {
            // 沿水平方向分离：把横向中心更靠后的分组向右推。
            target = ra.center.dx <= rb.center.dx ? siblings[j] : siblings[i];
            push = Offset(overlapX + gap, 0);
          } else {
            // 沿垂直方向分离：把纵向中心更靠后的分组向下推。
            target = ra.center.dy <= rb.center.dy ? siblings[j] : siblings[i];
            push = Offset(0, overlapY + gap);
          }
          for (final id in memberSets[target]!) {
            resolved[id] = resolved[id]!.shift(push);
          }
          boxes[target] = boxOf(target);
          moved = true;
        }
      }

      if (!moved) {
        break;
      }
    }
  }

  List<List<MapEntry<String, Rect>>> _groupVerticalLaneEntries(
    List<MapEntry<String, Rect>> entries,
  ) {
    final groups = <List<MapEntry<String, Rect>>>[];
    for (final entry in entries) {
      if (groups.isEmpty) {
        groups.add(<MapEntry<String, Rect>>[entry]);
        continue;
      }
      final previous = groups.last.last;
      if (_sharesHorizontalProjection(previous.value, entry.value)) {
        groups.last.add(entry);
        continue;
      }
      groups.add(<MapEntry<String, Rect>>[entry]);
    }
    return groups;
  }

  List<List<MapEntry<String, Rect>>> _groupHorizontalLaneEntries(
    List<MapEntry<String, Rect>> entries,
  ) {
    final groups = <List<MapEntry<String, Rect>>>[];
    for (final entry in entries) {
      if (groups.isEmpty) {
        groups.add(<MapEntry<String, Rect>>[entry]);
        continue;
      }
      final previous = groups.last.last;
      if (_sharesVerticalProjection(previous.value, entry.value)) {
        groups.last.add(entry);
        continue;
      }
      groups.add(<MapEntry<String, Rect>>[entry]);
    }
    return groups;
  }

  bool _sharesHorizontalProjection(Rect left, Rect right) {
    final overlap =
        math.min(left.right, right.right) - math.max(left.left, right.left);
    return overlap >= -alignmentTolerance;
  }

  bool _sharesVerticalProjection(Rect top, Rect bottom) {
    final overlap =
        math.min(top.bottom, bottom.bottom) - math.max(top.top, bottom.top);
    return overlap >= -alignmentTolerance;
  }
}
