import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';

import 'history_test_harness.dart';

void main() {
  testWidgets(
    'production delayed older paging retains drag and fling positions',
    (tester) async {
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(() {
        tester.view.resetPhysicalSize();
        tester.view.resetDevicePixelRatio();
      });
      // Empty pages are the negative control. The same gestures then run with
      // two and forty inserted rows, through ChatView's real notifications.
      for (final count in [30, 32, 90]) {
        for (final intent in ['stationary', 'drag', 'reverse', 'fling']) {
          final sid = 'paging-$count-$intent';
          final im = await initOffline(tester, sid);
          await runDb(
            tester,
            () => LocalDb.batchInsertMessages(
              List.generate(
                count,
                (i) => row(
                  sid,
                  i + 1,
                  content:
                      'message ${i + 1}\n${List.filled(1 + i % 3, 'offline line').join('\n')}',
                ),
              ),
            ),
          );
          final c = await openRoute(tester, sid);
          await settleInitial(tester, im);
          await tester.drag(chatList(c), const Offset(0, 80));
          await tester.pump();
          c.scrollController.jumpTo(
            c.scrollController.position.minScrollExtent + 330,
          );
          await tester.pump();
          expect(c.hasOlderHistory, isTrue);
          expect(c.isLoadingOlderHistory, isFalse);
          final hold = Completer<void>(), entered = Completer<void>();
          final db = await LocalDb.database;
          final tx = db.transaction((_) async {
            entered.complete();
            await hold.future;
          });
          await flushDb(tester, () => entered.isCompleted);
          final gesture = await tester.startGesture(
            tester.getCenter(chatList(c)),
          );
          for (var i = 0; i < 20 && !c.isLoadingOlderHistory; i++) {
            await gesture.moveBy(const Offset(0, 20));
            await tester.pump();
          }
          expect(c.isLoadingOlderHistory, isTrue);
          if (intent == 'drag' || intent == 'reverse') {
            await gesture.moveBy(Offset(0, intent == 'drag' ? 60 : -60));
            await tester.pump();
          } else if (intent == 'fling') {
            await gesture.up();
            await tester.fling(chatList(c), const Offset(0, -160), 1000);
            await tester.pump(const Duration(milliseconds: 16));
          }
          final id = leadingId(c, im)!;
          final before = metrics(c, msgId: id);
          final first = im.currentMessages.first.msgId;
          hold.complete();
          await runDb(tester, () => tx);
          await flushDb(tester, () => !c.isLoadingOlderHistory);
          await tester
              .pump(); // no elapsed time: subtracts normal fling movement
          final after = metrics(c, msgId: id);
          evidence('OLDER', {
            'count': count,
            'intent': intent,
            'before': before,
            'after': after,
            'error': (after['y'] as double?) == null
                ? null
                : (after['y'] as double) - (before['y'] as double),
          });
          expect(
            after['y'],
            isNotNull,
            reason: 'the reading message must stay mounted',
          );
          expect(after['y'] as double, closeTo(before['y'] as double, 1));
          if (intent == 'fling') {
            expect(before['activity'], 'BallisticScrollActivity');
            expect(after['activity'], 'BallisticScrollActivity');
          } else {
            expect(after['activity'], 'DragScrollActivity');
            await gesture.moveBy(const Offset(0, -20));
            await tester.pump();
            expect(
              metrics(c, msgId: id)['y'] as double,
              closeTo((after['y'] as double) - 20, 1),
              reason: 'the original drag must continue after publication',
            );
            await gesture.up();
          }
          if (count == 30) {
            expect(im.currentMessages.first.msgId, first);
          } else {
            expect(
              im.currentMessages.first.msgId,
              'probe-${count == 32 ? 1 : 21}',
            );
          }
          await settleHistory(tester);
          await closeOffline(tester);
        }
      }
    },
  );
}
