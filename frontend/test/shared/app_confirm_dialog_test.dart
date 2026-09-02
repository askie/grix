import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/app_dialog_style.dart';

/// 打开确认框并返回其结果的宿主；result 在交互后读取。
Future<void> _pumpConfirmHost(
  WidgetTester tester, {
  required void Function(bool) onResult,
  bool destructive = false,
  Color Function(Color error)? captureError,
}) async {
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) {
            captureError?.call(Theme.of(context).colorScheme.error);
            return ElevatedButton(
              onPressed: () async {
                final r = await showAppConfirmDialog(
                  context: context,
                  title: '标题',
                  message: '内容',
                  confirmText: 'OK',
                  cancelText: 'Cancel',
                  isDestructive: destructive,
                );
                onResult(r);
              },
              child: const Text('open'),
            );
          },
        ),
      ),
    ),
  );
  await tester.tap(find.text('open'));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('点击确认返回 true', (tester) async {
    bool? result;
    await _pumpConfirmHost(tester, onResult: (r) => result = r);
    await tester.tap(find.text('OK'));
    await tester.pumpAndSettle();
    expect(result, isTrue);
  });

  testWidgets('点击取消返回 false', (tester) async {
    bool? result;
    await _pumpConfirmHost(tester, onResult: (r) => result = r);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(result, isFalse);
  });

  testWidgets('destructive 时确认按钮用 error 色', (tester) async {
    late Color errorColor;
    await _pumpConfirmHost(
      tester,
      onResult: (_) {},
      destructive: true,
      captureError: (e) => errorColor = e,
    );
    final confirmBtn = tester.widget<TextButton>(
      find.widgetWithText(TextButton, 'OK'),
    );
    final cancelBtn = tester.widget<TextButton>(
      find.widgetWithText(TextButton, 'Cancel'),
    );
    expect(confirmBtn.style?.foregroundColor?.resolve({}), errorColor);
    expect(cancelBtn.style?.foregroundColor?.resolve({}), isNull);
  });

  testWidgets('桌面端 Enter 确认 / Esc 取消', (tester) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.macOS;
    try {
      bool? enterResult;
      await _pumpConfirmHost(tester, onResult: (r) => enterResult = r);
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await tester.pumpAndSettle();
      expect(enterResult, isTrue);

      bool? escResult;
      await _pumpConfirmHost(tester, onResult: (r) => escResult = r);
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.pumpAndSettle();
      expect(escResult, isFalse);
    } finally {
      debugDefaultTargetPlatformOverride = null;
    }
  });

  testWidgets('紧凑端按钮满足 48 触摸高', (tester) async {
    tester.view.devicePixelRatio = 1.0;
    tester.view.physicalSize = const Size(400, 800);
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await _pumpConfirmHost(tester, onResult: (_) {});
    final size = tester.getSize(find.widgetWithText(TextButton, 'OK'));
    expect(size.height, greaterThanOrEqualTo(kDialogButtonMinHeight));
  });
}
