import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

class AppTheme {
  static const String _webUiChineseFontFamily = 'GrixUiZh';

  // Grix 龙虾红主色系
  static const Color primaryColor = Color(0xFFE63946);
  static const Color primaryLight = Color(0xFFFF5D6A);
  static const Color primaryDark = Color(0xFFA51D2A);

  // 功能色
  static const Color successColor = Color(0xFF1F9D63);
  static const Color warningColor = Color(0xFFD4A11E);
  static const Color errorColor = Color(0xFFD64949);
  static const Color infoColor = Color(0xFFE63946);
  static const Color unreadBadgeColor = Color(0xFF1473E6);

  // 浅色 surfaces
  static const Color lightBg = Color(0xFFFDF9EF);
  static const Color lightCard = Color(0xFFFFFCF5);
  static const Color lightDivider = Color(0xFFE8DDC8);
  static const Color lightInput = Color(0xFFF7F0E0);
  static const Color lightTextPrimary = Color(0xFF2A2214);
  static const Color lightTextSecondary = Color(0xFF7A6641);

  // 深色 surfaces
  static const Color darkBg = Color(0xFF181208);
  static const Color darkCard = Color(0xFF241B0D);
  static const Color darkDivider = Color(0xFF4B3B1F);
  static const Color darkInput = Color(0xFF302311);
  static const Color darkTextPrimary = Color(0xFFF8F2E7);
  static const Color darkTextSecondary = Color(0xFFC2B397);

  // 红色系头像颜色组
  static const List<Color> avatarColors = [
    Color(0xFFE63946),
    Color(0xFFD62828),
    Color(0xFFF77F00),
    Color(0xFFFCBF49),
    Color(0xFF8B0000),
    Color(0xFFFF4D00),
    Color(0xFFE01E37),
    Color(0xFFC71F37),
  ];

  /// Simplified-Chinese system fonts for desktop targets. Without an explicit
  /// fallback, Skia resolves Han glyphs through whichever CJK font the OS
  /// lists first, which is often a Japanese or Korean face with different
  /// glyph shapes (e.g. 经/経).
  static const List<String> _windowsChineseFontFallback = <String>[
    'Microsoft YaHei UI',
    '微软雅黑',
    'Microsoft YaHei',
    'SimHei',
    'SimSun',
  ];
  static const List<String> _macChineseFontFallback = <String>[
    'PingFang SC',
    'Hiragino Sans GB',
    'Heiti SC',
  ];
  static const List<String> _linuxChineseFontFallback = <String>[
    'Noto Sans CJK SC',
    'Noto Sans SC',
    'Source Han Sans SC',
    'WenQuanYi Micro Hei',
  ];

  static List<String>? get textFontFallbackOrNull {
    if (kIsWeb) {
      // Reuse the app-owned Chinese UI font declared in web/index.html.
      return const <String>[_webUiChineseFontFamily];
    }
    switch (defaultTargetPlatform) {
      case TargetPlatform.windows:
        return _windowsChineseFontFallback;
      case TargetPlatform.macOS:
        return _macChineseFontFallback;
      case TargetPlatform.linux:
        return _linuxChineseFontFallback;
      case TargetPlatform.android:
      case TargetPlatform.iOS:
      case TargetPlatform.fuchsia:
        break;
    }
    return null;
  }

  /// Windows 上 Skia 对 CJK 的按名回退不稳定（会落到日文/繁体风格字形），
  /// 直接把简体 Windows 的系统界面字体设为主字体；其余平台沿用系统默认。
  static const String _windowsUiFontFamily = 'Microsoft YaHei UI';

  static String? get textFontFamilyOrNull {
    if (kIsWeb) return null;
    if (defaultTargetPlatform == TargetPlatform.windows) {
      return _windowsUiFontFamily;
    }
    return null;
  }

  static TextStyle applyTextFont(TextStyle style) => style;

  static TextTheme applyTextTheme(TextTheme theme) => theme;

  static Color getAvatarColor(String id) {
    final hash = id.hashCode.abs();
    return avatarColors[hash % avatarColors.length];
  }

  static double listAvatarCornerRadius(double size) => 0;

