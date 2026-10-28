import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/profile/widget_sites_view.dart';

class _FakeSessionService extends SessionService {
  _FakeSessionService(this.site);

  final WidgetSiteModel site;
  int updateCalls = 0;

  @override
  Future<WidgetSiteListResult> fetchWidgetSites({
    int status = 0,
    int limit = 50,
    int offset = 0,
  }) async {
    return WidgetSiteListResult(items: [site], total: 1);
  }

  @override
  Future<WidgetSiteDetailResult> fetchWidgetSiteDetail(String siteId) async {
    return WidgetSiteDetailResult(
      site: site,
      loaderUrl: 'https://example.com/w.js',
      embedCode: '<script src="https://example.com/w.js"></script>',
    );
  }

  @override
  Future<WidgetSessionModerationResult> updateWidgetSite({
    required String id,
    required String siteName,
    required List<String> allowedOrigins,
    required int status,
    WidgetDisplayConfig? displayConfig,
  }) async {
    updateCalls++;
    return const WidgetSessionModerationResult();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(Get.reset);

  testWidgets('edit dialog uses Save and reports save success', (tester) async {
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
    final sessionService = _FakeSessionService(site);
    Get.put<SessionService>(sessionService);

    tester.view.physicalSize = const Size(1200, 2000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const WidgetSitesView(),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('Demo Site'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Edit'));
    await tester.pumpAndSettle();

    expect(find.text('Save'), findsOneWidget);
    expect(find.text('Edit'), findsNothing);

    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();

    expect(sessionService.updateCalls, 1);
    expect(find.text('Saved'), findsOneWidget);
    expect(find.text('Created successfully'), findsNothing);
    await tester.pump(const Duration(seconds: 3));
  });

  test('widget site save translations exist in every locale', () async {
    const locales = <String>[
      'ar',
      'de_DE',
      'en_US',
      'es_ES',
      'fr_FR',
      'hi_IN',
      'ja_JP',
      'ko_KR',
      'pt_BR',
      'ru_RU',
      'zh_CN',
    ];
    for (final locale in locales) {
      final raw = await rootBundle.loadString('assets/i18n/$locale.json');
      final data = json.decode(raw) as Map<String, dynamic>;
      expect(
        (data['common_save'] as String?)?.trim(),
        isNotEmpty,
        reason: locale,
      );
      expect(
        (data['common_save_success'] as String?)?.trim(),
        isNotEmpty,
        reason: locale,
      );
    }
  });
}
