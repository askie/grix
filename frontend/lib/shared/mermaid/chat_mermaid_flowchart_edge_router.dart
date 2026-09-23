import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'chat_mermaid_model.dart';

/// 流程图连线的正交路由器。
///
/// 节点坐标由 Sugiyama 与后处理决定之后，这里只负责把边画成「不穿节点、端口
/// 分散、同通道错开」的折线。思路对齐 dagre：
///   · 前向边沿层间空隙走，跨多层时在中间层节点之间挑一条空闲竖直走廊；
///   · 回边从节点侧面出发、走空闲走廊回到上方，从目标顶部进入；
///   · 同一节点的多条出/入边按对端位置在节点边上分散端口；
///   · 同一空隙内互相重叠的水平段、同一走廊上的竖直段按车道错开。
///
/// 内部统一在「自上而下」坐标系里计算，其他方向通过坐标变换转换。
class ChatMermaidFlowchartEdgeRouter {
  const ChatMermaidFlowchartEdgeRouter({
    this.levelSeparation = 72,
    this.obstacleMargin = 20,
    this.laneGap = 12,
    this.laneInset = 14,
    this.portInset = 0.22,
  });

  /// 层间距，用于目标处于首层时估算「上方空隙」的位置。
  final double levelSeparation;

  /// 走廊与节点边框之间的最小距离。
  final double obstacleMargin;

  /// 同一通道内相邻车道的间距。
  final double laneGap;

  /// 最外侧车道与节点边框保留的距离：要装得下末段的箭头，不能贴着框走。
  final double laneInset;

  /// 端口分散时两侧保留的宽度比例。
  final double portInset;

  List<List<Offset>> route({
    required ChatMermaidFlowDirection direction,
    required List<ChatMermaidEdge> edges,
    required Map<String, Rect> anchorRects,
    required Iterable<Rect> obstacleRects,
    Iterable<Rect> corridorObstacleRects = const <Rect>[],
    Set<String> fixedPortIds = const <String>{},
  }) {
    final frame = _Frame(direction);
    final routes = _routeCanonical(
      frame: frame,
      edges: edges,
      anchorRects: anchorRects,
      obstacleRects: obstacleRects,
      corridorObstacleRects: corridorObstacleRects,
      fixedPortIds: fixedPortIds,
    ).routes;
    return <List<Offset>>[
      for (final points in routes)
        <Offset>[for (final point in points) frame.fromCanonical(point)],
    ];
  }

  /// 预走一遍线，把装不下车道的层间空隙撑开：返回下游各层整体推开后的
  /// [nodeRects]；所有空隙都装得下时返回 null。
  ///
  /// 布局阶段的层间距是固定值，扇入扇出密集的图在一个空隙里能挤出十来条
  /// 车道，压缩后线与线、线与节点边框只剩几个像素，看起来像被节点遮住。
  /// 与其压线，不如把下面的层往下挪。
  Map<String, Rect>? expandGapsForLanes({
    required ChatMermaidFlowDirection direction,
    required List<ChatMermaidEdge> edges,
    required Map<String, Rect> anchorRects,
    required Map<String, Rect> nodeRects,
    Iterable<Rect> corridorObstacleRects = const <Rect>[],
    Set<String> fixedPortIds = const <String>{},
  }) {
    final frame = _Frame(direction);
    final deficits = _routeCanonical(
      frame: frame,
      edges: edges,
      anchorRects: anchorRects,
      obstacleRects: nodeRects.values,
      corridorObstacleRects: corridorObstacleRects,
      fixedPortIds: fixedPortIds,
    ).gapDeficits;
    if (deficits.isEmpty) {
      return null;
    }
    final gaps = deficits.entries.toList()
      ..sort((a, b) => a.key.compareTo(b.key));
    return <String, Rect>{
      for (final entry in nodeRects.entries)
        entry.key: () {
          final canonical = frame.toCanonical(entry.value);
          var shift = 0.0;
          for (final gap in gaps) {
            if (canonical.top >= gap.key - 0.5) {
              shift += gap.value;
            }
          }
          return frame.toReal(canonical.shift(Offset(0, shift)));
        }(),
    };
  }

