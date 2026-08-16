import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:get/get.dart';

class ChatMarkdownMermaidZoomController extends ChangeNotifier {
  double _currentScale = 1;
  double _minScale = 1;
  double _maxScale = 1;
  VoidCallback? _zoomInAction;
  VoidCallback? _zoomOutAction;
  bool _isDisposed = false;

  bool get isBound => _zoomInAction != null && _zoomOutAction != null;

  bool get canZoomIn => isBound && _currentScale < (_maxScale - 0.001);

  bool get canZoomOut => isBound && _currentScale > (_minScale + 0.001);

  double get currentScale => _currentScale;

  void zoomIn() => _zoomInAction?.call();

  void zoomOut() => _zoomOutAction?.call();

  void bind({
    required double currentScale,
    required double minScale,
    required double maxScale,
    required VoidCallback onZoomIn,
    required VoidCallback onZoomOut,
  }) {
    final changed =
        !isBound ||
        (_currentScale - currentScale).abs() >= 0.001 ||
        (_minScale - minScale).abs() >= 0.001 ||
        (_maxScale - maxScale).abs() >= 0.001;
    _currentScale = currentScale;
    _minScale = minScale;
    _maxScale = maxScale;
    _zoomInAction = onZoomIn;
    _zoomOutAction = onZoomOut;
    if (changed) {
      _notifySafely();
    }
  }

  void unbind() {
    if (!isBound) {
      return;
    }
    _zoomInAction = null;
    _zoomOutAction = null;
    _notifySafely();
  }

  @override
  void dispose() {
    _isDisposed = true;
    super.dispose();
  }

  void _notifySafely() {
    if (_isDisposed) {
      return;
    }
    final scheduler = WidgetsBinding.instance.schedulerPhase;
    if (scheduler == SchedulerPhase.idle ||
        scheduler == SchedulerPhase.postFrameCallbacks) {
      notifyListeners();
      return;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_isDisposed) {
        return;
      }
      notifyListeners();
    });
  }
}

class ChatMarkdownMermaidZoomControls extends StatelessWidget {
  const ChatMarkdownMermaidZoomControls({
    super.key,
    required this.controller,
    required this.fillColor,
    required this.borderColor,
    required this.iconColor,
    this.zoomInTooltip,
    this.zoomOutTooltip,
  });

  static const double _buttonExtent = 24;
  static const double _iconSize = 14;

  final ChatMarkdownMermaidZoomController controller;
  final Color fillColor;
  final Color borderColor;
  final Color iconColor;
  final String? zoomInTooltip;
  final String? zoomOutTooltip;

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: controller,
      builder: (context, _) {
        final allowZoomIn = !controller.isBound || controller.canZoomIn;
        final allowZoomOut = !controller.isBound || controller.canZoomOut;

        return DecoratedBox(
          decoration: BoxDecoration(
            color: fillColor.withValues(alpha: 0.4),
            borderRadius: BorderRadius.circular(8),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              IconButton(
                onPressed: allowZoomIn ? controller.zoomIn : null,
                tooltip: zoomInTooltip ?? 'chat_mermaid_zoom_in'.tr,
                icon: const Icon(Icons.add_rounded),
                iconSize: _iconSize,
                color: iconColor,
                padding: EdgeInsets.zero,
                visualDensity: VisualDensity.compact,
                constraints: const BoxConstraints.tightFor(
                  width: _buttonExtent,
                  height: _buttonExtent,
                ),
                splashRadius: 12,
              ),
              IconButton(
                onPressed: allowZoomOut ? controller.zoomOut : null,
                tooltip: zoomOutTooltip ?? 'chat_mermaid_zoom_out'.tr,
                icon: const Icon(Icons.remove_rounded),
                iconSize: _iconSize,
                color: iconColor,
                padding: EdgeInsets.zero,
                visualDensity: VisualDensity.compact,
                constraints: const BoxConstraints.tightFor(
                  width: _buttonExtent,
                  height: _buttonExtent,
                ),
                splashRadius: 12,
              ),
            ],
          ),
        );
      },
    );
  }
}

class ChatMarkdownMermaidZoomableViewport extends StatefulWidget {
  const ChatMarkdownMermaidZoomableViewport({
    super.key,
    required this.viewportHeight,
    required this.canvasSize,
    required this.child,
    this.zoomController,
    this.minScale = 0.8,
    this.maxScale = 2.2,
    this.zoomStep = 0.2,
    this.boundaryMargin = const EdgeInsets.all(48),
    this.showControls = true,
    this.zoomInTooltip,
    this.zoomOutTooltip,
    this.controlsFillColor,
    this.controlsBorderColor,
    this.controlsIconColor,
    this.exportBoundaryKey,
  });

