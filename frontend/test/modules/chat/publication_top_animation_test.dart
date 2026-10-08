import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';
import 'history_test_harness.dart';

void main() {
  for (final count in [30, 100]) {
    testWidgets('loaded-top animation count=$count remains coherent', (
      tester,
    ) async {
      tester.view.physicalSize = const Size(400, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(() {
        tester.view.resetPhysicalSize();
        tester.view.resetDevicePixelRatio();
      });
      const sid = 'qa-loaded-top';
      final im = await initOffline(tester, sid);
      await runDb(
        tester,
        () => LocalDb.batchInsertMessages(
          List.generate(
            count,
            (i) => row(sid, i + 1, content: 'message ${i + 1}\nline'),
          ),
        ),
      );
      final c = await openRoute(tester, sid);
      await settleInitial(tester, im);
      await tester.drag(chatList(c), const Offset(0, 400));
      await settleHistory(tester);
      c.scrollController.jumpTo(
        c.scrollController.position.minScrollExtent + 600,
      );
      await tester.pump();
      final held = Completer<void>(), entered = Completer<void>();
      final db = await LocalDb.database;
      final tx = db.transaction((_) async {
        entered.complete();
        await held.future;
      });
      await flushDb(tester, () => entered.isCompleted);
      c.scrollToLoadedTop();
      await tester.pump();
      for (var i = 0; i < 20 && !c.isLoadingOlderHistory; i++) {
        await tester.pump(const Duration(milliseconds: 8));
      }
      expect(c.isLoadingOlderHistory, isTrue);
      final id = leadingId(c, im)!;
      final before = metrics(c, msgId: id);
      held.complete();
      await runDb(tester, () => tx);
      await flushDb(tester, () => !c.isLoadingOlderHistory);
      await tester.pump();
      final atPublish = metrics(c, msgId: id);
      await tester.pump(const Duration(milliseconds: 8));
      final next = metrics(c, msgId: id);
      evidence('QA-TOP-ANIMATION', {
        'id': id,
        'before': before,
        'atPublish': atPublish,
        'next': next,
        'count': count,
        'first': im.currentMessages.first.msgId,
        'min': c.scrollController.position.minScrollExtent,
      });
      final y1 = atPublish['y'] as double?, y2 = next['y'] as double?;
      await settleHistory(tester);
      await closeOffline(tester);
      expect(y1, isNotNull);
      expect(y2, isNotNull);
      expect(
        y1!,
        closeTo(before['y'] as double, 1),
        reason:
            'pagination at the same animation timestamp must retain same-ID geometry',
      );
      expect(
        (y2! - y1).abs(),
        lessThan(100),
        reason:
            '8ms of the original 600px easeOut must not jump an origin-sized distance',
      );
    });
  }
}
