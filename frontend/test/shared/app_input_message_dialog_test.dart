import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/app_dialog_style.dart';

Future<void> _pumpInputHost(
  WidgetTester tester, {
  required void Function(String?) onResult,
  String initialValue = '',
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => ElevatedButton(
            onPressed: () async {
              final r = await showAppInputDialog(
                context: context,
                title: '输入',
                initialValue: initialValue,
                confirmText: 'Save',
                cancelText: 'Cancel',
              );
              onResult(r);
            },
            child: const Text('open'),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('输入并保存回传文本', (tester) async {
    String? result = 'unset';
    await _pumpInputHost(tester, onResult: (r) => result = r);
    await tester.enterText(find.byType(TextField), '新名称');
    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();
    expect(result, '新名称');
  });

  testWidgets('取消返回 null', (tester) async {
    String? result = 'unset';
    await _pumpInputHost(tester, onResult: (r) => result = r);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(result, isNull);
  });

  testWidgets('提交动作（Enter）回传文本', (tester) async {
    String? result = 'unset';
    await _pumpInputHost(tester, onResult: (r) => result = r);
    await tester.enterText(find.byType(TextField), 'abc');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();
    expect(result, 'abc');
  });

  testWidgets('软键盘弹出时弹窗避让', (tester) async {
    tester.view.devicePixelRatio = 1.0;
    tester.view.physicalSize = const Size(400, 800);
    tester.view.viewInsets = const FakeViewPadding(bottom: 300);
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetViewInsets);

    await _pumpInputHost(tester, onResult: (_) {});
    final rect = tester.getRect(find.byType(TextField));
    // 键盘占据底部 300，输入框底边应被抬升到键盘区域之上。
    expect(rect.bottom, lessThanOrEqualTo(500 + 1));
  });

  testWidgets('信息框单按钮关闭', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => ElevatedButton(
              onPressed: () => showAppMessageDialog(
                context: context,
                title: '提示',
                message: '已完成',
                dismissText: 'Done',
              ),
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
    expect(find.text('已完成'), findsOneWidget);
    await tester.tap(find.text('Done'));
    await tester.pumpAndSettle();
    expect(find.text('已完成'), findsNothing);
  });
}
