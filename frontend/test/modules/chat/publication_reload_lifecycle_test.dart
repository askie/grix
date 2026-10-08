import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';

import 'history_test_harness.dart';

void main() {
  for (final action in ['release', 'retry', 'aba', 'close']) {
    testWidgets('bottom button pending publication lifecycle $action', (
      tester,
    ) async {
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(() {
        tester.view.resetPhysicalSize();
        tester.view.resetDevicePixelRatio();
      });
      const sid = 'bottom-lifecycle';
      final im = await initOffline(tester, sid);
      await runDb(
        tester,
        () => LocalDb.batchInsertMessages(
          List.generate(
            500,
            (i) => row(sid, i + 1, content: 'message ${i + 1}\nline'),
          ),
        ),
      );
      final c = await openRoute(tester, sid);
      await settleInitial(tester, im);
      await tester.drag(chatList(c), const Offset(0, 500));
      for (var i = 0; i < 7; i++) {
        await runDb(tester, im.loadOlderForCurrentSession);
        await tester.pump();
        c.scrollController.jumpTo(
          c.scrollController.position.minScrollExtent + 600,
        );
        await tester.pump();
      }
      await settleHistory(tester);
      expect(im.hasNewerMessages, isTrue);
      final ids = im.currentMessages.map((m) => m.msgId).toList();
      final older = im.hasOlderMessages, newer = im.hasNewerMessages;
      final held = Completer<void>(), started = Completer<void>();
      final db = await LocalDb.database;
      final tx = db.transaction((_) async {
        started.complete();
        await held.future;
      });
      await flushDb(tester, () => started.isCompleted);
      // Awaitable production button entry; the matching QA regression also
      // taps its actual widget. No service/controller override is involved.
      final button = c.onScrollToBottomButtonPressed();
      await tester.pump();
      final drag = await tester.startGesture(tester.getCenter(chatList(c)));
      await drag.moveBy(const Offset(0, 60));
      await tester.pump();
      final id = leadingId(c, im)!;
      final before = metrics(c, msgId: id);
      await drag.up();
      final gate = Completer<void>();
      if (action == 'aba') {
        im.enterSession('b');
        im.enterSession(sid, renderGate: gate.future);
      } else if (action == 'close') {
        await tester.pumpWidget(const SizedBox.shrink());
        await tester.pump();
      }
      held.complete();
      await runDb(tester, () => tx);
      await runDb(tester, () => button);
      await tester.pump();
      if (action == 'aba') {
        expect(im.currentMessages, isEmpty);
        gate.complete();
        await flushDb(tester, () => im.initialHistoryReady.value);
      } else if (action == 'close') {
        expect(im.currentMessages, isEmpty);
      } else {
        final after = metrics(c, msgId: id);
        expect(im.currentMessages.map((m) => m.msgId), ids);
        expect(im.hasOlderMessages, older);
        expect(im.hasNewerMessages, newer);
        expect(after['y'], isNotNull);
        expect(after['y'] as double, closeTo(before['y'] as double, 1));
        if (action == 'retry') {
          await settleHistory(tester);
          await runDb(tester, c.onScrollToBottomButtonPressed);
          await tester.pump();
          await settleHistory(tester);
          expect(im.currentMessages.last.msgId, 'probe-500');
          expect(c.scrollController.position.extentAfter, lessThanOrEqualTo(1));
        }
      }
      await settleHistory(tester);
      await closeOffline(tester);
    });
  }
}
