import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';

class ChatSelectionArea extends StatefulWidget {
  const ChatSelectionArea({
    super.key,
    required this.child,
    this.enabled = true,
    this.onSelectionCleared,
  });

  final Widget child;
  final bool enabled;
  final VoidCallback? onSelectionCleared;

  @override
  State<ChatSelectionArea> createState() => _ChatSelectionAreaState();
}

class _ChatSelectionAreaState extends State<ChatSelectionArea> {
  static const Duration _toolbarRestoreDelay = Duration(milliseconds: 120);
  static const double _desktopSelectionHitSlop = 1;

  final GlobalKey<SelectionAreaState> _selectionAreaKey =
      GlobalKey<SelectionAreaState>();
  final GlobalKey _selectionContentKey = GlobalKey();
  final SelectionListenerNotifier _selectionListenerNotifier =
      SelectionListenerNotifier();

  Timer? _toolbarRestoreTimer;
  OverlayEntry? _toolbarOverlayEntry;
  SelectionStatus _selectionStatus = SelectionStatus.none;
  SelectableRegionSelectionStatus _regionSelectionStatus =
      SelectableRegionSelectionStatus.finalized;
  bool _frameworkToolbarVisible = false;
  bool _selectionRectsRefreshScheduled = false;
  List<Rect> _desktopSelectionHitRects = const <Rect>[];
  bool _isInTree = true;
  bool _toolbarDismissedByButtonPress = false;

  bool get _shouldRestoreToolbar =>
      !kIsWeb &&
      (defaultTargetPlatform == TargetPlatform.android ||
          defaultTargetPlatform == TargetPlatform.iOS ||
          defaultTargetPlatform == TargetPlatform.fuchsia);

  bool get _shouldPreserveDesktopSecondaryTap =>
      !kIsWeb && defaultTargetPlatform == TargetPlatform.macOS;

  @override
  void activate() {
    super.activate();
    _isInTree = true;
  }

  @override
  void deactivate() {
    _isInTree = false;
    _toolbarRestoreTimer?.cancel();
    _removeToolbarOverlay();
    super.deactivate();
  }

  @override
  void dispose() {
    _toolbarRestoreTimer?.cancel();
    _removeToolbarOverlay();
    _selectionListenerNotifier.dispose();
    super.dispose();
  }

  bool _hasEverUncollapsed = false;

  void _handleSelectionStatusChanged(SelectionStatus status) {
    _selectionStatus = status;
    if (status == SelectionStatus.uncollapsed) {
      _hasEverUncollapsed = true;
      _toolbarDismissedByButtonPress = false;
    }
    // Only notify selection cleared if we've previously seen an uncollapsed
    // selection. This prevents the initial 'none' status from immediately
    // deactivating the selection mode.
    if (status == SelectionStatus.none &&
        _hasEverUncollapsed &&
        widget.onSelectionCleared != null) {
      _hasEverUncollapsed = false;
      widget.onSelectionCleared!();
    }
    if (status != SelectionStatus.uncollapsed) {
      _removeToolbarOverlay();
    }
    _scheduleToolbarRestoreIfNeeded();
    _scheduleDesktopSelectionRectsRefresh();
  }

  void _handleRegionSelectionStatusChanged(
    SelectableRegionSelectionStatus status,
  ) {
    _regionSelectionStatus = status;
    if (status != SelectableRegionSelectionStatus.finalized) {
      _removeToolbarOverlay();
    }
    _scheduleToolbarRestoreIfNeeded();
    _scheduleDesktopSelectionRectsRefresh();
  }

  void _handleFrameworkToolbarVisibilityChanged(bool visible) {
    _frameworkToolbarVisible = visible;
    if (visible) {
      _removeToolbarOverlay();
      return;
    }
    _scheduleToolbarRestoreIfNeeded();
  }

  void _scheduleToolbarRestoreIfNeeded() {
    _toolbarRestoreTimer?.cancel();
    if (!_isInTree ||
        !_shouldRestoreToolbar ||
        _frameworkToolbarVisible ||
        _toolbarOverlayEntry != null ||
        _toolbarDismissedByButtonPress ||
        _selectionStatus != SelectionStatus.uncollapsed ||
        _regionSelectionStatus != SelectableRegionSelectionStatus.finalized) {
      return;
    }
    _toolbarRestoreTimer = Timer(_toolbarRestoreDelay, _showToolbarOverlay);
  }

