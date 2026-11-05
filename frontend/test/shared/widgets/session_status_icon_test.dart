import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/session_status_icon.dart';

void main() {
  testWidgets('SessionStatusIcon renders shrink box when inactive and unpinned', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: SessionStatusIcon(
            isPinned: false,
            isActive: false,
          ),
        ),
      ),
    );

    expect(find.byType(Icon), findsNothing);
    final sizedBox = tester.widget<SizedBox>(find.byType(SizedBox));
    expect(sizedBox.width, 0.0);
    expect(sizedBox.height, 0.0);
  });

  testWidgets(
    'SessionStatusIcon aligns center and text offset identically for pin and green dot',
    (WidgetTester tester) async {
      final pinKey = GlobalKey();
      final dotKey = GlobalKey();
      final pinTextKey = GlobalKey();
      final dotTextKey = GlobalKey();

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    SessionStatusIcon(
                      key: pinKey,
                      isPinned: true,
                      isActive: false,
                      pinSize: 14,
                      dotSize: 8,
                      spacing: 6,
                    ),
                    Text('Pinned Agent', key: pinTextKey),
                  ],
                ),
                Row(
                  children: [
                    SessionStatusIcon(
                      key: dotKey,
                      isPinned: false,
                      isActive: true,
                      pinSize: 14,
                      dotSize: 8,
                      spacing: 6,
                    ),
                    Text('Active Agent', key: dotTextKey),
                  ],
                ),
              ],
            ),
          ),
        ),
      );

      // 1. Both SessionStatusIcon widgets occupy the exact same outer size (slot 14 + spacing 6 = 20)
      final pinSize = tester.getSize(find.byKey(pinKey));
      final dotSize = tester.getSize(find.byKey(dotKey));
      expect(pinSize.width, 20.0);
      expect(dotSize.width, 20.0);
      expect(pinSize.height, 14.0);
      expect(dotSize.height, 14.0);

      // 2. Both indicator centers are vertically aligned at the exact same X coordinate
      // Slot width is 14, so center of slot is at x = 7.0
      final pinCenter = tester.getCenter(find.byType(Icon));
      final dotCenter = tester.getCenter(find.byWidgetPredicate((w) {
        return w is Container &&
            w.decoration is BoxDecoration &&
            (w.decoration as BoxDecoration).shape == BoxShape.circle;
      }));
      expect(pinCenter.dx, 7.0);
      expect(dotCenter.dx, 7.0);

      // 3. Trailing text starts at the exact same horizontal position
      final pinTextPos = tester.getTopLeft(find.byKey(pinTextKey));
      final dotTextPos = tester.getTopLeft(find.byKey(dotTextKey));
      expect(pinTextPos.dx, 20.0);
      expect(dotTextPos.dx, 20.0);
    },
  );

  testWidgets(
    'SessionStatusIcon uses green color for pin when both pinned and active',
    (WidgetTester tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: SessionStatusIcon(
              isPinned: true,
              isActive: true,
              pinSize: 14,
            ),
          ),
        ),
      );

      final icon = tester.widget<Icon>(find.byType(Icon));
      expect(icon.icon, Icons.push_pin_rounded);
      expect(icon.color, Colors.green);
    },
  );
}
