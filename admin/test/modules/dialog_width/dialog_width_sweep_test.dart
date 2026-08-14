// 黑名单弹窗窄屏溢出回归测试。
//
// 保护点：
//   1. 批量导入 CSV 弹窗 —— 文件选择按钮 + 提示文案那一行
//      （link_blocklist_view.dart::_ImportDialogState._buildFilePane 里的 LayoutBuilder）
//   2. 新建/编辑规则弹窗 —— 类型 Dropdown + 严重度 Dropdown 并排那一行
//      （link_blocklist_view.dart::_RuleEditDialogState.build 里的 LayoutBuilder）
//
// 两处均采用 `if (constraints.maxWidth < 360) 竖排 else 横排` 的 breakpoint。
// 若该 breakpoint 或子控件宽度改变，请同步更新本文件里的 kNarrowBreakpoint 常量与结构。
//
// 覆盖：320 / 375（应竖排且不溢出）、1280（应横排且宽屏样式正常）。
//
// DialogContentBox 计算：screenWidth < maxWidth+96 时内容宽 = screenWidth-96，
// 否则取 maxWidth（460）。所以内容宽度：
//   - screen 320 → 224 → 竖排
//   - screen 375 → 279 → 竖排
//   - screen 1280 → 460 → 横排

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix_admin/modules/link_blocklist/link_blocklist_models.dart';
import 'package:grix_admin/shared/widgets/dialog_content_box.dart';

// 与 link_blocklist_view.dart 中的 LayoutBuilder 阈值保持一致。
const double kNarrowBreakpoint = 360;

/// 复用 link_blocklist_view.dart::_buildFilePane 里 LayoutBuilder 的等价结构。
Widget _importFilePaneRow() {
  return LayoutBuilder(
    builder: (context, constraints) {
      final button = FilledButton.tonalIcon(
        onPressed: () {},
        icon: const Icon(Icons.upload_file, size: 18),
        label: const Text('选择 CSV/TXT 文件'),
      );
      const hint = Text(
        '支持 CSV/TXT，UTF-8 编码，单文件 ≤ 5MB',
        style: TextStyle(fontSize: 12, color: Colors.black54),
      );
      if (constraints.maxWidth < kNarrowBreakpoint) {
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [button, const SizedBox(height: 8), hint],
        );
      }
      return Row(
        children: [
          button,
          const SizedBox(width: 12),
          const Expanded(child: hint),
        ],
      );
    },
  );
}

/// 复用 link_blocklist_view.dart::_RuleEditDialogState.build 里 LayoutBuilder 的等价结构。
Widget _ruleKindSeverityRow() {
  return LayoutBuilder(
    builder: (context, constraints) {
      final kindField = DropdownButtonFormField<String>(
        initialValue: kLinkRuleKinds.first,
        decoration: const InputDecoration(labelText: '类型'),
        items: [
          for (final k in kLinkRuleKinds)
            DropdownMenuItem(value: k, child: Text(k)),
        ],
        onChanged: (_) {},
      );
      final severityField = DropdownButtonFormField<String>(
        initialValue: kLinkRuleSeverities.first,
        decoration: const InputDecoration(labelText: '严重度'),
        items: [
          for (final s in kLinkRuleSeverities)
            DropdownMenuItem(value: s, child: Text(s)),
        ],
        onChanged: (_) {},
      );
      if (constraints.maxWidth < kNarrowBreakpoint) {
        return Column(
          children: [
            kindField,
            const SizedBox(height: 12),
            severityField,
          ],
        );
      }
      return Row(
        children: [
          Expanded(child: kindField),
          const SizedBox(width: 12),
          Expanded(child: severityField),
        ],
      );
    },
  );
}

Future<void> _pumpUnderDialog(
  WidgetTester tester, {
  required double screenWidth,
  required Widget child,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: MediaQuery(
        data: MediaQueryData(size: Size(screenWidth, 1200)),
        child: Scaffold(
          body: Center(
            child: AlertDialog(
              insetPadding: kDialogInsetPadding,
              title: const Text('测试弹窗'),
              content: DialogContentBox(
                maxWidth: 460,
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [child],
                ),
              ),
            ),
          ),
        ),
      ),
    ),
  );
  await tester.pump();
}