  ({List<List<Offset>> routes, Map<double, double> gapDeficits})
  _routeCanonical({
    required _Frame frame,
    required List<ChatMermaidEdge> edges,
    required Map<String, Rect> anchorRects,
    required Iterable<Rect> obstacleRects,
    required Iterable<Rect> corridorObstacleRects,
    required Set<String> fixedPortIds,
  }) {
    final canonicalAnchors = <String, Rect>{
      for (final entry in anchorRects.entries)
        entry.key: frame.toCanonical(entry.value),
    };
    final obstacles = obstacleRects.map(frame.toCanonical).toList();
    // 分组框只用于挑走廊（走线绕到框外），不参与层带与直线判定。
    final corridorObstacles = <Rect>[
      ...obstacles,
      ...corridorObstacleRects.map(frame.toCanonical),
    ];
    final bands = _Bands.fromRects(obstacles, levelSeparation: levelSeparation);

    final plans = <_EdgePlan?>[];
    for (final edge in edges) {
      final source = canonicalAnchors[edge.sourceId];
      final target = canonicalAnchors[edge.targetId];
      if (source == null || target == null || edge.sourceId == edge.targetId) {
        plans.add(null);
        continue;
      }
      plans.add(_EdgePlan(edge: edge, source: source, target: target));
    }

    _assignPorts(plans, fixedPortIds: fixedPortIds);

    // 先铺前向边，回边再挑走廊时可以避开已被占用的竖直通道，
    // 不至于和前向边并排贴着走一整段。
    final routes = List<List<Offset>>.filled(plans.length, const <Offset>[]);
    final occupied = <_Segment>[];
    for (var pass = 0; pass < 2; pass++) {
      for (var i = 0; i < plans.length; i++) {
        final plan = plans[i];
        if (plan == null || (plan.kind == _EdgeKind.backward) != (pass == 1)) {
          continue;
        }
        final points = plan.kind == _EdgeKind.backward
            ? _routeBackward(plan, bands, corridorObstacles, occupied)
            : _routeOne(plan, bands, obstacles, corridorObstacles);
        routes[i] = points;
        for (var j = 1; j + 2 < points.length; j++) {
          final a = points[j];
          final b = points[j + 1];
          if ((a.dx - b.dx).abs() < 0.5) {
            occupied.add(
              _Segment(
                route: i,
                index: j,
                key: a.dx,
                lo: math.min(a.dy, b.dy),
                hi: math.max(a.dy, b.dy),
              ),
            );
          }
        }
      }
    }
    final gapDeficits = _separateLanes(routes, bands, obstacles);
    return (routes: routes, gapDeficits: gapDeficits);
  }

  // ---------------------------------------------------------------- ports

  /// 菱形、圆形等非矩形节点的边框是斜的或弯的，分散端口会让线头悬在框外，
  /// 这些节点（[fixedPortIds]）始终从中心出入。
  void _assignPorts(
    List<_EdgePlan?> plans, {
    required Set<String> fixedPortIds,
  }) {
    final outgoing = <Rect, List<_EdgePlan>>{};
    final incoming = <Rect, List<_EdgePlan>>{};
    for (final plan in plans) {
      if (plan == null || plan.kind != _EdgeKind.forward) {
        continue;
      }
      if (!fixedPortIds.contains(plan.edge.sourceId)) {
        (outgoing[plan.source] ??= <_EdgePlan>[]).add(plan);
      }
      if (!fixedPortIds.contains(plan.edge.targetId)) {
        (incoming[plan.target] ??= <_EdgePlan>[]).add(plan);
      }
    }
    outgoing.forEach((rect, list) {
      list.sort((a, b) => a.target.center.dx.compareTo(b.target.center.dx));
      for (var i = 0; i < list.length; i++) {
        list[i].sourceX = _portX(rect, i, list.length);
      }
    });
    incoming.forEach((rect, list) {
      list.sort((a, b) => a.source.center.dx.compareTo(b.source.center.dx));
      for (var i = 0; i < list.length; i++) {
        list[i].targetX = _portX(rect, i, list.length);
      }
    });
  }

