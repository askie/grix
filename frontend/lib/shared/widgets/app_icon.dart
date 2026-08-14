import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';
import '../../app/themes/app_theme.dart';

/// 全局统一配置的 CSS 风格 SVG 图标组件
/// 替换所有遗留的 Unicode 字体图标 (如 Material Icons / Cupertino Icons)
class AppIcon extends StatelessWidget {
  final String svgPath;
  final double size;
  final Color? color;
  final BoxFit fit;

  const AppIcon(
    this.svgPath, {
    super.key,
    this.size = 24.0,
    this.color,
    this.fit = BoxFit.contain,
  });

  @override
  Widget build(BuildContext context) {
    final resolvedColor =
        color ?? IconTheme.of(context).color ?? AppTheme.primaryColor;

    return SvgPicture.asset(
      svgPath,
      width: size,
      height: size,
      colorFilter: ColorFilter.mode(
        resolvedColor,
        BlendMode.srcIn,
      ), // 默认跟随 IconTheme，避免在 Web 上依赖字体图标
      fit: fit,
    );
  }
}
