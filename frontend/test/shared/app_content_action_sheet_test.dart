import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/app_dialog_style.dart';

Future<void> _pumpHost(WidgetTester tester, VoidCallback onOpen) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) =>
              ElevatedButton(onPressed: onOpen, child: const Text('open')),
        ),
      ),
    ),
  );
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('内容弹窗渲染内容与操作并可关闭', (tester) async {
    bool? popped;
    await _pumpHost(tester, () {
      final ctx = tester.element(find.text('open'));
      showAppContentDialog<bool>(
        context: ctx,
        title: '设置',
        content: const Text('内容区'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('OK'),
          ),
        ],
      ).then((v) => popped = v);
    });
    expect(find.text('内容区'), findsOneWidget);
    expect(find.text('设置'), findsOneWidget);
    await tester.tap(find.text('OK'));
    await tester.pumpAndSettle();
    expect(popped, isTrue);
  });

  testWidgets('宽屏内容弹窗按档位封顶宽度', (tester) async {
    tester.view.devicePixelRatio = 1.0;
    tester.view.physicalSize = const Size(1200, 900);
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await _pumpHost(tester, () {
      final ctx = tester.element(find.text('open'));
      showAppContentDialog<void>(
        context: ctx,
        content: Container(key: const Key('body')),
        size: AppDialogSize.wide,
      );
    });
    expect(tester.getSize(find.byKey(const Key('body'))).width, 640);
  });

  testWidgets('紧凑端动作菜单走底部 sheet', (tester) async {
    tester.view.devicePixelRatio = 1.0;
    tester.view.physicalSize = const Size(400, 800);
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    var tapped = false;
    await _pumpHost(tester, () {
      final ctx = tester.element(find.text('open'));
      showAppActionSheet(
        context: ctx,
        items: [AppActionSheetItem(label: '操作A', onTap: () => tapped = true)],
      );
    });
    expect(find.byType(BottomSheet), findsOneWidget);
    expect(find.byType(AlertDialog), findsNothing);
    await tester.tap(find.text('操作A'));
    await tester.pumpAndSettle();
    expect(tapped, isTrue);
    expect(find.byType(BottomSheet), findsNothing);
  });

  testWidgets('宽屏端动作菜单走居中弹窗', (tester) async {
    tester.view.devicePixelRatio = 1.0;
    tester.view.physicalSize = const Size(1200, 900);
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    var tapped = false;
    await _pumpHost(tester, () {
      final ctx = tester.element(find.text('open'));
      showAppActionSheet(
        context: ctx,
        items: [AppActionSheetItem(label: '操作A', onTap: () => tapped = true)],
      );
    });
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(find.byType(BottomSheet), findsNothing);
    await tester.tap(find.text('操作A'));
    await tester.pumpAndSettle();
    expect(tapped, isTrue);
  });
}