  double _portX(Rect rect, int index, int count) {
    if (count <= 1) {
      return rect.center.dx;
    }
    final usable = rect.width * (1 - portInset * 2);
    final step = math.min(usable / (count - 1), 28.0);
    final span = step * (count - 1);
    return rect.center.dx - span / 2 + step * index;
  }

  // -------------------------------------------------------------- routing

  List<Offset> _routeOne(
    _EdgePlan plan,
    _Bands bands,
    List<Rect> obstacles,
    List<Rect> corridorObstacles,
  ) {
    switch (plan.kind) {
      case _EdgeKind.forward:
        return _routeForward(plan, bands, obstacles, corridorObstacles);
      case _EdgeKind.backward:
        return _routeBackward(plan, bands, corridorObstacles, const []);
      case _EdgeKind.lateral:
        return _routeLateral(plan);
    }
  }

  List<Offset> _routeForward(
    _EdgePlan plan,
    _Bands bands,
    List<Rect> obstacles,
    List<Rect> corridorObstacles,
  ) {
    final source = plan.source;
    final target = plan.target;
    final sx = plan.sourceX ?? source.center.dx;
    final tx = plan.targetX ?? target.center.dx;
    final start = Offset(sx, source.bottom);
    final end = Offset(tx, target.top);

    final sourceBand = bands.bandContaining(source.center.dy);
    final targetBand = bands.bandContaining(target.center.dy);
    final gapTop = sourceBand?.$2 ?? source.bottom;
    final gapBottom = targetBand?.$1 ?? target.top;

    final between = _obstaclesBetween(
      obstacles,
      top: gapTop,
      bottom: gapBottom,
      exclude: <Rect>[source, target],
    );

    if ((sx - tx).abs() < 0.5 && !_columnBlocked(sx, between)) {
      return <Offset>[start, end];
    }

    final y1 = bands.gapBelow(gapTop, gapBottom);
    final y2 = bands.gapAbove(gapBottom, gapTop);

    if (between.isEmpty || (y2 - y1).abs() < 0.5) {
      // 相邻层：一个 Z 形弯即可。
      final y = between.isEmpty ? (gapTop + gapBottom) / 2 : y1;
      return _dedupe(<Offset>[start, Offset(sx, y), Offset(tx, y), end]);
    }

    final corridorBlocks = corridorObstacles.where(
      (rect) => !rect.contains(source.center) && !rect.contains(target.center),
    );
    final corridor = _pickCorridor(
      preferred: <double>[tx, sx, (sx + tx) / 2],
      obstacles: _obstaclesBetween(
        corridorBlocks.toList(),
        top: y1,
        bottom: y2,
        exclude: <Rect>[source, target],
      ),
    );
    return _dedupe(<Offset>[
      start,
      Offset(sx, y1),
      Offset(corridor, y1),
      Offset(corridor, y2),
      Offset(tx, y2),
      end,
    ]);
  }

