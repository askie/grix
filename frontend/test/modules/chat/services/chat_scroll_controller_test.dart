import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_scroll_controller.dart';

Future<ChatScrollController> _pumpList(WidgetTester tester) async {
  final controller = ChatScrollController();
  await tester.pumpWidget(
    MaterialApp(
      home: ListView.builder(
        controller: controller,
        itemCount: 1000,
        itemBuilder: (_, index) => SizedBox(height: 40, child: Text('$index')),
      ),
    ),
  );
  controller.jumpTo(8000);
  await tester.pump();
  return controller;
}

void main() {
  testWidgets('shiftViewportTo keeps a fling moving', (tester) async {
    final controller = await _pumpList(tester);
    await tester.fling(find.byType(ListView), const Offset(0, -300), 3000);
    await tester.pump(const Duration(milliseconds: 16));
    await tester.pump(const Duration(milliseconds: 16));
    final beforeShift = controller.position.pixels;

    controller.shiftViewportTo(beforeShift + 400);
    await tester.pump(const Duration(milliseconds: 16));
    final afterShift = controller.position.pixels;
    await tester.pump(const Duration(milliseconds: 50));

    expect(afterShift, greaterThanOrEqualTo(beforeShift + 400));
    expect(controller.position.pixels, greaterThan(afterShift + 20));
    expect(controller.position.isScrollingNotifier.value, isTrue);
    await tester.pumpAndSettle();
  });

  testWidgets('jumpTo (control) stops the fling dead', (tester) async {
    final controller = await _pumpList(tester);
    await tester.fling(find.byType(ListView), const Offset(0, -300), 3000);
    await tester.pump(const Duration(milliseconds: 16));
    await tester.pump(const Duration(milliseconds: 16));

    final target = controller.position.pixels + 400;
    controller.jumpTo(target);
    await tester.pump(const Duration(milliseconds: 50));

    expect(controller.position.pixels, target);
    expect(controller.position.isScrollingNotifier.value, isFalse);
  });

  testWidgets('shiftViewportTo keeps the finger drag alive', (tester) async {
    final controller = await _pumpList(tester);
    final gesture = await tester.startGesture(
      tester.getCenter(find.byType(ListView)),
    );
    await gesture.moveBy(const Offset(0, -40));
    await gesture.moveBy(const Offset(0, -40));
    await tester.pump();
    final beforeShift = controller.position.pixels;

    controller.shiftViewportTo(beforeShift + 200);
    await tester.pump();
    await gesture.moveBy(const Offset(0, -50));
    await tester.pump();

    expect(controller.position.pixels, closeTo(beforeShift + 250, 0.5));
    await gesture.up();
    await tester.pumpAndSettle();
  });

  testWidgets('jumpTo (control) cancels the finger drag', (tester) async {
    final controller = await _pumpList(tester);
    final gesture = await tester.startGesture(
      tester.getCenter(find.byType(ListView)),
    );
    await gesture.moveBy(const Offset(0, -40));
    await gesture.moveBy(const Offset(0, -40));
    await tester.pump();
    final target = controller.position.pixels + 200;

    controller.jumpTo(target);
    await tester.pump();
    await gesture.moveBy(const Offset(0, -50));
    await tester.pump();

    expect(controller.position.pixels, target);
    await gesture.up();
    await tester.pumpAndSettle();
  });
}
