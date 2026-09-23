import 'package:graphview/GraphView.dart';

/// 修正 graphview 1.5.1 Sugiyama 的层内排序。
///
/// 上游 `median` 把「上一层所有后继的位置」算成一个中位数后赋给本层每个节点，
/// 于是排序键全相等、排序等于没排，层内顺序退化成建图时的插入顺序；反向扫描
/// 也误用了上一层。结果是同一个父节点的多个子节点会散落到整层两端，连线横跨
/// 全图。这里按 Gansner 等人的加权中位数逐节点计算：向下扫描看前驱在上一层的
/// 位置，向上扫描看后继在下一层的位置，无邻居的节点保持原位。
class ChatMermaidSugiyamaAlgorithm extends SugiyamaAlgorithm {
  ChatMermaidSugiyamaAlgorithm(super.configuration);

  /// 上游的循环只要 transpose 没换过位就立刻退出，于是通常只跑一趟向下扫描，
  /// 顶层顺序永远得不到下层的反向修正。这里下行/上行交替扫到顺序稳定为止，
  /// 并按总交叉数保留最优的一趟，避免来回震荡后停在较差的状态。
  @override
  void nodeOrdering() {
    for (final edge in graph.edges) {
      nodeData[edge.source]?.successorNodes.add(edge.destination);
      nodeData[edge.destination]?.predecessorNodes.add(edge.source);
    }
    var best = _snapshot();
    var bestCrossings = _countCrossings();
    var stableRounds = 0;
    for (var i = 0; i < configuration.iterations && bestCrossings > 0; i++) {
      final before = _snapshot();
      median(layers, i);
      final transposed =
          configuration.crossMinimizationStrategy ==
              CrossMinimizationStrategy.simple
          ? transposeSimple(layers)
          : transposeAccumulator(layers);
      final crossings = _countCrossings();
      if (crossings < bestCrossings) {
        bestCrossings = crossings;
        best = _snapshot();
      }
      final changed = transposed || !_sameOrder(before, layers);
      stableRounds = changed ? 0 : stableRounds + 1;
      // 下行、上行各一趟都没动过，再扫也不会变。
      if (stableRounds >= 2) {
        break;
      }
    }
    for (var l = 0; l < layers.length; l++) {
      layers[l] = best[l];
      for (var pos = 0; pos < layers[l].length; pos++) {
        nodeData[layers[l][pos]]?.position = pos;
      }
    }
  }

  List<List<Node>> _snapshot() => <List<Node>>[
    for (final layer in layers) List<Node>.of(layer),
  ];

  bool _sameOrder(List<List<Node>> a, List<List<Node>> b) {
    for (var l = 0; l < a.length; l++) {
      for (var i = 0; i < a[l].length; i++) {
        if (!identical(a[l][i], b[l][i])) {
          return false;
        }
      }
    }
    return true;
  }

  /// 相邻两层之间的交叉边对总数。
  int _countCrossings() {
    var total = 0;
    for (var l = 0; l + 1 < layers.length; l++) {
      final lowerIndex = <Node, int>{
        for (var i = 0; i < layers[l + 1].length; i++) layers[l + 1][i]: i,
      };
      final pairs = <(int, int)>[];
      for (var i = 0; i < layers[l].length; i++) {
        for (final successor in successorsOf(layers[l][i])) {
          final j = lowerIndex[successor];
          if (j != null) {
            pairs.add((i, j));
          }
        }
      }
      for (var a = 0; a < pairs.length; a++) {
        for (var b = a + 1; b < pairs.length; b++) {
          final du = pairs[a].$1 - pairs[b].$1;
          final dv = pairs[a].$2 - pairs[b].$2;
          if (du * dv < 0) {
            total++;
          }
        }
      }
    }
    return total;
  }

  @override
  void median(List<List<Node?>> layers, int currentIteration) {
    final downward = currentIteration.isEven;
    if (downward) {
      for (var i = 1; i < layers.length; i++) {
        _orderLayer(layers[i], layers[i - 1], predecessorsOf);
      }
    } else {
      for (var i = layers.length - 2; i >= 0; i--) {
        _orderLayer(layers[i], layers[i + 1], successorsOf);
      }
    }
  }

  void _orderLayer(
    List<Node?> layer,
    List<Node?> adjacentLayer,
    List<Node> Function(Node? node) neighborsOf,
  ) {
    final adjacentIndex = <Node, int>{
      for (var i = 0; i < adjacentLayer.length; i++)
        if (adjacentLayer[i] != null) adjacentLayer[i]!: i,
    };
    final movable = <(int, double, Node?)>[];
    final fixedSlots = <int>{};
    for (var i = 0; i < layer.length; i++) {
      final node = layer[i];
      final positions = <int>[
        for (final neighbor in neighborsOf(node))
          if (adjacentIndex.containsKey(neighbor)) adjacentIndex[neighbor]!,
      ]..sort();
      if (positions.isEmpty) {
        fixedSlots.add(i);
        continue;
      }
      movable.add((i, _weightedMedian(positions), node));
    }
    if (movable.length < 2) {
      return;
    }
    // 稳定排序：中位数相同保持原相对顺序。
    movable.sort((a, b) {
      final byMedian = a.$2.compareTo(b.$2);
      return byMedian != 0 ? byMedian : a.$1.compareTo(b.$1);
    });
    var next = 0;
    for (var i = 0; i < layer.length; i++) {
      if (fixedSlots.contains(i)) {
        continue;
      }
      layer[i] = movable[next++].$3;
    }
  }

  static double _weightedMedian(List<int> positions) {
    final count = positions.length;
    final mid = count ~/ 2;
    if (count == 1) {
      return positions[0].toDouble();
    }
    if (count == 2) {
      return (positions[0] + positions[1]) / 2;
    }
    if (count.isOdd) {
      return positions[mid].toDouble();
    }
    final left = positions[mid - 1] - positions[0];
    final right = positions[count - 1] - positions[mid];
    if (left + right == 0) {
      return (positions[mid - 1] + positions[mid]) / 2;
    }
    return (positions[mid - 1] * right + positions[mid] * left) /
        (left + right);
  }
}
