import 'package:flutter/foundation.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/widgets.dart';

import '../services/chat_scroll_controller.dart';

/// A lazy history window with a layout origin at a retained reading row.
/// Older rows grow before that origin, newer rows after it. Neither side's
/// estimated total extent is used to position the reader after pagination.
class ChatHistoryList extends StatefulWidget {
  const ChatHistoryList({
    super.key,
    required this.controller,
    required this.itemKeys,
    required this.itemKey,
    required this.delegate,
    required this.padding,
    required this.cacheExtent,
    required this.onRebase,
    this.initiallyAtBottom = false,
  });

  final ChatScrollController controller;
  final List<String> itemKeys;
  final GlobalKey? Function(String) itemKey;
  final SliverChildBuilderDelegate delegate;
  final EdgeInsets padding;
  final double cacheExtent;
  final VoidCallback onRebase;
  final bool initiallyAtBottom;

  @override
  State<ChatHistoryList> createState() => _ChatHistoryListState();
}

class _ChatHistoryListState extends State<ChatHistoryList> {
  final _viewportKey = GlobalKey();
  Key _centerKey = UniqueKey();
  int _split = 0;
  double _anchor = 0;
  double? _pendingPixels;
  double _capturedPixels = 0;
  String? _readingKey;
  double? _readingLeading;
  bool _initialLayoutChecked = false;

  @override
  void initState() {
    super.initState();
    if (widget.initiallyAtBottom && widget.itemKeys.isNotEmpty) {
      // Start at the tail without a speculative jump through all preceding
      // long bubbles. Only the viewport and cache are built on the first frame.
      _split = widget.itemKeys.length - 1;
      _anchor = 1;
    }
    widget.controller.anchorsHistoryInLayout = true;
    widget.controller.prepareForWindowChange = _captureReadingRow;
    widget.controller.readingRange = _visibleReadingRange;
  }

  (String, String)? _visibleReadingRange() {
    final viewport = _viewportKey.currentContext?.findRenderObject();
    if (viewport is! RenderBox || !widget.controller.hasClients) return null;
    String? first, last;
    for (final key in widget.itemKeys) {
      final box = widget.itemKey(key)?.currentContext?.findRenderObject();
      if (box is! RenderBox || !box.attached || !box.hasSize) continue;
      final top =
          box.localToGlobal(Offset.zero, ancestor: viewport).dy -
          widget.controller.unlaidScrollDelta;
      if (top + box.size.height <= 0 || top >= viewport.size.height) continue;
      first ??= key;
      last = key;
    }
    return first == null ? null : (first, last!);
  }

  void _captureReadingRow(Set<String> retained) {
    _readingKey = null;
    _readingLeading = null;
    final viewport = _viewportKey.currentContext?.findRenderObject();
    if (viewport is! RenderBox || !widget.controller.hasClients) return;
    final unlaidDelta = widget.controller.unlaidScrollDelta;
    // Grow backward from the trailing visible row: layout starts in the
    // reading viewport without measuring the inserted block.
    for (final key in widget.itemKeys) {
      if (!retained.contains(key)) continue;
      final box = widget.itemKey(key)?.currentContext?.findRenderObject();
      if (box is! RenderBox || !box.attached || !box.hasSize) continue;
      final top =
          box.localToGlobal(Offset.zero, ancestor: viewport).dy - unlaidDelta;
      if (top + box.size.height <= 0 || top >= viewport.size.height) continue;
      if (_readingLeading == null || top > _readingLeading!) {
        _readingKey = key;
        _readingLeading = top;
      }
    }
    _capturedPixels = widget.controller.position.pixels;
  }

