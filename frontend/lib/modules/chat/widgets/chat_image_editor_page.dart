import 'dart:async';
import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../shared/utils/toast_util.dart';
import '../../../shared/widgets/app_dialog_style.dart';
import '../../../shared/widgets/transparency_checkerboard.dart';
import '../models/chat_image_edit_result.dart';

enum _ChatImageEditorTool { crop, pen, arrow, circle, rectangle, text }

enum _CropDragTarget {
  top,
  right,
  bottom,
  left,
  topLeft,
  topRight,
  bottomRight,
  bottomLeft,
}

class ChatImageEditorPage extends StatefulWidget {
  const ChatImageEditorPage({
    super.key,
    required this.imageBytes,
    required this.fileName,
    required this.contentType,
  });

  final Uint8List imageBytes;
  final String fileName;
  final String contentType;

  static Future<ChatImageEditResult?> open({
    required Uint8List imageBytes,
    required String fileName,
    required String contentType,
  }) async {
    final routeFuture = Get.to<ChatImageEditResult>(
      () => ChatImageEditorPage(
        imageBytes: imageBytes,
        fileName: fileName,
        contentType: contentType,
      ),
      fullscreenDialog: true,
      transition: Transition.cupertino,
    );
    if (routeFuture == null) {
      return null;
    }
    return routeFuture;
  }

  @override
  State<ChatImageEditorPage> createState() => ChatImageEditorPageState();
}

class ChatImageEditorPageState extends State<ChatImageEditorPage> {
  static const double _minCropSide = 40;
  static const double _cropHandleHitRadiusScreen = 34;
  static const double _cropEdgeHitRadiusScreen = 24;
  static const double _cropTouchOverflowScreen = 24;
  static const double _minViewportScale = 1;
  static const double _maxViewportScale = 4;
  static const double _viewportZoomStep = 0.25;
  static const double _viewportScaleEpsilon = 0.001;
  static const double _viewportScrollZoomFactor = 0.0018;
  // 裁剪模式下画布的内边距。
  // 默认裁剪框等于整张图片，上、下边缘手柄会落在图片的最顶边与最底边，
  // 上方紧贴 AppBar、下方紧接工具栏，手指很难按住拖动。
  // 因此上下方向预留更大的呼吸空间，让上下手柄远离屏幕的上下边界。
  static const EdgeInsets _cropCanvasPadding = EdgeInsets.fromLTRB(
    28,
    72,
    28,
    72,
  );
  static const double _canvasToolbarGap = 24;
  static const List<Color> _colorPalette = <Color>[
    Color(0xFFFF3B30),
    Color(0xFFFF9500),
    Color(0xFFFFCC00),
    Color(0xFF34C759),
    Color(0xFF00C7BE),
    Color(0xFF007AFF),
    Color(0xFF5856D6),
    Color(0xFFFFFFFF),
  ];

  ui.Image? _decodedImage;
  bool _isDecoding = true;
  bool _isSubmitting = false;
  bool _isCommittingCrop = false;
  bool _uploadOriginal = false;
  String? _decodeError;

  final List<_ImageAnnotation> _annotations = <_ImageAnnotation>[];
  _ChatImageEditorTool _selectedTool = _ChatImageEditorTool.pen;
  Color _selectedColor = _colorPalette.first;
  double _strokeWidth = 4;
  double _textSize = 22;

  Rect? _cropRect;
  Rect? _lastDisplayRect;
  Rect? _lastBaseDisplayRect;
  _CropDragTarget? _activeCropDragTarget;
  Rect? _cropRectAtDragStart;
  Offset? _cropDragStartPoint;
  final List<_EditSnapshot> _undoHistory = <_EditSnapshot>[];
  Timer? _cropAutoFitTimer;

  List<Offset>? _activePenPoints;
  Offset? _activeShapeStart;
  Offset? _activeShapeCurrent;
  final Set<int> _activePointerIds = <int>{};
  Size _lastCanvasSize = Size.zero;
  double _viewportScale = _minViewportScale;
  Offset _viewportOffset = Offset.zero;
  bool _isViewportGestureActive = false;
  double _viewportGestureStartScale = _minViewportScale;
  Offset _viewportGestureSceneFocal = Offset.zero;

  @override
  void initState() {
    super.initState();
    _decodeSourceImage();
  }