  void _scheduleDesktopSelectionRectsRefresh() {
    if (!_isInTree || !_shouldPreserveDesktopSecondaryTap) {
      return;
    }
    if (_selectionStatus != SelectionStatus.uncollapsed ||
        _regionSelectionStatus != SelectableRegionSelectionStatus.finalized) {
      _setDesktopSelectionHitRects(const <Rect>[]);
      return;
    }
    if (_selectionRectsRefreshScheduled) {
      return;
    }
    _selectionRectsRefreshScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _selectionRectsRefreshScheduled = false;
      if (!mounted) {
        return;
      }
      _refreshDesktopSelectionHitRects();
    });
  }

  void _refreshDesktopSelectionHitRects() {
    final contentRenderObject =
        _selectionContentKey.currentContext?.findRenderObject();
    if (contentRenderObject == null) {
      _setDesktopSelectionHitRects(const <Rect>[]);
      return;
    }

    final rects = <Rect>[];
    _collectDesktopSelectionHitRects(contentRenderObject, rects);
    _setDesktopSelectionHitRects(rects);
  }

  void _collectDesktopSelectionHitRects(
    RenderObject renderObject,
    List<Rect> rects,
  ) {
    if (renderObject is Selectable) {
      final selectable = renderObject as Selectable;
      final geometry = selectable.value;
      if (geometry.status == SelectionStatus.uncollapsed) {
        final transform = selectable.getTransformTo(null);
        for (final rect in geometry.selectionRects) {
          if (rect.isEmpty) {
            continue;
          }
          rects.add(
            MatrixUtils.transformRect(transform, rect).inflate(
              _desktopSelectionHitSlop,
            ),
          );
        }
      }
    }

    renderObject.visitChildren((child) {
      _collectDesktopSelectionHitRects(child, rects);
    });
  }

  void _setDesktopSelectionHitRects(List<Rect> nextRects) {
    if (listEquals(_desktopSelectionHitRects, nextRects)) {
      return;
    }
    setState(() {
      _desktopSelectionHitRects = List<Rect>.unmodifiable(nextRects);
    });
  }

  bool _shouldHandleDesktopSecondaryTap(Offset globalPosition) {
    if (!_shouldPreserveDesktopSecondaryTap ||
        _selectionStatus != SelectionStatus.uncollapsed ||
        _regionSelectionStatus != SelectableRegionSelectionStatus.finalized ||
        _desktopSelectionHitRects.isEmpty) {
      return false;
    }
    for (final rect in _desktopSelectionHitRects) {
      if (rect.contains(globalPosition)) {
        return true;
      }
    }
    return false;
  }

  void _handleDesktopSecondaryTapDown(TapDownDetails details) {
    if (!_shouldHandleDesktopSecondaryTap(details.globalPosition)) {
      return;
    }

    final selectionAreaState = _selectionAreaKey.currentState;
    if (selectionAreaState == null) {
      return;
    }
    final SelectableRegionState selectableRegionState;
    try {
      selectableRegionState = selectionAreaState.selectableRegion;
    } catch (_) {
      return;
    }
    final buttonItems = selectableRegionState.contextMenuButtonItems
        .map(
          (item) => item.copyWith(
            onPressed: () {
              _removeToolbarOverlay();
              item.onPressed?.call();
            },
          ),
        )
        .toList(growable: false);
    if (buttonItems.isEmpty) {
      return;
    }

    _toolbarRestoreTimer?.cancel();
    _removeToolbarOverlay();
    _showToolbarOverlayEntry(
      anchors: TextSelectionToolbarAnchors(
        primaryAnchor: details.globalPosition,
      ),
      buttonItems: buttonItems,
    );
  }

  void _showToolbarOverlay() {
    if (!mounted ||
        !_shouldRestoreToolbar ||
        _frameworkToolbarVisible ||
        _toolbarOverlayEntry != null ||
        _selectionStatus != SelectionStatus.uncollapsed ||
        _regionSelectionStatus != SelectableRegionSelectionStatus.finalized) {
      return;
    }

    final selectionAreaState = _selectionAreaKey.currentState;
    if (selectionAreaState == null) {
      return;
    }
    final SelectableRegionState selectableRegionState;
    try {
      selectableRegionState = selectionAreaState.selectableRegion;
    } catch (_) {
      return;
    }
    final buttonItems = selectableRegionState.contextMenuButtonItems
        .map(
          (item) => item.copyWith(
            onPressed: () {
              _removeToolbarOverlay();
              item.onPressed?.call();
            },
          ),
        )
        .toList(growable: false);
    if (buttonItems.isEmpty) {
      return;
    }

    _showToolbarOverlayEntry(
      anchors: selectableRegionState.contextMenuAnchors,
      buttonItems: buttonItems,
    );
  }

  void _showToolbarOverlayEntry({
    required TextSelectionToolbarAnchors anchors,
    required List<ContextMenuButtonItem> buttonItems,
  }) {
    if (_toolbarOverlayEntry != null) {
      return;
    }

    final overlay = Overlay.maybeOf(context, rootOverlay: true);
    if (overlay == null) {
      return;
    }

    _toolbarOverlayEntry = OverlayEntry(
      builder: (context) => _ChatSelectionToolbarOverlay(
        anchors: anchors,
        buttonItems: buttonItems,
        onDismiss: _removeToolbarOverlay,
      ),
    );
    overlay.insert(_toolbarOverlayEntry!);
  }

  void _removeToolbarOverlay() {
    _toolbarOverlayEntry?.remove();
    _toolbarOverlayEntry?.dispose();
    _toolbarOverlayEntry = null;
  }

  Widget _buildContextMenu(
    BuildContext context,
    SelectableRegionState selectableRegionState,
  ) {
    final buttonItems = selectableRegionState.contextMenuButtonItems
        .map(
          (item) => item.copyWith(
            onPressed: () {
              _toolbarDismissedByButtonPress = true;
              _toolbarRestoreTimer?.cancel();
              item.onPressed?.call();
            },
          ),
        )
        .toList(growable: false);
    return _FrameworkContextMenuVisibility(
      onVisibilityChanged: _handleFrameworkToolbarVisibilityChanged,
      child: AdaptiveTextSelectionToolbar.buttonItems(
        anchors: selectableRegionState.contextMenuAnchors,
        buttonItems: buttonItems,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.enabled) {
      return widget.child;
    }

    final selectionArea = SelectionArea(
      key: _selectionAreaKey,
      contextMenuBuilder: _buildContextMenu,
      child: _ChatSelectionObserver(
        selectionNotifier: _selectionListenerNotifier,
        onSelectionStatusChanged: _handleSelectionStatusChanged,
        onRegionSelectionStatusChanged: _handleRegionSelectionStatusChanged,
        child: KeyedSubtree(key: _selectionContentKey, child: widget.child),
      ),
    );
    if (!_shouldPreserveDesktopSecondaryTap) {
      return selectionArea;
    }
    return RawGestureDetector(
      behavior: HitTestBehavior.translucent,
      gestures: {
        _DesktopSelectionSecondaryTapGestureRecognizer:
            GestureRecognizerFactoryWithHandlers<
              _DesktopSelectionSecondaryTapGestureRecognizer
            >(
              () => _DesktopSelectionSecondaryTapGestureRecognizer(),
              (recognizer) {
                recognizer
                  ..shouldHandleTap = _shouldHandleDesktopSecondaryTap
                  ..onSecondaryTapDown = _handleDesktopSecondaryTapDown;
              },
            ),
      },
      child: selectionArea,
    );
  }
}

