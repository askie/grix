import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';

import '../controllers/conversations_controller.dart';

typedef ConversationSliverItemBuilder =
    Widget Function(BuildContext context, ConversationListItem item);

class ConversationReorderSliverList extends StatefulWidget {
  const ConversationReorderSliverList({
    super.key,
    required this.sessions,
    required this.itemBuilder,
  });

  static const String _tileKeyPrefix = 'conversation_tile:';
  static const double _defaultTileHeight = 82;
  static const int _maxAnimatedItemsPerPass = 80;
  static const int _maxAnimatedMovesPerPass = 12;

  final List<ConversationListItem> sessions;
  final ConversationSliverItemBuilder itemBuilder;

  static ValueKey<String> tileKey(String groupKey) {
    return ValueKey<String>('$_tileKeyPrefix$groupKey');
  }

  static ValueKey<String> moveKey(String groupKey) {
    return ValueKey<String>('conversation_move:$groupKey');
  }

  static int? findIndexByKey(Key key, List<ConversationListItem> sessions) {
    if (key is! ValueKey<String>) {
      return null;
    }
    final value = key.value;
    if (!value.startsWith(_tileKeyPrefix)) {
      return null;
    }
    final groupKey = value.substring(_tileKeyPrefix.length);
    final index = sessions.indexWhere((item) => item.groupKey == groupKey);
    return index == -1 ? null : index;
  }

  @override
  State<ConversationReorderSliverList> createState() =>
      _ConversationReorderSliverListState();
}

class _ConversationReorderSliverListState
    extends State<ConversationReorderSliverList> {
  final Map<String, double> _tileHeights = <String, double>{};
  final Map<String, double> _moveOffsets = <String, double>{};
  final Map<String, int> _moveGenerations = <String, int>{};
  final Set<String> _anchoredTopKeys = <String>{};

  @override
  void didUpdateWidget(covariant ConversationReorderSliverList oldWidget) {
    super.didUpdateWidget(oldWidget);
    _prepareMoveAnimations(oldWidget.sessions, widget.sessions);
    _pruneStaleEntries(widget.sessions);
  }

  void _prepareMoveAnimations(
    List<ConversationListItem> previous,
    List<ConversationListItem> current,
  ) {
    if (previous.isEmpty || current.isEmpty) {
      return;
    }

    final trackedKeys = _buildTrackedKeys(previous, current);
    if (trackedKeys.isEmpty) {
      return;
    }

    final previousTops = _buildTopOffsets(previous, trackedKeys);
    final currentTops = _buildTopOffsets(current, trackedKeys);
    final previousIndexes = _buildIndexes(previous, trackedKeys);
    final currentIndexes = _buildIndexes(current, trackedKeys);
    final anchoredTopKeys = <String>{};
    final movedEntries = <MapEntry<String, double>>[];

    for (final item in current) {
      if (!trackedKeys.contains(item.groupKey)) {
        continue;
      }
      final previousTop = previousTops[item.groupKey];
      final currentTop = currentTops[item.groupKey];
      if (previousTop == null || currentTop == null) {
        continue;
      }
      final previousIndex = previousIndexes[item.groupKey];
      final currentIndex = currentIndexes[item.groupKey];
      if (previousIndex == 0 && currentIndex == 0) {
        anchoredTopKeys.add(item.groupKey);
        continue;
      }
      final delta = previousTop - currentTop;
      if (delta.abs() < 0.5) {
        continue;
      }
      movedEntries.add(MapEntry<String, double>(item.groupKey, delta));
    }

    movedEntries.sort((a, b) => b.value.abs().compareTo(a.value.abs()));
    final selectedKeys = movedEntries
        .take(ConversationReorderSliverList._maxAnimatedMovesPerPass)
        .map((entry) => entry.key)
        .toSet();

    _moveOffsets.removeWhere(
      (key, _) => trackedKeys.contains(key) && !selectedKeys.contains(key),
    );
    for (final entry in movedEntries) {
      if (!selectedKeys.contains(entry.key)) {
        continue;
      }
      _moveOffsets[entry.key] = entry.value;
      _moveGenerations.update(
        entry.key,
        (value) => value + 1,
        ifAbsent: () => 1,
      );
    }
    _anchoredTopKeys
      ..removeWhere((key) => trackedKeys.contains(key))
      ..addAll(anchoredTopKeys);
  }

  Map<String, int> _buildIndexes(
    List<ConversationListItem> items,
    Set<String> trackedKeys,
  ) {
    final indexes = <String, int>{};
    for (var index = 0; index < items.length; index++) {
      final item = items[index];
      if (!trackedKeys.contains(item.groupKey)) {
        continue;
      }
      indexes[item.groupKey] = index;
      if (indexes.length == trackedKeys.length) {
        break;
      }
    }
    return indexes;
  }

  Set<String> _buildTrackedKeys(
    List<ConversationListItem> previous,
    List<ConversationListItem> current,
  ) {
    final trackedKeys = <String>{};
    final previousLimit =
        previous.length < ConversationReorderSliverList._maxAnimatedItemsPerPass
        ? previous.length
        : ConversationReorderSliverList._maxAnimatedItemsPerPass;
    final currentLimit =
        current.length < ConversationReorderSliverList._maxAnimatedItemsPerPass
        ? current.length
        : ConversationReorderSliverList._maxAnimatedItemsPerPass;

    for (var index = 0; index < previousLimit; index++) {
      trackedKeys.add(previous[index].groupKey);
    }
    for (var index = 0; index < currentLimit; index++) {
      trackedKeys.add(current[index].groupKey);
    }
    return trackedKeys;
  }

  Map<String, double> _buildTopOffsets(
    List<ConversationListItem> items,
    Set<String> trackedKeys,
  ) {
    final offsets = <String, double>{};
    var top = 0.0;
    for (final item in items) {
      if (trackedKeys.contains(item.groupKey)) {
        offsets[item.groupKey] = top;
        if (offsets.length == trackedKeys.length) {
          break;
        }
      }
      top +=
          _tileHeights[item.groupKey] ??
          ConversationReorderSliverList._defaultTileHeight;
    }
    return offsets;
  }

  void _pruneStaleEntries(List<ConversationListItem> sessions) {
    final activeKeys = sessions.map((item) => item.groupKey).toSet();
    _tileHeights.removeWhere((key, _) => !activeKeys.contains(key));
    _moveOffsets.removeWhere((key, _) => !activeKeys.contains(key));
    _moveGenerations.removeWhere((key, _) => !activeKeys.contains(key));
    _anchoredTopKeys.removeWhere((key) => !activeKeys.contains(key));
  }

  void _handleTileSizeChanged(String groupKey, Size size) {
    final previousHeight = _tileHeights[groupKey];
    if (previousHeight != null && (previousHeight - size.height).abs() < 0.5) {
      return;
    }
    _tileHeights[groupKey] = size.height;
  }

  @override
  Widget build(BuildContext context) {
    final sessions = widget.sessions;
    return SliverList(
      delegate: SliverChildBuilderDelegate(
        (context, index) {
          final item = sessions[index];
          return _ConversationReorderTile(
            key: ConversationReorderSliverList.tileKey(item.groupKey),
            animationKey: ConversationReorderSliverList.moveKey(item.groupKey),
            moveOffset: _moveOffsets[item.groupKey] ?? 0,
            moveGeneration: _moveGenerations[item.groupKey] ?? 0,
            isAnchoredAtTop: _anchoredTopKeys.contains(item.groupKey),
            onSizeChanged: (size) =>
                _handleTileSizeChanged(item.groupKey, size),
            child: widget.itemBuilder(context, item),
          );
        },
        childCount: sessions.length,
        findChildIndexCallback: (key) =>
            ConversationReorderSliverList.findIndexByKey(key, sessions),
      ),
    );
  }
}

