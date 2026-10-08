import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/local_db_change_bus.dart';
import 'history_test_harness.dart';

void main() {
  testWidgets(
    'control old rowless DB update does not publish into another session',
    (tester) async {
      final im = await initOffline(tester, 'qa-async-update');
      await runDb(
        tester,
        () => LocalDb.batchInsertMessages([
          {...row('a', 1), 'local_seq': 'qa-client-1'},
          row('b', 2),
        ]),
      );
      im.enterSession('a');
      await flushDb(tester, () => im.initialHistoryReady.value);
      await runDb(
        tester,
        () => LocalDb.updateMessageStatusByLocalSeq(
          'qa-client-1',
          'failed_delegate',
        ),
      );
      final held = Completer<void>(), entered = Completer<void>();
      final db = await LocalDb.database;
      final tx = db.transaction((_) async {
        entered.complete();
        await held.future;
      });
      await flushDb(tester, () => entered.isCompleted);
      // Real rowless event shape emitted by outbound failure/status persistence.
      LocalDbChangeBus.instance.emitMessageChange(
        LocalMessageUpdated(sessionId: 'a', msgId: 'probe-1'),
      );
      final gate = Completer<void>();
      im.enterSession('b', renderGate: gate.future);
      held.complete();
      await runDb(tester, () => tx);
      await runDb(tester, () => LocalDb.getLatestMessages('a', limit: 31));
      await tester.pump();
      final ids = im.currentMessages.map((m) => m.msgId).toList();
      evidence('QA-ASYNC-UPDATE-ABA', {
        'idsBeforeGate': ids,
        'ready': im.initialHistoryReady.value,
        'generation': im.currentSessionGeneration,
      });
      gate.complete();
      await flushDb(tester, () => im.initialHistoryReady.value);
      await closeOffline(tester);
      expect(
        ids,
        isEmpty,
        reason:
            'superseded async DB fallback must not bypass the current render gate',
      );
    },
  );
}
