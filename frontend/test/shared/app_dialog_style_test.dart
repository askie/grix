import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/app_dialog_style.dart';

/// 在指定逻辑尺寸下捕获 BuildContext，用于验证响应式约束。
Future<BuildContext> _contextWithSize(WidgetTester tester, Size size) async {
  late BuildContext captured;
  await tester.pumpWidget(
    MediaQuery(
      data: MediaQueryData(size: size),
      child: Builder(
        builder: (context) {
          captured = context;
          return const SizedBox.shrink();
        },
      ),
    ),
  );
  return captured;
}

void main() {
  group('AppDialogSize', () {
    test('档位最大宽度取值固定', () {
      expect(AppDialogSize.compact.maxWidth, 360);
      expect(AppDialogSize.standard.maxWidth, 480);
      expect(AppDialogSize.wide.maxWidth, 640);
    });
  });

  group('resolveDialogConstraints', () {
    testWidgets('紧凑端取屏宽减两侧外边距', (tester) async {
      final context = await _contextWithSize(tester, const Size(400, 800));
      final c = resolveDialogConstraints(context, size: AppDialogSize.wide);
      expect(c.maxWidth, 400 - kDialogMobileMargin * 2);
      expect(c.maxHeight, closeTo(800 * 0.8, 0.001));
      expect(isCompactDialogWidth(context), isTrue);
    });

    testWidgets('宽屏端按档位封顶', (tester) async {
      final context = await _contextWithSize(tester, const Size(1200, 900));
      expect(
        resolveDialogConstraints(
          context,
          size: AppDialogSize.standard,
        ).maxWidth,
        480,
      );
      expect(
        resolveDialogConstraints(context, size: AppDialogSize.wide).maxWidth,
        640,
      );
      expect(isCompactDialogWidth(context), isFalse);
    });

    testWidgets('断点边界以下归为紧凑', (tester) async {
      final context = await _contextWithSize(tester, const Size(520, 700));
      final c = resolveDialogConstraints(context);
      expect(c.maxWidth, 520 - kDialogMobileMargin * 2);
    });
  });

  group('AppDialogTheme', () {
    testWidgets('统一标题字号与字重', (tester) async {
      late BuildContext dialogContext;
      await tester.pumpWidget(
        MaterialApp(
          home: AppDialogTheme(
            child: Builder(
              builder: (context) {
                dialogContext = context;
                return const SizedBox.shrink();
              },
            ),
          ),
        ),
      );
      final titleStyle = DialogTheme.of(dialogContext).titleTextStyle;
      expect(titleStyle?.fontSize, kDialogTitleFontSize);
      expect(titleStyle?.fontWeight, kDialogTitleFontWeight);
    });
  });
}