  List<Offset> _routeBackward(
    _EdgePlan plan,
    _Bands bands,
    List<Rect> obstacles,
    List<_Segment> occupied,
  ) {
    final source = plan.source;
    final target = plan.target;
    final tx = target.center.dx;
    // 走廊必须避开从目标层到起点层之间的所有节点（含起点、目标自身所在层）。
    final between = _obstaclesBetween(
      obstacles,
      top: target.top - 1,
      bottom: source.bottom + 1,
      exclude: const <Rect>[],
    );
    // 两侧各取最近的空闲走廊；水平引出段会穿过同层兄弟节点的一侧被排除，
    // 两侧都干净时取离起点更近的一侧。
    final rightCorridor = _pickCorridor(
      preferred: <double>[source.right + obstacleMargin],
      obstacles: between,
    );
    final leftCorridor = _pickCorridor(
      preferred: <double>[source.left - obstacleMargin],
      obstacles: between,
    );
    bool crossesSibling(double corridor) {
      final lo = math.min(corridor, source.center.dx);
      final hi = math.max(corridor, source.center.dx);
      return between.any(
        (rect) =>
            rect != source &&
            rect.top < source.center.dy &&
            rect.bottom > source.center.dy &&
            rect.right > lo &&
            rect.left < hi,
      );
    }

    final yAbove = bands.gapAbove(target.top, double.negativeInfinity);
    // 代价：穿兄弟节点不可选；已被其他边占用的竖直通道重罚；其余按距离。
    double cost(double corridor) {
      if (crossesSibling(corridor)) {
        return double.infinity;
      }
      final crowded = occupied.any(
        (segment) =>
            (segment.key - corridor).abs() < obstacleMargin &&
            segment.hi > yAbove &&
            segment.lo < source.center.dy,
      );
      final distance = corridor > source.center.dx
          ? corridor - source.right
          : source.left - corridor;
      return distance + (crowded ? 1000 : 0);
    }

    final rightCost = cost(rightCorridor);
    final leftCost = cost(leftCorridor);
    final corridor = leftCost < rightCost ? leftCorridor : rightCorridor;
    final exitRight = corridor >= source.center.dx;
    final start = Offset(
      exitRight ? source.right : source.left,
      source.center.dy,
    );
    return _dedupe(<Offset>[
      start,
      Offset(corridor, start.dy),
      Offset(corridor, yAbove),
      Offset(tx, yAbove),
      Offset(tx, target.top),
    ]);
  }

  List<Offset> _routeLateral(_EdgePlan plan) {
    final source = plan.source;
    final target = plan.target;
    final toRight = target.center.dx >= source.center.dx;
    final start = Offset(
      toRight ? source.right : source.left,
      source.center.dy,
    );
    final end = Offset(toRight ? target.left : target.right, target.center.dy);
    if ((start.dy - end.dy).abs() < 0.5) {
      return <Offset>[start, end];
    }
    final midX = (start.dx + end.dx) / 2;
    return _dedupe(<Offset>[
      start,
      Offset(midX, start.dy),
      Offset(midX, end.dy),
      end,
    ]);
  }

  // ------------------------------------------------------------ obstacles

  List<Rect> _obstaclesBetween(
    List<Rect> obstacles, {
    required double top,
    required double bottom,
    required List<Rect> exclude,
  }) {
    return <Rect>[
      for (final rect in obstacles)
        if (!exclude.contains(rect) && rect.bottom > top && rect.top < bottom)
          rect,
    ];
  }

  bool _columnBlocked(double x, List<Rect> obstacles) {
    for (final rect in obstacles) {
      if (x > rect.left - obstacleMargin / 2 &&
          x < rect.right + obstacleMargin / 2) {
        return true;
      }
    }
    return false;
  }

  /// 在 [obstacles] 之间挑一条竖直走廊：依次尝试 [preferred] 里的 x，第一个
  /// 未被遮挡的直接用；都被遮挡时取离首选 x 最近的空闲边界。
  double _pickCorridor({
    required List<double> preferred,
    required List<Rect> obstacles,
  }) {
    if (obstacles.isEmpty) {
      return preferred.first;
    }
    final blocked = _mergeIntervals(<(double, double)>[
      for (final rect in obstacles)
        (rect.left - obstacleMargin, rect.right + obstacleMargin),
    ]);
    bool isFree(double x) =>
        !blocked.any((interval) => x > interval.$1 && x < interval.$2);
    for (final x in preferred) {
      if (isFree(x)) {
        return x;
      }
    }
    final anchor = preferred.first;
    var best = anchor;
    var bestDistance = double.infinity;
    for (final interval in blocked) {
      for (final edge in <double>[interval.$1, interval.$2]) {
        if (!isFree(edge)) {
          continue;
        }
        final distance = (edge - anchor).abs();
        if (distance < bestDistance) {
          bestDistance = distance;
          best = edge;
        }
      }
    }
    return best;
  }

