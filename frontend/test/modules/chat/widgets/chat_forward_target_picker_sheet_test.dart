import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/models/chat_forward_target_option.dart';
import 'package:grix/modules/chat/widgets/chat_forward_target_picker_sheet.dart';
import 'package:get/get.dart';

void main() {
  final options = <ChatForwardTargetOption>[
    ChatForwardTargetOption(
      sessionId: 'sid_a',
      avatarColorSeed: 'sid_a',
      title: 'Alpha',
      subtitle: 'first',
      isGroup: false,
      activityAt: 2,
    ),
    ChatForwardTargetOption(
      sessionId: 'sid_b',
      avatarColorSeed: 'sid_b',
      title: 'Beta',
      subtitle: 'second',
      isGroup: false,
      activityAt: 1,
    ),
  ];

  group('ChatForwardTargetPickerSheet', () {
    testWidgets('clears hidden selection after search filter', (tester) async {
      await tester.pumpWidget(
        GetMaterialApp(
          home: Scaffold(
            body: ChatForwardTargetPickerSheet(options: options),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.text('Alpha'));
      await tester.pumpAndSettle();

      FilledButton button() =>
          tester.widget<FilledButton>(find.byType(FilledButton));

      expect(button().onPressed, isNotNull);

      await tester.enterText(find.byType(TextField), 'beta');
      await tester.pumpAndSettle();

      expect(button().onPressed, isNull);
    });

    testWidgets('hides add button without onSendToAgent', (tester) async {
      await tester.pumpWidget(
        GetMaterialApp(
          home: Scaffold(
            body: ChatForwardTargetPickerSheet(options: options),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byIcon(Icons.add_rounded), findsNothing);
    });

    testWidgets('add button closes sheet and invokes onSendToAgent', (
      tester,
    ) async {
      var callbackCount = 0;
      var resultSet = false;
      ChatForwardTargetOption? result;

      await tester.pumpWidget(
        GetMaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: ElevatedButton(
                onPressed: () {
                  ChatForwardTargetPickerSheet.show(
                    context,
                    options: options,
                    onSendToAgent: () => callbackCount++,
                  ).then((value) {
                    result = value;
                    resultSet = true;
                  });
                },
                child: const Text('open'),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();

      expect(find.byIcon(Icons.add_rounded), findsOneWidget);

      await tester.tap(find.byIcon(Icons.add_rounded));
      await tester.pumpAndSettle();

      expect(callbackCount, 1);
      expect(resultSet, isTrue);
      expect(result, isNull);
      expect(find.byType(ChatForwardTargetPickerSheet), findsNothing);
    });
  });
}