  // 按钮尺寸规范（Material 3）
  static const double btnHeightSmall = 32;
  static const double btnHeightMedium = 40;
  static const double btnHeightLarge = 48;

  static ButtonStyle elevatedBtnStyle({double? height}) {
    return ElevatedButton.styleFrom(
      minimumSize: Size.fromHeight(height ?? btnHeightMedium),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
    );
  }

  static ButtonStyle outlinedBtnStyle({double? height}) {
    return OutlinedButton.styleFrom(
      minimumSize: Size.fromHeight(height ?? btnHeightMedium),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
    );
  }

  static Color readableTextColorForBackground(Color backgroundColor) {
    final backgroundBrightness = ThemeData.estimateBrightnessForColor(
      backgroundColor,
    );
    return backgroundBrightness == Brightness.light
        ? lightTextPrimary
        : darkTextPrimary;
  }

  /// 卡片内成功态强调色（用于状态文字/图标）。
  ///
  /// 深色卡片背景较暗（如 [darkCard]），固定使用 Material `shade700`
  /// 时对比度不足 4.5:1，故深色模式下改用更亮的 `shade400`。
  static Color statusSuccessColor(Brightness brightness) {
    return brightness == Brightness.dark
        ? Colors.green.shade400
        : Colors.green.shade700;
  }

  /// 卡片内警示态强调色（用于状态文字/图标），原因同 [statusSuccessColor]。
  static Color statusWarningColor(Brightness brightness) {
    return brightness == Brightness.dark
        ? Colors.orange.shade400
        : Colors.orange.shade700;
  }

  /// 卡片内信息态强调色（用于状态文字/图标），原因同 [statusSuccessColor]。
  static Color statusInfoColor(Brightness brightness) {
    return brightness == Brightness.dark
        ? Colors.blue.shade400
        : Colors.blue.shade700;
  }

  static TextTheme _imTextTheme(Color primaryText, Color secondaryText) {
    return TextTheme(
      titleLarge: TextStyle(
        fontSize: 17,
        fontWeight: FontWeight.w700,
        letterSpacing: -0.1,
        color: primaryText,
      ),
      titleMedium: TextStyle(
        fontSize: 15,
        fontWeight: FontWeight.w600,
        color: primaryText,
      ),
      titleSmall: TextStyle(
        fontSize: 14,
        fontWeight: FontWeight.w600,
        color: primaryText,
      ),
      bodyLarge: TextStyle(fontSize: 14, height: 1.42, color: primaryText),
      bodyMedium: TextStyle(fontSize: 13, height: 1.38, color: primaryText),
      bodySmall: TextStyle(fontSize: 12, height: 1.33, color: secondaryText),
      labelLarge: TextStyle(
        fontSize: 13,
        fontWeight: FontWeight.w600,
        color: primaryText,
      ),
      labelMedium: TextStyle(
        fontSize: 12,
        fontWeight: FontWeight.w500,
        color: secondaryText,
      ),
      labelSmall: TextStyle(
        fontSize: 11,
        fontWeight: FontWeight.w500,
        color: secondaryText,
      ),
    );
  }

  static ThemeData get lightTheme => _buildTheme(
    brightness: Brightness.light,
    scaffoldBackgroundColor: lightBg,
    cardColor: lightCard,
    dividerColor: lightDivider,
    inputFillColor: lightInput,
    primaryTextColor: lightTextPrimary,
    secondaryTextColor: lightTextSecondary,
    selectedNavigationItemColor: primaryColor,
    unselectedNavigationItemColor: const Color(0xFFB78A8A),
    cursorColor: primaryColor,
  );

  static ThemeData get darkTheme => _buildTheme(
    brightness: Brightness.dark,
    scaffoldBackgroundColor: darkBg,
    cardColor: darkCard,
    dividerColor: darkDivider,
    inputFillColor: darkInput,
    primaryTextColor: darkTextPrimary,
    secondaryTextColor: darkTextSecondary,
    selectedNavigationItemColor: primaryLight,
    unselectedNavigationItemColor: const Color(0xFF8A5A5A),
    cursorColor: primaryLight,
  );