  List<(double, double)> _mergeIntervals(List<(double, double)> intervals) {
    if (intervals.isEmpty) {
      return intervals;
    }
    intervals.sort((a, b) => a.$1.compareTo(b.$1));
    final merged = <(double, double)>[intervals.first];
    for (final interval in intervals.skip(1)) {
      final last = merged.last;
      if (interval.$1 <= last.$2) {
        merged[merged.length - 1] = (last.$1, math.max(last.$2, interval.$2));
      } else {
        merged.add(interval);
      }
    }
    return merged;
  }

  // ---------------------------------------------------------------- lanes

  /// 把「同一水平线上 x 区间重叠的水平段」和「同一竖直线上 y 区间重叠的竖直段」
  /// 分配到不同车道并错开。首尾段贴着节点边框，不参与错开。
  ///
  /// 水平段的车道顺序按「目标越远越靠上」排：源在上、目标在下的两条同向线，
  /// 走得更远的那条走上面才不会被另一条的落线截断（同向不嵌套的两条线由此
  /// 零交叉，嵌套或反向的怎么排都得交叉一次）。
  ///
  /// 车道总宽受所在空隙限制：层间空隙（或走廊两侧最近的节点）装不下
  /// `laneGap × 车道数` 时按空隙等分压缩，否则外侧车道会溢出到相邻层节点
  /// 顶上、被节点遮住（扇入扇出多的图最常见）。返回每个装不下的层间空隙还缺
  /// 多少高度（键为空隙上沿的规范 y），供布局把下游层推开后重新走线。
  Map<double, double> _separateLanes(
    List<List<Offset>> routes,
    _Bands bands,
    List<Rect> obstacles,
  ) {
    final deficits = _separateAxis(routes, bands, obstacles, horizontal: true);
    _separateAxis(routes, bands, obstacles, horizontal: false);
    return deficits;
  }

  /// 一组同键车道可用的区间：水平段取所在层间空隙，竖直段取走廊两侧最近的
  /// 节点边界。`bounded` 表示两侧都有节点、空隙有限。
  ({double low, double high, bool bounded}) _laneBounds(
    double key,
    double lo,
    double hi,
    _Bands bands,
    List<Rect> obstacles, {
    required bool horizontal,
  }) {
    var low = double.negativeInfinity;
    var high = double.infinity;
    if (horizontal) {
      for (final band in bands._bands) {
        if (band.$2 <= key + 0.5) {
          low = math.max(low, band.$2);
        }
        if (band.$1 >= key - 0.5) {
          high = math.min(high, band.$1);
        }
      }
    } else {
      for (final rect in obstacles) {
        if (rect.bottom <= lo || rect.top >= hi) {
          continue;
        }
        if (rect.right <= key + 0.5) {
          low = math.max(low, rect.right);
        }
        if (rect.left >= key - 0.5) {
          high = math.min(high, rect.left);
        }
      }
    }
    final bounded = low.isFinite && high.isFinite;
    if (!low.isFinite) {
      low = key - levelSeparation / 2;
    }
    if (!high.isFinite) {
      high = key + levelSeparation / 2;
    }
    return (low: low, high: high, bounded: bounded);
  }

