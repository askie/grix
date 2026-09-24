import 'dart:async';
import 'dart:convert';
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_question_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_message_card_action.dart';
import 'package:grix/modules/chat/message_cards/services/chat_agent_card_action_encoder.dart';
import 'package:grix/modules/chat/message_cards/widgets/chat_agent_question_card_view.dart';

void main() {
  test('structured replies reject answers outside the declared option set', () {
    const card = ChatAgentQuestionCardData(
      requestId: 'req-option-only',
      questions: [
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Choose an environment.',
          options: ['production', 'staging'],
        ),
      ],
    );

    expect(
      () => ChatAgentCardActionEncoder.buildQuestionStructuredReplyAction(
        card,
        const {1: 'a free-form answer'},
      ),
      throwsArgumentError,
    );
  });

  test('closed multi-select accepts listed choices and rejects free text', () {
    const card = ChatAgentQuestionCardData(
      requestId: 'req-multi-option-only',
      questions: [
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Packages',
          prompt: 'Choose packages to install.',
          options: ['api', 'worker', 'web'],
          multiSelect: true,
        ),
      ],
    );

    final action =
        ChatAgentCardActionEncoder.buildQuestionStructuredReplyAction(
          card,
          const {1: 'api, web'},
        );
    final payload =
        jsonDecode(Uri.parse(action).queryParameters['d']!)
            as Map<String, dynamic>;
    expect(payload['response'], <String, dynamic>{
      'type': 'single',
      'value': 'api, web',
    });
    expect(
      () => ChatAgentCardActionEncoder.buildQuestionStructuredReplyAction(
        card,
        const {1: 'api, arbitrary text'},
      ),
      throwsArgumentError,
    );
  });

  test('closed multi-select rejects an ambiguous comma-delimited answer', () {
    const card = ChatAgentQuestionCardData(
      requestId: 'req-ambiguous-options',
      questions: [
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Selection',
          prompt: 'Choose one or more options.',
          options: ['A, B', 'A', 'B', 'C'],
          multiSelect: true,
        ),
      ],
    );

    expect(
      () => ChatAgentCardActionEncoder.buildQuestionStructuredReplyAction(
        card,
        const {1: 'A, B, C'},
      ),
      throwsArgumentError,
    );
  });

  testWidgets('closed multi-select submits options containing commas', (
    WidgetTester tester,
  ) async {
    var submittedAction = '';
    const card = ChatAgentQuestionCardData(
      requestId: 'req-comma-options',
      questions: [
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Selection',
          prompt: 'Choose one or more options.',
          options: ['A, B', 'C'],
          multiSelect: true,
        ),
      ],
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: ChatAgentQuestionCardView(
            card: card,
            isMine: false,
            fontScale: 1,
            onQuickAnswerTap: (action) async {
              submittedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    for (final option in const ['A, B', 'C']) {
      await tester.tap(
        find.byKey(
          Key('chat_message_card_agent_question_option_1_${option.hashCode}'),
        ),
      );
      await tester.pumpAndSettle();
    }
    await tester.tap(
      find.byKey(const Key('chat_message_card_agent_question_submit')),
    );
    await tester.pumpAndSettle();

    expect(submittedAction, isNotEmpty);
    final payload =
        jsonDecode(Uri.parse(submittedAction).queryParameters['d']!)
            as Map<String, dynamic>;
    expect(payload['response'], <String, dynamic>{
      'type': 'single',
      'value': 'A, B, C',
    });
  });

  testWidgets('question card keeps pending result inside the same card', (
    WidgetTester tester,
  ) async {
    var submittedAction = '';
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
            onQuickAnswerTap: (action) async {
              submittedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_question_input_1')),
      findsNothing,
    );
    expect(find.text('请选择上方提供的选项。'), findsOneWidget);

    await tester.tap(find.byType(ChoiceChip).first);
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(const Key('chat_message_card_agent_question_submit')),
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
    final actionPayload =
        jsonDecode(Uri.parse(submittedAction).queryParameters['d']!)
            as Map<String, dynamic>;
    expect(actionPayload['request_id'], 'req-question-1');
    expect(actionPayload['response'], <String, dynamic>{
      'type': 'single',
      'value': 'prod',
    });
  });

  testWidgets(
    'question with free-text capability submits arbitrary non-empty text',
    (WidgetTester tester) async {
      const answer = '先检查 worker 日志，再告诉我结论。';
      var submittedAction = '';
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: Scaffold(
            body: ChatAgentQuestionCardView(
              card: const ChatAgentQuestionCardData(
                requestId: 'req-free-text',
                questions: [
                  ChatAgentQuestionPrompt(
                    index: 1,
                    header: '处理方式',
                    prompt: '你希望我先做什么？',
                    options: ['先看日志', '直接给结论'],
                    allowFreeText: true,
                  ),
                ],
                footerText: '如果选项不合适，可以填写其他答案。',
              ),
              isMine: false,
              fontScale: 1,
              onQuickAnswerTap: (action) async {
                submittedAction = action;
                return const ChatMessageCardActionResult.submitted();
              },
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('chat_message_card_agent_question_input_1')),
        answer,
      );
      await tester.tap(
        find.byKey(const Key('chat_message_card_agent_question_submit')),
      );
      await tester.pumpAndSettle();

      final actionPayload =
          jsonDecode(Uri.parse(submittedAction).queryParameters['d']!)
              as Map<String, dynamic>;
      expect(actionPayload['request_id'], 'req-free-text');
      expect(actionPayload['response'], <String, dynamic>{
        'type': 'single',
        'value': answer,
      });
      expect(
        find.byKey(const Key('chat_message_card_agent_question_answered')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('chat_message_card_agent_question_input_1')),
        findsNothing,
      );
    },
  );

  testWidgets(
    'failed option reply leaves the question open without resending',
    (WidgetTester tester) async {
      var submissionCount = 0;
      final completion = Completer<ChatMessageCardActionResult>();
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: Scaffold(
            body: ChatAgentQuestionCardView(
              card: const ChatAgentQuestionCardData(
                requestId: 'req-failed-option',
                questions: [
                  ChatAgentQuestionPrompt(
                    index: 1,
                    header: 'Environment',
                    prompt: 'Choose an environment.',
                    options: ['production', 'staging'],
                  ),
                ],
              ),
              isMine: false,
              fontScale: 1,
              onQuickAnswerTap: (_) async {
                submissionCount++;
                return completion.future;
              },
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(
        find.byKey(const Key('chat_message_card_agent_question_option_0')),
      );
      await tester.pump();
      await tester.tap(
        find.byKey(const Key('chat_message_card_agent_question_option_0')),
        warnIfMissed: false,
      );
      await tester.pump();
      expect(submissionCount, 1);
      completion.complete(const ChatMessageCardActionResult.failed('选项未被接受'));
      await tester.pumpAndSettle();
      await tester.pump(const Duration(seconds: 1));

      expect(submissionCount, 1);
      expect(find.text('Choose an environment.'), findsOneWidget);
      expect(find.text('选项未被接受'), findsOneWidget);
      expect(
        find.byKey(const Key('chat_message_card_agent_question_option_0')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('chat_message_card_agent_question_answered')),
        findsNothing,
      );
    },
  );

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

  testWidgets(
    'question card renders the prompt once when the header repeats it',
    (WidgetTester tester) async {
      const repeated = '走哪条创建路径?';
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: Scaffold(
            body: ChatAgentQuestionCardView(
              card: const ChatAgentQuestionCardData(
                requestId: 'req-question-repeated-header',
                questions: [
                  ChatAgentQuestionPrompt(
                    index: 1,
                    header: repeated,
                    prompt: repeated,
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

      expect(find.text('1. $repeated'), findsOneWidget);
      expect(find.text(repeated), findsNothing);
    },
  );

  testWidgets('question card keeps both lines when the header differs', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatAgentQuestionCardView(
            card: const ChatAgentQuestionCardData(
              requestId: 'req-question-distinct-header',
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

    expect(find.text('1. Environment'), findsOneWidget);
    expect(find.text('Choose environment.'), findsOneWidget);
  });
}
