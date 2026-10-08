import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/local_db_change_bus.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/widgets/chat_scroll_to_bottom_button.dart';
import 'history_test_harness.dart';

class HeldHistory extends OfflineSession {
  final requested = Completer<void>();
  final result = Completer<SessionMessageHistoryResult>();
  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) async {
    if (beforeMsgId != null && beforeMsgId.isNotEmpty) {
      if (!requested.isCompleted) requested.complete();
      return result.future;
    }
    return const SessionMessageHistoryResult(
      code: 0,
      messages: [],
      hasMore: true,
    );
  }
}

void main() {
  testWidgets('gate snapshot must not resurrect revoked rows', (tester) async {
    final im = await initOffline(tester, 'qa-revoke');
    const sid = 'qa-revoke';
    await runDb(
      tester,
      () =>
          LocalDb.batchInsertMessages(List.generate(3, (i) => row(sid, i + 1))),
    );
    final gate = Completer<void>();
    im.enterSession(sid, renderGate: gate.future);
    // Queue barrier: the initial query has completed, but publication waits for gate.
    await runDb(tester, () => LocalDb.getLatestMessages(sid, limit: 31));
    await tester.pump();
    expect(im.currentMessages, isEmpty);
    await runDb(
      tester,
      () => im.applyLocalMessageRevoke(
        sessionId: sid,
        msgId: 'probe-2',
        reloadSessions: false,
      ),
    );
    expect(
      await runDb(tester, () => LocalDb.getMessageByMsgId('probe-2')),
      isNull,
    );
    gate.complete();
    await flushDb(tester, () => im.initialHistoryReady.value);
    evidence('QA-GATE-REVOKE', {
      'ids': im.currentMessages.map((m) => m.msgId).toList(),
      'dbRevoked': true,
    });
    final resurrected = im.currentMessages.any((m) => m.msgId == 'probe-2');
    await closeOffline(tester);
    expect(
      resurrected,
      isFalse,
      reason:
          'a committed revoke during transition must survive first publication',
    );
  });
  testWidgets('bottom reload must respect a later drag before DB publication', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(() {
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    });
    const sid = 'qa-bottom-reload';
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
    expect(im.currentMessages.last.msgId, isNot('probe-500'));
    final held = Completer<void>(), started = Completer<void>();
    final db = await LocalDb.database;
    final tx = db.transaction((_) async {
      started.complete();
      await held.future;
    });
    await flushDb(tester, () => started.isCompleted);
    expect(find.byType(ChatScrollToBottomButton), findsOneWidget);
    await tester.tap(find.byType(ChatScrollToBottomButton));
    await tester.pump();
    final drag = await tester.startGesture(tester.getCenter(chatList(c)));
    await drag.moveBy(const Offset(0, 60));
    await tester.pump();
    final id = leadingId(c, im)!;
    final before = metrics(c, msgId: id);
    held.complete();
    await runDb(tester, () => tx);
    // Drain the operation even when cancellation retains the old window.
    await runDb(tester, () => LocalDb.getLatestMessages(sid, limit: 31));
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 30)),
    );
    await tester.pump();
    await tester.pump();
    final after = metrics(c, msgId: id);
    evidence('QA-BOTTOM-RELOAD', {
      'id': id,
      'before': before,
      'after': after,
      'first': im.currentMessages.first.msgId,
      'last': im.currentMessages.last.msgId,
    });
    await drag.up();
    await settleHistory(tester);
    await closeOffline(tester);
    expect(
      after['y'],
      isNotNull,
      reason: 'latest user reading row must survive cancelled reload intent',
    );
    expect(after['y'] as double, closeTo(before['y'] as double, 1));
  });
  for (final remoteHasMore in [true, false]) {
    testWidgets(
      remoteHasMore
          ? 'remote older backfill must retain latest opposite-edge reading range'
          : 'remote capped terminal page stays locally pageable',
      (tester) async {
        tester.view.physicalSize = const Size(400, 800);
        tester.view.devicePixelRatio = 1;
        addTearDown(() {
          tester.view.resetPhysicalSize();
          tester.view.resetDevicePixelRatio();
        });
        const sid = 'qa-remote-trim';
        final im = await initOffline(tester, sid);
        final remote = HeldHistory();
        Get.delete<SessionService>(force: true);
        Get.put<SessionService>(remote);
        expect(identical(Get.find<SessionService>(), remote), isTrue);
        await runDb(
          tester,
          () => LocalDb.batchInsertMessages(
            List.generate(
              200,
              (i) => row(sid, i + 1, content: 'message ${i + 1}\noffline line'),
            ),
          ),
        );
        final c = await openRoute(tester, sid);
        await settleInitial(tester, im);
        await tester.drag(chatList(c), const Offset(0, 500));
        await tester.pump();
        for (var i = 0; i < 5; i++) {
          await runDb(tester, im.loadOlderForCurrentSession);
          await tester.pump();
          c.scrollController.jumpTo(
            c.scrollController.position.minScrollExtent + 600,
          );
          await tester.pump();
        }
        expect(im.currentMessages.length, 200);
        evidence('QA-REMOTE-pre', {
          'first': im.currentMessages.first.msgId,
          'last': im.currentMessages.last.msgId,
          'remoteIdentity': identical(Get.find<SessionService>(), remote),
          'historyCalls': remote.historyCalls,
        });
        // Trigger the empty local page via actual ChatView notifications.
        final trigger = await tester.startGesture(
          tester.getCenter(chatList(c)),
        );
        for (var move = 0; move < 40 && !remote.requested.isCompleted; move++) {
          await trigger.moveBy(const Offset(0, 20));
          await tester.pump();
          await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 5)),
          );
        }
        await trigger.up();
        await flushDb(tester, () => remote.requested.isCompleted);
        // Reader crossed the window while the actual HTTP history future was pending.
        await tester.drag(chatList(c), const Offset(0, -40000));
        await settleHistory(tester);
        await tester.drag(chatList(c), const Offset(0, 400));
        await settleHistory(tester);
        final drag = await tester.startGesture(tester.getCenter(chatList(c)));
        await drag.moveBy(const Offset(0, 20));
        await tester.pump();
        final id = leadingId(c, im)!;
        final before = metrics(c, msgId: id);
        final inserted = Completer<void>();
        final subscription = LocalDbChangeBus.instance.messageChanges.listen((
          change,
        ) {
          if (change is LocalMessagesInserted &&
              change.sessionId == sid &&
              change.msgIds.contains('probe--39')) {
            inserted.complete();
          }
        });
        remote.result.complete(
          SessionMessageHistoryResult(
            code: 0,
            hasMore: remoteHasMore,
            messages: List.generate(
              40,
              (i) => row(sid, i - 39, content: 'remote ${i - 39}'),
            ),
          ),
        );
        await flushDb(tester, () => inserted.isCompleted);
        unawaited(subscription.cancel());
        await runDb(tester, () => LocalDb.getMessageByMsgId('probe--39'));
        await tester.pump();
        await tester.pump();
        final after = metrics(c, msgId: id);
        evidence('QA-REMOTE-TRIM', {
          'id': id,
          'before': before,
          'after': after,
          'first': im.currentMessages.first.msgId,
          'last': im.currentMessages.last.msgId,
          'visible': im.currentWindowVisibleBubbleCount,
        });
        await drag.up();
        await settleHistory(tester);
        expect(im.currentWindowVisibleBubbleCount, lessThanOrEqualTo(200));
        expect(
          im.hasOlderMessages,
          isTrue,
          reason: 'capped-away rows are persisted older history',
        );
        // Actual later paging must reach both the archived oldest and the latest,
        // without gaps, despite keeping an opposite-edge reading window first.
        for (
          var i = 0;
          i < 7 && im.currentMessages.first.msgId != 'probe--39';
          i++
        ) {
          c.scrollController.jumpTo(
            c.scrollController.position.minScrollExtent + 400,
          );
          await tester.pump();
          await runDb(tester, im.loadOlderForCurrentSession);
          await tester.pump();
        }
        expect(im.currentMessages.first.msgId, 'probe--39');
        expect(im.hasNewerMessages, isTrue);
        for (
          var i = 0;
          i < 7 && im.currentMessages.last.msgId != 'probe-200';
          i++
        ) {
          c.scrollController.jumpTo(
            c.scrollController.position.maxScrollExtent - 400,
          );
          await tester.pump();
          await runDb(tester, im.loadNewerForCurrentSession);
          await tester.pump();
        }
        expect(im.currentMessages.last.msgId, 'probe-200');
        final numbers = im.currentMessages
            .map((m) => int.parse(m.msgId.substring(6)))
            .toList();
        for (var i = 1; i < numbers.length; i++) {
          expect(numbers[i], numbers[i - 1] + 1);
        }
        await closeOffline(tester);
        expect(
          after['y'],
          isNotNull,
          reason: 'remote publication must retain the active reading range',
        );
        expect(after['y'] as double, closeTo(before['y'] as double, 1));
      },
    );
  }
}
