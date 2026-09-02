// 公告文案编辑对话框（ReachAnnouncementDialog）渲染与交互测试。
//
// 保护点：版本发布公告改为草稿制后，塘主在后台通过该对话框编辑中英双语文案。
// 覆盖：
//   1. 打开时中英两个 tab 的四个字段按任务 content 预填
//   2. 切换到 English tab 能看到英文文案
//   3. 中文标题清空后点保存 → 本地校验拦截（不发请求，弹错误提示）

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix_admin/modules/reach/reach_announcement_dialog.dart';
import 'package:grix_admin/modules/reach/reach_models.dart';

ReachAnnouncementContent _sampleContent() => ReachAnnouncementContent(
  zh: ReachAnnouncementLocale(
    title: 'Grix 3.5.0 新版本已发布 🎉',
    body: '修复若干问题',
    emailSubject: 'Grix 3.5.0 新版本已发布',
    emailIntro: '立即更新体验',
  ),
  en: ReachAnnouncementLocale(
    title: 'Grix 3.5.0 is now available 🎉',
    body: 'Bug fixes',
    emailSubject: 'Grix 3.5.0 is now available',
    emailIntro: 'Update now',
  ),
);

Future<void> _pumpDialog(WidgetTester tester) async {
  await tester.pumpWidget(
    GetMaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => Center(
            child: ElevatedButton(
              onPressed: () => ReachAnnouncementDialog.show(
                taskId: '1',
                content: _sampleContent(),
              ),
              child: const Text('open'),
            ),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('打开后中文字段按任务文案预填', (tester) async {
    await _pumpDialog(tester);

    expect(find.text('编辑公告文案'), findsOneWidget);
    expect(find.text('Grix 3.5.0 新版本已发布 🎉'), findsOneWidget);
    expect(find.text('修复若干问题'), findsOneWidget);
    expect(find.text('Grix 3.5.0 新版本已发布'), findsOneWidget);
    expect(find.text('立即更新体验'), findsOneWidget);
  });

  testWidgets('切换 English tab 显示英文文案', (tester) async {
    await _pumpDialog(tester);

    await tester.tap(find.text('English'));
    await tester.pumpAndSettle();

    expect(find.text('Grix 3.5.0 is now available 🎉'), findsOneWidget);
    expect(find.text('Bug fixes'), findsOneWidget);
    expect(find.text('Update now'), findsOneWidget);
  });

  testWidgets('中文标题为空时保存被本地校验拦截', (tester) async {
    await _pumpDialog(tester);

    final titleField = find.widgetWithText(TextField, 'Grix 3.5.0 新版本已发布 🎉');
    expect(titleField, findsOneWidget);
    await tester.enterText(titleField, '');
    await tester.pump();

    await tester.tap(find.text('保存'));
    await tester.pump();

    // 校验失败：对话框仍在（没有因请求成功而关闭），且给出错误提示
    expect(find.text('编辑公告文案'), findsOneWidget);
    expect(find.text('中英文标题均不能为空'), findsOneWidget);

    // 等 toast 动画播完再结束测试，避免 Ticker 泄漏误报
    await tester.pump(const Duration(seconds: 5));
    await tester.pumpAndSettle();
  });
}
