import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/session_service.dart';
import 'history_test_harness.dart';

class _HeldOlderHistory extends OfflineSession {
  final requested = Completer<void>();
  final result = Completer<SessionMessageHistoryResult>();

  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) {
    if (beforeMsgId != null && beforeMsgId.isNotEmpty) {
      if (!requested.isCompleted) requested.complete();
      return result.future;
    }
    return Future.value(
      const SessionMessageHistoryResult(code: 0, messages: [], hasMore: false),
    );
  }
}

List<String> _ids(ImService im) =>
    im.currentMessages.map((m) => m.msgId).toList();

Future<ImService> _openHistory(WidgetTester tester, String sid) async {
  final im = await initOffline(tester, sid);
  await runDb(
    tester,
    () =>
        LocalDb.batchInsertMessages(List.generate(500, (i) => row(sid, i + 1))),
  );
  im.enterSession(sid);
  await flushDb(tester, () => im.initialHistoryReady.value);
  for (var i = 0; i < 7; i++) {
    await runDb(tester, im.loadOlderForCurrentSession);
  }
  expect(_ids(im), List.generate(200, (i) => 'probe-${191 + i}'));
  return im;
}

Future<(Completer<void>, Future<void>)> _holdDb(WidgetTester tester) async {
  final held = Completer<void>(), entered = Completer<void>();
  final db = await LocalDb.database;
  late Future<void> tx;
  await tester.runAsync(() async {
    tx = db.transaction((_) async {
      entered.complete();
      await held.future;
    });
  });
  await flushDb(tester, () => entered.isCompleted);
  return (held, tx);
}

