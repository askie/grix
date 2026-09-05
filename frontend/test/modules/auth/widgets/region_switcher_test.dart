import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/auth/widgets/region_switcher.dart';
import 'package:grix/shared/utils/app_region_config.dart';

Widget _host(Rx<AppRegion> region) {
  return GetMaterialApp(
    translations: AppTranslations(),
    locale: const Locale('en', 'US'),
    fallbackLocale: const Locale('en', 'US'),
    home: Scaffold(
      body: Center(
        child: RegionSwitcher(
          selectedRegion: region,
          onChanged: (value) => region.value = value,
        ),
      ),
    ),
  );
}

/// 文本里出现的 emoji 图标依赖系统字体，在缺少 emoji 字体的平台会显示成方框。
bool _rendersEmojiIcon(WidgetTester tester) {
  return tester
      .widgetList<Text>(find.byType(Text))
      .any((text) => (text.data ?? '').contains(RegExp('🇨🇳|🌐')));
}

void main() {
  test('bundles the region flag as a vector asset', () {
    expect(File('assets/icons/region_cn.svg').existsSync(), isTrue);
  });

  testWidgets('renders the cn region with a vector flag instead of emoji', (
    tester,
  ) async {
    await tester.pumpWidget(_host(AppRegion.cn.obs));
    await tester.pumpAndSettle();

    expect(_rendersEmojiIcon(tester), isFalse);
    expect(find.byType(SvgPicture), findsOneWidget);
  });

  testWidgets('renders the global region with a material icon', (tester) async {
    await tester.pumpWidget(_host(AppRegion.global.obs));
    await tester.pumpAndSettle();

    expect(_rendersEmojiIcon(tester), isFalse);
    expect(find.byIcon(Icons.public), findsOneWidget);
  });

  testWidgets('keeps the picker list free of emoji icons', (tester) async {
    await tester.pumpWidget(_host(AppRegion.cn.obs));
    await tester.pumpAndSettle();

    await tester.tap(find.byType(InkWell).first);
    await tester.pumpAndSettle();

    expect(find.text('Mainland China'), findsNWidgets(2));
    expect(_rendersEmojiIcon(tester), isFalse);
    expect(find.byType(SvgPicture), findsNWidgets(2));
    expect(find.byIcon(Icons.public), findsOneWidget);
  });
}
