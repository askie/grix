import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_pairing_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_question_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_conversation_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_exec_approval_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_egg_install_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_exec_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_message_card_action.dart';
import 'package:grix/modules/chat/message_cards/models/chat_tool_execution_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_user_profile_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_agent_card_action_encoder.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/shared/widgets/message_bubble.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('renders user profile card from message extra', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildUserProfileCard(
      userId: '1001',
      nickname: 'Alice',
      avatarUrl: '',
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-user-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_user_profile')),
      findsOneWidget,
    );
    expect(find.text('Profile Card'), findsOneWidget);
    expect(find.text('Alice'), findsOneWidget);
  });

  testWidgets('tapping user profile card triggers message card callback', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildUserProfileCard(
      userId: '1002',
      nickname: 'Bob',
      avatarUrl: '',
    );
    ChatUserProfileCardData? tappedCard;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-user-card-tap',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardTap: (card) {
              if (card is ChatUserProfileCardData) {
                tappedCard = card;
              }
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('chat_message_card_user_profile')));
    await tester.pumpAndSettle();

    expect(tappedCard, isNotNull);
    expect(tappedCard!.userId, '1002');
    expect(tappedCard!.displayName, 'Bob');
  });

  testWidgets('renders conversation card from message extra', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildConversationCard(
      sessionId: 'session-100',
      sessionType: 'group',
      title: '产品群',
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-conversation-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_conversation')),
      findsOneWidget,
    );
    expect(find.text('产品群'), findsOneWidget);
    expect(find.byIcon(Icons.groups_rounded), findsOneWidget);
  });

  testWidgets('tapping conversation card triggers message card callback', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildConversationCard(
      sessionId: 'session-200',
      sessionType: 'private',
      title: 'Alice',
      peerId: '1001',
      peerNickname: 'Alice',
    );
    ChatConversationCardData? tappedCard;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-conversation-card-tap',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardTap: (card) {
              if (card is ChatConversationCardData) {
                tappedCard = card;
              }
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('chat_message_card_conversation')));
    await tester.pumpAndSettle();

    expect(tappedCard, isNotNull);
    expect(tappedCard!.sessionId, 'session-200');
    expect(tappedCard!.normalizedSessionType, 'private');
    expect(tappedCard!.peerId, '1001');
  });

  testWidgets('renders conversation card from standalone grix card link', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildConversationCard(
      sessionId: 'session-300',
      sessionType: 'group',
      title: '研发群',
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-conversation-card-directive',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_conversation')),
      findsOneWidget,
    );
    expect(find.text('研发群'), findsOneWidget);
    expect(find.byIcon(Icons.groups_rounded), findsOneWidget);
  });

  testWidgets('old conversation directive now renders as plain text', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const Scaffold(
          body: MessageBubble(
            msgId: 'msg-conversation-card-inline-text',
            initialContent:
                '[[conversation-card|session_id=session-301|session_type=group|title=%E6%B5%8B%E8%AF%95%E7%BE%A4]]',
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_conversation')),
      findsNothing,
    );
    expect(
      find.textContaining('[[conversation-card|session_id=session-301'),
      findsOneWidget,
    );
  });

  testWidgets('renders exec approval card from message extra', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval_full_123',
      approvalSlug: 'req_123',
      command: 'rm -rf /tmp/demo && echo done',
      host: 'gateway',
      nodeId: 'node-1',
      cwd: '/tmp/demo',
      expiresInSeconds: 45,
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-exec-approval-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_exec_approval')),
      findsOneWidget,
    );
    expect(find.text('Exec Approval'), findsOneWidget);
    expect(find.text('Pending Command'), findsOneWidget);
    expect(find.text('rm -rf /tmp/demo && echo done'), findsOneWidget);
    expect(find.text('Allow Once'), findsOneWidget);
    expect(find.text('Allow Always'), findsOneWidget);
    expect(find.text('Deny'), findsOneWidget);
  });

  testWidgets('renders Claude approval request as exec approval card', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.encode(
      const ChatExecApprovalCardData(
        approvalId: 'req-123',
        approvalSlug: 'req-123',
        approvalCommandId: 'req-123',
        command: 'Tool: Bash\nCommand: pwd',
        host: 'Claude Grix',
        allowedDecisions: <String>['allow-once', 'deny'],
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-approval-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_exec_approval')),
      findsOneWidget,
    );
    expect(find.text('Tool: Bash\nCommand: pwd'), findsOneWidget);
    expect(find.text('Allow Once'), findsOneWidget);
    expect(find.text('Deny'), findsOneWidget);
    expect(find.text('Allow Always'), findsNothing);
  });

  testWidgets('renders Claude approval rule actions', (
    WidgetTester tester,
  ) async {
    ChatMessageCardAction? tappedAction;
    tester.view.physicalSize = const Size(1200, 1800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final envelope = ChatMessageCardCodec.encode(
      const ChatExecApprovalCardData(
        approvalId: 'req-rule-1',
        approvalSlug: 'req-rule-1',
        approvalCommandId: 'req-rule-1',
        command:
            'Tool: Bash\n'
            'Command: pwd\n\n'
            'Rule suggestions:\n'
            '1. {"tool":"Bash","cmd":"pwd"}\n'
            '2. {"tool":"Read","path":"/tmp/demo"}',
        host: 'Claude Grix',
        allowedDecisions: <String>[
          'allow-once',
          'allow-rule:1',
          'allow-rule:2',
          'deny',
        ],
        decisionCommands: <String, String>{
          'allow-rule:1': '/grix approval req-rule-1 allow-rule 1',
          'allow-rule:2': '/grix approval req-rule-1 allow-rule 2',
        },
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-approval-rule-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (action) async {
              tappedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Apply Rule 1'), findsOneWidget);
    expect(find.text('Apply Rule 2'), findsOneWidget);

    await tester.tap(
      find.byKey(const Key('chat_message_card_exec_approval_allow_rule_2')),
    );
    await tester.pump();

    expect(tappedAction, isNotNull);
    expect(tappedAction!.actionId, 'allow-rule:2');
  });

  testWidgets('tapping exec approval button triggers message card action', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval_full_123',
      approvalSlug: 'req_123',
      command: 'pwd',
      host: 'gateway',
    );
    ChatMessageCardAction? tappedAction;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-exec-approval-card-action',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (action) async {
              tappedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(const Key('chat_message_card_exec_approval_allow_once')),
    );
    await tester.pump();

    expect(tappedAction, isNotNull);
    expect(tappedAction!.actionId, 'allow-once');
    expect(tappedAction!.card, isA<ChatExecApprovalCardData>());
    expect(
      (tappedAction!.card as ChatExecApprovalCardData).approvalSlug,
      'req_123',
    );
  });

  testWidgets('deny exec approval action requires confirmation', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval_full_123',
      approvalSlug: 'req_123',
      command: 'pwd',
      host: 'gateway',
    );
    ChatMessageCardAction? tappedAction;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-exec-approval-card-deny-confirm',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (action) async {
              tappedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(const Key('chat_message_card_exec_approval_deny')),
    );
    await tester.pumpAndSettle();

    expect(find.text('Deny this command?'), findsOneWidget);
    expect(tappedAction, isNull);

    await tester.tap(find.text('Continue'));
    await tester.pump();

    expect(tappedAction, isNotNull);
    expect(tappedAction!.actionId, 'deny');
  });

  testWidgets('renders Claude question card and taps quick reply', (
    WidgetTester tester,
  ) async {
    ChatAgentQuestionCardData? tappedCard;
    ChatMessageCardAction? tappedAction;
    tester.view.physicalSize = const Size(1200, 1800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    const card = ChatAgentQuestionCardData(
      requestId: 'question-1',
      questions: <ChatAgentQuestionPrompt>[
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Choose the deployment target.',
          options: <String>['prod', 'staging'],
        ),
      ],
      footerText: 'Free text is allowed when none of the listed options fit.',
    );
    final envelope = ChatMessageCardCodec.encode(card);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-question-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (action) async {
              tappedAction = action;
              tappedCard = action.card as ChatAgentQuestionCardData;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_question')),
      findsOneWidget,
    );
    expect(find.text('Input Request'), findsOneWidget);
    expect(find.text('Choose the deployment target.'), findsOneWidget);

    await tester.tap(
      find.byKey(const Key('chat_message_card_agent_question_option_0')),
    );
    await tester.pump();

    expect(tappedAction, isNotNull);
    expect(
      tappedAction!.actionId,
      ChatAgentCardActionEncoder.buildQuestionQuickReplyAction(card, 'prod'),
    );
    expect(tappedCard, isNotNull);
    expect(tappedCard!.requestId, 'question-1');
  });

  testWidgets('Claude question card submits quick reply action', (
    WidgetTester tester,
  ) async {
    ChatMessageCardAction? tappedAction;
    tester.view.physicalSize = const Size(1200, 1800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    const card = ChatAgentQuestionCardData(
      requestId: 'question-1',
      questions: <ChatAgentQuestionPrompt>[
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Choose the deployment target.',
          options: <String>['prod', 'staging'],
        ),
      ],
      footerText: 'Free text is allowed when none of the listed options fit.',
    );
    final envelope = ChatMessageCardCodec.encode(card);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-question-card-answered',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (action) async {
              tappedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(const Key('chat_message_card_agent_question_option_1')),
    );
    await tester.pumpAndSettle();

    expect(tappedAction, isNotNull);
    expect(
      tappedAction!.actionId,
      ChatAgentCardActionEncoder.buildQuestionQuickReplyAction(card, 'staging'),
    );
    // 乐观提交：点选快捷回复后卡片显示"Answered:"并隐藏表单（提交按钮）
    expect(find.text('Answered: staging'), findsOneWidget);
    expect(find.text('Submitting'), findsOneWidget);
    expect(
      find.byKey(const Key('chat_message_card_agent_question_submit')),
      findsNothing,
    );
  });

  testWidgets('Claude question card submits structured answers', (
    WidgetTester tester,
  ) async {
    ChatMessageCardAction? tappedAction;
    tester.view.physicalSize = const Size(1200, 2000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    const card = ChatAgentQuestionCardData(
      requestId: 'question-2',
      questions: <ChatAgentQuestionPrompt>[
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Choose the deployment target.',
          options: <String>['prod', 'staging'],
        ),
        ChatAgentQuestionPrompt(
          index: 2,
          header: 'Region',
          prompt: 'Choose the deployment region.',
        ),
      ],
      footerText: 'Free text is allowed when none of the listed options fit.',
    );
    final envelope = ChatMessageCardCodec.encode(card);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-question-card-structured',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (action) async {
              tappedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(
        Key('chat_message_card_agent_question_option_1_${'prod'.hashCode}'),
      ),
    );
    await tester.pumpAndSettle();
    await tester.enterText(
      find.byKey(const Key('chat_message_card_agent_question_input_2')),
      'cn-hz',
    );
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(const Key('chat_message_card_agent_question_submit')),
    );
    await tester.pumpAndSettle();

    expect(tappedAction, isNotNull);
    expect(
      tappedAction!.actionId,
      ChatAgentCardActionEncoder.buildQuestionStructuredReplyAction(card, {
        1: 'prod',
        2: 'cn-hz',
      }),
    );
    expect(find.text('Answered: 1=prod; 2=cn-hz'), findsNothing);
  });

  testWidgets('Claude question card submits multi-select answer', (
    WidgetTester tester,
  ) async {
    ChatMessageCardAction? tappedAction;
    tester.view.physicalSize = const Size(1200, 1800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    const card = ChatAgentQuestionCardData(
      requestId: 'question-3',
      questions: <ChatAgentQuestionPrompt>[
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Packages',
          prompt: 'Choose packages to install.',
          options: <String>['api', 'worker', 'web'],
          multiSelect: true,
        ),
      ],
      footerText: 'Free text is allowed when none of the listed options fit.',
    );
    final envelope = ChatMessageCardCodec.encode(card);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-question-card-multiselect',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (action) async {
              tappedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(
        Key('chat_message_card_agent_question_option_1_${'api'.hashCode}'),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(
        Key('chat_message_card_agent_question_option_1_${'web'.hashCode}'),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(
      find.byKey(const Key('chat_message_card_agent_question_submit')),
    );
    await tester.pumpAndSettle();

    expect(tappedAction, isNotNull);
    expect(
      tappedAction!.actionId,
      ChatAgentCardActionEncoder.buildQuestionStructuredReplyAction(card, {
        1: 'api, web',
      }),
    );
    // 乐观提交：结构化提交后显示"Answered:"
    expect(find.text('Answered: api, web'), findsOneWidget);
  });

  testWidgets('Claude question card renders submitted answer from payload', (
    WidgetTester tester,
  ) async {
    const card = ChatAgentQuestionCardData(
      requestId: 'question-9',
      questions: [
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Choose environment.',
          options: ['prod', 'staging'],
        ),
      ],
      submittedAnswer: 'prod',
    );
    final envelope = ChatMessageCardCodec.encode(card);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-question-card-submitted',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_question_answered')),
      findsOneWidget,
    );
    expect(find.text('Answered: prod'), findsOneWidget);
    expect(
      find.byKey(const Key('chat_message_card_agent_question_submit')),
      findsNothing,
    );
  });

  testWidgets('Claude question card renders url mode actions', (
    WidgetTester tester,
  ) async {
    ChatMessageCardAction? tappedAction;
    const card = ChatAgentQuestionCardData(
      requestId: 'question-url-1',
      questions: <ChatAgentQuestionPrompt>[],
      mode: 'url',
      message: 'Open the authentication page to continue.',
      url: 'https://auth.example.com/login',
      openUrlLabel: 'Open authentication page',
      footerText:
          'Open the page, finish the flow, then tap Complete. Cancel if you do not want to continue.',
    );
    final envelope = ChatMessageCardCodec.encode(card);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-question-card-url',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (action) async {
              tappedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('https://auth.example.com/login'), findsOneWidget);
    expect(
      find.byKey(const Key('chat_message_card_agent_question_open_url')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('chat_message_card_agent_question_complete')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('chat_message_card_agent_question_cancel')),
      findsOneWidget,
    );

    await tester.tap(
      find.byKey(const Key('chat_message_card_agent_question_complete')),
    );
    await tester.pumpAndSettle();

    expect(tappedAction, isNotNull);
    expect(
      tappedAction!.actionId,
      ChatAgentCardActionEncoder.buildQuestionUrlCompleteAction(card),
    );
    expect(find.text('Answered: Authentication completed.'), findsNothing);
  });

  testWidgets('renders Claude pairing card from message text', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.encode(
      const ChatAgentPairingCardData(
        pairingCode: 'XRWEF5',
        instructionText:
            'Ask the Claude Code user to run /grix access pair <code> with this code to approve the sender.',
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-pairing-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_pairing')),
      findsOneWidget,
    );
    expect(find.text('XRWEF5'), findsOneWidget);
    expect(
      find.byKey(const Key('chat_message_card_agent_pairing_copy')),
      findsOneWidget,
    );
  });

  testWidgets('renders Claude approval error status card', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'approval',
        status: 'error',
        summary: 'Approval request req-404 was not found.',
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-approval-error-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_status')),
      findsOneWidget,
    );
    expect(find.text('Agent Status'), findsOneWidget);
    expect(find.text('Approval Status'), findsOneWidget);
    expect(
      find.text('Approval request req-404 was not found.'),
      findsOneWidget,
    );
  });

  testWidgets('renders Claude question status card', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'question',
        status: 'success',
        summary: 'Question request question-1 answers recorded.',
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-question-status-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_status')),
      findsOneWidget,
    );
    expect(find.text('Question Status'), findsOneWidget);
    expect(
      find.text('Question request question-1 answers recorded.'),
      findsOneWidget,
    );
  });

  testWidgets('renders Claude access status card', (WidgetTester tester) async {
    final envelope = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'access',
        status: 'success',
        summary: 'Paired! Say hi to Claude.',
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-access-status-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_status')),
      findsOneWidget,
    );
    expect(find.text('Access Status'), findsOneWidget);
    expect(find.text('Paired! Say hi to Claude.'), findsOneWidget);
  });

  testWidgets('renders Claude disabled access warning card', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'access',
        status: 'warning',
        summary: 'Claude Grix access is currently disabled for this channel.',
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-access-disabled-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_status')),
      findsOneWidget,
    );
    expect(find.text('Access Status'), findsOneWidget);
    expect(
      find.text('Claude Grix access is currently disabled for this channel.'),
      findsOneWidget,
    );
  });

  testWidgets('renders open workspace card from grix card message', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildAgentOpenSessionCard(
      summaryText: 'open 缺少目录路径。',
      detailText: '请输入工作目录来启动或恢复 Claude 会话。',
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-claude-open-session-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_open_session')),
      findsOneWidget,
    );
    expect(find.text('打开工作目录'), findsOneWidget);
    expect(find.text('open 缺少目录路径。'), findsOneWidget);
  });

  testWidgets('open session card action carries source message id', (
    WidgetTester tester,
  ) async {
    ChatMessageCardAction? tappedAction;
    final envelope = ChatMessageCardCodec.buildAgentOpenSessionCard(
      summaryText: 'open 缺少目录路径。',
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-open-session-source-id',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (action) async {
              tappedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.enterText(
      find.byKey(const Key('chat_message_card_agent_open_session_input')),
      '/workspace/demo',
    );
    await tester.tap(
      find.byKey(const Key('chat_message_card_agent_open_session_submit')),
    );
    await tester.pumpAndSettle();

    expect(tappedAction, isNotNull);
    expect(tappedAction!.sourceMessageId, 'msg-open-session-source-id');
  });

  testWidgets(
    'exec approval card shows submitting state while awaiting result',
    (WidgetTester tester) async {
      final envelope = ChatMessageCardCodec.buildExecApprovalCard(
        approvalId: 'approval_full_321',
        approvalSlug: 'req_321',
        command: 'pwd',
        host: 'gateway',
        expiresInSeconds: 10,
      );
      final completer = Completer<ChatMessageCardActionResult>();

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          home: Scaffold(
            body: MessageBubble(
              msgId: 'msg-exec-approval-card-submitting',
              initialContent: envelope.content,
              messageExtra: envelope.extra,
              onMessageCardAction: (_) => completer.future,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(
        find.byKey(const Key('chat_message_card_exec_approval_allow_once')),
      );
      await tester.pump();

      expect(find.text('Submitting'), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      expect(
        tester.widget<FilledButton>(find.byType(FilledButton).first).onPressed,
        isNull,
      );

      completer.complete(const ChatMessageCardActionResult.submitted());
      await tester.pump();
    },
  );

  testWidgets('exec approval card restores actions after submit failure', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval_full_654',
      approvalSlug: 'req_654',
      command: 'pwd',
      host: 'gateway',
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-exec-approval-card-failed',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (_) async =>
                const ChatMessageCardActionResult.failed(),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(const Key('chat_message_card_exec_approval_allow_once')),
    );
    await tester.pumpAndSettle();

    expect(
      find.text('Failed to submit approval. Please try again.'),
      findsOneWidget,
    );
    expect(
      tester.widget<FilledButton>(find.byType(FilledButton).first).onPressed,
      isNotNull,
    );
  });

  testWidgets(
    'exec approval card keeps submitting state across parent rebuilds',
    (WidgetTester tester) async {
      final envelope = ChatMessageCardCodec.buildExecApprovalCard(
        approvalId: 'approval_full_777',
        approvalSlug: 'req_777',
        command: 'pwd',
        host: 'gateway',
        expiresInSeconds: 10,
      );
      final completer = Completer<ChatMessageCardActionResult>();
      var rebuildTick = 0;

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          home: StatefulBuilder(
            builder: (context, setState) {
              return Scaffold(
                body: Column(
                  children: [
                    Text('tick:$rebuildTick'),
                    MessageBubble(
                      msgId: 'msg-exec-approval-card-rebuild',
                      initialContent: envelope.content,
                      messageExtra: envelope.extra,
                      onMessageCardAction: (_) => completer.future,
                    ),
                    TextButton(
                      onPressed: () {
                        setState(() {
                          rebuildTick++;
                        });
                      },
                      child: const Text('rebuild'),
                    ),
                  ],
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(
        find.byKey(const Key('chat_message_card_exec_approval_allow_once')),
      );
      await tester.pump();

      expect(find.text('Submitting'), findsOneWidget);

      await tester.tap(find.text('rebuild'));
      await tester.pump();

      expect(find.text('Submitting'), findsOneWidget);
      expect(
        tester.widget<FilledButton>(find.byType(FilledButton).first).onPressed,
        isNull,
      );

      completer.complete(const ChatMessageCardActionResult.submitted());
      await tester.pump();
    },
  );

  testWidgets(
    'exec approval card restores submitting state after unmount when approval is still pending',
    (WidgetTester tester) async {
      final envelope = ChatMessageCardCodec.buildExecApprovalCard(
        approvalId: 'approval_full_778',
        approvalSlug: 'req_778',
        command: 'pwd',
        host: 'gateway',
        expiresInSeconds: 10,
      );
      final pendingApprovalIds = <String>{};
      var showCard = true;

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          home: StatefulBuilder(
            builder: (context, setState) {
              return Scaffold(
                body: Column(
                  children: [
                    if (showCard)
                      MessageBubble(
                        msgId: 'msg-exec-approval-card-remount',
                        initialContent: envelope.content,
                        messageExtra: envelope.extra,
                        onMessageCardAction: (_) async {
                          pendingApprovalIds.add('approval_full_778');
                          return const ChatMessageCardActionResult.submitted();
                        },
                        isExecApprovalPending: (approvalId) =>
                            pendingApprovalIds.contains(approvalId),
                      ),
                    TextButton(
                      onPressed: () {
                        setState(() {
                          showCard = !showCard;
                        });
                      },
                      child: const Text('toggle'),
                    ),
                  ],
                ),
              );
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(
        find.byKey(const Key('chat_message_card_exec_approval_allow_once')),
      );
      await tester.pump();

      expect(find.text('Submitting'), findsOneWidget);

      await tester.tap(find.text('toggle'));
      await tester.pump();
      await tester.tap(find.text('toggle'));
      await tester.pump();

      expect(find.text('Submitting'), findsOneWidget);
      expect(
        tester.widget<FilledButton>(find.byType(FilledButton).first).onPressed,
        isNull,
      );
    },
  );

  testWidgets('ignored action result does not show submit failed message', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval_full_778',
      approvalSlug: 'req_778',
      command: 'pwd',
      host: 'gateway',
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-exec-approval-card-ignored',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (_) async =>
                const ChatMessageCardActionResult.ignored(),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(const Key('chat_message_card_exec_approval_allow_once')),
    );
    await tester.pump();

    expect(
      find.text('Failed to submit approval. Please try again.'),
      findsNothing,
    );
    expect(find.text('Submitting'), findsOneWidget);
  });

  testWidgets('renders exec status card from message extra', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildExecStatusCard(
      status: 'finished',
      summary:
          'Exec finished (gateway id=approval_full_123, session=sess_456, code 0)',
      detailText: 'stdout line 1\nstdout line 2',
      approvalId: 'approval_full_123',
      host: 'gateway',
      sessionId: 'sess_456',
      exitLabel: 'code 0',
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-exec-status-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_exec_status')),
      findsOneWidget,
    );
    expect(find.text('Exec Status'), findsOneWidget);
    expect(find.text('Finished'), findsOneWidget);
    expect(
      find.text(
        'Exec finished (gateway id=approval_full_123, session=sess_456, code 0)',
      ),
      findsOneWidget,
    );
    expect(find.text('stdout line 1\nstdout line 2'), findsOneWidget);
  });

  testWidgets('renders compact tool execution card and expands on tap', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.encode(
      const ChatToolExecutionCardData(
        summaryText: 'Tool: read /tmp/demo',
        detailText: '```txt\nhello\n```',
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-tool-execution-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_tool_execution')),
      findsOneWidget,
    );
    expect(find.text('Tool'), findsOneWidget);
    expect(find.text('Tool: read /tmp/demo'), findsOneWidget);
    expect(find.text('Open'), findsOneWidget);
    expect(find.text('```txt\nhello\n```'), findsNothing);

    await tester.tap(
      find.byKey(const Key('chat_message_card_tool_execution_toggle')),
    );
    await tester.pumpAndSettle();

    expect(find.text('Hide'), findsOneWidget);
    expect(find.text('```txt\nhello\n```'), findsOneWidget);
  });

  testWidgets('renders egg install status card from message extra', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.encode(
      const ChatEggInstallStatusCardData(
        installId: 'eggins_1',
        status: 'running',
        summary: 'Package downloaded and verified',
        step: 'downloaded',
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-egg-install-status-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_egg_install_status')),
      findsOneWidget,
    );
    expect(find.text('Install ID: eggins_1'), findsOneWidget);
    expect(find.text('In Progress'), findsOneWidget);
    expect(find.text('Package downloaded and verified'), findsOneWidget);
    expect(find.text('Egg Install'), findsNothing);

    final installIdTop = tester
        .getTopLeft(find.text('Install ID: eggins_1'))
        .dy;
    final statusTop = tester.getTopLeft(find.text('In Progress')).dy;
    final summaryTop = tester
        .getTopLeft(find.text('Package downloaded and verified'))
        .dy;

    expect(installIdTop, lessThan(statusTop));
    expect(statusTop, lessThan(summaryTop));
    expect(
      find.byKey(const Key('chat_message_card_egg_install_status_icon_box')),
      findsNothing,
    );
    expect(
      find.byKey(const Key('chat_message_card_egg_install_status_badge')),
      findsOneWidget,
    );
  });

  testWidgets('renders egg install agent-created step label', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.encode(
      const ChatEggInstallStatusCardData(
        installId: 'eggins_2',
        status: 'running',
        summary: 'Remote agent created',
        step: 'agent_created',
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-egg-install-agent-created-card',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Step: Remote Agent Created'), findsOneWidget);
    expect(find.text('Remote agent created'), findsOneWidget);
  });

  testWidgets(
    'exec status card renders decision label separately from reason',
    (WidgetTester tester) async {
      final envelope = ChatMessageCardCodec.buildExecStatusCard(
        status: 'resolved-allow-once',
        summary: 'Allow once selected by u_1.',
        approvalId: 'approval_full_123',
        decision: 'allow-once',
        reason: 'trusted build',
        resolvedById: 'u_1',
      );

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          home: Scaffold(
            body: MessageBubble(
              msgId: 'msg-exec-status-decision-card',
              initialContent: envelope.content,
              messageExtra: envelope.extra,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Reason: trusted build'), findsOneWidget);
      expect(find.text('Decision: Allowed Once'), findsOneWidget);
    },
  );

  testWidgets('renders resolved exec approval card without actions', (
    WidgetTester tester,
  ) async {
    const resolvedCard = ChatExecApprovalCardData(
      approvalId: 'approval_full_123',
      approvalSlug: 'req_123',
      approvalCommandId: 'approval_full_123',
      command: 'pwd',
      host: 'gateway',
      allowedDecisions: ['allow-once', 'allow-always', 'deny'],
      resolutionStatus: ChatExecStatusCardData(
        status: 'resolved-allow-once',
        summary: 'Allow once selected by u_1.',
        approvalId: 'approval_full_123',
        approvalCommandId: 'approval_full_123',
        decision: 'allow-once',
        resolvedById: 'u_1',
      ),
      executionStatus: ChatExecStatusCardData(
        status: 'finished',
        summary:
            'Exec finished (gateway id=approval_full_123, session=sess_456, code 0)',
        approvalId: 'approval_full_123',
        host: 'gateway',
        sessionId: 'sess_456',
        exitLabel: 'code 0',
      ),
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const Scaffold(
          body: MessageBubble(
            msgId: 'msg-exec-approval-card-resolved',
            messageCardDataOverride: resolvedCard,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Result'), findsOneWidget);
    expect(find.text('Allowed Once'), findsOneWidget);
    expect(
      find.text(
        'Exec finished (gateway id=approval_full_123, session=sess_456, code 0)',
      ),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('chat_message_card_exec_approval_allow_once')),
      findsNothing,
    );
    expect(
      find.byKey(const Key('chat_message_card_exec_approval_deny')),
      findsNothing,
    );
  });
}
