import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/modules/chat/bindings/chat_binding.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/data/providers/local_db.dart';

import 'history_test_harness.dart';

void main() {
  testWidgets(
    'production push gates cold and cached windows and cancels safely',
    (tester) async {
      final im = await initOffline(tester, 'route-gate');
      const sid = 'route-gate';
      await runDb(
        tester,
        () => LocalDb.batchInsertMessages(
          List.generate(31, (i) => row(sid, i + 1)),
        ),
      );
      var c = await openRoute(tester, sid, witness: true);
      var gate = false;
      c.routeTransitionSettled.then((_) => gate = true);
      var route = ModalRoute.of(
        tester.element(find.byType(ChatView, skipOffstage: false)),
      )!;
      final publications = <String>[];
      final worker = ever(im.currentMessages, (_) {
        if (im.currentMessages.isNotEmpty) {
          publications.add(route.animation!.status.name);
          evidence('PUBLISH', {
            'status': route.animation!.status.name,
            'value': route.animation!.value,
            'offstage': route.offstage,
            'rows': im.currentMessages.length,
          });
        }
      });
      await runDb(tester, () => LocalDb.getLatestMessages(sid, limit: 31));
      await tester.pump();
      expect(
        gate,
        isFalse,
        reason: 'offstage completed is a measurement animation',
      );
      expect(im.currentMessages, isEmpty);
      await tester.pump(const Duration(milliseconds: 100));
      expect(route.animation!.status, AnimationStatus.forward);
      expect(gate, isFalse);
      expect(im.currentMessages, isEmpty);
      await tester.pump(const Duration(milliseconds: 200));
      await flushDb(tester, () => im.initialHistoryReady.value);
      expect(gate, isTrue);
      expect(im.currentMessages.length, 30);
      expect(publications, isNotEmpty);
      expect(publications.every((s) => s == 'completed'), isTrue);
      Get.back();
      await tester.pumpAndSettle();

      // Re-enter without clearing the production window/render caches.
      Get.toNamed(
        AppRoutes.chat,
        arguments: {'session_id': sid, 'title': sid, 'type': 'private'},
      );
      await tester.pump();
      c = Get.find(tag: ChatBinding.controllerTagForSession(sid));
      route = ModalRoute.of(
        tester.element(find.byType(ChatView, skipOffstage: false)),
      )!;
      gate = false;
      c.routeTransitionSettled.then((_) => gate = true);
      await runDb(tester, () => LocalDb.getLatestMessages(sid, limit: 31));
      await tester.pump(const Duration(milliseconds: 100));
      expect(gate, isFalse);
      expect(
        im.currentMessages,
        isEmpty,
        reason: 'cache restore uses the same gate',
      );
      await tester.pump(const Duration(milliseconds: 200));
      await flushDb(tester, () => im.initialHistoryReady.value);
      expect(gate, isTrue);
      expect(im.currentMessages.length, 30);
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pumpAndSettle();
      // An already queued force expires when the user takes over, even if the
      // finger is lifted before the callback executes.
      await tester.drag(chatList(c), const Offset(0, 400));
      await tester.pumpAndSettle();
      c.scrollToBottom(force: true);
      final drag = await tester.startGesture(tester.getCenter(chatList(c)));
      await drag.moveBy(const Offset(0, 60));
      await drag.up();
      final takenPixels = c.scrollController.position.pixels;
      await tester.pump();
      expect(c.scrollController.position.pixels, closeTo(takenPixels, 1));
      expect(c.scrollController.position.extentAfter, greaterThan(300));
      worker.dispose();
      Get.back();
      await tester.pumpAndSettle();

      // A cancelled push must resolve the wait and leave no late publication.
      c = await openRoute(tester, sid);
      var cancelledGate = false;
      c.routeTransitionSettled.then((_) => cancelledGate = true);
      await tester.pump(const Duration(milliseconds: 100));
      expect(im.currentMessages, isEmpty);
      Get.back();
      await tester.pump(const Duration(milliseconds: 50));
      expect(im.currentMessages, isEmpty, reason: "cancelled push cannot publish");
      await tester.pumpAndSettle();
      expect(cancelledGate, isTrue);
      expect(im.currentSessionId, isNull);
      expect(im.currentMessages, isEmpty);
      await closeOffline(tester);
    },
  );
}
