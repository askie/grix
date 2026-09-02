import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_question_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_message_card_action.dart';
import 'package:grix/modules/chat/message_cards/widgets/chat_agent_question_card_view.dart';

void main() {
  testWidgets('question card keeps pending result inside the same card', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatAgentQuestionCardView(
            card: const ChatAgentQuestionCardData(
              requestId: 'req-question-1',
              questions: [
                ChatAgentQuestionPrompt(
                  index: 1,
                  header: 'Environment',
                  prompt: 'Choose environment.',
                  options: ['prod', 'staging'],
                ),
              ],
            ),
            isMine: false,
            fontScale: 1,
            onQuickAnswerTap: (_) async =>
                const ChatMessageCardActionResult.submitted(),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(const Key('chat_message_card_agent_question_option_0')),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_question_result')),
      findsOneWidget,
    );
    expect(find.text('提交中'), findsOneWidget);
    expect(find.text('已回答：prod'), findsOneWidget);
    expect(
      find.byKey(const Key('chat_message_card_agent_question_submit')),
      findsNothing,
    );
  });

  testWidgets('question card keeps focus on mouse outside tap', (
    WidgetTester tester,
  ) async {
    final previousPlatformOverride = debugDefaultTargetPlatformOverride;
    debugDefaultTargetPlatformOverride = TargetPlatform.macOS;

    try {
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: Scaffold(
            body: ChatAgentQuestionCardView(
              card: const ChatAgentQuestionCardData(
                requestId: 'req-question-1',
                questions: [
                  ChatAgentQuestionPrompt(
                    index: 1,
                    header: 'Environment',
                    prompt: 'Choose environment.',
                  ),
                ],
              ),
              isMine: false,
              fontScale: 1,
              onQuickAnswerTap: (_) async =>
                  const ChatMessageCardActionResult.submitted(),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      final input = tester.widget<TextField>(
        find.byKey(const Key('chat_message_card_agent_question_input_1')),
      );
      final focusNode = input.focusNode;
      expect(focusNode, isNotNull);
      focusNode!.requestFocus();
      await tester.pump();
      expect(focusNode.hasFocus, isTrue);

      final onTapOutside = input.onTapOutside;
      expect(
        onTapOutside,
        isNotNull,
        reason:
            'Desktop card inputs should preserve focus during paste menu use.',
      );
      onTapOutside!(const PointerDownEvent(kind: ui.PointerDeviceKind.mouse));
      await tester.pump();

      expect(
        focusNode.hasFocus,
        isTrue,
        reason: 'Desktop paste menu clicks should not steal focus.',
      );
    } finally {
      debugDefaultTargetPlatformOverride = previousPlatformOverride;
    }
  });

  testWidgets('question card shows mapped error result and keeps retry input', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatAgentQuestionCardView(
            card: const ChatAgentQuestionCardData(
              requestId: 'req-question-1',
              questions: [
                ChatAgentQuestionPrompt(
                  index: 1,
                  header: 'Environment',
                  prompt: 'Choose environment.',
                ),
              ],
              submittedAnswer: 'staging',
              submissionStatus: ChatAgentStatusCardData(
                category: 'question',
                status: 'error',
                summary:
                    'Question request req-question-1 could not be recorded.',
                detailText: 'The reply format is invalid.',
                referenceId: 'req-question-1',
              ),
            ),
            isMine: false,
            fontScale: 1,
            onQuickAnswerTap: (_) async =>
                const ChatMessageCardActionResult.submitted(),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_question_result')),
      findsOneWidget,
    );
    expect(
      find.text('Question request req-question-1 could not be recorded.'),
      findsOneWidget,
    );
    expect(find.text('已回答：staging'), findsOneWidget);
    expect(
      find.byKey(const Key('chat_message_card_agent_question_input_1')),
      findsOneWidget,
    );
  });

  testWidgets('question card shows countdown then locks after expiry', (
    WidgetTester tester,
  ) async {
    // widget 测试是假时钟，DateTime.now() 不随 pump 前进；用可控时间源对齐。
    var fakeNow = DateTime.now();
    final expiresAt = fakeNow.millisecondsSinceEpoch + 3 * 1000; // 3 秒后到期
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatAgentQuestionCardView(
            card: ChatAgentQuestionCardData(
              requestId: 'req-question-countdown',
              expiresAtMs: expiresAt,
              questions: const [
                ChatAgentQuestionPrompt(
                  index: 1,
                  header: 'Environment',
                  prompt: 'Choose environment.',
                  options: ['prod', 'staging'],
                ),
              ],
            ),
            isMine: false,
            fontScale: 1,
            nowProvider: () => fakeNow,
            onQuickAnswerTap: (_) async =>
                const ChatMessageCardActionResult.submitted(),
          ),
        ),
      ),
    );
    await tester.pump();

    expect(
      find.byKey(const Key('chat_message_card_agent_question_countdown')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('chat_message_card_agent_question_expired')),
      findsNothing,
    );
    final submitBefore = tester.widget<FilledButton>(
      find.byKey(const Key('chat_message_card_agent_question_submit')),
    );
    expect(submitBefore.onPressed, isNotNull);

    // 过期后：倒计时换成超时提示，提交与快捷选项全部禁用。
    fakeNow = fakeNow.add(const Duration(seconds: 4));
    await tester.pump(const Duration(seconds: 4));
    await tester.pump();

    expect(
      find.byKey(const Key('chat_message_card_agent_question_expired')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('chat_message_card_agent_question_countdown')),
      findsNothing,
    );
    final submitAfter = tester.widget<FilledButton>(
      find.byKey(const Key('chat_message_card_agent_question_submit')),
    );
    expect(submitAfter.onPressed, isNull);
    final quickOption = tester.widget<FilledButton>(
      find.byKey(const Key('chat_message_card_agent_question_option_0')),
    );
    expect(quickOption.onPressed, isNull);
  });

  testWidgets('question card without expiry shows no countdown', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatAgentQuestionCardView(
            card: const ChatAgentQuestionCardData(
              requestId: 'req-question-no-expiry',
              questions: [
                ChatAgentQuestionPrompt(
                  index: 1,
                  header: 'Environment',
                  prompt: 'Choose environment.',
                  options: ['prod', 'staging'],
                ),
              ],
            ),
            isMine: false,
            fontScale: 1,
            onQuickAnswerTap: (_) async =>
                const ChatMessageCardActionResult.submitted(),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_question_countdown')),
      findsNothing,
    );
    expect(
      find.byKey(const Key('chat_message_card_agent_question_expired')),
      findsNothing,
    );
  });

  testWidgets('expired question card is locked from the start', (
    WidgetTester tester,
  ) async {
    final expiredAt = DateTime.now().millisecondsSinceEpoch - 60 * 1000;
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatAgentQuestionCardView(
            card: ChatAgentQuestionCardData(
              requestId: 'req-question-expired',
              expiresAtMs: expiredAt,
              questions: const [
                ChatAgentQuestionPrompt(
                  index: 1,
                  header: 'Environment',
                  prompt: 'Choose environment.',
                  options: ['prod', 'staging'],
                ),
              ],
            ),
            isMine: false,
            fontScale: 1,
            onQuickAnswerTap: (_) async =>
                const ChatMessageCardActionResult.submitted(),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_question_expired')),
      findsOneWidget,
    );
    final submit = tester.widget<FilledButton>(
      find.byKey(const Key('chat_message_card_agent_question_submit')),
    );
    expect(submit.onPressed, isNull);

    // 点击快捷选项不应产生提交（不出现"提交中"面板）。
    await tester.tap(
      find.byKey(const Key('chat_message_card_agent_question_option_0')),
      warnIfMissed: false,
    );
    await tester.pumpAndSettle();
    expect(
      find.byKey(const Key('chat_message_card_agent_question_result')),
      findsNothing,
    );
  });
}
