import 'dart:math' as math;

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

/// 图片缩放控制器：对外暴露步进缩放与复位能力，并反映当前是否可继续缩放、是否处于基础缩放。
class ChatMarkdownImageZoomController extends ChangeNotifier {
  double _currentScale = 1;
  double _minScale = 1;
  double _maxScale = 1;
  bool _isAtBaseScale = true;
  VoidCallback? _zoomInAction;
  VoidCallback? _zoomOutAction;
  VoidCallback? _resetAction;
  bool _isDisposed = false;

  bool get isBound => _zoomInAction != null;

  bool get canZoomIn => isBound && _currentScale < (_maxScale - 0.001);

  bool get canZoomOut => isBound && _currentScale > (_minScale + 0.001);

  bool get isAtBaseScale => _isAtBaseScale;

  double get currentScale => _currentScale;

  void zoomIn() => _zoomInAction?.call();

  void zoomOut() => _zoomOutAction?.call();

  void reset() => _resetAction?.call();

  void bind({
    required double currentScale,
    required double minScale,
    required double maxScale,
    required bool isAtBaseScale,
    required VoidCallback onZoomIn,
    required VoidCallback onZoomOut,
    required VoidCallback onReset,
  }) {
    final changed = !isBound ||
        (_currentScale - currentScale).abs() >= 0.001 ||
        (_minScale - minScale).abs() >= 0.001 ||
        (_maxScale - maxScale).abs() >= 0.001 ||
        _isAtBaseScale != isAtBaseScale;
    _currentScale = currentScale;
    _minScale = minScale;
    _maxScale = maxScale;
    _isAtBaseScale = isAtBaseScale;
    _zoomInAction = onZoomIn;
    _zoomOutAction = onZoomOut;
    _resetAction = onReset;
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
    _resetAction = null;
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
    final phase = WidgetsBinding.instance.schedulerPhase;
    if (phase == SchedulerPhase.idle ||
        phase == SchedulerPhase.postFrameCallbacks) {
      notifyListeners();
      return;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_isDisposed) {
        notifyListeners();
      }
    });
  }
}

class ChatMarkdownZoomableImageViewport extends StatefulWidget {
  const ChatMarkdownZoomableImageViewport({
    super.key,
    required this.child,
    this.transformationController,
    this.controller,
    this.onDismiss,
    // 默认最小缩放为一步缩小档（1 / _zoomStep = 1 / 1.6），让预览在默认
    // 状态下也能向下缩小一级；调用方传入更小值可获得更多缩小档位。
    this.minScale = 0.625,
    // 最大放大 10 倍：聊天图片/流程图截图常含小字，6 倍不够看清细节。
    this.maxScale = 10,
    this.doubleTapScale = 2.5,
    this.boundaryMargin = const EdgeInsets.all(128),
  });

  final Widget child;
  final TransformationController? transformationController;
  final ChatMarkdownImageZoomController? controller;
  final VoidCallback? onDismiss;
  final double minScale;
  final double maxScale;
  final double doubleTapScale;
  final EdgeInsets boundaryMargin;

  @override
  State<ChatMarkdownZoomableImageViewport> createState() =>
      _ChatMarkdownZoomableImageViewportState();
}

