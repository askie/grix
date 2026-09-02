import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/progress_detail_bottom_sheet.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('ProgressDetailBottomSheet', () {
    Widget buildApp() {
      return MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) {
              return ElevatedButton(
                onPressed: () {
                  ProgressDetailBottomSheet.show(
                    context,
                    description: '5小时Token额度',
                    percent: 73.5,
                    detail: '已用 73.5% · 剩余 1h21m\n总额度 500,000',
                    accentColor: Colors.orange,
                  );
                },
                child: const Text('Show'),
              );
            },
          ),
        ),
      );
    }

    testWidgets('shows description and percentage', (tester) async {
      await tester.pumpWidget(buildApp());
      await tester.tap(find.text('Show'));
      await tester.pumpAndSettle();

      expect(find.text('5小时Token额度'), findsOneWidget);
      expect(find.text('73.5'), findsOneWidget);
      expect(find.text('%'), findsOneWidget);
    });

    testWidgets('shows detail text', (tester) async {
      await tester.pumpWidget(buildApp());
      await tester.tap(find.text('Show'));
      await tester.pumpAndSettle();

      expect(find.text('已用 73.5% · 剩余 1h21m\n总额度 500,000'), findsOneWidget);
    });

    testWidgets('displays integer percentage without decimals', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) {
                return ElevatedButton(
                  onPressed: () {
                    ProgressDetailBottomSheet.show(
                      context,
                      description: '测试',
                      percent: 80.0,
                      detail: '',
                      accentColor: Colors.green,
                    );
                  },
                  child: const Text('Show'),
                );
              },
            ),
          ),
        ),
      );

      await tester.tap(find.text('Show'));
      await tester.pumpAndSettle();

      expect(find.text('80'), findsOneWidget);
      expect(find.text('80.0'), findsNothing);
    });
  });
}