class _ChatSelectionObserver extends StatefulWidget {
  const _ChatSelectionObserver({
    required this.selectionNotifier,
    required this.onSelectionStatusChanged,
    required this.onRegionSelectionStatusChanged,
    required this.child,
  });

  final SelectionListenerNotifier selectionNotifier;
  final ValueChanged<SelectionStatus> onSelectionStatusChanged;
  final ValueChanged<SelectableRegionSelectionStatus>
      onRegionSelectionStatusChanged;
  final Widget child;

  @override
  State<_ChatSelectionObserver> createState() => _ChatSelectionObserverState();
}

class _ChatSelectionObserverState extends State<_ChatSelectionObserver> {
  ValueListenable<SelectableRegionSelectionStatus>? _regionStatusNotifier;

  @override
  void initState() {
    super.initState();
    widget.selectionNotifier.addListener(_handleSelectionChange);
  }

  @override
  void didUpdateWidget(covariant _ChatSelectionObserver oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.selectionNotifier != widget.selectionNotifier) {
      oldWidget.selectionNotifier.removeListener(_handleSelectionChange);
      widget.selectionNotifier.addListener(_handleSelectionChange);
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final nextNotifier = SelectableRegionSelectionStatusScope.maybeOf(context);
    if (nextNotifier == _regionStatusNotifier) {
      return;
    }
    _regionStatusNotifier?.removeListener(_handleRegionSelectionStatusChange);
    _regionStatusNotifier = nextNotifier;
    _regionStatusNotifier?.addListener(_handleRegionSelectionStatusChange);
    if (_regionStatusNotifier != null) {
      _handleRegionSelectionStatusChange();
    }
  }

