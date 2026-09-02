import 'package:flutter/material.dart';
import 'package:flutter_highlight/themes/a11y-dark.dart';
import 'package:flutter_highlight/themes/a11y-light.dart';

import '../../app/themes/app_theme.dart';

class ChatMarkdownStyleSheet {
  const ChatMarkdownStyleSheet({
    required this.paragraphStyle,
    required this.headingStyles,
    required this.linkStyle,
    required this.inlineCodeStyle,
    required this.preTextStyle,
    required this.preLabelStyle,
    required this.preStyleNotMatched,
    required this.preSyntaxTheme,
    required this.preBackgroundColor,
    required this.preDecoration,
    required this.prePadding,
    required this.preMargin,
    required this.tableBorderColor,
    required this.tableHeaderBackgroundColor,
    required this.tableCellPadding,
    required this.tableHeaderStyle,
    required this.tableBodyStyle,
    required this.blockquoteTextStyle,
    required this.blockquoteBorderColor,
    required this.blockquotePadding,
    required this.listMarkerStyle,
    required this.footnoteLabelStyle,
    required this.inlineMathStyle,
    required this.blockMathStyle,
    required this.dividerColor,
    required this.inlineCodeBackgroundColor,
    this.nestedListIndent = 10.0,
  });

  final TextStyle paragraphStyle;
  final Map<int, TextStyle> headingStyles;
  final TextStyle linkStyle;
  final TextStyle inlineCodeStyle;
  final TextStyle preTextStyle;
  final TextStyle preLabelStyle;
  final TextStyle preStyleNotMatched;
  final Map<String, TextStyle> preSyntaxTheme;
  final Color preBackgroundColor;
  final Decoration preDecoration;
  final EdgeInsetsGeometry prePadding;
  final EdgeInsetsGeometry preMargin;
  final Color tableBorderColor;
  final Color tableHeaderBackgroundColor;
  final EdgeInsetsGeometry tableCellPadding;
  final TextStyle tableHeaderStyle;
  final TextStyle tableBodyStyle;
  final TextStyle blockquoteTextStyle;
  final Color blockquoteBorderColor;
  final EdgeInsetsGeometry blockquotePadding;
  final TextStyle listMarkerStyle;
  final TextStyle footnoteLabelStyle;
  final TextStyle inlineMathStyle;
  final TextStyle blockMathStyle;
  final Color dividerColor;
  final Color inlineCodeBackgroundColor;
  final double nestedListIndent;

  factory ChatMarkdownStyleSheet.fromTheme({
    required ThemeData theme,
    required Color textColor,
    required bool isMine,
    double fontScale = 1.0,
  }) {
    final linkColor = isMine ? theme.colorScheme.onPrimary : theme.primaryColor;
    final inlineCodeBackgroundColor = isMine
        ? Colors.white.withValues(alpha: 0.18)
        : theme.primaryColor.withValues(alpha: 0.12);
    final inlineCodeColor = textColor;
    final isDark = theme.brightness == Brightness.dark;
    final preBackgroundColor = isDark ? AppTheme.darkCard : AppTheme.lightCard;
    final preTextColor = isDark
        ? AppTheme.darkTextPrimary
        : AppTheme.lightTextPrimary;
    final preSecondaryTextColor = isDark
        ? AppTheme.darkTextSecondary
        : AppTheme.lightTextSecondary;
    final preBorderColor =
        (isDark ? AppTheme.darkDivider : AppTheme.lightDivider).withValues(
          alpha: 0.92,
        );
    final preSyntaxTheme = isDark ? a11yDarkTheme : a11yLightTheme;
    final paragraphStyle = AppTheme.applyTextFont(
      TextStyle(color: textColor, fontSize: 14 * fontScale, height: 1.42),
    );
    final headingBase = paragraphStyle.copyWith(height: 1.32);

    return ChatMarkdownStyleSheet(
      paragraphStyle: paragraphStyle,
      headingStyles: <int, TextStyle>{
        1: headingBase.copyWith(
          fontSize: 20 * fontScale,
          fontWeight: FontWeight.w700,
        ),
        2: headingBase.copyWith(
          fontSize: 18 * fontScale,
          fontWeight: FontWeight.w700,
        ),
        3: headingBase.copyWith(
          fontSize: 17 * fontScale,
          fontWeight: FontWeight.w600,
        ),
        4: headingBase.copyWith(
          fontSize: 16 * fontScale,
          fontWeight: FontWeight.w600,
        ),
        5: headingBase.copyWith(
          fontSize: 15 * fontScale,
          fontWeight: FontWeight.w600,
        ),
        6: headingBase.copyWith(
          fontSize: 14 * fontScale,
          fontWeight: FontWeight.w600,
        ),
      },
      linkStyle: paragraphStyle.copyWith(
        color: linkColor,
        decoration: TextDecoration.underline,
      ),
      inlineCodeStyle: TextStyle(
        backgroundColor: inlineCodeBackgroundColor,
        color: inlineCodeColor,
        fontSize: 12.5 * fontScale,
        fontFamily: 'monospace',
        fontFamilyFallback: AppTheme.textFontFallbackOrNull,
      ),
      preTextStyle: TextStyle(
        color: preTextColor,
        fontSize: 13 * fontScale,
        fontFamily: 'monospace',
        fontFamilyFallback: AppTheme.textFontFallbackOrNull,
      ),
      preLabelStyle: AppTheme.applyTextFont(
        TextStyle(
          color: preSecondaryTextColor,
          fontSize: 12 * fontScale,
          fontWeight: FontWeight.w600,
        ),
      ),
      preStyleNotMatched: TextStyle(
        color: preTextColor,
        fontSize: 13 * fontScale,
        fontFamily: 'monospace',
        fontFamilyFallback: AppTheme.textFontFallbackOrNull,
      ),
      preSyntaxTheme: preSyntaxTheme,
      preBackgroundColor: preBackgroundColor,
      preDecoration: BoxDecoration(
        color: preBackgroundColor,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: preBorderColor),
      ),
      prePadding: const EdgeInsets.all(12),
      preMargin: EdgeInsets.zero,
      tableBorderColor: textColor.withValues(alpha: 0.3),
      tableHeaderBackgroundColor: isMine
          ? Colors.white.withValues(alpha: 0.08)
          : theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.65),
      tableCellPadding: const EdgeInsets.symmetric(
        horizontal: 12,
        vertical: 10,
      ),
      tableHeaderStyle: paragraphStyle.copyWith(fontWeight: FontWeight.w700),
      tableBodyStyle: paragraphStyle,
      blockquoteTextStyle: paragraphStyle.copyWith(
        color: textColor.withValues(alpha: 0.92),
      ),
      blockquoteBorderColor: textColor.withValues(alpha: 0.28),
      blockquotePadding: const EdgeInsets.only(left: 12),
      listMarkerStyle: paragraphStyle.copyWith(fontWeight: FontWeight.w600),
      footnoteLabelStyle: paragraphStyle.copyWith(
        fontSize: 12 * fontScale,
        fontWeight: FontWeight.w700,
        color: textColor.withValues(alpha: 0.76),
      ),
      inlineMathStyle: TextStyle(fontSize: 14 * fontScale, color: textColor),
      blockMathStyle: TextStyle(fontSize: 16 * fontScale, color: textColor),
      dividerColor: textColor.withValues(alpha: 0.2),
      inlineCodeBackgroundColor: inlineCodeBackgroundColor,
    );
  }

  TextStyle headingStyle(int level) =>
      headingStyles[level] ?? headingStyles[6] ?? paragraphStyle;
}
