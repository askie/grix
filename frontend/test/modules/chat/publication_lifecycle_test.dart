import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/local_db_change_bus.dart';

import 'history_test_harness.dart';

void main() {
  for (final cached in [false, true]) {
    for (final gated in [false, true]) {
      testWidgets('committed gate mutations cached=$cached gated=$gated', (
        tester,
      ) async {
        const sid = 'gate-mutations';
        final im = await initOffline(tester, sid);
        await runDb(
          tester,
          () => LocalDb.batchInsertMessages(
            List.generate(31, (i) => row(sid, i + 1)),
          ),
        );
        if (cached) {
          im.enterSession(sid);
          await flushDb(tester, () => im.initialHistoryReady.value);
          im.leaveSession(sid);
        }
        if (gated) {
          await openRoute(tester, sid);
        } else {
          im.enterSession(sid);
        }
        // Actual storage has read the pre-change snapshot. A route remains
        // forward at this barrier, while the no-gate control may publish it.
        await runDb(tester, () => LocalDb.getLatestMessages(sid, limit: 31));
        if (gated) expect(im.currentMessages, isEmpty);
        await runDb(
          tester,
          () => im.applyLocalMessageRevoke(
            sessionId: sid,
            msgId: 'probe-22',
            reloadSessions: false,
          ),
        );
        final edits = await runDb(
          tester,
          () => LocalDb.applyArchiveMessages([
            {
              ...row(sid, 21, content: 'edited during gate'),
              'state_version': '2',
            },
            row(sid, 32),
          ]),
        );
        expect(edits.persisted, isTrue);
        // Real persisted rows, with the same bus shape as archive backfill.
        LocalDbChangeBus.instance.emitMessageChange(
          LocalMessagesInserted(
            sessionId: sid,
            msgIds: edits.changedRows
                .map((r) => r['msg_id'] as String)
                .toList(),
            maxCreatedAt: 1700000032000,
            rows: edits.changedRows,
          ),
        );
        final snapshots = <List<String>>[];
        final worker = ever(im.currentMessages, (_) {
          snapshots.add(im.currentMessages.map((m) => m.msgId).toList());
        });
        await tester.pump(const Duration(milliseconds: 300));
        await flushDb(tester, () => im.initialHistoryReady.value);
        for (var i = 0; i < 8; i++) {
          await tester.pump(const Duration(milliseconds: 16));
          expect(im.currentMessages.any((m) => m.msgId == 'probe-22'), isFalse);
        }
        expect(snapshots.every((ids) => !ids.contains('probe-22')), isTrue);
        expect(
          im.currentMessages.singleWhere((m) => m.msgId == 'probe-21').content,
          'edited during gate',
        );
        expect(im.currentMessages.any((m) => m.msgId == 'probe-32'), isTrue);
        worker.dispose();
        await closeOffline(tester);
      });
    }
  }

  for (final action in ['same', 'aba', 'leave', 'reset']) {
    testWidgets('rowless failed_delegate read lifecycle $action', (
      tester,
    ) async {
      final im = await initOffline(tester, 'rowless-$action');
      await runDb(
        tester,
        () => LocalDb.batchInsertMessages([
          {...row('a', 1), 'local_seq': 'client-1'},
          row('b', 2),
        ]),
      );
      im.enterSession('a');
      await flushDb(tester, () => im.initialHistoryReady.value);
      await runDb(
        tester,
        () => LocalDb.updateMessageStatusByLocalSeq(
          'client-1',
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
      LocalDbChangeBus.instance.emitMessageChange(
        LocalMessageUpdated(sessionId: 'a', msgId: 'probe-1'),
      );
      final gate = Completer<void>();
      if (action == 'aba') {
        im.enterSession('b');
        im.enterSession('a', renderGate: gate.future);
      } else if (action == 'leave') {
        im.leaveSession();
      } else if (action == 'reset') {
        await im.resetForAccountSwitch();
      }
      held.complete();
      await runDb(tester, () => tx);
      await runDb(tester, () => LocalDb.getMessageByMsgId('probe-1'));
      await tester.pump();
      if (action == 'same') {
        expect(im.currentMessages.single.status, 'failed_delegate');
      } else {
        expect(im.currentMessages, isEmpty);
      }
      if (action == 'aba') {
        gate.complete();
        await flushDb(tester, () => im.initialHistoryReady.value);
        expect(im.currentMessages.single.status, 'failed_delegate');
      }
      await closeOffline(tester);
    });
  }

  testWidgets(
    'failed render gate never publishes cached or DB rows and re-entry recovers',
    (tester) async {
      const sid = 'gate-error';
      final im = await initOffline(tester, sid);
      await runDb(tester, () => LocalDb.batchInsertMessages([row(sid, 1)]));
      im.enterSession(sid);
      await flushDb(tester, () => im.initialHistoryReady.value);
      im.leaveSession();
      final gate = Completer<void>();
      im.enterSession(sid, renderGate: gate.future);
      await runDb(tester, () => LocalDb.getLatestMessages(sid, limit: 31));
      gate.completeError(StateError('cancelled transition'));
      await tester.pump();
      expect(im.currentMessages, isEmpty);
      im.leaveSession();
      im.enterSession(sid);
      await flushDb(tester, () => im.initialHistoryReady.value);
      expect(im.currentMessages.single.msgId, 'probe-1');
      await closeOffline(tester);
    },
  );
}
