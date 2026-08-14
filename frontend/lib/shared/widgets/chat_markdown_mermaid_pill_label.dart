import 'dart:math' as math;

import 'package:flutter/material.dart';

class ChatMarkdownMermaidPillLabel extends StatelessWidget {
  const ChatMarkdownMermaidPillLabel({
    super.key,
    required this.text,
    required this.style,
    required this.fillColor,
    required this.borderColor,
    this.horizontalPadding = 4,
    this.verticalPadding = 2,
    this.minWidth,
    this.maxWidth,
    this.maxLines,
    this.fontSizeDelta = -1,
    this.minFontSize = 10,
  });

  final String text;
  final TextStyle style;
  final Color fillColor;
  final Color borderColor;
  final double horizontalPadding;
  final double verticalPadding;
  final double? minWidth;
  final double? maxWidth;
  final int? maxLines;
  final double fontSizeDelta;
  final double minFontSize;

  @override
  Widget build(BuildContext context) {
    final baseFontSize = style.fontSize ?? 12;
    final effectiveStyle = style.copyWith(
      fontSize: math.max(minFontSize, baseFontSize + fontSizeDelta),
    );
    final content = Container(
      padding: EdgeInsets.symmetric(
        horizontal: horizontalPadding,
        vertical: verticalPadding,
      ),
      decoration: BoxDecoration(
        color: fillColor,
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: borderColor),
      ),
      alignment: Alignment.center,
      child: Text(
        text,
        style: effectiveStyle,
        textAlign: TextAlign.center,
        maxLines: maxLines,
        softWrap: true,
        // 边/子图标签的尺寸由布局引擎用未缩放的 TextPainter 预先测量并固定，
        // 渲染端同样关闭系统字体缩放，避免放大字号导致标签换行/溢出被裁切。
        textScaler: TextScaler.noScaling,
      ),
    );

    if (minWidth == null && maxWidth == null) {
      return content;
    }
    return ConstrainedBox(
      constraints: BoxConstraints(
        minWidth: minWidth ?? 0,
        maxWidth: maxWidth ?? double.infinity,
      ),
      child: content,
    );
  }
}
