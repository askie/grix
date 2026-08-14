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

  const commands = [
    CommandItemModel(
      id: 'stop',
      name: '/stop',
      description: '终止当前运行中的任务',
      exec: '/stop',
    ),
    CommandItemModel(
      id: 'compact',
      name: '/compact',
      description: '压缩会话上下文以节省 token',
      exec: '/compact',
    ),
  ];

  Future<void> pumpSheet(
    WidgetTester tester, {
    required bool showSkillLibrary,
    String title = '命令',
  }) async {
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.lightTheme,
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              onPressed: () => showChatCommandListSheet(
                context,
                title: title,
                commands: commands,
                onSelected: (_) {},
                showSkillLibrary: showSkillLibrary,
              ),
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
  }

  testWidgets('命令弹窗不展示标题和技能库 Tab，仅列命令', (tester) async {
    await pumpSheet(tester, showSkillLibrary: false);

    expect(find.text('命令'), findsNothing);
    expect(find.byType(TabBar), findsNothing);
    expect(find.byType(TabBarView), findsNothing);
    expect(find.text('/stop'), findsOneWidget);
    expect(find.text('/compact'), findsOneWidget);
  });

  testWidgets('技能弹窗不展示标题但保留「已启用 / 技能库」Tab', (tester) async {
    await pumpSheet(tester, showSkillLibrary: true, title: '');

    expect(find.text('命令'), findsNothing);
    expect(find.byType(TabBar), findsOneWidget);
    expect(find.text('已启用'), findsOneWidget);
    expect(find.text('技能库'), findsOneWidget);
    expect(find.text('/stop'), findsOneWidget);
  });
}
