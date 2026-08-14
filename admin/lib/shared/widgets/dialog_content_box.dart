import 'package:flutter/material.dart';

/// AlertDialog content 的自适应宽度容器：桌面/平板保持定宽卡片观感，
/// 手机窄屏下改为贴合屏幕宽度，避免固定宽度在窄屏被两侧默认边距压得表单拥挤。
class DialogContentBox extends StatelessWidget {
  const DialogContentBox({super.key, required this.child, this.maxWidth = 420});

  final Widget child;
  final double maxWidth;

  @override
  Widget build(BuildContext context) {
    final screenWidth = MediaQuery.of(context).size.width;
    final width = screenWidth < maxWidth + 96 ? screenWidth - 96 : maxWidth;
    return SizedBox(width: width, child: child);
  }
}

/// 配合 [DialogContentBox] 使用的窄屏 insetPadding：把 AlertDialog 默认的
/// 左右 40 缩小到 16，把腾出的空间让给表单内容。
const kDialogInsetPadding = EdgeInsets.symmetric(horizontal: 16, vertical: 24);
