import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';
import 'history_test_harness.dart';

void main() {
  testWidgets(
    'production heterogeneous capped window retains reading across twenty pages',
    (tester) async {
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(() {
        tester.view.resetPhysicalSize();
        tester.view.resetDevicePixelRatio();
      });
      const sid = 'trim-200';
      final im = await initOffline(tester, sid);
      await runDb(
        tester,
        () => LocalDb.batchInsertMessages(
          List.generate(
            1600,
            (i) => row(
              sid,
              i + 1,
              content:
                  'message ${i + 1}\n${List.filled((i + 1 >= 1331 && i + 1 <= 1370) || (i < 1330 && i ~/ 40 % 2 == 0) ? 8 : 1, 'offline line').join('\n')}',
            ),
          ),
        ),
      );
      final c = await openRoute(tester, sid);
      await settleInitial(tester, im);
      await tester.drag(chatList(c), const Offset(0, 500));
      await tester.pump();
      for (int i = 0; i < 5; i++) {
        await runDb(tester, im.loadOlderForCurrentSession);
        await tester.pump();
        // Establish a laid-out near-top position before starting the race.
        // Lazy extent estimates can change as heterogeneous rows are revealed.
        for (var locate = 0; locate < 6; locate++) {
          c.scrollController.jumpTo(
            c.scrollController.position.minScrollExtent + 600,
          );
          await tester.pump();
        }
      }
      await tester.pump(const Duration(milliseconds: 100));
      await settleHistory(tester);
      expect(im.currentMessages.length, 200);
      expect(im.currentWindowVisibleBubbleCount, 200);
      expect(im.hasNewerMessages, isTrue);
      final beforeRows = im.currentMessages.toList();
      c.scrollController.jumpTo(
        c.scrollController.position.minScrollExtent + 330,
      );
      await tester.pump();
      final hold = Completer<void>(), entered = Completer<void>();
      final db = await LocalDb.database;
      final tx = db.transaction((_) async {
        entered.complete();
        await hold.future;
      });
      await flushDb(tester, () => entered.isCompleted);
      String? anchor;
      Map<String, Object?>? capture;
      c.scrollController.addListener(() {
        if (c.isLoadingOlderHistory && capture == null) {
          anchor = leadingId(c, im);
          capture = metrics(c, msgId: anchor);
        }
      });
      final gesture = await tester.startGesture(tester.getCenter(chatList(c)));
      for (int i = 0; i < 8; i++) {
        await gesture.moveBy(const Offset(0, 20));
        await tester.pump();
      }
      expect(c.isLoadingOlderHistory, isTrue);
      final user = metrics(c, msgId: anchor);
      hold.complete();
      await runDb(tester, () => tx);
      await flushDb(tester, () => !c.isLoadingOlderHistory);
      await tester.pump();
      final after = metrics(c, msgId: anchor);
      final inserted = im.currentMessages
          .where((m) => !beforeRows.any((b) => b.msgId == m.msgId))
          .map((m) => m.msgId)
          .toList();
      final trimmed = beforeRows
          .where((m) => !im.currentMessages.any((b) => b.msgId == m.msgId))
          .map((m) => m.msgId)
          .toList();
      evidence('TRIM-restored', {
        'captured': capture,
        'user': user,
        'after': after,
        'inserted': inserted,
        'trimmed': trimmed,
        'visibleCount': im.currentWindowVisibleBubbleCount,
      });
      expect(after['y'], isNotNull);
      expect((after['y'] as double) - (user['y'] as double), closeTo(0, 1));
      expect(im.currentWindowVisibleBubbleCount, 200);
      expect(inserted.length, 40);
      expect(trimmed.length, 40);
      await gesture.up();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 120));
      await settleHistory(tester);
      evidence('TRIM-settled', {
        'metrics': metrics(c, msgId: anchor),
        'first': im.currentMessages.first.msgId,
        'last': im.currentMessages.last.msgId,
        'hasNewer': im.hasNewerMessages,
      });
      for (var page = 0; page < 20; page++) {
        final db = await LocalDb.database;
        final held = Completer<void>(), started = Completer<void>();
        final transaction = db.transaction((_) async {
          started.complete();
          await held.future;
        });
        await flushDb(tester, () => started.isCompleted);
        final oldWindow = im.currentMessages.map((m) => m.msgId).toSet();
        // Lay out the near-top area before starting this controlled race.
        // Its heterogeneous lazy extent estimate changes during relocation.
        for (var locate = 0; locate < 6; locate++) {
          c.scrollController.jumpTo(
            c.scrollController.position.minScrollExtent + 600,
          );
          await tester.pump();
        }
        final drag = await tester.startGesture(tester.getCenter(chatList(c)));
        for (var move = 0; move < 500 && !c.isLoadingOlderHistory; move++) {
          await drag.moveBy(const Offset(0, 20));
          await tester.pump();
        }
        evidence('REPEATED-trigger', {
          ...metrics(c),
          'min': c.scrollController.position.minScrollExtent,
        });
        expect(c.isLoadingOlderHistory, isTrue);
        final id = leadingId(c, im)!;
        final reading = metrics(c, msgId: id);
        held.complete();
        await runDb(tester, () => transaction);
        await flushDb(tester, () => !c.isLoadingOlderHistory);
        await tester.pump();
        final result = metrics(c, msgId: id);
        expect(result['y'], isNotNull);
        expect(result['y'] as double, closeTo(reading['y'] as double, 1));
        expect(im.currentWindowVisibleBubbleCount, 200);
        expect(
          im.currentMessages.where((m) => !oldWindow.contains(m.msgId)).length,
          40,
        );
        expect(im.hasOlderMessages, isTrue);
        expect(im.hasNewerMessages, isTrue);
        final numbers = im.currentMessages
            .map((m) => int.parse(m.msgId.split('-').last))
            .toList();
        for (var i = 1; i < numbers.length; i++) {
          expect(numbers[i], numbers[i - 1] + 1);
        }
        evidence('REPEATED', {
          'page': page,
          'id': id,
          'before': reading,
          'after': result,
          'first': numbers.first,
          'last': numbers.last,
          'visible': im.currentWindowVisibleBubbleCount,
        });
        await drag.up();
        await settleHistory(tester);
      }
      await closeOffline(tester);
    },
  );
}
