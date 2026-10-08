import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/local_db.dart';
import 'history_test_harness.dart';

void main() {
  for (final pendingNewer in [false, true]) {
    testWidgets(
      'pending newer page cannot corrupt latest reload pendingNewer=$pendingNewer',
      (tester) async {
        const sid = 'qa-reload-paging-race';
        final im = await initOffline(tester, sid);
        await runDb(
          tester,
          () => LocalDb.batchInsertMessages(
            List.generate(500, (i) => row(sid, i + 1)),
          ),
        );
        im.enterSession(sid);
        await flushDb(tester, () => im.initialHistoryReady.value);
        for (var i = 0; i < 7; i++) {
          await runDb(tester, im.loadOlderForCurrentSession);
        }
        expect(im.currentMessages.first.msgId, 'probe-191');
        expect(im.currentMessages.last.msgId, 'probe-390');
        expect(im.hasNewerMessages, isTrue);
        final held = Completer<void>(), entered = Completer<void>();
        final db = await LocalDb.database;
        final tx = db.transaction((_) async {
          entered.complete();
          await held.future;
        });
        await flushDb(tester, () => entered.isCompleted);
        final entry = im.currentSessionGeneration;
        final version = im.messageWindowVersionForTest;
        final windows = <List<String>>[];
        final worker = ever(
          im.currentMessages,
          (_) => windows.add(im.currentMessages.map((m) => m.msgId).toList()),
        );
        late Future<void> reload;
        await tester.runAsync(() async {
          reload = im.forceReloadSessionWindow(sid, triggerPullSync: false);
          // Successful offline HTTP result; latest query is now held at real DB.
          await Future<void>.delayed(const Duration(milliseconds: 30));
        });
        Future<void>? newer;
        if (pendingNewer) {
          await tester.runAsync(() async {
            newer = im.loadNewerForCurrentSession();
          });
        }
        held.complete();
        await runDb(tester, () => tx);
        await runDb(tester, () => reload);
        if (newer != null) await runDb(tester, () => newer!);
        final ids = im.currentMessages.map((m) => m.msgId).toList();
        final flag = im.hasNewerMessages;
        evidence('RELOAD-PAGING', {
          'pendingNewer': pendingNewer,
          'generation': im.currentSessionGeneration,
          'windowVersionBefore': version,
          'windowVersionAfter': im.messageWindowVersionForTest,
          'oldBoundary': ['probe-191', 'probe-390'],
          'hasOlder': im.hasOlderMessages,
          'windows': windows,
          'ids': ids,
          'hasNewer': flag,
        });
        expect(im.currentSessionGeneration, entry);
        expect(im.messageWindowVersionForTest, version + 1);
        expect(windows, [List.generate(30, (i) => 'probe-${471 + i}')]);
        worker.dispose();
        await closeOffline(tester);
        expect(ids, List.generate(30, (i) => 'probe-${471 + i}'));
        expect(
          ids.last,
          'probe-500',
          reason:
              'a page queried against a replaced window cannot append behind the authoritative latest tail',
        );
        expect(flag, isFalse);
        final nums = ids.map((id) => int.parse(id.substring(6))).toList();
        for (var i = 1; i < nums.length; i++) expect(nums[i], nums[i - 1] + 1);
      },
    );
  }
}
