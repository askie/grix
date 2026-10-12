import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/data/providers/local_db_change_bus.dart';
import 'package:grix/data/providers/session_service.dart';

import 'history_test_harness.dart';

class _DelayedArchiveSession extends SessionService {
  final Completer<SessionMessageHistoryResult> _older =
      Completer<SessionMessageHistoryResult>();

  bool delayOlder = false;
  int latestCalls = 0;
  int olderCalls = 0;

  @override
  bool get isInitialized => true;

  @override
  Future<SessionMessageHistoryResult> fetchMessageHistoryResult({
    required String sessionId,
    String? beforeMsgId,
    int limit = 20,
  }) {
    if (delayOlder && (beforeMsgId?.isNotEmpty ?? false)) {
      olderCalls++;
      return _older.future;
    }
    latestCalls++;
    return Future.value(
      const SessionMessageHistoryResult(code: 0, messages: [], hasMore: true),
    );
  }

  void releaseOlder(List<Map<String, dynamic>> rows) {
    if (_older.isCompleted) return;
    _older.complete(
      SessionMessageHistoryResult(code: 0, messages: rows, hasMore: false),
    );
  }

  @override
  Future<Map<String, dynamic>?> fetchSessionDetail(String sessionId) async {
    return const {'session_type': 1, 'members': []};
  }

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    return const SessionDetailResult(data: {'session_type': 1, 'members': []});
  }
}