class _ChatMarkdownZoomableImageViewportState
    extends State<ChatMarkdownZoomableImageViewport> {
  static const double _baseScale = 1;
  static const double _scaleEpsilon = 0.001;
  static const double _scrollZoomFactor = 0.0018;
  static const double _zoomStep = 1.6;
  static const double _dismissDragThreshold = 120;

  late final TransformationController _fallbackTransformationController;
  TransformationController? _listenedController;
  TapDownDetails? _lastDoubleTapDownDetails;
  Size _viewportSize = Size.zero;
  bool _isAtBaseScale = true;

  int _activePointerCount = 0;
  Offset? _dismissDragStart;
  bool _trackingDismissDrag = false;

  TransformationController get _transformationController =>
      widget.transformationController ?? _fallbackTransformationController;

  double get _currentScale =>
      _transformationController.value.getMaxScaleOnAxis();

  @override
  void initState() {
    super.initState();
    _fallbackTransformationController = TransformationController();
    _attachTransformListener();
  }

  @override
  void didUpdateWidget(covariant ChatMarkdownZoomableImageViewport oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.transformationController != widget.transformationController) {
      final previousValue = (oldWidget.transformationController ??
              _fallbackTransformationController)
          .value
          .clone();
      if (widget.transformationController != null) {
        widget.transformationController!.value = previousValue;
      } else {
        _fallbackTransformationController.value = previousValue;
      }
      _attachTransformListener();
    }
    if (oldWidget.controller != widget.controller) {
      oldWidget.controller?.unbind();
      _syncZoomController();
    }
  }

  @override
  void dispose() {
    _listenedController?.removeListener(_handleTransformChanged);
    widget.controller?.unbind();
    _fallbackTransformationController.dispose();
    super.dispose();
  }

  void _attachTransformListener() {
    final controller = _transformationController;
    if (identical(_listenedController, controller)) {
      return;
    }
    _listenedController?.removeListener(_handleTransformChanged);
    controller.addListener(_handleTransformChanged);
    _listenedController = controller;
  }

  void _handleTransformChanged() {
    _syncZoomController();
    final atBase = !_hasNonIdentityTransform;
    if (atBase != _isAtBaseScale && mounted) {
      setState(() => _isAtBaseScale = atBase);
    }
  }

  void _syncZoomController() {
    final controller = widget.controller;
    if (controller == null) {
      return;
    }
    controller.bind(
      currentScale: _currentScale,
      minScale: widget.minScale,
      maxScale: widget.maxScale,
      isAtBaseScale: !_hasNonIdentityTransform,
      onZoomIn: () => _zoomBy(_zoomStep),
      onZoomOut: () => _zoomBy(1 / _zoomStep),
      onReset: _resetTransform,
    );
  }

  void _resetTransform() {
    _transformationController.value = Matrix4.identity();
  }

  bool get _hasNonIdentityTransform {
    final matrix = _transformationController.value;
    final translation = matrix.getTranslation();
    return (_currentScale - _baseScale).abs() > _scaleEpsilon ||
        translation.x.abs() > _scaleEpsilon ||
        translation.y.abs() > _scaleEpsilon;
  }

  void _zoomBy(double factor) {
    if (_viewportSize == Size.zero) {
      return;
    }
    _setScale(
      targetScale: _currentScale * factor,
      focalPoint: Offset(_viewportSize.width / 2, _viewportSize.height / 2),
    );
  }

  void _setScale({
    required double targetScale,
    required Offset focalPoint,
  }) {
    final currentScale = _currentScale;
    final clampedScale =
        targetScale.clamp(widget.minScale, widget.maxScale).toDouble();
    if ((clampedScale - currentScale).abs() < _scaleEpsilon) {
      return;
    }
    // 触及最小缩放时吸附回原始比例，仅在最小缩放即原始比例（1）时才有意义；
    // 最小缩放小于 1 时，缩小到最小档是合法状态，不能吸附回 1。
    if (widget.minScale >= _baseScale - _scaleEpsilon &&
        (clampedScale - widget.minScale).abs() < _scaleEpsilon) {
      _resetTransform();
      return;
    }

    final currentTranslation = _transformationController.value.getTranslation();
    final sceneFocal = Offset(
      (focalPoint.dx - currentTranslation.x) / currentScale,
      (focalPoint.dy - currentTranslation.y) / currentScale,
    );
    final nextTranslation = Offset(
      focalPoint.dx - (sceneFocal.dx * clampedScale),
      focalPoint.dy - (sceneFocal.dy * clampedScale),
    );

    // 三个轴必须等比缩放：getMaxScaleOnAxis 取各轴最大值，z 轴若固定为 1，
    // 缩小到 1 以下时读回的缩放值会被钳回 1，导致缩放状态计算错误。
    _transformationController.value = Matrix4.identity()
      ..translateByDouble(nextTranslation.dx, nextTranslation.dy, 0, 1)
      ..scaleByDouble(clampedScale, clampedScale, clampedScale, 1);
  }

  void _handleInteractionEnd(ScaleEndDetails details) {
    // 与 _setScale 同理：只有最小缩放即原始比例时，捏合回最小档才吸附复位。
    if (widget.minScale >= _baseScale - _scaleEpsilon &&
        _currentScale <= widget.minScale + _scaleEpsilon &&
        _hasNonIdentityTransform) {
      _resetTransform();
    }
  }

  void _handleDoubleTap(Size viewportSize) {
    final focalPoint = _lastDoubleTapDownDetails?.localPosition ??
        Offset(viewportSize.width / 2, viewportSize.height / 2);
    if (_hasNonIdentityTransform) {
      _resetTransform();
      _lastDoubleTapDownDetails = null;
      return;
    }
    _setScale(
      targetScale: widget.doubleTapScale,
      focalPoint: focalPoint,
    );
    _lastDoubleTapDownDetails = null;
  }

  void _handlePointerSignal(
    PointerSignalEvent event,
    Size viewportSize,
  ) {
    if (event is! PointerScrollEvent) {
      return;
    }

    final delta =
        event.scrollDelta.dy != 0 ? event.scrollDelta.dy : event.scrollDelta.dx;
    if (delta == 0) {
      return;
    }

    final renderBox = context.findRenderObject();
    final focalPoint = renderBox is RenderBox
        ? renderBox.globalToLocal(event.position)
        : Offset(viewportSize.width / 2, viewportSize.height / 2);
    final zoomFactor = math.pow(1 + _scrollZoomFactor, -delta).toDouble();
    _setScale(
      targetScale: _currentScale * zoomFactor,
      focalPoint: focalPoint,
    );
  }

  void _handlePointerDown(PointerDownEvent event) {
    _activePointerCount++;
    if (_activePointerCount == 1 &&
        event.kind == PointerDeviceKind.touch &&
        widget.onDismiss != null &&
        !_hasNonIdentityTransform) {
      _dismissDragStart = event.position;
      _trackingDismissDrag = true;
    } else {
      _trackingDismissDrag = false;
    }
  }

  void _handlePointerMove(PointerMoveEvent event) {
    if (_activePointerCount > 1) {
      _trackingDismissDrag = false;
    }
  }

  void _handlePointerUp(PointerUpEvent event) {
    final wasTracking = _trackingDismissDrag;
    final start = _dismissDragStart;
    _activePointerCount = math.max(0, _activePointerCount - 1);
    if (_activePointerCount == 0) {
      _trackingDismissDrag = false;
      _dismissDragStart = null;
    }
    if (wasTracking && start != null) {
      final delta = event.position - start;
      if (delta.dy > _dismissDragThreshold && delta.dy > delta.dx.abs()) {
        widget.onDismiss?.call();
      }
    }
  }

  void _handlePointerCancel(PointerCancelEvent event) {
    _activePointerCount = math.max(0, _activePointerCount - 1);
    if (_activePointerCount == 0) {
      _trackingDismissDrag = false;
      _dismissDragStart = null;
    }
  }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        _viewportSize = Size(
          constraints.maxWidth.isFinite ? constraints.maxWidth : 0,
          constraints.maxHeight.isFinite ? constraints.maxHeight : 0,
        );
        _syncZoomController();

        return Listener(
          onPointerSignal: (event) =>
              _handlePointerSignal(event, _viewportSize),
          onPointerDown: _handlePointerDown,
          onPointerMove: _handlePointerMove,
          onPointerUp: _handlePointerUp,
          onPointerCancel: _handlePointerCancel,
          child: MouseRegion(
            cursor: _isAtBaseScale
                ? SystemMouseCursors.basic
                : SystemMouseCursors.move,
            child: GestureDetector(
              behavior: HitTestBehavior.opaque,
              // 未放大时单击图片直接关闭预览；放大状态下单击不关闭，避免查看时误触。
              onTap: widget.onDismiss == null
                  ? null
                  : () {
                      if (!_hasNonIdentityTransform) {
                        widget.onDismiss!.call();
                      }
                    },
              onDoubleTapDown: (details) {
                _lastDoubleTapDownDetails = details;
              },
              onDoubleTap: () => _handleDoubleTap(_viewportSize),
              child: InteractiveViewer(
                transformationController: _transformationController,
                minScale: widget.minScale,
                maxScale: widget.maxScale,
                boundaryMargin: widget.boundaryMargin,
                panEnabled: !_isAtBaseScale,
                onInteractionEnd: _handleInteractionEnd,
                clipBehavior: Clip.none,
                child: SizedBox(
                  width: _viewportSize.width,
                  height: _viewportSize.height,
                  child: widget.child,
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}