  static ThemeData _buildTheme({
    required Brightness brightness,
    required Color scaffoldBackgroundColor,
    required Color cardColor,
    required Color dividerColor,
    required Color inputFillColor,
    required Color primaryTextColor,
    required Color secondaryTextColor,
    required Color selectedNavigationItemColor,
    required Color unselectedNavigationItemColor,
    required Color cursorColor,
  }) {
    final isDark = brightness == Brightness.dark;
    return ThemeData(
      useMaterial3: true,
      brightness: brightness,
      primaryColor: primaryColor,
      fontFamily: textFontFamilyOrNull,
      fontFamilyFallback: textFontFallbackOrNull,
      scaffoldBackgroundColor: scaffoldBackgroundColor,
      textTheme: applyTextTheme(
        _imTextTheme(primaryTextColor, secondaryTextColor),
      ),
      colorScheme: isDark ? _darkColorScheme : _lightColorScheme,
      appBarTheme: AppBarTheme(
        backgroundColor: cardColor,
        foregroundColor: primaryTextColor,
        elevation: 0,
        scrolledUnderElevation: 0.5,
        surfaceTintColor: Colors.transparent,
        toolbarHeight: 44,
      ),
      bottomNavigationBarTheme: BottomNavigationBarThemeData(
        backgroundColor: cardColor,
        selectedItemColor: selectedNavigationItemColor,
        unselectedItemColor: unselectedNavigationItemColor,
        type: BottomNavigationBarType.fixed,
        elevation: 8,
        selectedLabelStyle: const TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w600,
        ),
        unselectedLabelStyle: const TextStyle(fontSize: 11),
      ),
      navigationRailTheme: NavigationRailThemeData(
        backgroundColor: cardColor,
        minWidth: 128,
        minExtendedWidth: 128,
        selectedIconTheme: IconThemeData(color: selectedNavigationItemColor),
        unselectedIconTheme: IconThemeData(
          color: unselectedNavigationItemColor,
        ),
        selectedLabelTextStyle: TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w600,
          color: selectedNavigationItemColor,
        ),
        unselectedLabelTextStyle: TextStyle(
          fontSize: 11,
          color: unselectedNavigationItemColor,
        ),
      ),
      dividerTheme: DividerThemeData(
        color: dividerColor,
        thickness: 0.5,
        space: 0,
      ),
      cardTheme: CardThemeData(
        color: cardColor,
        elevation: 0,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
      listTileTheme: const ListTileThemeData(
        contentPadding: EdgeInsets.symmetric(horizontal: 14, vertical: 2),
      ),
      textSelectionTheme: TextSelectionThemeData(
        cursorColor: cursorColor,
        selectionColor: const Color(0x66E63946),
        selectionHandleColor: cursorColor,
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: inputFillColor,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: BorderSide.none,
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: BorderSide.none,
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: const BorderSide(color: primaryColor, width: 1.5),
        ),
        hintStyle: TextStyle(fontSize: 14, color: secondaryTextColor),
        contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
      ),
      elevatedButtonTheme: ElevatedButtonThemeData(style: elevatedBtnStyle()),
      outlinedButtonTheme: OutlinedButtonThemeData(style: outlinedBtnStyle()),
    );
  }

  // ----------------------------------------------------------------------
  // ColorScheme 完整定义（Material3）
  //
  // 设计原则：
  // 1. `secondary` 在本项目内沿用"次级文字色"语义（暗棕系），不当作次级强调块色，
  //    以兼容代码中大量 `colorScheme.secondary` 直接当文字色使用的既有用法。
  // 2. 真正用于次级块状控件（FilledButton.tonal、Chip、Card 等）的配色，
  //    走独立 token：`secondaryContainer / onSecondaryContainer`。
  // 3. 所有 (容器, on前景) 配对必须满足 WCAG AA（≥4.5:1）对比度，杜绝
  //    "棕底 + 黑字看不清"这类组合。
  // 4. 全部 M3 关键 token 显式定义，不依赖 Flutter 默认 baseline 调色板，
  //    避免被默认紫色调与龙虾红 surfaceTint 合成出违和的颜色。
  // ----------------------------------------------------------------------

  static const ColorScheme _lightColorScheme = ColorScheme(
    brightness: Brightness.light,
    // 主色：龙虾红
    primary: primaryColor,
    onPrimary: Colors.white,
    primaryContainer: Color(0xFFFFD9DC), // 浅粉，与红主色协调
    onPrimaryContainer: Color(0xFF6E0F18), // 深红字
    // 次级色：保留"次级文字色"语义（暗棕灰），不作块色
    secondary: lightTextSecondary,
    onSecondary: Colors.white,
    secondaryContainer: Color(0xFFF5E6C8), // 蜂蜜米底，与米黄主背景协调
    onSecondaryContainer: Color(0xFF3A2B14), // 深咖字，对比 ≈17:1
    // 第三级强调：砖橙系，用于状态/装饰性强调
    tertiary: Color(0xFFB0571F),
    onTertiary: Colors.white,
    tertiaryContainer: Color(0xFFFFE0CC),
    onTertiaryContainer: Color(0xFF4D1F00),
    // 错误
    error: errorColor,
    onError: Colors.white,
    errorContainer: Color(0xFFFFDAD6),
    onErrorContainer: Color(0xFF410002),
    // 表面与背景
    surface: lightCard,
    onSurface: lightTextPrimary,
    surfaceContainerLowest: Color(0xFFFFFCF5),
    surfaceContainerLow: Color(0xFFFCF7EA),
    surfaceContainer: Color(0xFFFAF4E5),
    surfaceContainerHigh: Color(0xFFF7EFDD),
    surfaceContainerHighest: Color(0xFFF1E7CF),
    surfaceTint: primaryColor,
    // 次级表面（onSurfaceVariant 直接服务于 surfaceContainerHighest 上的次级文字/图标）
    onSurfaceVariant: lightTextSecondary, // 与 secondary 同源，保持一致
    // 边框
    outline: lightDivider,
    outlineVariant: Color(0xFFD8C8A6),
    // 反相
    inverseSurface: Color(0xFF2A2214),
    onInverseSurface: lightBg,
    inversePrimary: Color(0xFFFF8A95),
    // 阴影/遮罩
    shadow: Colors.black,
    scrim: Colors.black,
  );

  static const ColorScheme _darkColorScheme = ColorScheme(
    brightness: Brightness.dark,
    // 主色：龙虾红，深色模式下保持主调
    primary: primaryColor,
    onPrimary: Colors.white,
    primaryContainer: Color(0xFF6E0F18),
    onPrimaryContainer: Color(0xFFFFD9DC),
    // 次级色：深色模式下"次级文字色"为亮米色
    secondary: darkTextSecondary,
    onSecondary: Color(0xFF1F1709),
    secondaryContainer: Color(0xFF4A3A1F), // 深咖底
    onSecondaryContainer: Color(0xFFF5E6C8), // 米色字，对比 ≈11:1
    // 第三级强调
    tertiary: Color(0xFFFFB077),
    onTertiary: Color(0xFF3A1A00),
    tertiaryContainer: Color(0xFF5A2A0E),
    onTertiaryContainer: Color(0xFFFFE0CC),
    // 错误
    error: errorColor,
    onError: Colors.white,
    errorContainer: Color(0xFF93000A),
    onErrorContainer: Color(0xFFFFDAD6),
    // 表面与背景
    surface: darkCard,
    onSurface: darkTextPrimary,
    surfaceContainerLowest: Color(0xFF120D07),
    surfaceContainerLow: Color(0xFF1A130A),
    surfaceContainer: Color(0xFF1F1709),
    surfaceContainerHigh: Color(0xFF2A2010),
    surfaceContainerHighest: Color(0xFF332811),
    surfaceTint: primaryColor,
    // 次级表面（onSurfaceVariant 直接服务于 surfaceContainerHighest 上的次级文字/图标）
    onSurfaceVariant: darkTextSecondary,
    // 边框
    outline: darkDivider,
    outlineVariant: Color(0xFF6B5630),
    // 反相
    inverseSurface: lightBg,
    onInverseSurface: Color(0xFF2A2214),
    inversePrimary: Color(0xFFA51D2A),
    // 阴影/遮罩
    shadow: Colors.black,
    scrim: Colors.black,
  );
}