  @override
  void dispose() {
    _cropAutoFitTimer?.cancel();
    _decodedImage?.dispose();
    for (final _EditSnapshot snapshot in _undoHistory) {
      snapshot.image?.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        backgroundColor: const Color(0xFF141414),
        foregroundColor: Colors.white,
        title: Text('chat_image_editor_title'.tr),
        leading: IconButton(
          icon: const Icon(Icons.close_rounded),
          onPressed: _isSubmitting ? null : () => Get.back(),
        ),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 8),
            child: _isSubmitting
                ? TextButton.icon(
                    onPressed: null,
                    icon: const SizedBox(
                      width: 14,
                      height: 14,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                    label: Text('chat_image_editor_processing'.tr),
                    style: TextButton.styleFrom(foregroundColor: Colors.white),
                  )
                : IconButton(
                    onPressed: _decodedImage == null
                        ? null
                        : _submitEditedImage,
                    icon: const Icon(Icons.check_rounded),
                    tooltip: 'chat_image_editor_confirm'.tr,
                    color: Colors.white,
                  ),
          ),
        ],
      ),
      body: Column(
        children: [
          // 缩放/平移后 CustomPaint 会画到子树边界外；若不裁剪，溢出像素会
          // 画进下方工具栏区域，看起来像图片被工具栏挡住。
          Expanded(
            child: ClipRect(
              key: const Key('chat_image_editor_canvas_clip'),
              child: _buildCanvasArea(),
            ),
          ),
          const SizedBox(height: _canvasToolbarGap),
          _buildBottomToolbarArea(),
        ],
      ),
    );
  }

  Widget _buildBottomToolbarArea() {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [_buildToolBar(), _buildBottomBar()],
    );
  }

  Widget _buildToolBar() {
    return Container(
      color: const Color(0xFF141414),
      padding: const EdgeInsets.fromLTRB(10, 8, 10, 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(
              children: [
                _buildToolButton(
                  tool: _ChatImageEditorTool.crop,
                  icon: Icons.crop_rounded,
                  label: 'chat_image_editor_tool_crop'.tr,
                ),
                _buildToolButton(
                  tool: _ChatImageEditorTool.pen,
                  icon: Icons.brush_rounded,
                  label: 'chat_image_editor_tool_pen'.tr,
                ),
                _buildToolButton(
                  tool: _ChatImageEditorTool.arrow,
                  icon: Icons.trending_flat_rounded,
                  label: 'chat_image_editor_tool_arrow'.tr,
                ),
                _buildToolButton(
                  tool: _ChatImageEditorTool.circle,
                  icon: Icons.circle_outlined,
                  label: 'chat_image_editor_tool_circle'.tr,
                ),
                _buildToolButton(
                  tool: _ChatImageEditorTool.rectangle,
                  icon: Icons.crop_square_rounded,
                  label: 'chat_image_editor_tool_rectangle'.tr,
                ),
                _buildToolButton(
                  tool: _ChatImageEditorTool.text,
                  icon: Icons.text_fields_rounded,
                  label: 'chat_image_editor_tool_text'.tr,
                ),
              ],
            ),
          ),
          const SizedBox(height: 8),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(
              children: _colorPalette
                  .map((Color color) => _buildColorChip(color))
                  .toList(growable: false),
            ),
          ),
          const SizedBox(height: 8),
          _buildSizeSlider(),
        ],
      ),
    );
  }

  Widget _buildToolButton({
    required _ChatImageEditorTool tool,
    required IconData icon,
    required String label,
  }) {
    final bool selected = _selectedTool == tool;
    return Padding(
      padding: const EdgeInsets.only(right: 8),
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: (_uploadOriginal || _isCommittingCrop || _isSubmitting)
            ? null
            : () => _selectTool(tool),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
          decoration: BoxDecoration(
            color: selected ? const Color(0xFF2D79F3) : const Color(0xFF222222),
            borderRadius: BorderRadius.circular(8),
          ),
          child: Row(
            children: [
              Icon(icon, size: 18, color: Colors.white),
              const SizedBox(width: 5),
              Text(
                label,
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildColorChip(Color color) {
    final bool selected = _selectedColor.toARGB32() == color.toARGB32();
    return Padding(
      padding: const EdgeInsets.only(right: 8),
      child: InkWell(
        borderRadius: BorderRadius.circular(14),
        onTap: _uploadOriginal
            ? null
            : () {
                setState(() {
                  _selectedColor = color;
                });
              },
        child: Container(
          width: 28,
          height: 28,
          decoration: BoxDecoration(
            color: color,
            shape: BoxShape.circle,
            border: Border.all(
              color: selected ? const Color(0xFF2D79F3) : Colors.white24,
              width: selected ? 2.5 : 1,
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildSizeSlider() {
    final bool isTextMode = _selectedTool == _ChatImageEditorTool.text;
    final String label = isTextMode
        ? 'chat_image_editor_text_size'.tr
        : 'chat_image_editor_stroke_width'.tr;
    final double value = isTextMode ? _textSize : _strokeWidth;
    final double min = isTextMode ? 12 : 1;
    final double max = isTextMode ? 48 : 16;

    return Row(
      children: [
        SizedBox(
          width: 40,
          child: Text(
            label,
            style: const TextStyle(color: Colors.white70, fontSize: 12),
          ),
        ),
        Expanded(
          child: Slider(
            value: value,
            min: min,
            max: max,
            divisions: (max - min).round(),
            onChanged: _uploadOriginal
                ? null
                : (double next) {
                    setState(() {
                      if (isTextMode) {
                        _textSize = next;
                      } else {
                        _strokeWidth = next;
                      }
                    });
                  },
          ),
        ),
        SizedBox(
          width: 34,
          child: Text(
            value.toStringAsFixed(0),
            textAlign: TextAlign.right,
            style: const TextStyle(color: Colors.white70, fontSize: 12),
          ),
        ),
      ],
    );
  }

  Widget _buildCanvasArea() {
    if (_isDecoding) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_decodedImage == null) {
      return Center(
        child: Text(
          _decodeError ?? 'chat_image_editor_load_failed'.tr,
          style: const TextStyle(color: Colors.white70),
        ),
      );
    }

    final ui.Image image = _decodedImage!;

    return LayoutBuilder(
      builder: (BuildContext context, BoxConstraints constraints) {
        final EdgeInsets imagePadding = _isCropInteractionEnabled
            ? _cropCanvasPadding
            : EdgeInsets.zero;
        final Size canvasSize = Size(
          math.max(0.0, constraints.maxWidth - imagePadding.horizontal),
          math.max(0.0, constraints.maxHeight - imagePadding.vertical),
        );
        _lastCanvasSize = constraints.biggest;
        final Rect baseDisplayRect = _computeFittedRect(
          canvasSize: canvasSize,
          imageSize: Size(image.width.toDouble(), image.height.toDouble()),
        ).shift(Offset(imagePadding.left, imagePadding.top));
        _lastBaseDisplayRect = baseDisplayRect;
        final Offset clampedViewportOffset = _clampViewportOffset(
          offset: _viewportOffset,
          scale: _viewportScale,
        );
        if ((clampedViewportOffset - _viewportOffset).distance >
            _viewportScaleEpsilon) {
          _viewportOffset = clampedViewportOffset;
        }
        final Rect displayRect = _transformViewportRect(baseDisplayRect);
        _lastDisplayRect = displayRect;

        final Rect effectiveCropRect = _effectiveCropRect(image);
        final bool showCropHandles = _isCropInteractionEnabled;

        return Listener(
          behavior: HitTestBehavior.opaque,
          onPointerDown: _handlePointerDown,
          onPointerUp: _handlePointerUpOrCancel,
          onPointerCancel: _handlePointerUpOrCancel,
          onPointerSignal: _handlePointerSignal,
          child: GestureDetector(
            behavior: HitTestBehavior.opaque,
            onScaleStart: _handleScaleStart,
            onScaleUpdate: _handleScaleUpdate,
            onScaleEnd: _handleScaleEnd,
            onTapUp: _handleTapUp,
            child: CustomPaint(
              size: Size.infinite,
              painter: _ChatImageEditorPainter(
                image: image,
                imageDisplayRect: displayRect,
                cropRect: effectiveCropRect,
                showCropHandles: showCropHandles,
                annotations: _annotations,
                activeAnnotation: _buildActiveShapeAnnotation(),
                activePenPoints: _activePenPoints,
                activePenColor: _selectedColor,
                activePenStrokeWidthImage: _currentStrokeWidthImage,
              ),
            ),
          ),
        );
      },
    );
  }

  Widget _buildBottomBar() {
    return Container(
      color: const Color(0xFF141414),
      padding: const EdgeInsets.fromLTRB(10, 4, 10, 10),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          LayoutBuilder(
            builder: (BuildContext context, BoxConstraints constraints) {
              final bool useCompactLayout = constraints.maxWidth < 540;
              final Widget uploadOriginalToggle = _buildUploadOriginalToggle();
              final Widget viewportControls = _buildViewportControls();
              final Widget actionButtons = _buildBottomBarActionButtons();

              if (useCompactLayout) {
                return Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    uploadOriginalToggle,
                    const SizedBox(height: 4),
                    SingleChildScrollView(
                      scrollDirection: Axis.horizontal,
                      child: Row(
                        children: [
                          viewportControls,
                          const SizedBox(width: 12),
                          actionButtons,
                        ],
                      ),
                    ),
                  ],
                );
              }

              return Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(child: uploadOriginalToggle),
                  const SizedBox(width: 8),
                  viewportControls,
                  const SizedBox(width: 8),
                  Flexible(
                    child: Align(
                      alignment: Alignment.centerRight,
                      child: actionButtons,
                    ),
                  ),
                ],
              );
            },
          ),
          if (_selectedTool == _ChatImageEditorTool.text && !_uploadOriginal)
            Align(
              alignment: Alignment.centerLeft,
              child: Padding(
                padding: const EdgeInsets.only(left: 12, bottom: 2),
                child: Text(
                  'chat_image_editor_text_tip'.tr,
                  style: const TextStyle(color: Colors.white60, fontSize: 11),
                ),
              ),
            ),
          if (_selectedTool == _ChatImageEditorTool.crop && !_uploadOriginal)
            Align(
              alignment: Alignment.centerLeft,
              child: Padding(
                padding: const EdgeInsets.only(left: 12, bottom: 2),
                child: Text(
                  'chat_image_editor_crop_tip'.tr,
                  style: const TextStyle(color: Colors.white60, fontSize: 11),
                ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildUploadOriginalToggle() {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.center,
      children: [
        Checkbox(
          value: _uploadOriginal,
          onChanged: (bool? checked) {
            setState(() {
              _uploadOriginal = checked ?? false;
              _clearActiveDrafts();
            });
          },
        ),
        Expanded(
          child: Text(
            'chat_image_editor_upload_original'.tr,
            style: const TextStyle(color: Colors.white, fontSize: 13),
          ),
        ),
      ],
    );
  }

  Widget _buildViewportControls() {
    final bool canZoomIn = _viewportScale < _maxViewportScale;
    final bool canZoomOut = _viewportScale > _minViewportScale;
    final int zoomPercent = (_viewportScale * 100).round();

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
      decoration: BoxDecoration(
        color: const Color(0xFF1D1D1D),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: Colors.white12),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          IconButton(
            key: const Key('chat_image_editor_zoom_out_button'),
            tooltip: 'chat_image_editor_zoom_out'.tr,
            visualDensity: VisualDensity.compact,
            onPressed: canZoomOut
                ? () => _stepViewportZoom(-_viewportZoomStep)
                : null,
            icon: const Icon(Icons.remove_rounded, size: 18),
            color: Colors.white,
            disabledColor: Colors.white24,
          ),
          TextButton(
            key: const Key('chat_image_editor_zoom_reset_button'),
            onPressed: _resetViewport,
            style: TextButton.styleFrom(
              minimumSize: const Size(0, 32),
              padding: const EdgeInsets.symmetric(horizontal: 8),
              foregroundColor: Colors.white,
            ),
            child: Text('$zoomPercent%'),
          ),
          IconButton(
            key: const Key('chat_image_editor_zoom_in_button'),
            tooltip: 'chat_image_editor_zoom_in'.tr,
            visualDensity: VisualDensity.compact,
            onPressed: canZoomIn
                ? () => _stepViewportZoom(_viewportZoomStep)
                : null,
            icon: const Icon(Icons.add_rounded, size: 18),
            color: Colors.white,
            disabledColor: Colors.white24,
          ),
        ],
      ),
    );
  }

  Widget _buildBottomBarActionButtons() {
    return Wrap(
      alignment: WrapAlignment.end,
      spacing: 4,
      runSpacing: 4,
      children: [
        _buildBottomBarActionButton(
          onPressed: (_uploadOriginal || _undoHistory.isEmpty) ? null : _undo,
          label: 'chat_image_editor_undo'.tr,
        ),
        _buildBottomBarActionButton(
          onPressed: (_uploadOriginal || _annotations.isEmpty)
              ? null
              : _clearAnnotations,
          label: 'chat_image_editor_clear'.tr,
        ),
        _buildBottomBarActionButton(
          onPressed: (_uploadOriginal || _cropRect == null) ? null : _resetCrop,
          label: 'chat_image_editor_reset_crop'.tr,
        ),
      ],
    );
  }

  Widget _buildBottomBarActionButton({
    required VoidCallback? onPressed,
    required String label,
  }) {
    return TextButton(onPressed: onPressed, child: Text(label));
  }

  double get _currentStrokeWidthImage {
    return _strokeWidth * _imagePixelPerScreenPixel;
  }

  double get _currentTextSizeImage {
    return _textSize * _imagePixelPerScreenPixel;
  }

  double get _imagePixelPerScreenPixel {
    final ui.Image? image = _decodedImage;
    final Rect? displayRect = _lastDisplayRect;
    if (image == null || displayRect == null || displayRect.width <= 0) {
      return 1;
    }
    return image.width / displayRect.width;
  }

  double get _cropHandleHitRadiusImage {
    return _cropHandleHitRadiusScreen * _imagePixelPerScreenPixel;
  }

  double get _cropEdgeHitRadiusImage {
    return _cropEdgeHitRadiusScreen * _imagePixelPerScreenPixel;
  }

  bool get _isCropInteractionEnabled {
    return _selectedTool == _ChatImageEditorTool.crop && !_uploadOriginal;
  }

  Rect _imageBounds(ui.Image image) {
    return Rect.fromLTWH(0, 0, image.width.toDouble(), image.height.toDouble());
  }

  Rect _effectiveCropRect(ui.Image image) {
    final Rect imageBounds = _imageBounds(image);
    final Rect? crop = _cropRect;
    if (crop == null) {
      return imageBounds;
    }
    final Rect clipped = crop.intersect(imageBounds);
    if (clipped.width < 1 || clipped.height < 1) {
      return imageBounds;
    }
    return clipped;
  }

  Rect _computeFittedRect({required Size canvasSize, required Size imageSize}) {
    if (canvasSize.width <= 0 ||
        canvasSize.height <= 0 ||
        imageSize.width <= 0 ||
        imageSize.height <= 0) {
      return Rect.zero;
    }
    final double scale = math.min(
      canvasSize.width / imageSize.width,
      canvasSize.height / imageSize.height,
    );
    final double targetWidth = imageSize.width * scale;
    final double targetHeight = imageSize.height * scale;
    final double left = (canvasSize.width - targetWidth) / 2;
    final double top = (canvasSize.height - targetHeight) / 2;
    return Rect.fromLTWH(left, top, targetWidth, targetHeight);
  }

  Rect _transformViewportRect(Rect baseRect) {
    return Rect.fromLTRB(
      baseRect.left * _viewportScale + _viewportOffset.dx,
      baseRect.top * _viewportScale + _viewportOffset.dy,
      baseRect.right * _viewportScale + _viewportOffset.dx,
      baseRect.bottom * _viewportScale + _viewportOffset.dy,
    );
  }

  Offset _clampViewportOffset({required Offset offset, required double scale}) {
    final Rect? baseRect = _lastBaseDisplayRect;
    final Size viewportSize = _lastCanvasSize;
    if (baseRect == null ||
        viewportSize.width <= 0 ||
        viewportSize.height <= 0 ||
        scale <= 0) {
      return offset;
    }

    double clampAxis({
      required double currentOffset,
      required double scaledStart,
      required double scaledEnd,
      required double viewportExtent,
    }) {
      final double scaledSize = scaledEnd - scaledStart;
      if (scaledSize <= viewportExtent) {
        return (viewportExtent - scaledSize) / 2 - scaledStart;
      }
      final double minOffset = viewportExtent - scaledEnd;
      final double maxOffset = -scaledStart;
      return currentOffset.clamp(minOffset, maxOffset).toDouble();
    }

    return Offset(
      clampAxis(
        currentOffset: offset.dx,
        scaledStart: baseRect.left * scale,
        scaledEnd: baseRect.right * scale,
        viewportExtent: viewportSize.width,
      ),
      clampAxis(
        currentOffset: offset.dy,
        scaledStart: baseRect.top * scale,
        scaledEnd: baseRect.bottom * scale,
        viewportExtent: viewportSize.height,
      ),
    );
  }

  void _stepViewportZoom(double delta) {
    final Rect? displayRect = _lastDisplayRect;
    final Size viewportSize = _lastCanvasSize;
    final Offset focalPoint =
        displayRect?.center ??
        Offset(viewportSize.width / 2, viewportSize.height / 2);
    _setViewportScale(
      targetScale: _viewportScale + delta,
      focalPoint: focalPoint,
    );
  }

  void _setViewportScale({
    required double targetScale,
    required Offset focalPoint,
  }) {
    final Rect? baseRect = _lastBaseDisplayRect;
    final double clampedScale = targetScale
        .clamp(_minViewportScale, _maxViewportScale)
        .toDouble();
    if ((clampedScale - _viewportScale).abs() < _viewportScaleEpsilon) {
      return;
    }
    if (baseRect == null) {
      setState(() {
        _viewportScale = clampedScale;
        if ((_viewportScale - _minViewportScale).abs() <
            _viewportScaleEpsilon) {
          _viewportOffset = Offset.zero;
        }
      });
      return;
    }

    final Offset sceneFocal = Offset(
      (focalPoint.dx - _viewportOffset.dx) / _viewportScale,
      (focalPoint.dy - _viewportOffset.dy) / _viewportScale,
    );
    final Offset nextOffset = _clampViewportOffset(
      offset: Offset(
        focalPoint.dx - sceneFocal.dx * clampedScale,
        focalPoint.dy - sceneFocal.dy * clampedScale,
      ),
      scale: clampedScale,
    );

    setState(() {
      _viewportScale = clampedScale;
      _viewportOffset = nextOffset;
    });
  }

  void _resetViewport() {
    if ((_viewportScale - _minViewportScale).abs() < _viewportScaleEpsilon &&
        _viewportOffset.distance < _viewportScaleEpsilon) {
      return;
    }
    setState(() {
      _viewportScale = _minViewportScale;
      _viewportOffset = Offset.zero;
    });
  }

  Offset? _toImageOffset(
    Offset localPosition, {
    double outsideToleranceScreen = 0,
  }) {
    final ui.Image? image = _decodedImage;
    final Rect? displayRect = _lastDisplayRect;
    if (image == null ||
        displayRect == null ||
        displayRect.width <= 0 ||
        displayRect.height <= 0 ||
        !displayRect.inflate(outsideToleranceScreen).contains(localPosition)) {
      return null;
    }

    final double clampedX = localPosition.dx
        .clamp(displayRect.left, displayRect.right)
        .toDouble();
    final double clampedY = localPosition.dy
        .clamp(displayRect.top, displayRect.bottom)
        .toDouble();

    final double normalizedX =
        (clampedX - displayRect.left) / displayRect.width;
    final double normalizedY =
        (clampedY - displayRect.top) / displayRect.height;

    return Offset(
      (normalizedX * image.width).clamp(0, image.width.toDouble()),
      (normalizedY * image.height).clamp(0, image.height.toDouble()),
    );
  }

  Rect _editableCropRect(ui.Image image) {
    final Rect imageBounds = _imageBounds(image);
    final Rect? crop = _cropRect;
    if (crop == null) {
      return imageBounds;
    }
    final Rect clipped = crop.intersect(imageBounds);
    if (clipped.width < 1 || clipped.height < 1) {
      return imageBounds;
    }
    return clipped;
  }

  _CropDragTarget? _hitTestCropDragTarget({
    required Offset imagePoint,
    required Rect cropRect,
  }) {
    final double cornerRadius = _cropHandleHitRadiusImage;
    final double edgeRadius = _cropEdgeHitRadiusImage;

    if ((imagePoint - cropRect.topLeft).distance <= cornerRadius) {
      return _CropDragTarget.topLeft;
    }
    if ((imagePoint - cropRect.topRight).distance <= cornerRadius) {
      return _CropDragTarget.topRight;
    }
    if ((imagePoint - cropRect.bottomRight).distance <= cornerRadius) {
      return _CropDragTarget.bottomRight;
    }
    if ((imagePoint - cropRect.bottomLeft).distance <= cornerRadius) {
      return _CropDragTarget.bottomLeft;
    }

    final bool withinHorizontal =
        imagePoint.dx >= cropRect.left - edgeRadius &&
        imagePoint.dx <= cropRect.right + edgeRadius;
    final bool withinVertical =
        imagePoint.dy >= cropRect.top - edgeRadius &&
        imagePoint.dy <= cropRect.bottom + edgeRadius;

    if (withinHorizontal &&
        (imagePoint.dy - cropRect.top).abs() <= edgeRadius) {
      return _CropDragTarget.top;
    }
    if (withinVertical &&
        (imagePoint.dx - cropRect.right).abs() <= edgeRadius) {
      return _CropDragTarget.right;
    }
    if (withinHorizontal &&
        (imagePoint.dy - cropRect.bottom).abs() <= edgeRadius) {
      return _CropDragTarget.bottom;
    }
    if (withinVertical && (imagePoint.dx - cropRect.left).abs() <= edgeRadius) {
      return _CropDragTarget.left;
    }
    return null;
  }

  double _resolveMinCropSide(Rect imageBounds) {
    return math.max(
      1,
      math.min(_minCropSide, math.min(imageBounds.width, imageBounds.height)),
    );
  }

  Rect _applyCropDrag({
    required Rect initial,
    required _CropDragTarget target,
    required Offset delta,
    required Rect imageBounds,
  }) {
    final double minSide = _resolveMinCropSide(imageBounds);
    double left = initial.left;
    double top = initial.top;
    double right = initial.right;
    double bottom = initial.bottom;

    switch (target) {
      case _CropDragTarget.top:
        top = (initial.top + delta.dy).clamp(
          imageBounds.top,
          initial.bottom - minSide,
        );
        break;
      case _CropDragTarget.right:
        right = (initial.right + delta.dx).clamp(
          initial.left + minSide,
          imageBounds.right,
        );
        break;
      case _CropDragTarget.bottom:
        bottom = (initial.bottom + delta.dy).clamp(
          initial.top + minSide,
          imageBounds.bottom,
        );
        break;
      case _CropDragTarget.left:
        left = (initial.left + delta.dx).clamp(
          imageBounds.left,
          initial.right - minSide,
        );
        break;
      case _CropDragTarget.topLeft:
        left = (initial.left + delta.dx).clamp(
          imageBounds.left,
          initial.right - minSide,
        );
        top = (initial.top + delta.dy).clamp(
          imageBounds.top,
          initial.bottom - minSide,
        );
        break;
      case _CropDragTarget.topRight:
        right = (initial.right + delta.dx).clamp(
          initial.left + minSide,
          imageBounds.right,
        );
        top = (initial.top + delta.dy).clamp(
          imageBounds.top,
          initial.bottom - minSide,
        );
        break;
      case _CropDragTarget.bottomRight:
        right = (initial.right + delta.dx).clamp(
          initial.left + minSide,
          imageBounds.right,
        );
        bottom = (initial.bottom + delta.dy).clamp(
          initial.top + minSide,
          imageBounds.bottom,
        );
        break;
      case _CropDragTarget.bottomLeft:
        left = (initial.left + delta.dx).clamp(
          imageBounds.left,
          initial.right - minSide,
        );
        bottom = (initial.bottom + delta.dy).clamp(
          initial.top + minSide,
          imageBounds.bottom,
        );
        break;
    }

    return Rect.fromLTRB(left, top, right, bottom);
  }

  void _handlePointerDown(PointerDownEvent event) {
    _activePointerIds.add(event.pointer);
  }

  void _handlePointerUpOrCancel(PointerEvent event) {
    _activePointerIds.remove(event.pointer);
  }

  void _handlePointerSignal(PointerSignalEvent event) {
    if (event is! PointerScrollEvent || _decodedImage == null) {
      return;
    }

    final double delta = event.scrollDelta.dy != 0
        ? event.scrollDelta.dy
        : event.scrollDelta.dx;
    if (delta == 0) {
      return;
    }

    final Offset focalPoint = event.localPosition;
    final double zoomFactor = math
        .pow(1 + _viewportScrollZoomFactor, -delta)
        .toDouble();
    _setViewportScale(
      targetScale: _viewportScale * zoomFactor,
      focalPoint: focalPoint,
    );
  }

  void _handleScaleStart(ScaleStartDetails details) {
    if (_decodedImage == null) {
      return;
    }
    if (_activePointerIds.length >= 2) {
      _beginViewportGesture(details.localFocalPoint);
      return;
    }
    _handleToolDragStart(details.localFocalPoint);
  }

  void _handleScaleUpdate(ScaleUpdateDetails details) {
    if (_decodedImage == null) {
      return;
    }
    if (_isViewportGestureActive || _activePointerIds.length >= 2) {
      if (!_isViewportGestureActive) {
        _beginViewportGesture(details.localFocalPoint);
      }
      _updateViewportGesture(details);
      return;
    }
    _handleToolDragUpdate(details.localFocalPoint);
  }

  void _handleScaleEnd(ScaleEndDetails details) {
    if (_decodedImage == null) {
      return;
    }
    if (_isViewportGestureActive) {
      _isViewportGestureActive = false;
      return;
    }
    _handleToolDragEnd();
  }

  void _beginViewportGesture(Offset localFocalPoint) {
    _clearActiveDrafts();
    _isViewportGestureActive = true;
    _viewportGestureStartScale = _viewportScale;
    _viewportGestureSceneFocal = Offset(
      (localFocalPoint.dx - _viewportOffset.dx) / _viewportScale,
      (localFocalPoint.dy - _viewportOffset.dy) / _viewportScale,
    );
  }

  void _updateViewportGesture(ScaleUpdateDetails details) {
    final double nextScale = (_viewportGestureStartScale * details.scale)
        .clamp(_minViewportScale, _maxViewportScale)
        .toDouble();
    final Offset nextOffset = _clampViewportOffset(
      offset: Offset(
        details.localFocalPoint.dx - _viewportGestureSceneFocal.dx * nextScale,
        details.localFocalPoint.dy - _viewportGestureSceneFocal.dy * nextScale,
      ),
      scale: nextScale,
    );

    setState(() {
      _viewportScale = nextScale;
      _viewportOffset = nextOffset;
    });
  }

  void _handleToolDragStart(Offset localPosition) {
    if (_uploadOriginal || _decodedImage == null) {
      return;
    }
    final double outsideTolerance = _selectedTool == _ChatImageEditorTool.crop
        ? _cropTouchOverflowScreen
        : 0;
    final Offset? imagePoint = _toImageOffset(
      localPosition,
      outsideToleranceScreen: outsideTolerance,
    );
    if (imagePoint == null) {
      return;
    }

    setState(() {
      switch (_selectedTool) {
        case _ChatImageEditorTool.crop:
          _cropAutoFitTimer?.cancel();
          _cropAutoFitTimer = null;
          final ui.Image image = _decodedImage!;
          final Rect cropRect = _editableCropRect(image);
          final _CropDragTarget? dragTarget = _hitTestCropDragTarget(
            imagePoint: imagePoint,
            cropRect: cropRect,
          );
          if (dragTarget == null) {
            _activeCropDragTarget = null;
            _cropRectAtDragStart = null;
            _cropDragStartPoint = null;
            break;
          }
          _pushUndoSnapshot();
          _cropRect = cropRect;
          _activeCropDragTarget = dragTarget;
          _cropRectAtDragStart = cropRect;
          _cropDragStartPoint = imagePoint;
          break;
        case _ChatImageEditorTool.pen:
          _activePenPoints = <Offset>[imagePoint];
          break;
        case _ChatImageEditorTool.arrow:
        case _ChatImageEditorTool.circle:
        case _ChatImageEditorTool.rectangle:
          _activeShapeStart = imagePoint;
          _activeShapeCurrent = imagePoint;
          break;
        case _ChatImageEditorTool.text:
          break;
      }
    });
  }

  void _handleToolDragUpdate(Offset localPosition) {
    if (_uploadOriginal || _decodedImage == null) {
      return;
    }
    final double outsideTolerance = _selectedTool == _ChatImageEditorTool.crop
        ? _cropTouchOverflowScreen
        : 0;
    final Offset? imagePoint = _toImageOffset(
      localPosition,
      outsideToleranceScreen: outsideTolerance,
    );
    if (imagePoint == null) {
      return;
    }

    switch (_selectedTool) {
      case _ChatImageEditorTool.crop:
        final _CropDragTarget? dragTarget = _activeCropDragTarget;
        final Rect? dragStartRect = _cropRectAtDragStart;
        final Offset? dragStartPoint = _cropDragStartPoint;
        if (dragTarget == null ||
            dragStartRect == null ||
            dragStartPoint == null) {
          return;
        }
        final Rect imageBounds = _imageBounds(_decodedImage!);
        setState(() {
          _cropRect = _applyCropDrag(
            initial: dragStartRect,
            target: dragTarget,
            delta: imagePoint - dragStartPoint,
            imageBounds: imageBounds,
          );
        });
        break;
      case _ChatImageEditorTool.pen:
        final List<Offset>? points = _activePenPoints;
        if (points == null) {
          return;
        }
        if (points.isNotEmpty && (points.last - imagePoint).distance < 0.4) {
          return;
        }
        setState(() {
          points.add(imagePoint);
        });
        break;
      case _ChatImageEditorTool.arrow:
      case _ChatImageEditorTool.circle:
      case _ChatImageEditorTool.rectangle:
        if (_activeShapeStart == null) {
          return;
        }
        setState(() {
          _activeShapeCurrent = imagePoint;
        });
        break;
      case _ChatImageEditorTool.text:
        break;
    }
  }

  void _handleToolDragEnd() {
    if (_uploadOriginal || _decodedImage == null) {
      return;
    }

    setState(() {
      switch (_selectedTool) {
        case _ChatImageEditorTool.crop:
          _activeCropDragTarget = null;
          _cropRectAtDragStart = null;
          _cropDragStartPoint = null;
          _cropAutoFitTimer?.cancel();
          _cropAutoFitTimer = Timer(const Duration(seconds: 1), () {
            if (!mounted) return;
            setState(() {
              _fitViewportToCropRect();
            });
          });
          break;
        case _ChatImageEditorTool.pen:
          final List<Offset>? points = _activePenPoints;
          if (points != null && points.length >= 2) {
            _pushUndoSnapshot();
            _annotations.add(
              _PenAnnotation(
                points: List<Offset>.of(points),
                color: _selectedColor,
                strokeWidthImage: _currentStrokeWidthImage,
              ),
            );
          }
          _activePenPoints = null;
          break;
        case _ChatImageEditorTool.arrow:
          final Offset? start = _activeShapeStart;
          final Offset? end = _activeShapeCurrent;
          if (start != null && end != null && (end - start).distance >= 1) {
            _pushUndoSnapshot();
            _annotations.add(
              _ArrowAnnotation(
                start: start,
                end: end,
                color: _selectedColor,
                strokeWidthImage: _currentStrokeWidthImage,
              ),
            );
          }
          _activeShapeStart = null;
          _activeShapeCurrent = null;
          break;
        case _ChatImageEditorTool.circle:
          final Rect rect = _normalizeRect(
            _activeShapeStart,
            _activeShapeCurrent,
          );
          if (rect.width >= 2 && rect.height >= 2) {
            _pushUndoSnapshot();
            _annotations.add(
              _CircleAnnotation(
                rect: rect,
                color: _selectedColor,
                strokeWidthImage: _currentStrokeWidthImage,
              ),
            );
          }
          _activeShapeStart = null;
          _activeShapeCurrent = null;
          break;
        case _ChatImageEditorTool.rectangle:
          final Rect rect = _normalizeRect(
            _activeShapeStart,
            _activeShapeCurrent,
          );
          if (rect.width >= 2 && rect.height >= 2) {
            _pushUndoSnapshot();
            _annotations.add(
              _RectangleAnnotation(
                rect: rect,
                color: _selectedColor,
                strokeWidthImage: _currentStrokeWidthImage,
              ),
            );
          }
          _activeShapeStart = null;
          _activeShapeCurrent = null;
          break;
        case _ChatImageEditorTool.text:
          break;
      }
    });
  }

  Future<void> _handleTapUp(TapUpDetails details) async {
    if (_uploadOriginal || _selectedTool != _ChatImageEditorTool.text) {
      return;
    }

    final Offset? imagePoint = _toImageOffset(details.localPosition);
    if (imagePoint == null) {
      return;
    }

    final String? text = await _promptTextInput(context);
    if (!mounted) {
      return;
    }
    final String trimmed = (text ?? '').trim();
    if (trimmed.isEmpty) {
      return;
    }

    setState(() {
      _pushUndoSnapshot();
      _annotations.add(
        _TextAnnotation(
          anchor: imagePoint,
          text: trimmed,
          color: _selectedColor,
          fontSizeImage: _currentTextSizeImage,
        ),
      );
    });
  }

  _ImageAnnotation? _buildActiveShapeAnnotation() {
    switch (_selectedTool) {
      case _ChatImageEditorTool.arrow:
        final Offset? start = _activeShapeStart;
        final Offset? end = _activeShapeCurrent;
        if (start == null || end == null) {
          return null;
        }
        return _ArrowAnnotation(
          start: start,
          end: end,
          color: _selectedColor,
          strokeWidthImage: _currentStrokeWidthImage,
        );
      case _ChatImageEditorTool.circle:
        final Rect rect = _normalizeRect(
          _activeShapeStart,
          _activeShapeCurrent,
        );
        if (rect.width < 2 || rect.height < 2) {
          return null;
        }
        return _CircleAnnotation(
          rect: rect,
          color: _selectedColor,
          strokeWidthImage: _currentStrokeWidthImage,
        );
      case _ChatImageEditorTool.rectangle:
        final Rect rect = _normalizeRect(
          _activeShapeStart,
          _activeShapeCurrent,
        );
        if (rect.width < 2 || rect.height < 2) {
          return null;
        }
        return _RectangleAnnotation(
          rect: rect,
          color: _selectedColor,
          strokeWidthImage: _currentStrokeWidthImage,
        );
      case _ChatImageEditorTool.crop:
      case _ChatImageEditorTool.pen:
      case _ChatImageEditorTool.text:
        return null;
    }
  }

  Rect _normalizeRect(Offset? first, Offset? second) {
    if (first == null || second == null) {
      return Rect.zero;
    }
    final double left = math.min(first.dx, second.dx);
    final double top = math.min(first.dy, second.dy);
    final double right = math.max(first.dx, second.dx);
    final double bottom = math.max(first.dy, second.dy);
    return Rect.fromLTRB(left, top, right, bottom);
  }

  Future<void> _decodeSourceImage() async {
    try {
      final ui.Codec codec = await ui.instantiateImageCodec(widget.imageBytes);
      final ui.FrameInfo frameInfo = await codec.getNextFrame();
      codec.dispose();

      if (!mounted) {
        frameInfo.image.dispose();
        return;
      }

      setState(() {
        _decodedImage = frameInfo.image;
        _isDecoding = false;
      });
    } catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _decodeError = 'chat_image_editor_decode_failed'.tr;
        _isDecoding = false;
      });
      debugPrint('decode image error: $error');
    }
  }

  Future<void> _submitEditedImage() async {
    if (_isSubmitting || _decodedImage == null) {
      return;
    }

    setState(() {
      _isSubmitting = true;
    });

    try {
      final ChatImageEditResult result;
      if (_uploadOriginal) {
        result = ChatImageEditResult(
          bytes: widget.imageBytes,
          fileName: widget.fileName,
          contentType: widget.contentType,
          uploadOriginal: true,
        );
      } else {
        result = ChatImageEditResult(
          bytes: await _renderEditedImageBytes(),
          fileName: _buildEditedFileName(widget.fileName),
          contentType: 'image/png',
          uploadOriginal: false,
        );
      }

      if (!mounted) {
        return;
      }
      Get.back(result: result);
    } catch (error) {
      if (mounted) {
        CustomToast.show('chat_image_editor_process_failed'.tr, isError: true);
      }
      debugPrint('submit edited image error: $error');
    } finally {
      if (mounted) {
        setState(() {
          _isSubmitting = false;
        });
      }
    }
  }

  Future<Uint8List> _renderEditedImageBytes() async {
    final ui.Image image = _decodedImage!;
    final Rect cropRect = _effectiveCropRect(image);

    final int targetWidth = cropRect.width.round().clamp(1, image.width);
    final int targetHeight = cropRect.height.round().clamp(1, image.height);

    final ui.PictureRecorder recorder = ui.PictureRecorder();
    final Canvas canvas = Canvas(recorder);
    final Rect outputRect = Rect.fromLTWH(
      0,
      0,
      targetWidth.toDouble(),
      targetHeight.toDouble(),
    );

    canvas.drawImageRect(image, cropRect, outputRect, Paint());

    final _ImageCoordinateMapper mapper = _ImageCoordinateMapper(
      sourceRect: cropRect,
      destinationRect: outputRect,
    );

    canvas.save();
    canvas.clipRect(outputRect);
    for (final _ImageAnnotation annotation in _annotations) {
      annotation.paint(canvas, mapper);
    }
    canvas.restore();

    final ui.Picture picture = recorder.endRecording();
    final ui.Image renderedImage = await picture.toImage(
      targetWidth,
      targetHeight,
    );
    final ByteData? encoded = await renderedImage.toByteData(
      format: ui.ImageByteFormat.png,
    );
    renderedImage.dispose();

    if (encoded == null) {
      throw StateError('encode edited image failed');
    }
    return encoded.buffer.asUint8List();
  }

  String _buildEditedFileName(String sourceName) {
    final String trimmed = sourceName.trim();
    if (trimmed.isEmpty) {
      return 'image_edited.png';
    }
    final int dotIndex = trimmed.lastIndexOf('.');
    final String baseName = dotIndex > 0
        ? trimmed.substring(0, dotIndex)
        : trimmed;
    return '${baseName}_edited.png';
  }

  Future<String?> _promptTextInput(BuildContext context) async {
    final TextEditingController controller = TextEditingController();
    final String? result = await showAppDialog<String>(
      context: context,
      builder: (BuildContext dialogContext) {
        return AlertDialog(
          title: Text('chat_image_editor_text_dialog_title'.tr),
          content: TextField(
            controller: controller,
            autofocus: true,
            maxLines: 3,
            decoration: InputDecoration(
              hintText: 'chat_image_editor_text_dialog_hint'.tr,
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(),
              child: Text('common_cancel'.tr),
            ),
            TextButton(
              onPressed: () =>
                  Navigator.of(dialogContext).pop(controller.text.trim()),
              child: Text('common_confirm'.tr),
            ),
          ],
        );
      },
    );
    controller.dispose();
    return result;
  }

  void _pushUndoSnapshot() {
    _undoHistory.add(
      _EditSnapshot(cropRect: _cropRect, annotationCount: _annotations.length),
    );
  }

  void _undo() {
    if (_undoHistory.isEmpty) {
      return;
    }
    final _EditSnapshot snapshot = _undoHistory.removeLast();
    setState(() {
      if (snapshot.image != null) {
        // 撤销「离开裁剪工具时的 bake」：还原 bake 前的整图与批注。
        _decodedImage?.dispose();
        _decodedImage = snapshot.image;
        _annotations
          ..clear()
          ..addAll(snapshot.annotations ?? const <_ImageAnnotation>[]);
        _cropRect = snapshot.cropRect;
      } else {
        _cropRect = snapshot.cropRect;
        while (_annotations.length > snapshot.annotationCount) {
          _annotations.removeLast();
        }
      }
      _clearActiveDrafts();
      // 非裁剪工具下不要自动放大到裁剪框，否则又会把内容顶到工具栏下。
      if (_isCropInteractionEnabled) {
        _fitViewportToCropRect();
      } else {
        _viewportScale = _minViewportScale;
        _viewportOffset = Offset.zero;
      }
    });
  }

  void _clearAnnotations() {
    if (_annotations.isEmpty) {
      return;
    }
    _pushUndoSnapshot();
    setState(() {
      _annotations.clear();
      _clearActiveDrafts();
    });
  }

  void _resetCrop() {
    _pushUndoSnapshot();
    _cropAutoFitTimer?.cancel();
    _cropAutoFitTimer = null;
    setState(() {
      _cropRect = null;
      _activeCropDragTarget = null;
      _cropRectAtDragStart = null;
      _cropDragStartPoint = null;
      _viewportScale = _minViewportScale;
      _viewportOffset = Offset.zero;
    });
  }

  Future<void> _selectTool(_ChatImageEditorTool tool) async {
    if (_selectedTool == tool || _isCommittingCrop || _isSubmitting) {
      return;
    }

    _cropAutoFitTimer?.cancel();
    _cropAutoFitTimer = null;

    // 离开裁剪工具时真正执行裁剪：像素 bake 成新图并 100% 重新适配画布。
    // 否则裁剪只是原图上的选区+视口放大，切到画笔后仍按放大原图展示，易被工具栏挡住。
    if (_selectedTool == _ChatImageEditorTool.crop &&
        tool != _ChatImageEditorTool.crop) {
      final bool committed = await _commitCropIfNeeded();
      if (!mounted || !committed) {
        return;
      }
    }

    if (!mounted) {
      return;
    }
    setState(() {
      _selectedTool = tool;
      _clearActiveDrafts();
      if (tool == _ChatImageEditorTool.crop) {
        _viewportScale = _minViewportScale;
        _viewportOffset = Offset.zero;
      }
    });
  }

  bool _isNonTrivialCrop(ui.Image image, Rect cropRect) {
    final Rect imageBounds = _imageBounds(image);
    return (cropRect.width - imageBounds.width).abs() > 0.5 ||
        (cropRect.height - imageBounds.height).abs() > 0.5 ||
        cropRect.left.abs() > 0.5 ||
        cropRect.top.abs() > 0.5;
  }

  /// 将当前裁剪框 bake 进 `_decodedImage`。无实际裁剪时只重置视口。
  /// 返回 false 表示失败（保持仍在裁剪工具，便于重试）。
  Future<bool> _commitCropIfNeeded() async {
    final ui.Image? image = _decodedImage;
    if (image == null || _isCommittingCrop) {
      return false;
    }

    final Rect cropRect = _effectiveCropRect(image);
    if (!_isNonTrivialCrop(image, cropRect)) {
      setState(() {
        _cropRect = null;
        _viewportScale = _minViewportScale;
        _viewportOffset = Offset.zero;
      });
      return true;
    }

    _isCommittingCrop = true;
    try {
      final ui.Image cropped = await _rasterizeCrop(image, cropRect);
      if (!mounted) {
        cropped.dispose();
        return false;
      }

      final List<_ImageAnnotation> previousAnnotations =
          List<_ImageAnnotation>.of(_annotations);
      final Rect? previousCropRect = _cropRect;
      final List<_ImageAnnotation> remapped = _shiftAnnotations(
        _annotations,
        cropRect.topLeft,
      );

      setState(() {
        _undoHistory.add(
          _EditSnapshot(
            cropRect: previousCropRect,
            annotationCount: previousAnnotations.length,
            image: image,
            annotations: previousAnnotations,
          ),
        );
        _decodedImage = cropped;
        _annotations
          ..clear()
          ..addAll(remapped);
        _cropRect = null;
        _viewportScale = _minViewportScale;
        _viewportOffset = Offset.zero;
        _clearActiveDrafts();
        _lastDisplayRect = null;
        _lastBaseDisplayRect = null;
      });
      // `image` 由 undo snapshot 持有，成功 bake 后不要 dispose。
      return true;
    } catch (error) {
      debugPrint('commit crop error: $error');
      if (mounted) {
        CustomToast.show('chat_image_editor_process_failed'.tr, isError: true);
      }
      return false;
    } finally {
      _isCommittingCrop = false;
    }
  }

  Future<ui.Image> _rasterizeCrop(ui.Image image, Rect cropRect) async {
    final int targetWidth = cropRect.width.round().clamp(1, image.width);
    final int targetHeight = cropRect.height.round().clamp(1, image.height);
    final ui.PictureRecorder recorder = ui.PictureRecorder();
    final Canvas canvas = Canvas(recorder);
    canvas.drawImageRect(
      image,
      cropRect,
      Rect.fromLTWH(0, 0, targetWidth.toDouble(), targetHeight.toDouble()),
      Paint(),
    );
    final ui.Picture picture = recorder.endRecording();
    return picture.toImage(targetWidth, targetHeight);
  }

  List<_ImageAnnotation> _shiftAnnotations(
    List<_ImageAnnotation> source,
    Offset origin,
  ) {
    if (origin == Offset.zero || source.isEmpty) {
      return List<_ImageAnnotation>.of(source);
    }
    return source
        .map((_ImageAnnotation annotation) {
          if (annotation is _PenAnnotation) {
            return _PenAnnotation(
              points: annotation.points
                  .map((Offset point) => point - origin)
                  .toList(growable: false),
              color: annotation.color,
              strokeWidthImage: annotation.strokeWidthImage,
            );
          }
          if (annotation is _ArrowAnnotation) {
            return _ArrowAnnotation(
              start: annotation.start - origin,
              end: annotation.end - origin,
              color: annotation.color,
              strokeWidthImage: annotation.strokeWidthImage,
            );
          }
          if (annotation is _CircleAnnotation) {
            return _CircleAnnotation(
              rect: annotation.rect.shift(-origin),
              color: annotation.color,
              strokeWidthImage: annotation.strokeWidthImage,
            );
          }
          if (annotation is _RectangleAnnotation) {
            return _RectangleAnnotation(
              rect: annotation.rect.shift(-origin),
              color: annotation.color,
              strokeWidthImage: annotation.strokeWidthImage,
            );
          }
          if (annotation is _TextAnnotation) {
            return _TextAnnotation(
              anchor: annotation.anchor - origin,
              text: annotation.text,
              color: annotation.color,
              fontSizeImage: annotation.fontSizeImage,
            );
          }
          return annotation;
        })
        .toList(growable: false);
  }

  void _fitViewportToCropRect() {
    final ui.Image? image = _decodedImage;
    final Rect? baseRect = _lastBaseDisplayRect;
    final Size canvasSize = _lastCanvasSize;
    if (image == null || baseRect == null || canvasSize.width <= 0) {
      return;
    }
    final Rect cropRect = _effectiveCropRect(image);
    final Rect imageBounds = _imageBounds(image);
    final bool cropChanged =
        (cropRect.width - imageBounds.width).abs() > 0.5 ||
        (cropRect.height - imageBounds.height).abs() > 0.5 ||
        cropRect.left.abs() > 0.5 ||
        cropRect.top.abs() > 0.5;
    if (!cropChanged) {
      _viewportScale = _minViewportScale;
      _viewportOffset = Offset.zero;
      return;
    }
    final double normLeft =
        (cropRect.left - imageBounds.left) / imageBounds.width;
    final double normTop =
        (cropRect.top - imageBounds.top) / imageBounds.height;
    final double normRight =
        (cropRect.right - imageBounds.left) / imageBounds.width;
    final double normBottom =
        (cropRect.bottom - imageBounds.top) / imageBounds.height;
    final double cropW = normRight - normLeft;
    final double cropH = normBottom - normTop;
    if (cropW <= 0 || cropH <= 0) {
      return;
    }
    final EdgeInsets padding = _isCropInteractionEnabled
        ? _cropCanvasPadding
        : EdgeInsets.zero;
    final double availW = canvasSize.width - padding.horizontal;
    final double availH = canvasSize.height - padding.vertical;
    if (availW <= 0 || availH <= 0) {
      return;
    }
    final double fitScale = math.min(
      availW / (cropW * baseRect.width),
      availH / (cropH * baseRect.height),
    );
    final double clampedScale = fitScale
        .clamp(_minViewportScale, _maxViewportScale)
        .toDouble();
    final double cropCenterNormX = (normLeft + normRight) / 2;
    final double cropCenterNormY = (normTop + normBottom) / 2;
    final double baseCenterX = baseRect.left + baseRect.width * cropCenterNormX;
    final double baseCenterY = baseRect.top + baseRect.height * cropCenterNormY;
    final Offset nextOffset = _clampViewportOffset(
      offset: Offset(
        canvasSize.width / 2 - baseCenterX * clampedScale,
        canvasSize.height / 2 - baseCenterY * clampedScale,
      ),
      scale: clampedScale,
    );
    _viewportScale = clampedScale;
    _viewportOffset = nextOffset;
  }

  void _clearActiveDrafts() {
    _activePenPoints = null;
    _activeShapeStart = null;
    _activeShapeCurrent = null;
    _activeCropDragTarget = null;
    _cropRectAtDragStart = null;
    _cropDragStartPoint = null;
  }

  @visibleForTesting
  Size? get debugDecodedImageSize {
    final ui.Image? image = _decodedImage;
    if (image == null) {
      return null;
    }
    return Size(image.width.toDouble(), image.height.toDouble());
  }

  @visibleForTesting
  void debugSetCropRect(Rect rect) {
    _cropRect = rect;
  }

  @visibleForTesting
  double get debugViewportScale => _viewportScale;

  @visibleForTesting
  Future<void> debugSelectCropTool() => _selectTool(_ChatImageEditorTool.crop);

  @visibleForTesting
  Future<void> debugSelectPenTool() => _selectTool(_ChatImageEditorTool.pen);

  @visibleForTesting
  void debugReplaceDecodedImage(ui.Image image) {
    _decodedImage?.dispose();
    _decodedImage = image;
    _isDecoding = false;
    _decodeError = null;
    _cropRect = null;
    _viewportScale = _minViewportScale;
    _viewportOffset = Offset.zero;
  }
}

class _ChatImageEditorPainter extends CustomPainter {
  const _ChatImageEditorPainter({
    required this.image,
    required this.imageDisplayRect,
    required this.cropRect,
    required this.showCropHandles,
    required this.annotations,
    required this.activeAnnotation,
    required this.activePenPoints,
    required this.activePenColor,
    required this.activePenStrokeWidthImage,
  });

  final ui.Image image;
  final Rect imageDisplayRect;
  final Rect cropRect;
  final bool showCropHandles;
  final List<_ImageAnnotation> annotations;
  final _ImageAnnotation? activeAnnotation;
  final List<Offset>? activePenPoints;
  final Color activePenColor;
  final double activePenStrokeWidthImage;

  @override
  void paint(Canvas canvas, Size size) {
    final Rect imageBounds = Rect.fromLTWH(
      0,
      0,
      image.width.toDouble(),
      image.height.toDouble(),
    );

    canvas.drawRect(
      Rect.fromLTWH(0, 0, size.width, size.height),
      Paint()..color = Colors.black,
    );

    // 图片显示区域铺棋盘格底纹，避免透明 PNG 内容与纯色背景撞色。
    paintTransparencyCheckerboard(canvas, imageDisplayRect);

    canvas.drawImageRect(image, imageBounds, imageDisplayRect, Paint());

    final _ImageCoordinateMapper previewMapper = _ImageCoordinateMapper(
      sourceRect: imageBounds,
      destinationRect: imageDisplayRect,
    );

    canvas.save();
    canvas.clipRect(imageDisplayRect);

    for (final _ImageAnnotation annotation in annotations) {
      annotation.paint(canvas, previewMapper);
    }

    if (activePenPoints != null && activePenPoints!.length >= 2) {
      _PenAnnotation(
        points: activePenPoints!,
        color: activePenColor,
        strokeWidthImage: activePenStrokeWidthImage,
      ).paint(canvas, previewMapper);
    }

    activeAnnotation?.paint(canvas, previewMapper);

    canvas.restore();

    final Rect cropDisplayRect = previewMapper.mapRect(cropRect);
    final bool cropChanged =
        (cropRect.width - imageBounds.width).abs() > 0.5 ||
        (cropRect.height - imageBounds.height).abs() > 0.5 ||
        cropRect.left.abs() > 0.5 ||
        cropRect.top.abs() > 0.5;

    if (cropChanged) {
      _paintCropMask(canvas, imageDisplayRect, cropDisplayRect);
    }
    _paintCropFrame(canvas, cropDisplayRect);
    if (showCropHandles) {
      _paintCropHandles(canvas, cropDisplayRect);
    }
  }

  void _paintCropMask(Canvas canvas, Rect imageRect, Rect cropRectScreen) {
    final Paint maskPaint = Paint()
      ..color = Colors.black.withValues(alpha: 0.45);

    if (cropRectScreen.top > imageRect.top) {
      canvas.drawRect(
        Rect.fromLTRB(
          imageRect.left,
          imageRect.top,
          imageRect.right,
          cropRectScreen.top,
        ),
        maskPaint,
      );
    }

    if (cropRectScreen.bottom < imageRect.bottom) {
      canvas.drawRect(
        Rect.fromLTRB(
          imageRect.left,
          cropRectScreen.bottom,
          imageRect.right,
          imageRect.bottom,
        ),
        maskPaint,
      );
    }

    if (cropRectScreen.left > imageRect.left) {
      canvas.drawRect(
        Rect.fromLTRB(
          imageRect.left,
          cropRectScreen.top,
          cropRectScreen.left,
          cropRectScreen.bottom,
        ),
        maskPaint,
      );
    }

    if (cropRectScreen.right < imageRect.right) {
      canvas.drawRect(
        Rect.fromLTRB(
          cropRectScreen.right,
          cropRectScreen.top,
          imageRect.right,
          cropRectScreen.bottom,
        ),
        maskPaint,
      );
    }
  }

  void _paintCropFrame(Canvas canvas, Rect cropRectScreen) {
    canvas.drawRect(
      cropRectScreen,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.5
        ..color = Colors.white,
    );
  }

  void _paintCropHandles(Canvas canvas, Rect cropRectScreen) {
    if (cropRectScreen.width <= 0 || cropRectScreen.height <= 0) {
      return;
    }

    final Paint cornerPaint = Paint()..color = Colors.white;
    const double cornerRadius = 7;
    final List<Offset> corners = <Offset>[
      cropRectScreen.topLeft,
      cropRectScreen.topRight,
      cropRectScreen.bottomRight,
      cropRectScreen.bottomLeft,
    ];
    for (final Offset corner in corners) {
      canvas.drawCircle(corner, cornerRadius, cornerPaint);
    }

    final Paint edgePaint = Paint()
      ..color = Colors.white.withValues(alpha: 0.9);
    const double edgeWidth = 20;
    const double edgeThickness = 6;
    final List<Rect> edges = <Rect>[
      Rect.fromCenter(
        center: Offset(cropRectScreen.center.dx, cropRectScreen.top),
        width: edgeWidth,
        height: edgeThickness,
      ),
      Rect.fromCenter(
        center: Offset(cropRectScreen.right, cropRectScreen.center.dy),
        width: edgeThickness,
        height: edgeWidth,
      ),
      Rect.fromCenter(
        center: Offset(cropRectScreen.center.dx, cropRectScreen.bottom),
        width: edgeWidth,
        height: edgeThickness,
      ),
      Rect.fromCenter(
        center: Offset(cropRectScreen.left, cropRectScreen.center.dy),
        width: edgeThickness,
        height: edgeWidth,
      ),
    ];

    for (final Rect edge in edges) {
      canvas.drawRRect(
        RRect.fromRectAndRadius(edge, const Radius.circular(2)),
        edgePaint,
      );
    }
  }

  @override
  bool shouldRepaint(covariant _ChatImageEditorPainter oldDelegate) {
    return oldDelegate.image != image ||
        oldDelegate.imageDisplayRect != imageDisplayRect ||
        oldDelegate.cropRect != cropRect ||
        oldDelegate.showCropHandles != showCropHandles ||
        oldDelegate.activeAnnotation != activeAnnotation ||
        oldDelegate.activePenPoints != activePenPoints ||
        oldDelegate.activePenColor != activePenColor ||
        oldDelegate.activePenStrokeWidthImage != activePenStrokeWidthImage ||
        oldDelegate.annotations != annotations;
  }
}

class _ImageCoordinateMapper {
  const _ImageCoordinateMapper({
    required this.sourceRect,
    required this.destinationRect,
  });

  final Rect sourceRect;
  final Rect destinationRect;

  double get scaleX => destinationRect.width / sourceRect.width;
  double get scaleY => destinationRect.height / sourceRect.height;
  double get strokeScale => (scaleX + scaleY) / 2;

  Offset mapOffset(Offset source) {
    final double normalizedX = (source.dx - sourceRect.left) / sourceRect.width;
    final double normalizedY = (source.dy - sourceRect.top) / sourceRect.height;
    return Offset(
      destinationRect.left + normalizedX * destinationRect.width,
      destinationRect.top + normalizedY * destinationRect.height,
    );
  }

  Rect mapRect(Rect source) {
    final Offset lt = mapOffset(source.topLeft);
    final Offset rb = mapOffset(source.bottomRight);
    return Rect.fromPoints(lt, rb);
  }
}

abstract class _ImageAnnotation {
  const _ImageAnnotation({required this.color});

  final Color color;

  void paint(Canvas canvas, _ImageCoordinateMapper mapper);
}

class _PenAnnotation extends _ImageAnnotation {
  const _PenAnnotation({
    required this.points,
    required super.color,
    required this.strokeWidthImage,
  });

  final List<Offset> points;
  final double strokeWidthImage;

  @override
  void paint(Canvas canvas, _ImageCoordinateMapper mapper) {
    if (points.isEmpty) {
      return;
    }

    final Paint paint = Paint()
      ..style = PaintingStyle.stroke
      ..strokeCap = StrokeCap.round
      ..strokeJoin = StrokeJoin.round
      ..strokeWidth = math.max(strokeWidthImage * mapper.strokeScale, 1)
      ..color = color;

    if (points.length == 1) {
      canvas.drawCircle(
        mapper.mapOffset(points.first),
        paint.strokeWidth / 2,
        paint..style = PaintingStyle.fill,
      );
      return;
    }

    final Path path = Path();
    final Offset first = mapper.mapOffset(points.first);
    path.moveTo(first.dx, first.dy);
    for (int i = 1; i < points.length; i++) {
      final Offset point = mapper.mapOffset(points[i]);
      path.lineTo(point.dx, point.dy);
    }

    canvas.drawPath(path, paint);
  }
}

class _ArrowAnnotation extends _ImageAnnotation {
  const _ArrowAnnotation({
    required this.start,
    required this.end,
    required super.color,
    required this.strokeWidthImage,
  });

  final Offset start;
  final Offset end;
  final double strokeWidthImage;

  @override
  void paint(Canvas canvas, _ImageCoordinateMapper mapper) {
    final Offset mappedStart = mapper.mapOffset(start);
    final Offset mappedEnd = mapper.mapOffset(end);
    final double scaledStroke = math.max(
      strokeWidthImage * mapper.strokeScale,
      1,
    );

    final Paint paint = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = scaledStroke
      ..strokeCap = StrokeCap.round
      ..strokeJoin = StrokeJoin.round
      ..color = color;

    canvas.drawLine(mappedStart, mappedEnd, paint);

    final Offset delta = mappedEnd - mappedStart;
    final double length = delta.distance;
    if (length < 2) {
      return;
    }

    final double headLength = math.max(10, scaledStroke * 3);
    const double headAngle = math.pi / 7;
    final double angle = math.atan2(delta.dy, delta.dx);

    final Offset head1 = Offset(
      mappedEnd.dx - headLength * math.cos(angle - headAngle),
      mappedEnd.dy - headLength * math.sin(angle - headAngle),
    );
    final Offset head2 = Offset(
      mappedEnd.dx - headLength * math.cos(angle + headAngle),
      mappedEnd.dy - headLength * math.sin(angle + headAngle),
    );

    canvas.drawLine(mappedEnd, head1, paint);
    canvas.drawLine(mappedEnd, head2, paint);
  }
}

class _CircleAnnotation extends _ImageAnnotation {
  const _CircleAnnotation({
    required this.rect,
    required super.color,
    required this.strokeWidthImage,
  });

  final Rect rect;
  final double strokeWidthImage;

  @override
  void paint(Canvas canvas, _ImageCoordinateMapper mapper) {
    final Rect mappedRect = mapper.mapRect(rect);
    canvas.drawOval(
      mappedRect,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = math.max(strokeWidthImage * mapper.strokeScale, 1)
        ..color = color,
    );
  }
}

class _RectangleAnnotation extends _ImageAnnotation {
  const _RectangleAnnotation({
    required this.rect,
    required super.color,
    required this.strokeWidthImage,
  });

  final Rect rect;
  final double strokeWidthImage;

  @override
  void paint(Canvas canvas, _ImageCoordinateMapper mapper) {
    final Rect mappedRect = mapper.mapRect(rect);
    canvas.drawRect(
      mappedRect,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = math.max(strokeWidthImage * mapper.strokeScale, 1)
        ..color = color,
    );
  }
}

class _TextAnnotation extends _ImageAnnotation {
  const _TextAnnotation({
    required this.anchor,
    required this.text,
    required super.color,
    required this.fontSizeImage,
  });

  final Offset anchor;
  final String text;
  final double fontSizeImage;

  @override
  void paint(Canvas canvas, _ImageCoordinateMapper mapper) {
    final Offset mappedAnchor = mapper.mapOffset(anchor);
    final double fontSize = math.max(fontSizeImage * mapper.strokeScale, 10);

    final TextPainter painter = TextPainter(
      text: TextSpan(
        text: text,
        style: TextStyle(
          color: color,
          fontSize: fontSize,
          fontWeight: FontWeight.w600,
        ),
      ),
      textDirection: TextDirection.ltr,
      maxLines: 10,
    )..layout(maxWidth: mapper.destinationRect.width);

    painter.paint(canvas, mappedAnchor);
  }
}

class _EditSnapshot {
  const _EditSnapshot({
    required this.cropRect,
    required this.annotationCount,
    this.image,
    this.annotations,
  });

  final Rect? cropRect;
  final int annotationCount;

  /// 非空表示这是一次「裁剪 bake」快照；撤销时还原该图片（所有权在 snapshot）。
  final ui.Image? image;
  final List<_ImageAnnotation>? annotations;
}
