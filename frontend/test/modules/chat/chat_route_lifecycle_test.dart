import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/local_db_change_bus.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';

import 'history_test_harness.dart';

void main() {
  testWidgets('embedded entry and superseded cache/DB gates finish safely', (
    tester,
  ) async {
    final im = await initOffline(tester, 'lifecycle');
    await runDb(
      tester,
      () => LocalDb.batchInsertMessages([
        ...List.generate(3, (i) => row('a', i + 1)),
        row('b', 10),
      ]),
    );
    final c = Get.put(
      ChatController(
        routeArguments: const {
          'session_id': 'a',
          'title': 'a',
          'type': 'private',
        },
      ),
    );
    await tester.pumpWidget(GetMaterialApp(home: ChatView(embedded: true)));
    await tester.pump();
    await flushDb(tester, () => im.initialHistoryReady.value);
    expect(im.currentMessages.length, 3);
    var embeddedGate = false;
    c.routeTransitionSettled.then((_) => embeddedGate = true);
    await tester.pump();
    expect(embeddedGate, isTrue);
    await tester.pumpWidget(const SizedBox.shrink());
    Get.delete<ChatController>(force: true);
    await tester.pump();

    // A -> B -> A: an earlier A must not publish into a later A entry.
    final old = Completer<void>(), current = Completer<void>();
    im.enterSession('a', renderGate: old.future);
    await runDb(tester, () => LocalDb.getLatestMessages('a', limit: 31));
    im.enterSession('b');
    await flushDb(tester, () => im.initialHistoryReady.value);
    await runDb(tester, () => LocalDb.batchInsertMessages([row('a', 4)]));
    im.enterSession('a', renderGate: current.future);
    LocalDbChangeBus.instance.emitMessageChange(
      LocalMessagesInserted(
        sessionId: "a",
        msgIds: ["probe-4"],
        maxCreatedAt: row("a", 4)["created_at"] as int,
        rows: [row("a", 4)],
      ),
    );
    await tester.pump();
    expect(im.currentMessages, isEmpty);
    old.complete();
    await tester.pump();
    expect(im.currentMessages, isEmpty);
    expect(im.initialHistoryReady.value, isFalse);
    current.complete();
    await flushDb(tester, () => im.initialHistoryReady.value);
    expect(im.currentMessages.map((m) => m.msgId), [
      'probe-1',
      'probe-2',
      'probe-3',
      'probe-4',
    ]);

    // A stale real local pagination query cannot publish after P -> B -> P.
    await runDb(
      tester,
      () => LocalDb.batchInsertMessages(
        List.generate(100, (i) => row('p', i + 101)),
      ),
    );
    im.enterSession('p');
    await flushDb(tester, () => im.initialHistoryReady.value);
    final hold = Completer<void>(), entered = Completer<void>();
    final db = await LocalDb.database;
    final transaction = db.transaction((_) async {
      entered.complete();
      await hold.future;
    });
    await flushDb(tester, () => entered.isCompleted);
    final page = im.loadOlderForCurrentSession();
    im.enterSession('b');
    im.enterSession('p');
    final firsts = <String>[];
    final worker = ever(im.currentMessages, (_) {
      if (im.currentMessages.isNotEmpty) {
        firsts.add(im.currentMessages.first.msgId);
      }
    });
    hold.complete();
    await runDb(tester, () => transaction);
    await runDb(tester, () => page);
    await flushDb(tester, () => im.initialHistoryReady.value);
    expect(firsts, isNotEmpty);
    expect(firsts.every((id) => id == 'probe-171'), isTrue);
    expect(im.currentMessages.length, 30);
    worker.dispose();
    im.leaveSession('p');

    im.leaveSession('a');
    final closed = Completer<void>();
    im.enterSession('a', renderGate: closed.future);
    im.leaveSession('a');
    closed.complete();
    await runDb(tester, () => LocalDb.getLatestMessages('a', limit: 31));
    await tester.pump();
    expect(im.currentMessages, isEmpty);
    expect(im.currentSessionId, isNull);
    await closeOffline(tester);
  });
}