void main() {
  group('黑名单批量导入弹窗 · 文件选择行', () {
    testWidgets('screen 320：竖排，无 overflow', (tester) async {
      await _pumpUnderDialog(
        tester,
        screenWidth: 320,
        child: _importFilePaneRow(),
      );

      expect(tester.takeException(), isNull, reason: '320px 不应触发 overflow 断言');

      final buttonBottom =
          tester.getBottomLeft(find.byType(FilledButton)).dy;
      final hintTop =
          tester.getTopLeft(find.text('支持 CSV/TXT，UTF-8 编码，单文件 ≤ 5MB')).dy;
      expect(
        hintTop,
        greaterThanOrEqualTo(buttonBottom),
        reason: '窄屏下提示文案应位于按钮下方（竖排）',
      );
    });

    testWidgets('screen 375：竖排，无 overflow', (tester) async {
      await _pumpUnderDialog(
        tester,
        screenWidth: 375,
        child: _importFilePaneRow(),
      );

      expect(tester.takeException(), isNull, reason: '375px 不应触发 overflow 断言');

      final buttonBottom =
          tester.getBottomLeft(find.byType(FilledButton)).dy;
      final hintTop =
          tester.getTopLeft(find.text('支持 CSV/TXT，UTF-8 编码，单文件 ≤ 5MB')).dy;
      expect(
        hintTop,
        greaterThanOrEqualTo(buttonBottom),
        reason: '窄屏下提示文案应位于按钮下方（竖排）',
      );
    });

    testWidgets('screen 1280：横排，按钮与提示同一行且无 overflow', (tester) async {
      await _pumpUnderDialog(
        tester,
        screenWidth: 1280,
        child: _importFilePaneRow(),
      );

      expect(tester.takeException(), isNull, reason: '宽屏不应触发 overflow 断言');

      final buttonCenter =
          tester.getCenter(find.byType(FilledButton)).dy;
      final hintCenter =
          tester.getCenter(find.text('支持 CSV/TXT，UTF-8 编码，单文件 ≤ 5MB')).dy;
      expect(
        (buttonCenter - hintCenter).abs(),
        lessThan(30),
        reason: '宽屏下按钮与提示应在同一行',
      );
    });
  });

  group('黑名单新建/编辑规则弹窗 · 类型 + 严重度行', () {
    testWidgets('screen 320：竖排，无 overflow', (tester) async {
      await _pumpUnderDialog(
        tester,
        screenWidth: 320,
        child: _ruleKindSeverityRow(),
      );

      expect(tester.takeException(), isNull, reason: '320px 不应触发 overflow 断言');

      // 严重度字段的 top 应在类型字段的 bottom 之下 —— 竖排。
      final kindBottom = tester
          .getBottomLeft(find.byType(DropdownButtonFormField<String>).first)
          .dy;
      final severityTop = tester
          .getTopLeft(find.byType(DropdownButtonFormField<String>).last)
          .dy;
      expect(
        severityTop,
        greaterThan(kindBottom),
        reason: '窄屏下严重度应位于类型下方（竖排）',
      );
    });

    testWidgets('screen 375：竖排，无 overflow', (tester) async {
      await _pumpUnderDialog(
        tester,
        screenWidth: 375,
        child: _ruleKindSeverityRow(),
      );

      expect(tester.takeException(), isNull, reason: '375px 不应触发 overflow 断言');

      final kindBottom = tester
          .getBottomLeft(find.byType(DropdownButtonFormField<String>).first)
          .dy;
      final severityTop = tester
          .getTopLeft(find.byType(DropdownButtonFormField<String>).last)
          .dy;
      expect(
        severityTop,
        greaterThan(kindBottom),
        reason: '窄屏下严重度应位于类型下方（竖排）',
      );
    });

    testWidgets('screen 1280：横排，类型/严重度同一行且无 overflow', (tester) async {
      await _pumpUnderDialog(
        tester,
        screenWidth: 1280,
        child: _ruleKindSeverityRow(),
      );

      expect(tester.takeException(), isNull, reason: '宽屏不应触发 overflow 断言');

      final kindCenter = tester
          .getCenter(find.byType(DropdownButtonFormField<String>).first)
          .dy;
      final severityCenter = tester
          .getCenter(find.byType(DropdownButtonFormField<String>).last)
          .dy;
      expect(
        (kindCenter - severityCenter).abs(),
        lessThan(30),
        reason: '宽屏下类型与严重度应在同一行',
      );
    });
  });
}