class _ConversationReorderTile extends StatefulWidget {
  const _ConversationReorderTile({
    super.key,
    required this.animationKey,
    required this.moveOffset,
    required this.moveGeneration,
    required this.isAnchoredAtTop,
    required this.onSizeChanged,
    required this.child,
  });

  final Key animationKey;
  final double moveOffset;
  final int moveGeneration;
  final bool isAnchoredAtTop;
  final ValueChanged<Size> onSizeChanged;
  final Widget child;

  @override
  State<_ConversationReorderTile> createState() =>
      _ConversationReorderTileState();
}

class _ConversationReorderTileState extends State<_ConversationReorderTile>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller;
  Animation<double> _moveAnimation = const AlwaysStoppedAnimation<double>(0);

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 260),
    );
  }

  @override
  void didUpdateWidget(covariant _ConversationReorderTile oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.isAnchoredAtTop) {
      _stopMoveAnimation();
      return;
    }
    if (widget.moveGeneration == oldWidget.moveGeneration) {
      return;
    }
    _startMoveAnimation(widget.moveOffset);
  }

  void _startMoveAnimation(double beginOffset) {
    final effectiveBeginOffset = beginOffset + _moveAnimation.value;
    if (effectiveBeginOffset.abs() < 0.5) {
      return;
    }
    _moveAnimation = Tween<double>(
      begin: effectiveBeginOffset,
      end: 0,
    ).animate(CurvedAnimation(parent: _controller, curve: Curves.easeOutCubic));
    _controller.forward(from: 0);
  }

  void _stopMoveAnimation() {
    if (_moveAnimation.value.abs() < 0.5 && !_controller.isAnimating) {
      return;
    }
    _controller.stop();
    _moveAnimation = const AlwaysStoppedAnimation<double>(0);
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return RepaintBoundary(
      child: _SizeReportingWidget(
        onSizeChanged: widget.onSizeChanged,
        child: AnimatedBuilder(
          animation: _controller,
          child: widget.child,
          builder: (context, child) {
            return Transform.translate(
              key: widget.animationKey,
              offset: Offset(0, _moveAnimation.value),
              child: child,
            );
          },
        ),
      ),
    );
  }
}

class _SizeReportingWidget extends SingleChildRenderObjectWidget {
  const _SizeReportingWidget({
    required this.onSizeChanged,
    required super.child,
  });

  final ValueChanged<Size> onSizeChanged;

  @override
  RenderObject createRenderObject(BuildContext context) {
    return _RenderSizeReporting(onSizeChanged);
  }

  @override
  void updateRenderObject(
    BuildContext context,
    covariant _RenderSizeReporting renderObject,
  ) {
    renderObject.onSizeChanged = onSizeChanged;
  }
}

class _RenderSizeReporting extends RenderProxyBox {
  _RenderSizeReporting(this.onSizeChanged);

  ValueChanged<Size> onSizeChanged;
  Size? _previousSize;

  @override
  void performLayout() {
    super.performLayout();
    final size = child?.size;
    if (size == null || size == _previousSize) {
      return;
    }
    _previousSize = size;
    WidgetsBinding.instance.addPostFrameCallback((_) => onSizeChanged(size));
  }
}