  @override
  void dispose() {
    widget.selectionNotifier.removeListener(_handleSelectionChange);
    _regionStatusNotifier?.removeListener(_handleRegionSelectionStatusChange);
    super.dispose();
  }

  void _handleSelectionChange() {
    if (!widget.selectionNotifier.registered) {
      return;
    }
    widget.onSelectionStatusChanged(widget.selectionNotifier.selection.status);
  }

  void _handleRegionSelectionStatusChange() {
    final notifier = _regionStatusNotifier;
    if (notifier == null) {
      return;
    }
    widget.onRegionSelectionStatusChanged(notifier.value);
  }

  @override
  Widget build(BuildContext context) {
    return SelectionListener(
      selectionNotifier: widget.selectionNotifier,
      child: widget.child,
    );
  }
}

class _FrameworkContextMenuVisibility extends StatefulWidget {
  const _FrameworkContextMenuVisibility({
    required this.onVisibilityChanged,
    required this.child,
  });

  final ValueChanged<bool> onVisibilityChanged;
  final Widget child;

  @override
  State<_FrameworkContextMenuVisibility> createState() =>
      _FrameworkContextMenuVisibilityState();
}

class _FrameworkContextMenuVisibilityState
    extends State<_FrameworkContextMenuVisibility> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) {
        widget.onVisibilityChanged(true);
      }
    });
  }

  @override
  void dispose() {
    widget.onVisibilityChanged(false);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

class _ChatSelectionToolbarOverlay extends StatelessWidget {
  const _ChatSelectionToolbarOverlay({
    required this.anchors,
    required this.buttonItems,
    required this.onDismiss,
  });

  final TextSelectionToolbarAnchors anchors;
  final List<ContextMenuButtonItem> buttonItems;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    return Material(
      type: MaterialType.transparency,
      child: Stack(
        children: [
          Positioned.fill(
            child: GestureDetector(
              behavior: HitTestBehavior.translucent,
              onTap: onDismiss,
            ),
          ),
          AdaptiveTextSelectionToolbar.buttonItems(
            anchors: anchors,
            buttonItems: buttonItems,
          ),
        ],
      ),
    );
  }
}

class _DesktopSelectionSecondaryTapGestureRecognizer
    extends OneSequenceGestureRecognizer {
  ValueChanged<TapDownDetails>? onSecondaryTapDown;
  bool Function(Offset globalPosition)? shouldHandleTap;

  int? _trackedPointer;

  @override
  void addAllowedPointer(PointerDownEvent event) {
    if (event.buttons != kSecondaryMouseButton ||
        !(shouldHandleTap?.call(event.position) ?? false)) {
      return;
    }

    _trackedPointer = event.pointer;
    startTrackingPointer(event.pointer);
    resolve(GestureDisposition.accepted);
    onSecondaryTapDown?.call(
      TapDownDetails(
        globalPosition: event.position,
        localPosition: event.localPosition,
        kind: event.kind,
      ),
    );
  }

  @override
  void handleEvent(PointerEvent event) {
    if (_trackedPointer != event.pointer) {
      return;
    }
    if (event is PointerUpEvent || event is PointerCancelEvent) {
      stopTrackingPointer(event.pointer);
    }
  }

  @override
  void didStopTrackingLastPointer(int pointer) {
    if (_trackedPointer == pointer) {
      _trackedPointer = null;
    }
  }

  @override
  String get debugDescription => 'chatSelectionSecondaryTap';
}
