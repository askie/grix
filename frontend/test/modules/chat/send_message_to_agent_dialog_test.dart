import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/widgets/send_message_to_agent_dialog.dart';

void main() {
  setUp(() {
    Get.put(AgentService());
    Get.put(ImService());
    Get.put(SessionService());
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('dialog uses top-right close and expanded message field', (
    tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => FilledButton(
              onPressed: () {
                showSendMessageToAgentDialog(context, initialMessage: 'hello');
              },
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('send_message_to_agent_cancel_button')),
      findsOneWidget,
    );
    expect(find.byIcon(Icons.close), findsOneWidget);
    expect(
      find.byKey(const Key('send_message_to_agent_text_field')),
      findsOneWidget,
    );
    final messageField = tester.widget<TextField>(
      find.byKey(const Key('send_message_to_agent_text_field')),
    );
    expect(messageField.minLines, 7);
    expect(messageField.maxLines, 12);

    await tester.tap(
      find.byKey(const Key('send_message_to_agent_cancel_button')),
    );
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
  });

  testWidgets('message field and agent picker follow the dark theme', (
    tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.darkTheme,
        darkTheme: AppTheme.darkTheme,
        themeMode: ThemeMode.dark,
        home: Scaffold(
          body: Builder(
            builder: (context) => FilledButton(
              onPressed: () {
                showSendMessageToAgentDialog(context, initialMessage: 'hello');
              },
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    final messageField = tester.widget<TextField>(
      find.byKey(const Key('send_message_to_agent_text_field')),
    );
    expect(messageField.decoration?.fillColor, isNot(Colors.white));

    final picker = tester.widget<OutlinedButton>(
      find.byKey(const Key('send_message_to_agent_picker_button')),
    );
    final background = picker.style?.backgroundColor?.resolve({});
    expect(background, isNot(Colors.white));
    expect(background, isNot(isNull));
    expect(ThemeData.estimateBrightnessForColor(background!), Brightness.dark);
  });
}