  final double viewportHeight;
  final Size canvasSize;
  final Widget child;
  final ChatMarkdownMermaidZoomController? zoomController;
  final double minScale;
  final double maxScale;
  final double zoomStep;
  final EdgeInsets boundaryMargin;
  final bool showControls;
  final String? zoomInTooltip;
  final String? zoomOutTooltip;
  final Color? controlsFillColor;
  final Color? controlsBorderColor;
  final Color? controlsIconColor;

  /// 导出整图时挂载的边界 Key。
  ///
  /// 不为空时，视口会用 [RepaintBoundary] 包裹内部的全画布层，
  /// 使截图覆盖完整画布（不受视口高度裁切与缩放影响），保证导出完整高清。
  final GlobalKey? exportBoundaryKey;

  @override
  State<ChatMarkdownMermaidZoomableViewport> createState() =>
      _ChatMarkdownMermaidZoomableViewportState();
}

class _ChatMarkdownMermaidZoomableViewportState
    extends State<ChatMarkdownMermaidZoomableViewport> {
  static const double _controlsInset = 6;
  static const double _epsilon = 0.001;
  static const double _minExtent = 1;

  late final TransformationController _transformationController;
  late final ChatMarkdownMermaidZoomController _fallbackZoomController;
  Size? _viewportSize;
  Size? _lastFittedViewport;
  Size? _lastFittedCanvas;
  double _effectiveMinScale = 0.8;
  bool _hasUserInteracted = false;

  ChatMarkdownMermaidZoomController get _activeZoomController =>
      widget.zoomController ?? _fallbackZoomController;

  @override
  void initState() {
    super.initState();
    _transformationController = TransformationController();
    _fallbackZoomController = ChatMarkdownMermaidZoomController();
    _effectiveMinScale = widget.minScale;
    _syncZoomController();
  }

  @override
  void didUpdateWidget(
    covariant ChatMarkdownMermaidZoomableViewport oldWidget,
  ) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.zoomController != widget.zoomController) {
      oldWidget.zoomController?.unbind();
    }
    final needsRefit =
        oldWidget.canvasSize != widget.canvasSize ||
        oldWidget.viewportHeight != widget.viewportHeight ||
        oldWidget.minScale != widget.minScale ||
        oldWidget.maxScale != widget.maxScale;
    if (needsRefit) {
      _hasUserInteracted = false;
      _lastFittedViewport = null;
      _lastFittedCanvas = null;
      _effectiveMinScale = widget.minScale;
    }
    _syncZoomController();
  }

  @override
  void dispose() {
    _activeZoomController.unbind();
    _transformationController.dispose();
    super.dispose();
  }

  double get _currentScale =>
      _transformationController.value.getMaxScaleOnAxis();

  void _setTransform({required double scale, required Offset translation}) {
    // 三个轴必须等比缩放：getMaxScaleOnAxis 取各轴最大值，z 轴若固定为 1，
    // 适配缩放小于 1 时读回的缩放值会被钳回 1，导致步进缩放计算错误。
    _transformationController.value = Matrix4.identity()
      ..translateByDouble(translation.dx, translation.dy, 0, 1)
      ..scaleByDouble(scale, scale, scale, 1);
  }

  bool _isSameSize(Size? a, Size b) {
    if (a == null) {
      return false;
    }
    return (a.width - b.width).abs() < _epsilon &&
        (a.height - b.height).abs() < _epsilon;
  }

  double _resolveFitScale(Size viewportSize) {
    final canvasWidth = math.max(widget.canvasSize.width, _minExtent);
    final canvasHeight = math.max(widget.canvasSize.height, _minExtent);
    final viewportWidth = math.max(viewportSize.width, _minExtent);
    final viewportHeight = math.max(viewportSize.height, _minExtent);
    final fitScale = math.min(
      1.0,
      math.min(viewportWidth / canvasWidth, viewportHeight / canvasHeight),
    );
    final boundedFitScale = fitScale.clamp(0.01, widget.maxScale).toDouble();
    _effectiveMinScale = math.min(widget.minScale, boundedFitScale);
    return boundedFitScale
        .clamp(_effectiveMinScale, widget.maxScale)
        .toDouble();
  }

  void _fitToViewport(Size viewportSize) {
    final fitScale = _resolveFitScale(viewportSize);
    final dx = (viewportSize.width - (widget.canvasSize.width * fitScale)) / 2;
    final dy =
        (viewportSize.height - (widget.canvasSize.height * fitScale)) / 2;
    _setTransform(scale: fitScale, translation: Offset(dx, dy));
    _lastFittedViewport = viewportSize;
    _lastFittedCanvas = widget.canvasSize;
    _syncZoomController();
  }

  void _ensureFitTransform(Size viewportSize) {
    final needsFit =
        !_hasUserInteracted ||
        !_isSameSize(_lastFittedViewport, viewportSize) ||
        !_isSameSize(_lastFittedCanvas, widget.canvasSize);
    if (!needsFit) {
      return;
    }
    _fitToViewport(viewportSize);
  }

  void _zoomBy(double delta) {
    final viewportSize = _viewportSize;
    if (viewportSize == null) {
      return;
    }
    final currentScale = _currentScale;
    final targetScale = (currentScale + delta).clamp(
      _effectiveMinScale,
      widget.maxScale,
    );
    if ((targetScale - currentScale).abs() < _epsilon) {
      return;
    }

    final currentTranslation = _transformationController.value.getTranslation();
    final focalPoint = Offset(viewportSize.width / 2, viewportSize.height / 2);
    final sceneFocal = Offset(
      (focalPoint.dx - currentTranslation.x) / currentScale,
      (focalPoint.dy - currentTranslation.y) / currentScale,
    );
    final nextTranslation = Offset(
      focalPoint.dx - (sceneFocal.dx * targetScale),
      focalPoint.dy - (sceneFocal.dy * targetScale),
    );
    _setTransform(scale: targetScale.toDouble(), translation: nextTranslation);
    _hasUserInteracted = true;
    _syncZoomController();
    setState(() {});
  }

  void _syncZoomController() {
    _activeZoomController.bind(
      currentScale: _currentScale,
      minScale: _effectiveMinScale,
      maxScale: widget.maxScale,
      onZoomIn: () => _zoomBy(widget.zoomStep),
      onZoomOut: () => _zoomBy(-widget.zoomStep),
    );
  }

  @override
  Widget build(BuildContext context) {
    final iconColor =
        widget.controlsIconColor ??
        (Theme.of(context).colorScheme.onSurface.withValues(alpha: 0.88));
    final borderColor =
        widget.controlsBorderColor ?? iconColor.withValues(alpha: 0.2);
    final fillColor =
        widget.controlsFillColor ??
        (ThemeData.estimateBrightnessForColor(iconColor) == Brightness.dark
            ? Colors.white.withValues(alpha: 0.94)
            : const Color(0xFF111827).withValues(alpha: 0.9));

    return SizedBox(
      width: double.infinity,
      height: widget.viewportHeight,
      child: LayoutBuilder(
        builder: (context, constraints) {
          final width = constraints.maxWidth.isFinite
              ? constraints.maxWidth
              : widget.canvasSize.width;
          final viewportSize = Size(
            math.max(width, _minExtent),
            math.max(widget.viewportHeight, _minExtent),
          );
          _viewportSize = viewportSize;
          _ensureFitTransform(viewportSize);

          return Stack(
            children: [
              Positioned.fill(
                child: _ChatMarkdownMermaidGestureShield(
                  child: InteractiveViewer(
                    transformationController: _transformationController,
                    constrained: false,
                    boundaryMargin: widget.boundaryMargin,
                    minScale: _effectiveMinScale,
                    maxScale: widget.maxScale,
                    scaleEnabled: false,
                    trackpadScrollCausesScale: false,
                    onInteractionStart: (_) {
                      _hasUserInteracted = true;
                    },
                    onInteractionEnd: (_) {
                      _syncZoomController();
                      setState(() {});
                    },
                    child: SizedBox(
                      width: widget.canvasSize.width,
                      height: widget.canvasSize.height,
                      child: widget.exportBoundaryKey != null
                          ? RepaintBoundary(
                              key: widget.exportBoundaryKey,
                              child: widget.child,
                            )
                          : widget.child,
                    ),
                  ),
                ),
              ),
              if (widget.showControls)
                Positioned(
                  top: _controlsInset,
                  right: _controlsInset,
                  child: ChatMarkdownMermaidZoomControls(
                    controller: _activeZoomController,
                    fillColor: fillColor,
                    borderColor: borderColor,
                    iconColor: iconColor,
                    zoomInTooltip: widget.zoomInTooltip,
                    zoomOutTooltip: widget.zoomOutTooltip,
                  ),
                ),
            ],
          );
        },
      ),
    );
  }
}

class _ChatMarkdownMermaidGestureShield extends StatelessWidget {
  const _ChatMarkdownMermaidGestureShield({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onLongPress: () {},
      onSecondaryTapDown: (_) {},
      child: child,
    );
  }
}