  Map<double, double> _separateAxis(
    List<List<Offset>> routes,
    _Bands bands,
    List<Rect> obstacles, {
    required bool horizontal,
  }) {
    double along(Offset p) => horizontal ? p.dx : p.dy;
    double across(Offset p) => horizontal ? p.dy : p.dx;
    final segments = <_Segment>[];
    for (var r = 0; r < routes.length; r++) {
      final points = routes[r];
      for (var i = 1; i + 2 < points.length; i++) {
        final a = points[i];
        final b = points[i + 1];
        final isHorizontal = (a.dy - b.dy).abs() < 0.5;
        if (isHorizontal != horizontal) {
          continue;
        }
        segments.add(
          _Segment(
            route: r,
            index: i,
            key: across(a),
            lo: math.min(along(a), along(b)),
            hi: math.max(along(a), along(b)),
            from: along(a),
            toward: along(b),
            fromDir: (across(points[i - 1]) - across(a)).sign,
            towardDir: (across(points[i + 2]) - across(b)).sign,
          ),
        );
      }
    }
    final groups = <int, List<_Segment>>{};
    for (final segment in segments) {
      (groups[segment.key.round()] ??= <_Segment>[]).add(segment);
    }
    final deficits = <double, double>{};
    for (final group in groups.values) {
      if (group.length < 2) {
        continue;
      }
      final lanes = _assignLanes(group);
      final laneCount = lanes.reduce(math.max) + 1;
      if (laneCount < 2) {
        continue;
      }
      final key = group.first.key;
      final bounds = _laneBounds(
        key,
        group.map((s) => s.lo).reduce(math.min),
        group.map((s) => s.hi).reduce(math.max),
        bands,
        obstacles,
        horizontal: horizontal,
      );
      final low = bounds.low + laneInset;
      final high = bounds.high - laneInset;
      final available = math.max(0.0, high - low);
      final needed = (laneCount - 1) * laneGap;
      final fits = needed <= available;
      if (!fits && horizontal && bounds.bounded) {
        deficits[bounds.low] = math.max(
          deficits[bounds.low] ?? 0,
          needed - available,
        );
      }
      final gap = fits ? laneGap : available / (laneCount - 1);
      final center = fits ? key : (low + high) / 2;
      for (var i = 0; i < group.length; i++) {
        final segment = group[i];
        final offset = center - key + (lanes[i] - (laneCount - 1) / 2) * gap;
        final points = routes[segment.route];
        for (final index in <int>[segment.index, segment.index + 1]) {
          final point = points[index];
          points[index] = horizontal
              ? Offset(point.dx, point.dy + offset)
              : Offset(point.dx + offset, point.dy);
        }
      }
    }
    return deficits;
  }

