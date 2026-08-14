import 'dart:ui';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/capture_export_pixel_ratio.dart';

void main() {
  group('resolveCaptureExportPixelRatio', () {
    test('低像素比显示器也按最低超采样倍率输出，保证文字清晰', () {
      final ratio = resolveCaptureExportPixelRatio(
        logicalSize: const Size(400, 300),
        devicePixelRatio: 1.0,
      );
      expect(ratio, 3.0);
    });

    test('高分屏跟随更高的设备像素比', () {
      final ratio = resolveCaptureExportPixelRatio(
        logicalSize: const Size(400, 300),
        devicePixelRatio: 3.5,
      );
      expect(ratio, 3.5);
    });

    test('大图按最长边封顶，避免超过纹理上限', () {
      // 最长边 4000，3 倍超采样会到 12000，超过默认 8192 上限。
      final ratio = resolveCaptureExportPixelRatio(
        logicalSize: const Size(4000, 1000),
        devicePixelRatio: 1.0,
      );
      expect(ratio, closeTo(8192.0 / 4000.0, 1e-9));
      // 封顶后输出最长边恰好不超过上限。
      expect(4000 * ratio, lessThanOrEqualTo(8192.0));
    });

    test('封顶不会把图缩小到 1 倍以下', () {
      // 极端超大图：最长边已超过上限，比例会被压到 <1，但下限保护为 1。
      final ratio = resolveCaptureExportPixelRatio(
        logicalSize: const Size(20000, 1000),
        devicePixelRatio: 1.0,
      );
      expect(ratio, 1.0);
    });

    test('零尺寸时回退到期望密度且不为 0', () {
      final ratio = resolveCaptureExportPixelRatio(
        logicalSize: Size.zero,
        devicePixelRatio: 1.0,
      );
      expect(ratio, 3.0);
    });

    test('可自定义最低超采样与最长边上限', () {
      final ratio = resolveCaptureExportPixelRatio(
        logicalSize: const Size(1000, 500),
        devicePixelRatio: 1.0,
        minSupersample: 2.0,
        maxOutputEdge: 4096.0,
      );
      // 2 倍 → 2000，未触顶。
      expect(ratio, 2.0);
    });
  });
}
