import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/agent_toolbar_model.dart';
import 'package:grix/modules/chat/chat_view.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(Get.reset);

  test('chatToolbarSelectUsesSheet switches at the long-list threshold', () {
    expect(chatToolbarSelectUsesSheet(0), isFalse);
    expect(
      chatToolbarSelectUsesSheet(kChatToolbarSelectSheetMinOptions - 1),
      isFalse,
    );
    expect(
      chatToolbarSelectUsesSheet(kChatToolbarSelectSheetMinOptions),
      isTrue,
    );
    expect(chatToolbarSelectUsesSheet(120), isTrue);
  });

  testWidgets('toolbar model sheet uses the generic keyword placeholder', (
    tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.lightTheme,
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              onPressed: () => showChatToolbarSelectSheet(
                context: context,
                title: '模型',
                options: const [
                  AgentToolbarOptionModel(
                    optionId: 'model-1',
                    label: '模型一',
                    disabled: false,
                  ),
                ],
                currentValue: '',
                onSelected: (_) {},
              ),
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    final searchField = tester.widget<TextField>(
      find.byKey(const Key('chat_sheet_search_field')),
    );
    expect(searchField.decoration?.hintText, '输入关键词');
  });
}
