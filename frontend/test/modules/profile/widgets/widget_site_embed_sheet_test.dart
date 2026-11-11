import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/profile/widgets/widget_site_embed_sheet.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const base =
      '<script src="https://grix.example/public/widget/widget.js" data-site-key="wk_abc" defer></script>';

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(Get.reset);

  test('follow browser omits data-locale', () {
    expect(applyEmbedLocale(base, null), base);
    expect(applyEmbedLocale(base, ''), base);
    expect(applyEmbedLocale(base, '  '), base);
  });

  test('fixed locale injects data-locale before defer', () {
    expect(
      applyEmbedLocale(base, 'zh_CN'),
      '<script src="https://grix.example/public/widget/widget.js" data-site-key="wk_abc" data-locale="zh_CN" defer></script>',
    );
  });

  test('switching locale replaces previous data-locale', () {
    final withZh = applyEmbedLocale(base, 'zh_CN');
    expect(
      applyEmbedLocale(withZh, 'ja_JP'),
      '<script src="https://grix.example/public/widget/widget.js" data-site-key="wk_abc" data-locale="ja_JP" defer></script>',
    );
    expect(applyEmbedLocale(withZh, null), base);
  });

  test('SDK documentation links point at the public grix repository', () {
    expect(widgetSdkDocLinks.map((link) => link.platform), [
      'ios',
      'android',
      'flutter',
    ]);
    expect(widgetSdkDocLinks.map((link) => link.url), [
      'https://github.com/askie/grix/tree/main/sdk/ios',
      'https://github.com/askie/grix/tree/main/sdk/android',
      'https://github.com/askie/grix/tree/main/sdk/flutter',
    ]);
  });

  Future<void> pumpSheet(
    WidgetTester tester, {
    required WidgetSiteModel site,
    VoidCallback? onEdit,
  }) async {
    Get.put<SessionService>(SessionService.forTest(Dio()));
    tester.view.physicalSize = const Size(1200, 2400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: Scaffold(
          body: SingleChildScrollView(
            child: WidgetSiteEmbedSheet(
              site: site,
              baseEmbedCode: base,
              onEdit: onEdit ?? () {},
              onDelete: () {},
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  WidgetSiteModel siteWithApps(List<String> apps) {
    return WidgetSiteModel(
      id: '1',
      siteKey: 'wk_abc',
      siteName: 'Demo Site',
      allowedOrigins: const ['https://example.com'],
      allowedApps: apps,
      status: 1,
      createdAt: 0,
      updatedAt: 0,
      displayConfig: const WidgetDisplayConfig(),
    );
  }

  testWidgets('shows app integration section and missing allowlist hint', (
    tester,
  ) async {
    await pumpSheet(tester, site: siteWithApps(const []));

    expect(
      find.text('Use in an App (iOS / Android / Flutter)'),
      findsOneWidget,
    );
    expect(
      find.text(
        "To use the widget in an app, first add the app's bundle ID / package name in the site settings.",
      ),
      findsOneWidget,
    );
    expect(
      find.text(
        'This site has no app allowlist yet. Add a bundle ID / package name to enable app access.',
      ),
      findsOneWidget,
    );
    expect(
      find.byKey(const ValueKey('widget_apps_configure_button')),
      findsOneWidget,
    );
    expect(find.text('iOS SDK'), findsOneWidget);
    expect(find.text('Android SDK'), findsOneWidget);
    expect(find.text('Flutter SDK'), findsOneWidget);
  });

  testWidgets('hides missing allowlist hint when allowed_apps is configured', (
    tester,
  ) async {
    await pumpSheet(
      tester,
      site: siteWithApps(const ['com.example.ios', 'com.example.android']),
    );

    expect(
      find.text(
        'This site has no app allowlist yet. Add a bundle ID / package name to enable app access.',
      ),
      findsNothing,
    );
    expect(
      find.byKey(const ValueKey('widget_apps_configure_button')),
      findsNothing,
    );
    expect(find.byKey(const ValueKey('widget_sdk_link_ios')), findsOneWidget);
    expect(
      find.byKey(const ValueKey('widget_sdk_link_android')),
      findsOneWidget,
    );
    expect(
      find.byKey(const ValueKey('widget_sdk_link_flutter')),
      findsOneWidget,
    );
  });

  testWidgets('copy site key button writes the site key to the clipboard', (
    tester,
  ) async {
    String? copiedText;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, (call) async {
          if (call.method == 'Clipboard.setData') {
            copiedText = (call.arguments as Map)['text'] as String?;
          }
          return null;
        });
    addTearDown(() {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, null);
    });

    await pumpSheet(tester, site: siteWithApps(const []));
    await tester.tap(find.byKey(const ValueKey('widget_copy_site_key_button')));
    await tester.pump();
    expect(copiedText, 'wk_abc');
    await tester.pump(const Duration(seconds: 4));
  });
}
