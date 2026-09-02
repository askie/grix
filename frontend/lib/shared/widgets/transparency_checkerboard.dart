import 'package:flutter/material.dart';

/// 透明图片的棋盘格底纹。
///
/// 透明 PNG 在纯色背景下，与背景同色的内容会“消失”：纯黑底吃掉黑色内容，
/// 纯浅底吃掉白色内容。棋盘格用两种中性灰交错，对深浅内容都保留足够对比，
/// 是看图器/编辑器表示透明区域的通用做法。
class TransparencyCheckerboard extends StatelessWidget {
  const TransparencyCheckerboard({super.key, this.cellSize = 12});

  /// 单个方格边长（逻辑像素）。
  final double cellSize;

  @override
  Widget build(BuildContext context) {
    return CustomPaint(
      size: Size.infinite,
      painter: _TransparencyCheckerboardPainter(cellSize: cellSize),
    );
  }
}

class _TransparencyCheckerboardPainter extends CustomPainter {
  const _TransparencyCheckerboardPainter({required this.cellSize});

  final double cellSize;

  @override
  void paint(Canvas canvas, Size size) {
    paintTransparencyCheckerboard(
      canvas,
      Offset.zero & size,
      cellSize: cellSize,
    );
  }

  @override
  bool shouldRepaint(_TransparencyCheckerboardPainter oldDelegate) {
    return oldDelegate.cellSize != cellSize;
  }
}

/// 棋盘格的两种中性灰：偏亮的一格让黑色内容浮现，偏暗的一格让白色内容浮现。
const Color _checkerboardLightCell = Color(0xFF8A8A8A);
const Color _checkerboardDarkCell = Color(0xFF5E5E5E);

/// 在指定矩形区域内绘制棋盘格底纹。
///
/// 既被 [TransparencyCheckerboard] 复用，也可在已有 [Canvas] 上直接调用，
/// 用于自绘场景（例如图片编辑器画布只在图片显示区域铺底纹）。
void paintTransparencyCheckerboard(
  Canvas canvas,
  Rect rect, {
  double cellSize = 12,
}) {
  if (rect.isEmpty || cellSize <= 0) {
    return;
  }

  final Paint lightPaint = Paint()..color = _checkerboardLightCell;
  final Paint darkPaint = Paint()..color = _checkerboardDarkCell;

  // 先整体铺一层亮色，再补上交错的暗格，减少绘制调用。
  canvas.save();
  canvas.clipRect(rect);
  canvas.drawRect(rect, lightPaint);

  final int columns = (rect.width / cellSize).ceil();
  final int rows = (rect.height / cellSize).ceil();
  for (int row = 0; row < rows; row++) {
    for (int col = 0; col < columns; col++) {
      if ((row + col).isEven) {
        continue;
      }
      final double left = rect.left + col * cellSize;
      final double top = rect.top + row * cellSize;
      canvas.drawRect(Rect.fromLTWH(left, top, cellSize, cellSize), darkPaint);
    }
  }
  canvas.restore();
}

/// 把图片渲染在棋盘格底纹之上，且底纹严格贴合图片显示区域。
///
/// 先解析图片真实宽高比，再用 [AspectRatio] 生成与图片等比的盒子，
/// 棋盘格只铺在这个盒子内（即图片本身那一块），盒子之外保持透明，
/// 由外层背景（如深色看图器）填充。这样既能看清透明内容，又不会满屏格子。
class CheckerboardBackedImage extends StatefulWidget {
  const CheckerboardBackedImage({
    super.key,
    required this.image,
    this.loadingBuilder,
    this.errorBuilder,
    this.fit = BoxFit.contain,
    this.cellSize = 12,
  });

  final ImageProvider image;
  final WidgetBuilder? loadingBuilder;
  final WidgetBuilder? errorBuilder;
  final BoxFit fit;
  final double cellSize;

  @override
  State<CheckerboardBackedImage> createState() =>
      _CheckerboardBackedImageState();
}

class _CheckerboardBackedImageState extends State<CheckerboardBackedImage> {
  ImageStream? _stream;
  ImageStreamListener? _listener;
  double? _aspectRatio;
  bool _failed = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _resolveImage();
  }

  @override
  void didUpdateWidget(CheckerboardBackedImage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.image != widget.image) {
      _aspectRatio = null;
      _failed = false;
      _resolveImage();
    }
  }

  @override
  void dispose() {
    _detachListener();
    super.dispose();
  }

  void _resolveImage() {
    final ImageStream stream = widget.image.resolve(
      createLocalImageConfiguration(context),
    );
    if (_stream?.key == stream.key) {
      return;
    }
    _detachListener();
    final ImageStreamListener listener = ImageStreamListener(
      _handleImage,
      onError: _handleError,
    );
    _stream = stream;
    _listener = listener;
    stream.addListener(listener);
  }

  void _detachListener() {
    final ImageStream? stream = _stream;
    final ImageStreamListener? listener = _listener;
    if (stream != null && listener != null) {
      stream.removeListener(listener);
    }
    _listener = null;
  }

  void _handleImage(ImageInfo info, bool synchronousCall) {
    final double width = info.image.width.toDouble();
    final double height = info.image.height.toDouble();
    final double ratio = height <= 0 ? 1 : width / height;
    info.dispose();
    if (!mounted || _aspectRatio == ratio) {
      return;
    }
    setState(() => _aspectRatio = ratio);
  }

  void _handleError(Object error, StackTrace? stackTrace) {
    if (mounted) {
      setState(() => _failed = true);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_failed) {
      return widget.errorBuilder?.call(context) ?? const SizedBox.shrink();
    }
    final double? ratio = _aspectRatio;
    if (ratio == null) {
      return widget.loadingBuilder?.call(context) ?? const SizedBox.shrink();
    }
    return AspectRatio(
      aspectRatio: ratio,
      child: Stack(
        fit: StackFit.expand,
        children: [
          TransparencyCheckerboard(cellSize: widget.cellSize),
          Image(image: widget.image, fit: widget.fit),
        ],
      ),
    );
  }
}
