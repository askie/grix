import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/profile/widgets/widget_site_form_dialog.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(() {
    Get.reset();
  });

  Future<WidgetSiteFormResult?> pumpDialogAndRun(
    WidgetTester tester, {
    WidgetSiteModel? initial,
    required Future<void> Function(WidgetTester tester) interact,
  }) async {
    WidgetSiteFormResult? result;
    tester.view.physicalSize = const Size(1200, 2000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('en', 'US'),
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: ElevatedButton(
                onPressed: () async {
                  result = await showDialog<WidgetSiteFormResult>(
                    context: context,
                    builder: (_) => WidgetSiteFormDialog(
                      initial: initial,
                      confirmLabel: 'OK',
                    ),
                  );
                },
                child: const Text('open'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    await interact(tester);
    return result;
  }

  Future<void> fillRequiredAndConfirm(WidgetTester tester) async {
    await tester.enterText(find.byType(TextField).at(0), 'My Site');
    await tester.enterText(find.byType(TextField).at(1), 'https://example.com');
    await tester.tap(find.text('OK'));
    await tester.pumpAndSettle();
  }

  WidgetSiteModel siteWith(String themeColor) => WidgetSiteModel(
    id: '1',
    siteKey: 'sk',
    siteName: 'Site',
    allowedOrigins: const ['https://example.com'],
    status: 1,
    createdAt: 0,
    updatedAt: 0,
    displayConfig: WidgetDisplayConfig(themeColor: themeColor),
  );

  testWidgets('主题色不再提供文本输入，展示预设色板且默认选中默认主题色', (tester) async {
    final result = await pumpDialogAndRun(
      tester,
      interact: fillRequiredAndConfirm,
    );

    expect(result, isNotNull);
    // 未选择时提交默认主题色，而不是空字符串
    expect(result!.displayConfig.themeColor, '#0f766e');
  });

  testWidgets('点选色板中的其他颜色后提交对应色值', (tester) async {
    final result = await pumpDialogAndRun(
      tester,
      interact: (tester) async {
        await tester.ensureVisible(
          find.byKey(const ValueKey('theme_color_#e11d48')),
        );
        await tester.tap(find.byKey(const ValueKey('theme_color_#e11d48')));
        await tester.pump();
        await fillRequiredAndConfirm(tester);
      },
    );

    expect(result, isNotNull);
    expect(result!.displayConfig.themeColor, '#e11d48');
  });

  testWidgets('编辑态：历史自定义色不在预设中时追加为可选项且不丢失', (tester) async {
    final result = await pumpDialogAndRun(
      tester,
      initial: siteWith('#123456'),
      interact: (tester) async {
        // 自定义色作为额外色块出现且保持选中
        expect(
          find.byKey(const ValueKey('theme_color_#123456')),
          findsOneWidget,
        );
        await tester.tap(find.text('OK'));
        await tester.pumpAndSettle();
      },
    );

    expect(result, isNotNull);
    expect(result!.displayConfig.themeColor, '#123456');
  });

  testWidgets('编辑态：历史三位缩写 hex 归一化为六位并保持选中', (tester) async {
    final result = await pumpDialogAndRun(
      tester,
      initial: siteWith('abc'),
      interact: (tester) async {
        expect(
          find.byKey(const ValueKey('theme_color_#aabbcc')),
          findsOneWidget,
        );
        await tester.tap(find.text('OK'));
        await tester.pumpAndSettle();
      },
    );

    expect(result, isNotNull);
    expect(result!.displayConfig.themeColor, '#aabbcc');
  });

  testWidgets('编辑态：历史非法色值回落为默认主题色', (tester) async {
    final result = await pumpDialogAndRun(
      tester,
      initial: siteWith('not-a-color'),
      interact: (tester) async {
        await tester.tap(find.text('OK'));
        await tester.pumpAndSettle();
      },
    );

    expect(result, isNotNull);
    expect(result!.displayConfig.themeColor, '#0f766e');
  });
}
