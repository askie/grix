import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix_admin/modules/link_blocklist/link_blocklist_models.dart';
import 'package:grix_admin/modules/link_blocklist/link_blocklist_view.dart';

/// 覆盖 2026-06-25 优化后的卡片关键不变量：
///   1. 窄屏（< 560）actions 行被压到 info 行的下方，不再挤占主信息宽度
///   2. 宽屏（>= 560）actions 与 info 在同一行（与原桌面布局兼容）
///   3. 长 note 不被竖切（之前 `fake-store-scam` 在窄屏被压成 3 行竖排）
void main() {
  group('LinkBlocklistRuleCard 布局', () {
    final rule = LinkBlocklistRule(
      id: 1,
      kind: 'domain',
      value: 'vivaharmoni.com',
      severity: 'malicious',
      source: 'hagezi_fake',
      enabled: true,
      note: 'fake-store-scam',
      hitCount: 0,
    );

    Widget wrap(Widget child, {required double width}) {
      return MaterialApp(
        home: Scaffold(
          body: Center(
            child: SizedBox(width: width, child: child),
          ),
        ),
      );
    }

    LinkBlocklistRuleCard buildCard() => LinkBlocklistRuleCard(
      rule: rule,
      onEdit: () {},
      onToggleEnabled: () {},
      onDelete: () {},
    );

    testWidgets('窄屏 360：actions 行位于 note 之下', (tester) async {
      await tester.pumpWidget(wrap(buildCard(), width: 360));
      await tester.pump();

      // 主信息（域名）和 note 应都存在
      expect(find.text('vivaharmoni.com'), findsOneWidget);
      expect(find.text('fake-store-scam'), findsOneWidget);

      // 编辑按钮的 top 应在 note 文字 bottom 之下
      final noteBottom = tester.getBottomLeft(find.text('fake-store-scam')).dy;
      final editTop = tester.getTopLeft(find.byIcon(Icons.edit_outlined)).dy;
      expect(
        editTop,
        greaterThan(noteBottom),
        reason: '窄屏下编辑按钮应在 note 之下，否则 actions 会挤占主信息宽度导致 note 折叠',
      );
    });

    testWidgets('宽屏 800：actions 与域名在同一行（y 接近）', (tester) async {
      await tester.pumpWidget(wrap(buildCard(), width: 800));
      await tester.pump();

      final valueCenter = tester.getCenter(find.text('vivaharmoni.com')).dy;
      final editCenter = tester.getCenter(find.byIcon(Icons.edit_outlined)).dy;
      // 同一行允许 < 30 dp 差距（按钮 icon 与 14pt 文字基线小幅偏移正常）
      expect(
        (valueCenter - editCenter).abs(),
        lessThan(30),
        reason: '宽屏下 actions 应保持在与主信息同一行',
      );
    });

    testWidgets('note 一行连贯渲染，不被字符切成多行', (tester) async {
      await tester.pumpWidget(wrap(buildCard(), width: 360));
      await tester.pump();

      // 拿到 note Text widget，确认 maxLines 限制存在
      final noteWidget = tester.widget<Text>(find.text('fake-store-scam'));
      expect(noteWidget.maxLines, 2);
      expect(noteWidget.overflow, TextOverflow.ellipsis);
    });

    testWidgets('点击编辑/删除/Switch 触发对应回调', (tester) async {
      var edits = 0, deletes = 0, toggles = 0;
      await tester.pumpWidget(
        wrap(
          LinkBlocklistRuleCard(
            rule: rule,
            onEdit: () => edits++,
            onDelete: () => deletes++,
            onToggleEnabled: () => toggles++,
          ),
          width: 800,
        ),
      );
      await tester.pump();

      await tester.tap(find.byIcon(Icons.edit_outlined));
      await tester.tap(find.byIcon(Icons.delete_outline));
      await tester.tap(find.byType(Switch));
      await tester.pump();

      expect(edits, 1);
      expect(deletes, 1);
      expect(toggles, 1);
    });
  });
}
