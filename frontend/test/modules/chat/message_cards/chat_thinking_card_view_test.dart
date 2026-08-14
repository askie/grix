import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:grix/app/settings/chat_thinking_collapse_service.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/message_cards/models/chat_thinking_card_data.dart';
import 'package:grix/modules/chat/message_cards/widgets/chat_thinking_card_view.dart';

void main() {
  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    await Get.putAsync<ChatThinkingCollapseService>(
      () => ChatThinkingCollapseService().init(),
    );
  });

  tearDown(Get.reset);

  Future<void> pumpTwoCards(WidgetTester tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const Scaffold(
          body: SingleChildScrollView(
            child: Column(
              children: [
                ChatThinkingCardView(
                  card: ChatThinkingCardData(content: 'alpha\nbeta\ngamma'),
                  isMine: false,
                  fontScale: 1,
                ),
                ChatThinkingCardView(
                  card: ChatThinkingCardData(content: 'one\ntwo\nthree'),
                  isMine: false,
                  fontScale: 1,
                ),
              ],
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('collapsing one bubble collapses all and shows only last line', (
    WidgetTester tester,
  ) async {
    await pumpTwoCards(tester);

    // 默认展开：两个气泡都显示全文。
    expect(find.text('alpha\nbeta\ngamma'), findsOneWidget);
    expect(find.text('one\ntwo\nthree'), findsOneWidget);

    // 点击其一折叠 → 全局生效，两个气泡都进入折叠态，只显示最后一行。
    await tester.tap(find.byType(InkWell).first);
    await tester.pumpAndSettle();

    expect(find.text('alpha\nbeta\ngamma'), findsNothing);
    expect(find.text('one\ntwo\nthree'), findsNothing);
    expect(find.text('gamma'), findsOneWidget);
    expect(find.text('three'), findsOneWidget);

    // 再次点击 → 全局展开，两个气泡都恢复全文。
    await tester.tap(find.byType(InkWell).last);
    await tester.pumpAndSettle();

    expect(find.text('alpha\nbeta\ngamma'), findsOneWidget);
    expect(find.text('one\ntwo\nthree'), findsOneWidget);
  });

  testWidgets('collapse hint and chevron pin to the right of the header', (
    WidgetTester tester,
  ) async {
    await pumpTwoCards(tester);

    final cardFinder = find.byType(ChatThinkingCardView).first;
    final chevronFinder = find
        .descendant(of: cardFinder, matching: find.byType(Icon))
        .last;

    final cardRight = tester.getBottomRight(cardFinder).dx;
    final chevronRight = tester.getBottomRight(chevronFinder).dx;

    // 卡片 padding=12,箭头右缘应贴到卡片右缘 padding 之内的边界(容差 2px)。
    expect(cardRight - chevronRight, closeTo(12, 2));
  });
}
