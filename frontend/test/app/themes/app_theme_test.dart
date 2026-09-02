import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/shared/widgets/chat_markdown_style_sheet.dart';

void main() {
  test('apple platforms keep default text font resolution', () {
    debugDefaultTargetPlatformOverride = TargetPlatform.iOS;
    try {
      final style = AppTheme.applyTextFont(const TextStyle(fontSize: 14));
      final theme = AppTheme.lightTheme;

      expect(style.fontFamily, isNull);
      expect(style.fontFamilyFallback, isNull);
      expect(AppTheme.textFontFamilyOrNull, isNull);
      expect(AppTheme.textFontFallbackOrNull, isNull);
      expect(theme.textTheme.bodyMedium?.fontFamilyFallback, isNull);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  test('windows pins the Simplified-Chinese UI font as the primary family', () {
    debugDefaultTargetPlatformOverride = TargetPlatform.windows;
    try {
      final theme = AppTheme.lightTheme;

      expect(AppTheme.textFontFamilyOrNull, 'Microsoft YaHei UI');
      expect(theme.textTheme.bodyMedium?.fontFamily, 'Microsoft YaHei UI');
      expect(AppTheme.textFontFallbackOrNull, contains('微软雅黑'));
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  test('non-apple platforms keep default text font resolution', () {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    try {
      final style = AppTheme.applyTextFont(const TextStyle(fontSize: 14));
      final theme = AppTheme.lightTheme;

      expect(style.fontFamily, isNull);
      expect(style.fontFamilyFallback, isNull);
      expect(AppTheme.textFontFamilyOrNull, isNull);
      expect(AppTheme.textFontFallbackOrNull, isNull);
      expect(theme.textTheme.bodyMedium?.fontFamilyFallback, isNull);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  test('markdown text styles inherit default platform fonts', () {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    try {
      final styleSheet = ChatMarkdownStyleSheet.fromTheme(
        theme: AppTheme.lightTheme,
        textColor: AppTheme.lightTextPrimary,
        isMine: false,
      );

      expect(styleSheet.paragraphStyle.fontFamily, isNull);
      expect(styleSheet.paragraphStyle.fontFamilyFallback, isNull);
      expect(styleSheet.preTextStyle.fontFamilyFallback, isNull);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  test('markdown heading scale stays compact for chat readability', () {
    final styleSheet = ChatMarkdownStyleSheet.fromTheme(
      theme: AppTheme.lightTheme,
      textColor: AppTheme.lightTextPrimary,
      isMine: false,
    );

    expect(styleSheet.headingStyle(1).fontSize, 20);
    expect(styleSheet.headingStyle(2).fontSize, 18);
    expect(styleSheet.headingStyle(3).fontSize, 17);
    expect(styleSheet.headingStyle(4).fontSize, 16);
    expect(styleSheet.headingStyle(5).fontSize, 15);
    expect(styleSheet.headingStyle(6).fontSize, 14);
    expect(styleSheet.headingStyle(1).fontWeight, FontWeight.w700);
    expect(styleSheet.headingStyle(3).fontWeight, FontWeight.w600);
    expect(styleSheet.headingStyle(1).height, 1.32);
  });

  test('dark theme inline code on light chip uses dark readable text', () {
    final styleSheet = ChatMarkdownStyleSheet.fromTheme(
      theme: AppTheme.darkTheme,
      textColor: AppTheme.lightTextPrimary,
      isMine: false,
    );

    expect(styleSheet.inlineCodeStyle.color, AppTheme.lightTextPrimary);
  });

  test('readable text color switches with surface brightness', () {
    expect(
      AppTheme.readableTextColorForBackground(Colors.white),
      AppTheme.lightTextPrimary,
    );
    expect(
      AppTheme.readableTextColorForBackground(Colors.black),
      AppTheme.darkTextPrimary,
    );
  });
}