void main() {
  testWidgets('pending older cannot prepend to a replaced latest window', (
    tester,
  ) async {
    const sid = 'reload-pending-older';
    final im = await _openHistory(tester, sid);
    final version = im.messageWindowVersionForTest;
    final entry = im.currentSessionGeneration;
    final (held, tx) = await _holdDb(tester);
    final windows = <List<String>>[];
    final worker = ever(im.currentMessages, (_) => windows.add(_ids(im)));
    late Future<void> reload, older;
    await tester.runAsync(() async {
      reload = im.forceReloadSessionWindow(sid, triggerPullSync: false);
      await Future<void>.delayed(const Duration(milliseconds: 30));
      older = im.loadOlderForCurrentSession();
    });
    held.complete();
    await runDb(tester, () => tx);
    await runDb(tester, () => reload);
    await runDb(tester, () => older);
    expect(_ids(im), List.generate(30, (i) => 'probe-${471 + i}'));
    expect(windows, [_ids(im)]);
    expect(im.hasNewerMessages, isFalse);
    expect(im.hasOlderMessages, isTrue);
    expect(im.currentSessionGeneration, entry);
    expect(im.messageWindowVersionForTest, version + 1);
    evidence('RELOAD-OLDER', {
      'windows': windows,
      'entry': entry,
      'windowVersion': im.messageWindowVersionForTest,
      'hasOlder': im.hasOlderMessages,
      'hasNewer': im.hasNewerMessages,
    });
    worker.dispose();
    // The old future also released the paging lock. A fresh older query works.
    await runDb(tester, im.loadOlderForCurrentSession);
    expect(_ids(im), List.generate(70, (i) => 'probe-${431 + i}'));
    await closeOffline(tester);
  });

  testWidgets(
    'a legal page may finish before latest and repeated reloads still work',
    (tester) async {
      const sid = 'reload-after-page';
      final im = await _openHistory(tester, sid);
      final version = im.messageWindowVersionForTest;
      await runDb(tester, im.loadNewerForCurrentSession);
      expect(_ids(im), List.generate(200, (i) => 'probe-${231 + i}'));
      expect(im.messageWindowVersionForTest, version);
      await runDb(
        tester,
        () => im.forceReloadSessionWindow(sid, triggerPullSync: false),
      );
      expect(_ids(im), List.generate(30, (i) => 'probe-${471 + i}'));
      await runDb(
        tester,
        () => im.forceReloadSessionWindow(sid, triggerPullSync: false),
      );
      expect(_ids(im), List.generate(30, (i) => 'probe-${471 + i}'));
      expect(im.messageWindowVersionForTest, version + 2);
      expect(im.hasNewerMessages, isFalse);
      await closeOffline(tester);
    },
  );

  testWidgets('cancelled latest reload preserves a valid pending newer page', (
    tester,
  ) async {
    const sid = 'reload-cancelled-page';
    final im = await _openHistory(tester, sid);
    final version = im.messageWindowVersionForTest;
    final (held, tx) = await _holdDb(tester);
    var publish = true;
    late Future<void> reload, newer;
    await tester.runAsync(() async {
      reload = im.forceReloadSessionWindow(
        sid,
        triggerPullSync: false,
        shouldPublish: () => publish,
      );
      await Future<void>.delayed(const Duration(milliseconds: 30));
      newer = im.loadNewerForCurrentSession();
    });
    publish = false;
    held.complete();
    await runDb(tester, () => tx);
    await runDb(tester, () => reload);
    await runDb(tester, () => newer);
    expect(_ids(im), List.generate(200, (i) => 'probe-${231 + i}'));
    expect(im.messageWindowVersionForTest, version);
    expect(im.hasNewerMessages, isTrue);
    // Neither a cancelled replace nor the completed page leaves a stuck lock.
    await runDb(tester, im.loadNewerForCurrentSession);
    expect(_ids(im), List.generate(200, (i) => 'probe-${271 + i}'));
    await closeOffline(tester);
  });

  for (final empty in [false, true]) {
    testWidgets('moved boundary invalidates pending newer empty=$empty', (
      tester,
    ) async {
      const sid = 'page-boundary-changed';
      final im = await _openHistory(tester, sid);
      final version = im.messageWindowVersionForTest;
      if (empty) {
        final db = await LocalDb.database;
        await runDb(
          tester,
          () => db.delete(
            'messages',
            where: 'session_id = ? AND created_at > ?',
            whereArgs: [sid, 1700000000000 + 390 * 1000],
          ),
        );
      }
      final (held, tx) = await _holdDb(tester);
      late Future<void> older, newer;
      await tester.runAsync(() async {
        older = im.loadOlderForCurrentSession();
        newer = im.loadNewerForCurrentSession();
      });
      held.complete();
      await runDb(tester, () => tx);
      await runDb(tester, () => older);
      await runDb(tester, () => newer);
      expect(_ids(im), List.generate(200, (i) => 'probe-${151 + i}'));
      expect(im.messageWindowVersionForTest, version);
      expect(im.hasNewerMessages, isTrue);
      await runDb(tester, im.loadNewerForCurrentSession);
      expect(_ids(im), List.generate(200, (i) => 'probe-${191 + i}'));
      await closeOffline(tester);
    });
  }

  for (final outcome in ['rows', 'empty', 'hasMore', 'error']) {
    testWidgets(
      'obsolete remote older $outcome cannot publish or change latest flags',
      (tester) async {
        final sid = 'reload-remote-$outcome';
        final im = await initOffline(tester, sid);
        await runDb(
          tester,
          () => LocalDb.batchInsertMessages(
            List.generate(30, (i) => row(sid, i + 1)),
          ),
        );
        im.enterSession(sid);
        await flushDb(tester, () => im.initialHistoryReady.value);
        final remote = _HeldOlderHistory();
        Get.delete<SessionService>(force: true);
        Get.put<SessionService>(remote);
        late Future<void> older;
        await tester.runAsync(() async {
          older = im.loadOlderForCurrentSessionAwaitingBackfill();
        });
        await flushDb(tester, () => remote.requested.isCompleted);
        await runDb(
          tester,
          () => im.forceReloadSessionWindow(sid, triggerPullSync: false),
        );
        final ids = _ids(im);
        final version = im.messageWindowVersionForTest;
        expect(im.hasOlderMessages, isFalse);
        expect(im.hasNewerMessages, isFalse);
        final windows = <List<String>>[];
        final worker = ever(im.currentMessages, (_) => windows.add(_ids(im)));
        if (outcome == 'error') {
          remote.result.completeError(StateError('offline archive failure'));
        } else {
          remote.result.complete(
            SessionMessageHistoryResult(
              code: 0,
              hasMore: outcome == 'hasMore',
              messages: outcome == 'rows'
                  ? List.generate(40, (i) => row(sid, i - 39))
                  : [],
            ),
          );
        }
        await runDb(tester, () => older);
        expect(_ids(im), ids);
        expect(windows, isEmpty);
        expect(im.messageWindowVersionForTest, version);
        expect(im.hasOlderMessages, isFalse);
        expect(im.hasNewerMessages, isFalse);
        evidence('RELOAD-REMOTE', {
          'outcome': outcome,
          'windows': windows,
          'entry': im.currentSessionGeneration,
          'windowVersion': version,
          'ids': ids,
          'hasOlder': im.hasOlderMessages,
          'hasNewer': im.hasNewerMessages,
        });
        if (outcome == 'rows') {
          expect(
            await runDb(tester, () => LocalDb.getMessageByMsgId('probe-0')),
            isNotNull,
            reason: 'obsolete paging still persists archive rows',
          );
        }
        worker.dispose();
        await closeOffline(tester);
      },
    );
  }
}
