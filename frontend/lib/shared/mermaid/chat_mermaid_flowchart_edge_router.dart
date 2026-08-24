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
    this.portInset = 0.22,
  });

  /// 层间距，用于目标处于首层时估算「上方空隙」的位置。
  final double levelSeparation;

  /// 走廊与节点边框之间的最小距离。
  final double obstacleMargin;

  /// 同一通道内相邻车道的间距。
  final double laneGap;

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
    _separateLanes(routes);
    return <List<Offset>>[
      for (final points in routes)
        <Offset>[for (final point in points) frame.fromCanonical(point)],
    ];
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

    final between = _obstaclesBetween(
      obstacles,
      top: source.bottom,
      bottom: target.top,
      exclude: <Rect>[source, target],
    );

    if ((sx - tx).abs() < 0.5 && !_columnBlocked(sx, between)) {
      return <Offset>[start, end];
    }

    final gapBelow = bands.gapBelow(source.bottom, target.top);
    final gapAbove = bands.gapAbove(target.top, source.bottom);
    final y1 = math.min(gapBelow, gapAbove);
    final y2 = math.max(gapBelow, gapAbove);

    if (between.isEmpty || (y2 - y1).abs() < 0.5) {
      // 相邻层：一个 Z 形弯即可。
      final y = between.isEmpty ? (source.bottom + target.top) / 2 : y1;
      return _dedupe(<Offset>[start, Offset(sx, y), Offset(tx, y), end]);
    }

    final corridor = _pickCorridor(
      preferred: <double>[tx, sx, (sx + tx) / 2],
      obstacles: _obstaclesBetween(
        corridorObstacles,
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
  void _separateLanes(List<List<Offset>> routes) {
    _separateAxis(routes, horizontal: true);
    _separateAxis(routes, horizontal: false);
  }

  void _separateAxis(List<List<Offset>> routes, {required bool horizontal}) {
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
        final key = horizontal ? a.dy : a.dx;
        final lo = horizontal ? math.min(a.dx, b.dx) : math.min(a.dy, b.dy);
        final hi = horizontal ? math.max(a.dx, b.dx) : math.max(a.dy, b.dy);
        segments.add(_Segment(route: r, index: i, key: key, lo: lo, hi: hi));
      }
    }
    final groups = <int, List<_Segment>>{};
    for (final segment in segments) {
      (groups[segment.key.round()] ??= <_Segment>[]).add(segment);
    }
    for (final group in groups.values) {
      if (group.length < 2) {
        continue;
      }
      group.sort((a, b) => a.lo.compareTo(b.lo));
      final laneEnds = <double>[];
      final lanes = <int>[];
      for (final segment in group) {
        var lane = -1;
        for (var i = 0; i < laneEnds.length; i++) {
          if (segment.lo > laneEnds[i] + 1) {
            lane = i;
            break;
          }
        }
        if (lane < 0) {
          laneEnds.add(segment.hi);
          lane = laneEnds.length - 1;
        } else {
          laneEnds[lane] = segment.hi;
        }
        lanes.add(lane);
      }
      final laneCount = laneEnds.length;
      if (laneCount < 2) {
        continue;
      }
      for (var i = 0; i < group.length; i++) {
        final segment = group[i];
        final offset = (lanes[i] - (laneCount - 1) / 2) * laneGap;
        final points = routes[segment.route];
        for (final index in <int>[segment.index, segment.index + 1]) {
          final point = points[index];
          points[index] = horizontal
              ? Offset(point.dx, point.dy + offset)
              : Offset(point.dx + offset, point.dy);
        }
      }
    }
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
  });

  final int route;
  final int index;
  final double key;
  final double lo;
  final double hi;
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
