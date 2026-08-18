import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/modules/chat/widgets/chat_voice_command_button.dart';

void main() {
  testWidgets('hold shows screen overlay and release hides it', (tester) async {
    final isListening = false.obs;
    final isAwaitingResponse = false.obs;
    final transcriptPreview = ''.obs;
    var started = 0;
    var submitted = 0;
    var cancelled = 0;

    await tester.pumpWidget(
      GetMaterialApp(
        home: Scaffold(
          body: ChatVoiceCommandButton(
            isListening: isListening,
            isAwaitingResponse: isAwaitingResponse,
            transcriptPreview: transcriptPreview,
            onStart: () async {
              started += 1;
              isListening.value = true;
            },
            onStopAndSubmit: () async {
              submitted += 1;
              isListening.value = false;
            },
            onCancel: () async {
              cancelled += 1;
              isListening.value = false;
            },
          ),
        ),
      ),
    );

    expect(
      find.byKey(const Key('chat_voice_command_hold_overlay')),
      findsNothing,
    );

    final button = find.byKey(const Key('chat_voice_command_button'));
    final gesture = await tester.startGesture(tester.getCenter(button));
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 50));

    expect(started, 1);
    expect(
      find.byKey(const Key('chat_voice_command_hold_overlay')),
      findsOneWidget,
    );
    expect(find.text('松开填入'), findsWidgets);

    transcriptPreview.value = '打开目录';
    await tester.pump();
    expect(find.text('打开目录'), findsOneWidget);

    await gesture.up();
    await tester.pump();

    expect(submitted, 1);
    expect(cancelled, 0);
    expect(
      find.byKey(const Key('chat_voice_command_hold_overlay')),
      findsNothing,
    );
  });

  testWidgets('tap does not show hold overlay', (tester) async {
    final isListening = false.obs;
    final isAwaitingResponse = false.obs;
    final transcriptPreview = ''.obs;

    await tester.pumpWidget(
      GetMaterialApp(
        home: Scaffold(
          body: ChatVoiceCommandButton(
            isListening: isListening,
            isAwaitingResponse: isAwaitingResponse,
            transcriptPreview: transcriptPreview,
            onStart: () async {},
            onStopAndSubmit: () async {},
            onCancel: () async {},
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('chat_voice_command_button')));
    await tester.pump();

    expect(
      find.byKey(const Key('chat_voice_command_hold_overlay')),
      findsNothing,
    );
    await tester.pump(const Duration(seconds: 3));
  });

  testWidgets('cancelled hold hides overlay without submit', (tester) async {
    final isListening = false.obs;
    final isAwaitingResponse = false.obs;
    final transcriptPreview = ''.obs;
    var submitted = 0;
    var cancelled = 0;

    await tester.pumpWidget(
      GetMaterialApp(
        home: Scaffold(
          body: ChatVoiceCommandButton(
            isListening: isListening,
            isAwaitingResponse: isAwaitingResponse,
            transcriptPreview: transcriptPreview,
            onStart: () async {
              isListening.value = true;
            },
            onStopAndSubmit: () async {
              submitted += 1;
              isListening.value = false;
            },
            onCancel: () async {
              cancelled += 1;
              isListening.value = false;
            },
          ),
        ),
      ),
    );

    final button = find.byKey(const Key('chat_voice_command_button'));
    final gesture = await tester.startGesture(tester.getCenter(button));
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 50));
    expect(
      find.byKey(const Key('chat_voice_command_hold_overlay')),
      findsOneWidget,
    );

    await gesture.cancel();
    await tester.pump();

    expect(submitted, 0);
    expect(cancelled, 1);
    expect(
      find.byKey(const Key('chat_voice_command_hold_overlay')),
      findsNothing,
    );
  });
}
