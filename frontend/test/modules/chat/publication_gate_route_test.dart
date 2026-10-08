import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';
import 'history_test_harness.dart';

void main() {
  for (final revoke in [false, true]) {
    testWidgets('actual GetPage gate revoke=$revoke', (tester) async {
      const sid = 'qa-route-revoke';
      final im = await initOffline(tester, sid);
      await runDb(
        tester,
        () => LocalDb.batchInsertMessages(
          List.generate(31, (i) => row(sid, i + 1)),
        ),
      );
      final c = await openRoute(tester, sid);
      await runDb(tester, () => LocalDb.getLatestMessages(sid, limit: 31));
      await tester.pump();
      expect(im.currentMessages, isEmpty);
      if (revoke) {
        await runDb(
          tester,
          () => im.applyLocalMessageRevoke(
            sessionId: sid,
            msgId: 'probe-22',
            reloadSessions: false,
          ),
        );
      }
      final dbRow = await runDb(
        tester,
        () => LocalDb.getMessageByMsgId('probe-22'),
      );
      expect(dbRow == null, revoke);
      await tester.pump(const Duration(milliseconds: 300));
      await flushDb(tester, () => im.initialHistoryReady.value);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 350));
      await tester.pumpAndSettle();
      final shown = im.currentMessages.any((m) => m.msgId == 'probe-22');
      evidence('QA-ACTUAL-ROUTE-REVOKE', {
        'revoke': revoke,
        'dbRow': dbRow == null ? 'absent' : 'present',
        'shown': shown,
        'rows': im.currentMessages.length,
        'metrics': metrics(c, msgId: 'probe-22'),
      });
      await closeOffline(tester);
      expect(
        shown,
        !revoke,
        reason:
            'first production route publication must reflect completed revoke',
      );
    });
  }
}
