import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/platform/desktop/desktop_window_service.dart';

void main() {
  group('DesktopWindowService window size persistence', () {
    test('历史 0x0 配置回退到默认窗口尺寸', () {
      expect(
        DesktopWindowService.sanitizeRestoredSize(0, 0),
        const Size(1280, 800),
      );
    });

    test('缺失或非有限配置回退到默认窗口尺寸', () {
      expect(
        DesktopWindowService.sanitizeRestoredSize(null, 720),
        const Size(1280, 800),
      );
      expect(
        DesktopWindowService.sanitizeRestoredSize(double.nan, 720),
        const Size(1280, 800),
      );
      expect(
        DesktopWindowService.sanitizeRestoredSize(1280, double.infinity),
        const Size(1280, 800),
      );
    });

    test('小于最小值的历史配置会被钳制', () {
      expect(
        DesktopWindowService.sanitizeRestoredSize(320, 480),
        const Size(400, 600),
      );
      expect(
        DesktopWindowService.sanitizeRestoredSize(900, 480),
        const Size(900, 600),
      );
    });

    test('合法历史配置保持不变', () {
      expect(
        DesktopWindowService.sanitizeRestoredSize(1440, 900),
        const Size(1440, 900),
      );
    });

    test('拒绝持久化零尺寸、非有限尺寸和小于最小值的尺寸', () {
      expect(DesktopWindowService.isPersistableSize(Size.zero), isFalse);
      expect(
        DesktopWindowService.isPersistableSize(
          const Size(double.infinity, 800),
        ),
        isFalse,
      );
      expect(
        DesktopWindowService.isPersistableSize(const Size(399, 600)),
        isFalse,
      );
      expect(
        DesktopWindowService.isPersistableSize(const Size(400, 600)),
        isTrue,
      );
    });
  });
}
