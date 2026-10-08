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
  testWidgets('control gate without revoke preserves original rows', (
    tester,
  ) async {
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
    expect(
      await runDb(tester, () => LocalDb.getMessageByMsgId('probe-2')),
      isNotNull,
    );
    gate.complete();
    await flushDb(tester, () => im.initialHistoryReady.value);
    evidence('QA-GATE-REVOKE', {
      'ids': im.currentMessages.map((m) => m.msgId).toList(),
      'dbRevoked': false,
    });
    final resurrected = im.currentMessages.any((m) => m.msgId == 'probe-2');
    await closeOffline(tester);
    expect(
      resurrected,
      isTrue,
      reason: 'unchanged DB row must appear after gate',
    );
  });
  testWidgets(
    'control bottom reload without later drag reaches latest bottom',
    (tester) async {
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
      await tester.pump();
      final id = leadingId(c, im)!;
      final before = metrics(c, msgId: id);
      held.complete();
      await runDb(tester, () => tx);
      await flushDb(tester, () => im.currentMessages.last.msgId == 'probe-500');
      await tester.pump();
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      await settleHistory(tester);
      final after = metrics(c, msgId: id);
      evidence('QA-BOTTOM-RELOAD', {
        'id': id,
        'before': before,
        'after': after,
        'first': im.currentMessages.first.msgId,
        'last': im.currentMessages.last.msgId,
      });
      final distance = c.scrollController.position.extentAfter;
      await closeOffline(tester);
      expect(distance, lessThanOrEqualTo(1));
    },
  );
  testWidgets('control remote older backfill at older edge keeps reading', (
    tester,
  ) async {
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
    final trigger = await tester.startGesture(tester.getCenter(chatList(c)));
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
    c.scrollController.jumpTo(
      c.scrollController.position.minScrollExtent + 600,
    );
    await tester.pump();
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
        hasMore: true,
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
    await closeOffline(tester);
    expect(
      after['y'],
      isNotNull,
      reason: 'remote publication must retain the active reading range',
    );
    expect(after['y'] as double, closeTo(before['y'] as double, 1));
  });
}
