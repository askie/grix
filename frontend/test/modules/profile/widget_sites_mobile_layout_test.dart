import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/profile/widgets/widget_site_form_dialog.dart';

// Reproduces the widget-sites edit form at iPhone width, with the REAL Chinese
// translations loaded, across normal and enlarged system font scales, to
// detect layout overflow that breaks the config UI on phones.
void main() {
  const phone = Size(375, 812); // iPhone X logical size

  const zh = <String, String>{
    'settings_widget_sites_edit_title': '编辑网站',
    'settings_widget_sites_create_title': '创建网站接入',
    'settings_widget_sites_name_label': '站点名称',
    'settings_widget_sites_origins_label': '允许域名（每行一个）',
    'settings_widget_sites_appearance': '外观与行为',
    'settings_widget_sites_title_label': '对话框标题',
    'settings_widget_sites_theme_color_label': '主题色（十六进制）',
    'settings_widget_sites_button_label': '悬浮按钮文字',
    'settings_widget_sites_welcome_label': '欢迎语',
    'settings_widget_sites_position_label': '悬浮位置',
    'settings_widget_sites_position_right': '右侧',
    'settings_widget_sites_position_left': '左侧',
    'settings_widget_sites_auto_expand_label': '页面加载后自动展开对话框',
    'settings_widget_sites_cancel': '取消',
  };

  const site = WidgetSiteModel(
    id: '1',
    siteKey: 'sk_demo',
    siteName: 'Demo Site',
    allowedOrigins: ['https://example.com'],
    status: 1,
    createdAt: 0,
    updatedAt: 0,
    displayConfig: WidgetDisplayConfig(),
  );

  Future<void> pumpForm(WidgetTester tester, double textScale) async {
    tester.view.devicePixelRatio = 1.0;
    tester.view.physicalSize = phone;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    AppTranslations.testKeys = {'zh_CN': zh};

    await tester.pumpWidget(GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('zh', 'CN'),
      fallbackLocale: const Locale('zh', 'CN'),
      builder: (ctx, child) => MediaQuery(
        data: MediaQuery.of(ctx).copyWith(
          textScaler: TextScaler.linear(textScale),
        ),
        child: child!,
      ),
      home: Scaffold(
        body: Builder(builder: (ctx) {
          return Center(
            child: ElevatedButton(
              onPressed: () => showDialog<void>(
                context: ctx,
                builder: (_) =>
                    WidgetSiteFormDialog(initial: site, confirmLabel: '保存'),
              ),
              child: const Text('open'),
            ),
          );
        }),
      ),
    ));

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
  }

  testWidgets('edit form fits at iPhone width, normal font', (tester) async {
    await pumpForm(tester, 1.0);
    expect(tester.takeException(), isNull,
        reason: 'edit form overflowed at scale 1.0');
    expect(find.text('外观与行为'), findsOneWidget);
    expect(find.text('保存'), findsOneWidget);
  });

  testWidgets('edit form fits at iPhone width, large font (1.5)',
      (tester) async {
    await pumpForm(tester, 1.5);
    expect(tester.takeException(), isNull,
        reason: 'edit form overflowed at scale 1.5');
    expect(find.text('外观与行为'), findsOneWidget);
  });
}