void main() {
  testWidgets(
    'real Windows chat entry stays stable through slow DB and archive fill',
    (tester) async {
      debugDefaultTargetPlatformOverride = TargetPlatform.windows;
      tester.view.physicalSize = const Size(1200, 800);
      tester.view.devicePixelRatio = 1;
      addTearDown(() {
        debugDefaultTargetPlatformOverride = null;
        tester.view.resetPhysicalSize();
        tester.view.resetDevicePixelRatio();
      });

      const sid = 'windows-entry-stability';
      const anchorId = 'probe-1';
      final im = await initOffline(tester, sid);
      Get.delete<SessionService>(force: true);
      final archive =
          Get.put<SessionService>(_DelayedArchiveSession())
              as _DelayedArchiveSession;
      addTearDown(() {
        archive.releaseOlder(const []);
      });

      await runDb(
        tester,
        () => LocalDb.batchInsertMessages([
          row(sid, 1, content: 'cached message 1'),
          row(sid, 2, content: 'cached message 2'),
          row(sid, 3, content: 'cached message 3'),
        ]),
      );

      // Populate the production ImService cache, then re-enter through the
      // real route/controller/view while an authoritative SQLite read waits.
      im.enterSession(sid);
      await flushDb(tester, () => im.initialHistoryReady.value);
      await flushDb(tester, () => archive.latestCalls > 0);
      im.leaveSession(sid);
      archive.delayOlder = true;

      final database = await LocalDb.database;
      final dbEntered = Completer<void>();
      final dbRelease = Completer<void>();
      addTearDown(() {
        if (!dbRelease.isCompleted) dbRelease.complete();
      });
      final blockedTransaction = database.transaction((_) async {
        dbEntered.complete();
        await dbRelease.future;
      });
      await flushDb(tester, () => dbEntered.isCompleted);

      final controller = await openRoute(tester, sid, witness: true);
      await tester.pump(const Duration(milliseconds: 100));
      expect(im.currentMessages, isEmpty, reason: 'cache waits for the route');
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pump();
      expect(im.currentMessages.length, 3);
      expect(im.initialHistoryReady.value, isFalse);

      Map<String, Object?> sample(String frame) {
        final current = metrics(controller, msgId: anchorId);
        final value = <String, Object?>{
          'frame': frame,
          'platform': defaultTargetPlatform.name,
          'entry': 'production-chat-route',
          'generation': im.currentSessionGeneration,
          'windowIds': im.currentMessages.map((m) => m.msgId).toList(),
          'initialReady': im.initialHistoryReady.value,
          'autoFillPages': controller.initialAutoFillPagesForTest,
          'latestArchiveCalls': archive.latestCalls,
          'olderArchiveCalls': archive.olderCalls,
          ...current,
        };
        evidence('ENTRY-STABILITY', value);
        return value;
      }

      void expectAnchorStable(
        Map<String, Object?> baseline,
        Map<String, Object?> current,
      ) {
        expect(current['generation'], baseline['generation']);
        expect(current['y'], isNotNull);
        expect(
          (current['y']! as double) - (baseline['y']! as double),
          closeTo(0, 1),
          reason: '${current['frame']} moved $anchorId without user input',
        );
      }

      for (var frame = 0; frame < 40; frame++) {
        final current = metrics(controller, msgId: anchorId);
        if (current['y'] != null &&
            (current['pixels']! as double) <= (current['max']! as double) + 1) {
          break;
        }
        await tester.pump(const Duration(milliseconds: 16));
      }
      final firstVisible = sample('first-visible-cache');
      expect(firstVisible['y'], isNotNull);
      await tester.pump();
      expectAnchorStable(firstVisible, sample('next-frame-cache'));
      await tester.pump(const Duration(milliseconds: 80));
      expectAnchorStable(firstVisible, sample('80ms-cache'));
      await tester.pump(const Duration(milliseconds: 170));
      expectAnchorStable(firstVisible, sample('250ms-cache'));
      await tester.pump(const Duration(milliseconds: 750));
      expectAnchorStable(firstVisible, sample('1s-cache-db-blocked'));

      dbRelease.complete();
      await runDb(tester, () => blockedTransaction);
      await flushDb(tester, () => im.initialHistoryReady.value);
      expectAnchorStable(firstVisible, sample('db-authoritative'));
      await flushDb(tester, () => archive.olderCalls > 0);
      expect(controller.initialAutoFillPagesForTest, 1);
      await tester.pump(const Duration(seconds: 1));
      expectAnchorStable(firstVisible, sample('1s-archive-pending'));

      archive.releaseOlder([
        for (var id = -15; id <= 0; id++)
          row(sid, id, content: 'archive message $id'),
      ]);
      await flushDb(tester, () => im.currentMessages.length == 19);
      final archivePublished = sample('archive-published');
      expectAnchorStable(firstVisible, archivePublished);
      await tester.pump(const Duration(milliseconds: 80));
      expectAnchorStable(firstVisible, sample('80ms-after-archive'));
      await tester.pump(const Duration(milliseconds: 170));
      expectAnchorStable(firstVisible, sample('250ms-after-archive'));
      await tester.pump(const Duration(milliseconds: 750));
      expectAnchorStable(firstVisible, sample('1s-after-archive'));
      await flushDb(tester, () => !im.hasOlderMessages);
      expectAnchorStable(firstVisible, sample('archive-settled'));

      expect(im.currentMessages.last.msgId, 'probe-3');
      expect(controller.scrollController.historyContentFitsViewport, isFalse);

      final liveRow = row(sid, 4, content: 'live message 4')
        ..['status'] = 'sending';
      LocalDbChangeBus.instance.emitMessageChange(
        LocalMessagesInserted(
          sessionId: sid,
          msgIds: const ['probe-4'],
          maxCreatedAt: liveRow['created_at']! as int,
          rows: [liveRow],
        ),
      );
      expect(
        im.currentMessages.last.msgId,
        'probe-4',
        reason: 'live delivery must publish synchronously',
      );
      await tester.pump();
      final livePublished = metrics(controller, msgId: 'probe-4');
      evidence('ENTRY-STABILITY', {
        'frame': 'live-published',
        'generation': im.currentSessionGeneration,
        ...livePublished,
      });
      expect(livePublished['y'], isNotNull);
      await tester.pump(const Duration(milliseconds: 80));
      final liveSettled = metrics(controller, msgId: 'probe-4');
      expect(liveSettled['y'], isNotNull);

      final ackRow = Map<String, dynamic>.of(liveRow)
        ..['status'] = 'sent'
        ..['state_version'] = '2';
      LocalDbChangeBus.instance.emitMessageChange(
        LocalMessageUpdated(sessionId: sid, msgId: 'probe-4', row: ackRow),
      );
      await tester.pump();
      final ackPublished = metrics(controller, msgId: 'probe-4');
      evidence('ENTRY-STABILITY', {
        'frame': 'ack-published',
        'generation': im.currentSessionGeneration,
        ...ackPublished,
      });
      expect(ackPublished['y'], closeTo(liveSettled['y']! as double, 1));

      await closeOffline(tester);
      debugDefaultTargetPlatformOverride = null;
      tester.view.resetPhysicalSize();
      tester.view.resetDevicePixelRatio();
    },
  );
}
