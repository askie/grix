import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/modules/chat/chat_view.dart';

import 'history_test_harness.dart';

void main() {
  testWidgets('production cached push waits for the onstage transition', (
    tester,
  ) async {
    const sid = 'cached-push';
    final im = await initOffline(tester, sid);
    await runDb(
      tester,
      () => LocalDb.batchInsertMessages(
        List.generate(31, (i) => row(sid, i + 1)),
      ),
    );
    // Populate the actual service cache before mounting the production route.
    im.enterSession(sid);
    await flushDb(tester, () => im.initialHistoryReady.value);
    im.leaveSession(sid);
    final publications = <Map<String, Object?>>[];
    final worker = ever(im.currentMessages, (_) {
      if (im.currentMessages.isEmpty) return;
      final route = ModalRoute.of(
        tester.element(find.byType(ChatView, skipOffstage: false)),
      )!;
      publications.add({
        'status': route.animation!.status.name,
        'value': route.animation!.value,
        'offstage': route.offstage,
        'rows': im.currentMessages.length,
      });
    });
    final c = await openRoute(tester, sid, witness: true);
    await runDb(tester, () => LocalDb.getLatestMessages(sid, limit: 31));
    await tester.pump(const Duration(milliseconds: 100));
    expect(im.currentMessages, isEmpty, reason: 'cached window respects push');
    await tester.pump(const Duration(milliseconds: 200));
    await flushDb(tester, () => im.initialHistoryReady.value);
    await c.routeTransitionSettled;
    expect(im.currentMessages.length, 30);
    expect(publications, isNotEmpty);
    expect(
      publications.every(
        (p) => p['status'] == 'completed' && p['offstage'] == false,
      ),
      isTrue,
    );
    evidence('CACHE-PUBLISH', {'publications': publications});
    worker.dispose();
    await closeOffline(tester);
  });
}
