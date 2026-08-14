import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:grix/shared/utils/sheet_guard.dart';

void main() {
  setUp(SheetGuard.reset);

  group('SheetGuard.run', () {
    test('同 tag 未结束前重复触发被忽略', () async {
      final completer = Completer<int?>();
      var openCount = 0;

      final first = SheetGuard.run<int>('menu', () {
        openCount++;
        return completer.future;
      });
      final second = await SheetGuard.run<int>('menu', () {
        openCount++;
        return Future.value(2);
      });

      expect(second, isNull);
      expect(openCount, 1);

      completer.complete(1);
      expect(await first, 1);
    });

    test('结束后可再次打开；不同 tag 互不影响', () async {
      expect(await SheetGuard.run<int>('menu', () async => 1), 1);
      expect(await SheetGuard.run<int>('menu', () async => 2), 2);

      final blocker = Completer<void>();
      unawaited(SheetGuard.run<void>('menu', () => blocker.future));
      expect(await SheetGuard.run<int>('other', () async => 3), 3);
      blocker.complete();
    });

    test('open 抛错后守卫释放，可再次打开', () async {
      await expectLater(
        SheetGuard.run<void>('menu', () async => throw StateError('boom')),
        throwsStateError,
      );
      expect(await SheetGuard.run<int>('menu', () async => 4), 4);
    });
  });

  group('popSheetOnce', () {
    testWidgets('双击菜单项只 pop 一层，动作只执行一次', (tester) async {
      var actionCount = 0;

      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                onPressed: () {
                  showModalBottomSheet<void>(
                    context: context,
                    builder: (sheetContext) => TextButton(
                      onPressed: () {
                        if (!popSheetOnce(sheetContext)) return;
                        actionCount++;
                      },
                      child: const Text('item'),
                    ),
                  );
                },
                child: const Text('open'),
              ),
            ),
          ),
        ),
      );

      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();
      expect(find.text('item'), findsOneWidget);

      // 第一次点击触发 pop，退场动画期间立刻再点一次
      await tester.tap(find.text('item'));
      await tester.pump(const Duration(milliseconds: 16));
      await tester.tap(find.text('item'), warnIfMissed: false);
      await tester.pumpAndSettle();

      expect(actionCount, 1);
      // 底层页面没有被多 pop 掉
      expect(find.text('open'), findsOneWidget);
    });

    testWidgets('带返回值 pop 只生效一次', (tester) async {
      final results = <String?>[];

      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                onPressed: () async {
                  final r = await showModalBottomSheet<String>(
                    context: context,
                    builder: (sheetContext) => TextButton(
                      onPressed: () => popSheetOnce(sheetContext, 'picked'),
                      child: const Text('item'),
                    ),
                  );
                  results.add(r);
                },
                child: const Text('open'),
              ),
            ),
          ),
        ),
      );

      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();

      await tester.tap(find.text('item'));
      await tester.pump(const Duration(milliseconds: 16));
      await tester.tap(find.text('item'), warnIfMissed: false);
      await tester.pumpAndSettle();

      expect(results, ['picked']);
      expect(find.text('open'), findsOneWidget);
    });
  });
}
