import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/modules/auth/widgets/auth_language_switcher.dart';

void main() {
  testWidgets('语言选择弹窗内列表可滚动到末尾语言项', (tester) async {
    Get.testMode = true;
    addTearDown(Get.reset);

    await tester.pumpWidget(
      const GetMaterialApp(
        home: Scaffold(body: Center(child: AuthLanguageSwitcher())),
      ),
    );

    // 打开语言选择底部弹窗
    await tester.tap(find.byType(AuthLanguageSwitcher));
    await tester.pumpAndSettle();

    // 弹窗内的语言列表必须是可滚动的 ListView
    final listFinder = find.byType(ListView);
    expect(listFinder, findsOneWidget);

    // 末尾语言项（印地语）在限高弹窗内初始不可见，需滚动才能到达
    final hindiFinder = find.text('हिन्दी');
    await tester.scrollUntilVisible(
      hindiFinder,
      120,
      scrollable: find.descendant(
        of: listFinder,
        matching: find.byType(Scrollable),
      ),
    );

    expect(hindiFinder, findsOneWidget);
  });
}