  @override
  void didUpdateWidget(ChatHistoryList oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (listEquals(widget.itemKeys, oldWidget.itemKeys)) return;
    final commonLength = widget.itemKeys.length < oldWidget.itemKeys.length
        ? widget.itemKeys.length
        : oldWidget.itemKeys.length;
    if (commonLength > 1 &&
        _split < widget.itemKeys.length - 1 &&
        listEquals(
          widget.itemKeys.take(commonLength - 1).toList(),
          oldWidget.itemKeys.take(commonLength - 1).toList(),
        )) {
      // Appends and tail-only trims do not move rows before the origin.
      // Keep their coordinates and the existing lazy children intact.
      return;
    }
    final anchor = _readingKey;
    if (anchor != null && widget.itemKeys.contains(anchor)) {
      _split = widget.itemKeys.indexOf(anchor);
      final viewport =
          _viewportKey.currentContext!.findRenderObject() as RenderBox;
      _pendingPixels = viewport.size.height * _anchor - _readingLeading!;
      widget.onRebase();
    } else {
      // A full window reset/jump has no retained reading row.
      _pendingPixels = null;
      _split = widget.initiallyAtBottom ? widget.itemKeys.length - 1 : 0;
      _anchor = widget.initiallyAtBottom ? 1 : 0;
      _initialLayoutChecked = false;
    }
    _readingKey = null;
    _readingLeading = null;
    // Old child layout offsets belong to the old origin. Recreate just the
    // two slivers; message GlobalKeys can still retain the bubble states.
    _centerKey = UniqueKey();
  }

  void _beforeLayout() {
    if (_pendingPixels case final pixels?) {
      if (widget.controller.hasClients) {
        final position = widget.controller.position;
        widget.controller.rebaseViewportTo(
          pixels + position.pixels - _capturedPixels,
        );
      }
      _pendingPixels = null;
    }
  }

  void _afterLayout() {
    widget.controller.recordLayoutPixels();
    if (_initialLayoutChecked) return;
    final position = widget.controller.position;
    if (position.outOfRange) return;
    _initialLayoutChecked = true;
    if (!widget.initiallyAtBottom ||
        widget.controller.historyContentFitsViewport != true) {
      return;
    }
    // Preserve the existing top alignment of conversations shorter than a
    // screen. Tail-first layout is only needed for scrollable first windows.
    final layoutOrigin = _centerKey;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted ||
          layoutOrigin != _centerKey ||
          widget.controller.historyContentFitsViewport != true ||
          !widget.controller.hasClients ||
          widget.controller.position.userScrollDirection !=
              ScrollDirection.idle) {
        return;
      }
      setState(() {
        _anchor = 0;
        _split = 0;
        _centerKey = UniqueKey();
        _capturedPixels = widget.controller.position.pixels;
        _pendingPixels = 0;
      });
    });
  }

  @override
  void dispose() {
    widget.controller.anchorsHistoryInLayout = false;
    widget.controller.prepareForWindowChange = null;
    widget.controller.readingRange = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return _HistoryLayoutMarker(
      key: _viewportKey,
      beforeLayout: _beforeLayout,
      afterLayout: _afterLayout,
      child: _CenteredHistoryListView(
        controller: widget.controller,
        delegate: widget.delegate,
        split: _split,
        centerKey: _centerKey,
        viewportAnchor: _anchor,
        onLayout: _afterLayout,
        padding: widget.padding,
        cacheExtent: widget.cacheExtent,
      ),
    );
  }
}

class _CenteredHistoryListView extends ListView {
  const _CenteredHistoryListView({
    required ChatScrollController super.controller,
    required SliverChildBuilderDelegate delegate,
    required this.split,
    required this.centerKey,
    required this.viewportAnchor,
    required this.onLayout,
    required EdgeInsets super.padding,
    required double super.cacheExtent,
  }) : super.custom(
         primary: false,
         keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
         childrenDelegate: delegate,
       );

  final int split;
  final Key centerKey;
  final double viewportAnchor;
  final VoidCallback onLayout;

  @override
  Key get center => centerKey;

  @override
  Widget buildViewport(
    BuildContext context,
    ViewportOffset offset,
    AxisDirection axisDirection,
    List<Widget> slivers,
  ) {
    return _HistoryViewport(
      axisDirection: axisDirection,
      offset: offset,
      center: centerKey,
      anchor: viewportAnchor,
      cacheExtent: cacheExtent,
      slivers: slivers,
      afterLayout: onLayout,
      scrollController: controller! as ChatScrollController,
    );
  }

