import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'history_test_harness.dart';

void main() {
  testWidgets(
    'production zero duration push publishes without a forward tick',
    (tester) async {
      final im = await initOffline(tester, 'zero');
      await runDb(
        tester,
        () => LocalDb.batchInsertMessages(
          List.generate(3, (i) => row('zero', i + 1)),
        ),
      );
      final c = await openRoute(tester, 'zero', zero: true);
      final route = ModalRoute.of(tester.element(find.byType(ChatView)))!;
      expect(route.transitionDuration, Duration.zero);
      await tester.pump();
      await flushDb(tester, () => im.initialHistoryReady.value);
      var gate = false;
      c.routeTransitionSettled.then((_) => gate = true);
      await tester.pump();
      expect(gate, isTrue);
      expect(im.currentMessages.length, 3);
      await closeOffline(tester);
    },
  );
}
