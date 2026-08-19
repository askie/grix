import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/widgets/chat_voice_command_button.dart';

void main() {
  testWidgets('tap starts listening without overlay', (tester) async {
    final isListening = false.obs;
    final isAwaitingResponse = false.obs;
    final transcriptPreview = ''.obs;
    var started = 0;
    var submitted = 0;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
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
          ),
        ),
      ),
    );

    expect(
      find.byKey(const Key('chat_voice_command_hold_overlay')),
      findsNothing,
    );

    await tester.tap(find.byKey(const Key('chat_voice_command_button')));
    await tester.pump();

    expect(started, 1);
    expect(submitted, 0);
    expect(find.byIcon(Icons.mic_rounded), findsOneWidget);
    expect(
      tester.getSize(find.byKey(const Key('chat_voice_command_button'))),
      const Size(24, 24),
    );
    expect(find.text('松开填入'), findsNothing);
    expect(
      find.byKey(const Key('chat_voice_command_hold_overlay')),
      findsNothing,
    );
  });

  testWidgets('second tap stops listening', (tester) async {
    final isListening = false.obs;
    final isAwaitingResponse = false.obs;
    final transcriptPreview = ''.obs;
    var started = 0;
    var submitted = 0;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
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
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('chat_voice_command_button')));
    await tester.pump();
    await tester.tap(find.byKey(const Key('chat_voice_command_button')));
    await tester.pump();

    expect(started, 1);
    expect(submitted, 1);
  });

  testWidgets('tapping a grouped send control does not count as tap outside', (
    tester,
  ) async {
    final isListening = false.obs;
    final isAwaitingResponse = false.obs;
    final transcriptPreview = ''.obs;
    var submitted = 0;
    var sent = 0;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: Row(
            children: [
              ChatVoiceCommandButton(
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
              ),
              TapRegion(
                groupId: ChatVoiceCommandButton.composerTapGroupId,
                child: GestureDetector(
                  key: const Key('grouped_send'),
                  behavior: HitTestBehavior.opaque,
                  onTap: () => sent += 1,
                  child: const ColoredBox(
                    color: Color(0x01000000),
                    child: SizedBox(width: 40, height: 40),
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('chat_voice_command_button')));
    await tester.pump();
    expect(submitted, 0);

    await tester.tap(find.byKey(const Key('grouped_send')));
    await tester.pump();

    expect(sent, 1);
    expect(submitted, 0);
    expect(isListening.value, isTrue);
  });

  testWidgets('tap outside stops listening', (tester) async {
    final isListening = false.obs;
    final isAwaitingResponse = false.obs;
    final transcriptPreview = ''.obs;
    var submitted = 0;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: Column(
            children: [
              const SizedBox(
                key: Key('outside_target'),
                width: 80,
                height: 80,
                child: ColoredBox(color: Color(0x01000000)),
              ),
              ChatVoiceCommandButton(
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
              ),
            ],
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('chat_voice_command_button')));
    await tester.pump();
    expect(submitted, 0);

    await tester.tap(find.byKey(const Key('outside_target')));
    await tester.pump();

    expect(submitted, 1);
    expect(
      find.byKey(const Key('chat_voice_command_hold_overlay')),
      findsNothing,
    );
  });

  testWidgets('tap outside does nothing while idle', (tester) async {
    final isListening = false.obs;
    final isAwaitingResponse = false.obs;
    final transcriptPreview = ''.obs;
    var started = 0;
    var submitted = 0;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: Column(
            children: [
              const SizedBox(
                key: Key('outside_target'),
                width: 80,
                height: 80,
                child: ColoredBox(color: Color(0x01000000)),
              ),
              ChatVoiceCommandButton(
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
              ),
            ],
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('outside_target')));
    await tester.pump();

    expect(started, 0);
    expect(submitted, 0);
  });

  testWidgets('listening tooltip uses listening copy and preview', (
    tester,
  ) async {
    final isListening = true.obs;
    final isAwaitingResponse = false.obs;
    final transcriptPreview = ''.obs;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatVoiceCommandButton(
            isListening: isListening,
            isAwaitingResponse: isAwaitingResponse,
            transcriptPreview: transcriptPreview,
            onStart: () async {},
            onStopAndSubmit: () async {},
          ),
        ),
      ),
    );

    expect(tester.widget<Tooltip>(find.byType(Tooltip)).message, '正在聆听');

    transcriptPreview.value = '打开目录';
    await tester.pump();
    expect(tester.widget<Tooltip>(find.byType(Tooltip)).message, '打开目录');
  });

  testWidgets('listening pulse shows a breathing glow', (tester) async {
    final isListening = true.obs;
    final isAwaitingResponse = false.obs;
    final transcriptPreview = ''.obs;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatVoiceCommandButton(
            isListening: isListening,
            isAwaitingResponse: isAwaitingResponse,
            transcriptPreview: transcriptPreview,
            onStart: () async {},
            onStopAndSubmit: () async {},
          ),
        ),
      ),
    );

    await tester.pump(const Duration(milliseconds: 550));
    final breath = find.byKey(const Key('chat_voice_command_breath'));
    expect(breath, findsOneWidget);
    expect(tester.widget<Opacity>(breath).opacity, greaterThan(0));
  });

  testWidgets('idle tooltip uses tap copy', (tester) async {
    final isListening = false.obs;
    final isAwaitingResponse = false.obs;
    final transcriptPreview = ''.obs;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: ChatVoiceCommandButton(
            isListening: isListening,
            isAwaitingResponse: isAwaitingResponse,
            transcriptPreview: transcriptPreview,
            onStart: () async {},
            onStopAndSubmit: () async {},
          ),
        ),
      ),
    );

    final tooltip = tester.widget<Tooltip>(find.byType(Tooltip));
    expect(tooltip.message, 'Tap to talk');
    expect(find.text('Hold to talk'), findsNothing);
    expect(find.text('点击说话'), findsNothing);
  });

  testWidgets('tap still starts while awaiting a previous command', (
    tester,
  ) async {
    final isListening = false.obs;
    final isAwaitingResponse = true.obs;
    final transcriptPreview = ''.obs;
    var started = 0;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
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
              isListening.value = false;
            },
          ),
        ),
      ),
    );

    await tester.tap(find.byKey(const Key('chat_voice_command_button')));
    await tester.pump();

    expect(started, 1);
  });
}