  SliverChildBuilderDelegate _delegate(bool before) {
    final source = childrenDelegate as SliverChildBuilderDelegate;
    final count = source.childCount!;
    int sourceIndex(int index) => before ? split - 1 - index : split + index;
    return SliverChildBuilderDelegate(
      (context, index) => source.builder(context, sourceIndex(index)),
      childCount: before ? split : count - split,
      findChildIndexCallback: (key) {
        final index = source.findChildIndexCallback?.call(key);
        if (index == null || (before ? index >= split : index < split)) {
          return null;
        }
        return before ? split - 1 - index : index - split;
      },
      semanticIndexCallback: (widget, index) => sourceIndex(index),
    );
  }

  @override
  List<Widget> buildSlivers(BuildContext context) {
    final insets = padding! as EdgeInsets;
    return [
      SliverPadding(
        key: ValueKey(centerKey),
        padding: insets.copyWith(top: split == 0 ? 0 : insets.top, bottom: 0),
        sliver: SliverList(delegate: _delegate(true)),
      ),
      SliverPadding(
        key: centerKey,
        padding: insets.copyWith(top: split == 0 ? insets.top : 0),
        sliver: SliverList(delegate: _delegate(false)),
      ),
    ];
  }
}

class _HistoryViewport extends Viewport {
  _HistoryViewport({
    required super.axisDirection,
    required super.offset,
    required super.center,
    required super.anchor,
    required super.cacheExtent,
    required super.slivers,
    required this.afterLayout,
    required this.scrollController,
  });
  final VoidCallback afterLayout;
  final ChatScrollController scrollController;

  @override
  RenderViewport createRenderObject(BuildContext context) =>
      _HistoryRenderViewport(
        axisDirection: axisDirection,
        crossAxisDirection: Viewport.getDefaultCrossAxisDirection(
          context,
          axisDirection,
        ),
        offset: offset,
        anchor: anchor,
        cacheExtent: cacheExtent,
        afterLayout: afterLayout,
        scrollController: scrollController,
      );

  @override
  void updateRenderObject(
    BuildContext context,
    _HistoryRenderViewport viewport,
  ) {
    super.updateRenderObject(context, viewport);
    viewport.afterLayout = afterLayout;
  }
}

class _HistoryRenderViewport extends RenderViewport {
  _HistoryRenderViewport({
    required super.axisDirection,
    required super.crossAxisDirection,
    required super.offset,
    required super.anchor,
    required super.cacheExtent,
    required this.afterLayout,
    required this.scrollController,
  });
  VoidCallback afterLayout;
  final ChatScrollController scrollController;

  @override
  void performLayout() {
    super.performLayout();
    var extent = 0.0;
    var sliver = firstChild;
    while (sliver != null) {
      extent += sliver.geometry?.scrollExtent ?? 0;
      sliver = childAfter(sliver);
    }
    scrollController.historyContentFitsViewport = extent <= size.height + 0.5;
    afterLayout();
  }
}

class _HistoryLayoutMarker extends SingleChildRenderObjectWidget {
  const _HistoryLayoutMarker({
    super.key,
    required this.beforeLayout,
    required this.afterLayout,
    required super.child,
  });
  final VoidCallback beforeLayout;
  final VoidCallback afterLayout;

  @override
  RenderObject createRenderObject(BuildContext context) =>
      _HistoryLayoutRenderBox(beforeLayout, afterLayout);

  @override
  void updateRenderObject(BuildContext context, _HistoryLayoutRenderBox box) {
    box.beforeLayout = beforeLayout;
    box.afterLayout = afterLayout;
    box.markNeedsLayout();
  }
}

class _HistoryLayoutRenderBox extends RenderProxyBox {
  _HistoryLayoutRenderBox(this.beforeLayout, this.afterLayout);
  VoidCallback beforeLayout;
  VoidCallback afterLayout;

  @override
  void performLayout() {
    beforeLayout();
    super.performLayout();
    afterLayout();
  }
}