  /// 给一组同键平行段分车道（车道 0 在最上/最左），返回与 [group] 对齐的车道号。
  ///
  /// 两段区间重叠时，「谁在上」决定要不要多一次交叉：一段两端的引线各有方向
  /// （朝上/朝下），朝下的引线会穿过压在它下面的段，朝上的会穿过压在上面的段。
  /// 对每一对重叠段比较两种叠放的交叉数，少的那种记为约束；按约束拓扑序放段，
  /// 同层次再挑第一条不重叠的车道。约束成环时按 [_Segment.rank] 先放的优先。
  List<int> _assignLanes(List<_Segment> group) {
    final n = group.length;
    bool overlaps(_Segment a, _Segment b) =>
        a.lo <= b.hi + 1 && a.hi >= b.lo - 1;
    bool inside(double x, _Segment s) => x > s.lo + 0.5 && x < s.hi - 0.5;
    // upper 压在 lower 上面时的交叉数。
    int crossings(_Segment upper, _Segment lower) {
      var count = 0;
      if (upper.fromDir > 0 && inside(upper.from, lower)) count++;
      if (upper.towardDir > 0 && inside(upper.toward, lower)) count++;
      if (lower.fromDir < 0 && inside(lower.from, upper)) count++;
      if (lower.towardDir < 0 && inside(lower.toward, upper)) count++;
      return count;
    }

    // above[j] 记录必须压在 j 上面的段，below[i] 是其反向索引。
    final above = List<List<int>>.generate(n, (_) => <int>[]);
    final below = List<List<int>>.generate(n, (_) => <int>[]);
    for (var i = 0; i < n; i++) {
      for (var j = i + 1; j < n; j++) {
        if (!overlaps(group[i], group[j])) {
          continue;
        }
        final ij = crossings(group[i], group[j]);
        final ji = crossings(group[j], group[i]);
        if (ij < ji) {
          above[j].add(i);
          below[i].add(j);
        } else if (ji < ij) {
          above[i].add(j);
          below[j].add(i);
        }
      }
    }
    final order = List<int>.generate(n, (i) => i)
      ..sort((a, b) => group[a].rank.compareTo(group[b].rank));
    final pending = <int>[for (final upper in above) upper.length];
    final lanes = List<int>.filled(n, -1);
    final laneIntervals = <List<(double, double)>>[];
    var placedCount = 0;
    while (placedCount < n) {
      var pick = -1;
      for (final i in order) {
        if (lanes[i] < 0 && pending[i] == 0) {
          pick = i;
          break;
        }
      }
      if (pick < 0) {
        // 约束成环：放 rank 最靠前的，忽略它尚未满足的约束。
        pick = order.firstWhere((i) => lanes[i] < 0);
      }
      var required = 0;
      for (final upper in above[pick]) {
        if (lanes[upper] >= 0) {
          required = math.max(required, lanes[upper] + 1);
        }
      }
      final segment = group[pick];
      var lane = -1;
      for (var i = required; i < laneIntervals.length; i++) {
        final clash = laneIntervals[i].any(
          (used) => segment.lo <= used.$2 + 1 && segment.hi >= used.$1 - 1,
        );
        if (!clash) {
          lane = i;
          break;
        }
      }
      if (lane < 0) {
        lane = math.max(required, laneIntervals.length);
        while (laneIntervals.length <= lane) {
          laneIntervals.add(<(double, double)>[]);
        }
      }
      laneIntervals[lane].add((segment.lo, segment.hi));
      lanes[pick] = lane;
      placedCount++;
      for (final lower in below[pick]) {
        pending[lower]--;
      }
    }
    return lanes;
  }

  List<Offset> _dedupe(List<Offset> points) {
    final result = <Offset>[];
    for (final point in points) {
      if (result.isNotEmpty && (result.last - point).distance < 0.5) {
        continue;
      }
      result.add(point);
    }
    // 去掉共线的中间点（三点同 x 或同 y）。
    var i = 1;
    while (i + 1 < result.length) {
      final a = result[i - 1];
      final b = result[i];
      final c = result[i + 1];
      final sameX = (a.dx - b.dx).abs() < 0.5 && (b.dx - c.dx).abs() < 0.5;
      final sameY = (a.dy - b.dy).abs() < 0.5 && (b.dy - c.dy).abs() < 0.5;
      if (sameX || sameY) {
        result.removeAt(i);
      } else {
        i++;
      }
    }
    return result;
  }
}

enum _EdgeKind { forward, backward, lateral }

class _EdgePlan {
  _EdgePlan({required this.edge, required this.source, required this.target})
    : kind = _classify(source, target);

  final ChatMermaidEdge edge;
  final Rect source;
  final Rect target;
  final _EdgeKind kind;
  double? sourceX;
  double? targetX;

  static _EdgeKind _classify(Rect source, Rect target) {
    if (target.top >= source.bottom - 0.5) {
      return _EdgeKind.forward;
    }
    if (target.bottom <= source.top + 0.5) {
      return _EdgeKind.backward;
    }
    return _EdgeKind.lateral;
  }
}

class _Segment {
  const _Segment({
    required this.route,
    required this.index,
    required this.key,
    required this.lo,
    required this.hi,
    this.from = 0,
    this.toward = 0,
    this.fromDir = 0,
    this.towardDir = 0,
  });

