import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

class SvgIcon extends StatelessWidget {
  final String assetName;
  final double size;
  final Color? color;

  const SvgIcon(this.assetName, {super.key, this.size = 24.0, this.color});

  @override
  Widget build(BuildContext context) {
    // 如果没有硬性指定 color，则默认跟随当前主题文字主色
    final defaultColor = Theme.of(context).colorScheme.onSurface;

    return SvgPicture.asset(
      assetName,
      width: size,
      height: size,
      colorFilter: ColorFilter.mode(color ?? defaultColor, BlendMode.srcIn),
    );
  }
}
