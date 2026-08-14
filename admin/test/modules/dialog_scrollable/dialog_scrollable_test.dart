// 窄屏 + 软键盘弹起下 AlertDialog(scrollable: true) 抗溢出回归测试。
//
// 保护点：commit 6a5514a9 的四处 AlertDialog（修改密码 / 新建/编辑规则 /
// 在线测试 / 批量导入 CSV）默认 scrollable=false，content 只包在 Flexible
// 中；窄屏(360x640) + 软键盘顶起 300px 时，dialog 可用高度不足会导致
// content 纵向 overflow 且无法滚动。修复方式统一加 scrollable: true，
// 让 Flutter 用 SingleChildScrollView 包住 content。
//
// 本测试覆盖：
//   1. 反向证据：scrollable=false + 高 content + 窄屏 + viewInsets → 应触发 overflow
//   2. 正向：同场景 scrollable=true → 不 overflow 且 Scrollable 存在
//   3. 兼容：窄屏但键盘未弹起 → scrollable=true 不破坏原有布局
//   4. 兼容：宽屏 → scrollable=true 不破坏原有布局
//
// 为避免拖入 GetX/Service 等私有依赖，用最小等价 widget 树复现关键结构。
//
// 与业务 dialog 的对应关系：
//   - AlertDialog(insetPadding: kDialogInsetPadding, scrollable: true,
//     content: DialogContentBox(...))
//   - 与 change_password_dialog.dart、link_blocklist_view.dart 的 4 个弹窗
//     在 build 出的 dialog 结构上一致。

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix_admin/shared/widgets/dialog_content_box.dart';

/// 构造一段高度显著大于窄屏可用空间的表单内容，模拟"批量导入CSV"或"新建规则"
/// 弹窗里多字段 + 提示 + 按钮堆叠的实际观感。
Widget _tallFormContent({double totalHeight = 520}) {
  const rows = 10;
  return Column(
    mainAxisSize: MainAxisSize.min,
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      for (int i = 0; i < rows; i++)
        Container(
          height: totalHeight / rows,
          margin: const EdgeInsets.symmetric(vertical: 2),
          alignment: Alignment.centerLeft,
          padding: const EdgeInsets.symmetric(horizontal: 8),
          decoration: BoxDecoration(
            color: Colors.blue.withValues(alpha: 0.08),
            border: Border.all(color: Colors.blue.withValues(alpha: 0.2)),
          ),
          child: Text('字段 $i'),
        ),
    ],
  );
}

/// 构造尺寸较小、正常场景下能完整显示的内容，用来验证不破坏正常交互。
Widget _shortFormContent() {
  return const Column(
    mainAxisSize: MainAxisSize.min,
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      TextField(decoration: InputDecoration(labelText: '原密码')),
      SizedBox(height: 12),
      TextField(decoration: InputDecoration(labelText: '新密码')),
      SizedBox(height: 12),
      TextField(decoration: InputDecoration(labelText: '确认新密码')),
    ],
  );
}

Future<void> _pumpDialog(
  WidgetTester tester, {
  required Size screenSize,
  required EdgeInsets viewInsets,
  required bool scrollable,
  required Widget content,
  double contentMaxWidth = 460,
}) async {
  // MaterialApp 提供 MaterialLocalizations；内层 MediaQuery 覆盖 size/viewInsets。
  await tester.pumpWidget(
    MaterialApp(
      home: MediaQuery(
        data: MediaQueryData(size: screenSize, viewInsets: viewInsets),
        child: Scaffold(
          // resizeToAvoidBottomInset 关掉，避免 Scaffold 自己吃掉 viewInsets 让
          // AlertDialog 拿到零高度键盘。我们要的是"键盘真的顶起 dialog 可用空间"。
          resizeToAvoidBottomInset: false,
          body: Center(
            child: AlertDialog(
              insetPadding: kDialogInsetPadding,
              scrollable: scrollable,
              title: const Text('测试弹窗'),
              content: DialogContentBox(
                maxWidth: contentMaxWidth,
                child: content,
              ),
              actions: [
                TextButton(onPressed: () {}, child: const Text('取消')),
                FilledButton(onPressed: () {}, child: const Text('确定')),
              ],
            ),
          ),
        ),
      ),
    ),
  );
  await tester.pump();
}