  final int route;
  final int index;
  final double key;
  final double lo;
  final double hi;

  /// 段两端在走线方向上的坐标：[from] 靠近源，[toward] 靠近目标。
  final double from;
  final double toward;

  /// 两端引线的走向：+1 朝规范坐标的下/右，-1 朝上/左，0 无引线。
  final double fromDir;
  final double towardDir;

  /// 约束成环时的兜底顺序：越小越靠上。向右走的目标越远越靠上，向左走的同理。
  double get rank => toward > from ? -toward : toward;
}

/// 节点按 y 区间重叠聚成的层带；层带之间的空隙是水平走线的位置。
class _Bands {
  _Bands._(this._bands, this.levelSeparation);

  final List<(double, double)> _bands;
  final double levelSeparation;

  factory _Bands.fromRects(List<Rect> rects, {double levelSeparation = 72}) {
    final intervals = <(double, double)>[
      for (final rect in rects) (rect.top, rect.bottom),
    ]..sort((a, b) => a.$1.compareTo(b.$1));
    final bands = <(double, double)>[];
    for (final interval in intervals) {
      if (bands.isNotEmpty && interval.$1 < bands.last.$2) {
        bands[bands.length - 1] = (
          bands.last.$1,
          math.max(bands.last.$2, interval.$2),
        );
      } else {
        bands.add(interval);
      }
    }
    return _Bands._(bands, levelSeparation);
  }

  /// 紧挨 [y] 之下的空隙中线；空隙不能越过 [limit]。
  double gapBelow(double y, double limit) {
    for (final band in _bands) {
      if (band.$1 >= y - 0.5) {
        final gapEnd = math.min(band.$1, limit);
        return (y + gapEnd) / 2;
      }
    }
    return (y + limit) / 2;
  }

  /// 包含规范坐标 [y] 的层带（以节点中心定位层）。
  (double, double)? bandContaining(double y) {
    for (final band in _bands) {
      if (y >= band.$1 - 0.5 && y <= band.$2 + 0.5) {
        return band;
      }
    }
    return null;
  }

  /// 紧挨 [y] 之上的空隙中线；空隙不能越过 [limit]（可为负无穷）。
  double gapAbove(double y, double limit) {
    for (final band in _bands.reversed) {
      if (band.$2 <= y + 0.5) {
        final gapStart = math.max(band.$2, limit);
        return (gapStart + y) / 2;
      }
    }
    if (limit.isFinite) {
      return (limit + y) / 2;
    }
    return y - levelSeparation / 2;
  }
}

/// 把任意方向的坐标映射到「自上而下」规范坐标系。
class _Frame {
  const _Frame(this.direction);

  final ChatMermaidFlowDirection direction;

  Rect toCanonical(Rect rect) {
    final a = fromCanonicalPoint(rect.topLeft, inverse: false);
    final b = fromCanonicalPoint(rect.bottomRight, inverse: false);
    return Rect.fromPoints(a, b);
  }

  Offset fromCanonical(Offset point) =>
      fromCanonicalPoint(point, inverse: true);

  Rect toReal(Rect rect) {
    final a = fromCanonical(rect.topLeft);
    final b = fromCanonical(rect.bottomRight);
    return Rect.fromPoints(a, b);
  }

  /// 变换是自逆的（转置 / 取反各自对合），正反向共用一个实现。
  Offset fromCanonicalPoint(Offset point, {required bool inverse}) {
    switch (direction) {
      case ChatMermaidFlowDirection.topDown:
        return point;
      case ChatMermaidFlowDirection.bottomTop:
        return Offset(point.dx, -point.dy);
      case ChatMermaidFlowDirection.leftRight:
        return Offset(point.dy, point.dx);
      case ChatMermaidFlowDirection.rightLeft:
        return inverse
            ? Offset(-point.dy, point.dx)
            : Offset(point.dy, -point.dx);
    }
  }
}
