import 'dart:math' as math;
import 'dart:ui';

/// 计算 [RenderRepaintBoundary.toImage] 导出位图时使用的像素密度（pixelRatio）。
///
/// 设计目标：
/// 1. 文字清晰：导出密度不依赖设备像素比，始终按较高的超采样倍率渲染，
///    这样在像素比为 1 的显示器（外接屏、桌面/Web 普通屏）上也能输出高清图片。
/// 2. 按图形自适应：根据图形自身的逻辑尺寸，限制输出图片的最长边像素数，
///    避免超过 GPU 纹理上限而导致截图失败、被降采样而变模糊。
///
/// 参数：
/// - [logicalSize]：被截取边界的逻辑尺寸（通常为整张画布大小）。
/// - [devicePixelRatio]：当前显示器的像素比，高分屏会被采纳为更高的密度。
/// - [minSupersample]：最低超采样倍率，保证细小文字也有足够像素。
/// - [maxOutputEdge]：输出图片允许的最长边像素数（GPU 纹理安全上限）。
double resolveCaptureExportPixelRatio({
  required Size logicalSize,
  required double devicePixelRatio,
  double minSupersample = 3.0,
  double maxOutputEdge = 8192.0,
}) {
  // 期望密度：至少 minSupersample 倍；高分屏则跟随设备像素比。
  var ratio = math.max(devicePixelRatio, minSupersample);

  // 按最长边封顶，规避超大图形超过 GPU 纹理上限。
  final longestEdge = math.max(logicalSize.width, logicalSize.height);
  if (longestEdge > 0) {
    final maxRatioByEdge = maxOutputEdge / longestEdge;
    if (ratio > maxRatioByEdge) {
      ratio = maxRatioByEdge;
    }
  }

  // 任何情况下都不缩小原图。
  return math.max(ratio, 1.0);
}