void main() {
  group('AlertDialog(scrollable) · 窄屏 + 软键盘弹起', () {
    testWidgets('反向证据：scrollable=false + 高content 会 overflow', (tester) async {
      // 由 pumpWidget 期间的布局异常会走 FlutterError.onError；用捕获器接住，
      // 避免污染其它测试并明确验证"未修复时必然 overflow"这一前提。
      final errors = <FlutterErrorDetails>[];
      final previous = FlutterError.onError;
      FlutterError.onError = errors.add;
      try {
        await _pumpDialog(
          tester,
          screenSize: const Size(360, 640),
          viewInsets: const EdgeInsets.only(bottom: 300),
          scrollable: false,
          content: _tallFormContent(totalHeight: 520),
        );
      } finally {
        FlutterError.onError = previous;
      }
      // 兜底：pump 过程中挂进 tester 的异常也吸收掉。
      final tossed = tester.takeException();

      final hasOverflow = errors.any((e) =>
              e.exceptionAsString().contains('overflowed')) ||
          (tossed != null && tossed.toString().contains('overflowed'));
      expect(
        hasOverflow,
        isTrue,
        reason: '窄屏 + 软键盘顶起时 scrollable=false + 高 content 必须触发 overflow，'
            '否则本回归失去验证力',
      );
    });

    testWidgets('正向：scrollable=true + 高content 不 overflow 且提供滚动容器',
        (tester) async {
      await _pumpDialog(
        tester,
        screenSize: const Size(360, 640),
        viewInsets: const EdgeInsets.only(bottom: 300),
        scrollable: true,
        content: _tallFormContent(totalHeight: 520),
      );

      expect(
        tester.takeException(),
        isNull,
        reason: '加 scrollable:true 后不应再触发 overflow 异常',
      );
      // AlertDialog(scrollable: true) 会用 SingleChildScrollView 包裹 title+content。
      expect(
        find.byType(SingleChildScrollView),
        findsWidgets,
        reason: 'scrollable:true 应产生 SingleChildScrollView 让内容可滚动',
      );
    });

    testWidgets('回归：320px 极窄屏 + 键盘弹起 scrollable=true 仍不 overflow',
        (tester) async {
      await _pumpDialog(
        tester,
        screenSize: const Size(320, 568),
        viewInsets: const EdgeInsets.only(bottom: 260),
        scrollable: true,
        content: _tallFormContent(totalHeight: 480),
      );

      expect(
        tester.takeException(),
        isNull,
        reason: '极窄屏 + 键盘弹起下 scrollable:true 应稳定',
      );
    });
  });

  group('AlertDialog(scrollable) · 兼容：不破坏原有交互', () {
    testWidgets('窄屏 + 键盘未弹起 + 正常content 无异常', (tester) async {
      await _pumpDialog(
        tester,
        screenSize: const Size(360, 640),
        viewInsets: EdgeInsets.zero,
        scrollable: true,
        content: _shortFormContent(),
        contentMaxWidth: 360,
      );

      expect(tester.takeException(), isNull);
      // 三个输入框都能渲染出来。
      expect(find.byType(TextField), findsNWidgets(3));
      expect(find.text('取消'), findsOneWidget);
      expect(find.text('确定'), findsOneWidget);
    });

    testWidgets('宽屏 1280 + 正常content 无异常', (tester) async {
      await _pumpDialog(
        tester,
        screenSize: const Size(1280, 800),
        viewInsets: EdgeInsets.zero,
        scrollable: true,
        content: _shortFormContent(),
      );

      expect(tester.takeException(), isNull);
      expect(find.byType(TextField), findsNWidgets(3));
    });

    testWidgets('宽屏 1280 + 键盘弹起（外接键盘等场景）无异常', (tester) async {
      // 平板/桌面外接软键盘等罕见组合下，viewInsets 也可能非零。
      await _pumpDialog(
        tester,
        screenSize: const Size(1280, 800),
        viewInsets: const EdgeInsets.only(bottom: 300),
        scrollable: true,
        content: _shortFormContent(),
      );

      expect(tester.takeException(), isNull);
    });
  });
}
