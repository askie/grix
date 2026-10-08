import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';
import 'history_test_harness.dart';

void main() {
  testWidgets('production delayed newer paging preserves a reverse drag', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(() {
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    });
    const sid = 'newer-race';
    final im = await initOffline(tester, sid);
    await runDb(
      tester,
      () => LocalDb.batchInsertMessages(
        List.generate(
          500,
          (i) => row(sid, i + 1, content: 'message ${i + 1}\noffline line'),
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
      c.scrollController.jumpTo(
        c.scrollController.position.minScrollExtent + 330,
      );
      await tester.pump();
    }
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pumpAndSettle();
    expect(im.hasNewerMessages, isTrue);
    expect(im.currentMessages.length, 200);
    final hold = Completer<void>(), entered = Completer<void>();
    final db = await LocalDb.database;
    final tx = db.transaction((_) async {
      entered.complete();
      await hold.future;
    });
    await flushDb(tester, () => entered.isCompleted);
    c.scrollController.jumpTo(
      c.scrollController.position.maxScrollExtent - 330,
    );
    await tester.pump();
    final gesture = await tester.startGesture(tester.getCenter(chatList(c)));
    for (
      int i = 0;
      i < 30 &&
          c.scrollController.position.maxScrollExtent -
                  c.scrollController.position.pixels >
              160;
      i++
    ) {
      await gesture.moveBy(const Offset(0, -20));
      await tester.pump();
    }
    final firstNewer = im.currentMessages.last.msgId;
    final id = leadingId(c, im);
    evidence('NEWER-trigger-zone', metrics(c, msgId: id));
    await gesture.moveBy(const Offset(0, 500));
    await tester.pump();
    final user = metrics(c, msgId: id);
    evidence('NEWER-reverse-user', user);
    hold.complete();
    await runDb(tester, () => tx);
    await flushDb(tester, () => im.currentMessages.last.msgId != firstNewer);
    await tester.pump();
    await tester.pump();
    final after = metrics(c, msgId: id);
    evidence('NEWER-restored', {
      'user': user,
      'after': after,
      'first': im.currentMessages.first.msgId,
      'last': im.currentMessages.last.msgId,
      'hasNewer': im.hasNewerMessages,
    });
    expect(after['y'], isNotNull);
    expect((after['y'] as double) - (user['y'] as double), closeTo(0, 1));
    expect(
      c.scrollController.position.maxScrollExtent -
          c.scrollController.position.pixels,
      greaterThan(500),
    );
    expect(
      (user['max'] as double) - (user['pixels'] as double),
      greaterThan(500),
    );
    await gesture.up();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 120));
    await tester.pumpAndSettle();
    evidence('NEWER-settled', metrics(c));
    expect(
      c.scrollController.position.maxScrollExtent -
          c.scrollController.position.pixels,
      greaterThan(500),
    );
    await closeOffline(tester);
  });
}
