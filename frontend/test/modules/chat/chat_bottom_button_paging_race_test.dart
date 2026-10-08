import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/modules/chat/widgets/chat_scroll_to_bottom_button.dart';
import 'history_test_harness.dart';

void main() {
  for (final pendingNewer in [false, true]) {
    testWidgets(
      'bottom button reaches latest with automatic newer=$pendingNewer',
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
        final entry = im.currentSessionGeneration;
        final version = im.messageWindowVersionForTest;
        final windows = <List<String>>[];
        final worker = ever(im.currentMessages, (_) {
          windows.add(im.currentMessages.map((m) => m.msgId).toList());
        });
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
        await tester.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 30)),
        );
        // Existing bottom-follow positioning consumer does not change user intent.
        if (pendingNewer) {
          c.scrollToBottom();
          await tester.pump();
          await tester.pump(const Duration(milliseconds: 100));
        }
        final id = leadingId(c, im)!;
        final before = metrics(c, msgId: id);
        held.complete();
        await runDb(tester, () => tx);
        await runDb(tester, () => LocalDb.getLatestMessages(sid, limit: 31));
        await tester.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 30)),
        );
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
        final actualIds = im.currentMessages.map((m) => m.msgId).toList();
        final hasNewer = im.hasNewerMessages;
        final bottomVisible = c.scrollToBottomButtonVisible.value;
        evidence('QA-STRICT-BOTTOM', {
          'ids': actualIds,
          'windows': windows,
          'generation': im.currentSessionGeneration,
          'windowVersionBefore': version,
          'windowVersionAfter': im.messageWindowVersionForTest,
          'hasOlder': im.hasOlderMessages,
          'hasNewer': hasNewer,
          'bottomVisible': bottomVisible,
        });
        expect(im.currentSessionGeneration, entry);
        expect(im.messageWindowVersionForTest, version + 1);
        expect(windows, [List.generate(30, (i) => 'probe-${471 + i}')]);
        worker.dispose();
        await closeOffline(tester);
        expect(distance, lessThanOrEqualTo(1));
        expect(actualIds, List.generate(30, (i) => 'probe-${471 + i}'));
        expect(
          actualIds.last,
          'probe-500',
          reason: 'untaken bottom button intent must finish at latest',
        );
        expect(hasNewer, isFalse);
        expect(bottomVisible, isFalse);
        final nums = actualIds.map((id) => int.parse(id.substring(6))).toList();
        for (var i = 1; i < nums.length; i++) expect(nums[i], nums[i - 1] + 1);
      },
    );
  }
}
