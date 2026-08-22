import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/circular_progress_button.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('CircularProgressButton', () {
    Widget buildSubject({
      String centerText = '5H',
      double percent = 75.0,
      bool disabled = false,
      VoidCallback? onTap,
    }) {
      return MaterialApp(
        home: Scaffold(
          body: CircularProgressButton(
            centerText: centerText,
            percent: percent,
            ringColor: Colors.blue,
            size: 36,
            strokeWidth: 2.5,
            disabled: disabled,
            onTap: onTap,
          ),
        ),
      );
    }

    testWidgets('renders center text', (tester) async {
      await tester.pumpWidget(buildSubject(centerText: '5H'));
      expect(find.text('5H'), findsOneWidget);
    });

    testWidgets('renders CJK center text', (tester) async {
      await tester.pumpWidget(buildSubject(centerText: '压缩'));
      expect(find.text('压缩'), findsOneWidget);
    });

    testWidgets('taps when enabled', (tester) async {
      var tapped = false;
      await tester.pumpWidget(buildSubject(onTap: () => tapped = true));
      await tester.tap(find.byType(CircularProgressButton));
      expect(tapped, isTrue);
    });

    testWidgets('does not tap when disabled', (tester) async {
      var tapped = false;
      await tester.pumpWidget(buildSubject(
        disabled: true,
        onTap: () => tapped = true,
      ));
      await tester.tap(find.byType(CircularProgressButton));
      expect(tapped, isFalse);
    });

    testWidgets('uses correct size', (tester) async {
      await tester.pumpWidget(buildSubject());
      final sizedBox = tester.widget<SizedBox>(find.byType(SizedBox).first);
      expect(sizedBox.width, 36);
      expect(sizedBox.height, 36);
    });

    testWidgets('renders inner ring without throwing', (tester) async {
      await tester.pumpWidget(
        const MaterialApp(
          home: Scaffold(
            body: CircularProgressButton(
              centerText: '5H',
              percent: 40,
              ringColor: Colors.blue,
              size: 36,
              strokeWidth: 2.5,
              innerPercent: 65,
            ),
          ),
        ),
      );
      expect(find.byType(CircularProgressButton), findsOneWidget);
      expect(find.text('5H'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });

    testWidgets('inner ring tolerates 0 and 100', (tester) async {
      for (final v in [0.0, 100.0]) {
        await tester.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: CircularProgressButton(
                centerText: '7D',
                percent: 10,
                ringColor: Colors.blue,
                size: 36,
                strokeWidth: 2.5,
                innerPercent: v,
                innerColor: Colors.green,
              ),
            ),
          ),
        );
        expect(tester.takeException(), isNull);
      }
    });
  });
}
