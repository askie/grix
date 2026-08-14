import 'package:flutter/material.dart';

/// 统一设计色板（单一事实来源）。
///
/// 对齐 Grix 产品品牌：龙虾红主色 + 暖中性面 + 一组语义功能色。
/// 所有组件应通过 Theme 或本类引用颜色，避免散落硬编码。
class AppPalette {
  AppPalette._();

  // ---- 品牌主色 ----
  static const Color brand = Color(0xFFE63946);
  static const Color brandDark = Color(0xFFA51D2A);
  static const Color brandLight = Color(0xFFFF5D6A);

  /// 主色柔和底（选中态背景、主色标签底）。
  static const Color brandSoft = Color(0xFFFBE7E9);

  // ---- 中性面（暖色调）----
  static const Color bg = Color(0xFFFBF7EE); // 应用背景
  static const Color surface = Color(0xFFFFFFFF); // 卡片/内容面
  static const Color surfaceAlt = Color(0xFFFFFCF5); // 侧栏/次级面
  static const Color border = Color(0xFFECE3D2); // 细边框
  static const Color divider = Color(0xFFEFE7D8);

  // ---- 文本 ----
  static const Color textPrimary = Color(0xFF221C12);
  static const Color textSecondary = Color(0xFF6F6149);
  static const Color textTertiary = Color(0xFF9A8A6E);

  // ---- 语义功能色（含柔和底，用于状态标签）----
  static const Color success = Color(0xFF1F9D63);
  static const Color successSoft = Color(0xFFE3F3EA);
  static const Color warning = Color(0xFFC8881A);
  static const Color warningSoft = Color(0xFFF8EFD9);
  static const Color danger = Color(0xFFD64949);
  static const Color dangerSoft = Color(0xFFFBE6E6);
  static const Color info = Color(0xFF3B6FE0);
  static const Color infoSoft = Color(0xFFE6EDFB);
}

/// 统一圆角令牌。
class AppRadius {
  AppRadius._();
  static const double card = 14;
  static const double button = 10;
  static const double input = 10;
  static const double chip = 8;
}

/// 状态语义类型，配合 [AppStatusStyle] 取统一配色。
enum StatusKind { neutral, success, warning, danger, info }

/// 状态标签配色（前景 + 柔和底），全应用统一。
class AppStatusStyle {
  AppStatusStyle._();

  static Color foreground(StatusKind kind) {
    switch (kind) {
      case StatusKind.success:
        return AppPalette.success;
      case StatusKind.warning:
        return AppPalette.warning;
      case StatusKind.danger:
        return AppPalette.danger;
      case StatusKind.info:
        return AppPalette.info;
      case StatusKind.neutral:
        return AppPalette.textSecondary;
    }
  }

  static Color background(StatusKind kind) {
    switch (kind) {
      case StatusKind.success:
        return AppPalette.successSoft;
      case StatusKind.warning:
        return AppPalette.warningSoft;
      case StatusKind.danger:
        return AppPalette.dangerSoft;
      case StatusKind.info:
        return AppPalette.infoSoft;
      case StatusKind.neutral:
        return AppPalette.border;
    }
  }
}
