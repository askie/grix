import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_open_session_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_question_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_exec_approval_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_message_card_action.dart';
import 'package:grix/modules/chat/message_cards/models/chat_message_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_agent_card_action_encoder.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';
import 'package:grix/shared/widgets/message_bubble.dart';

import 'markdown_link_finder.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  ChatMessageCardEnvelope buildEnvelope(ChatMessageCardData card) {
    return ChatMessageCardCodec.encode(card);
  }

  Widget buildScrollableBubbleApp({
    required Locale locale,
    required MessageBubble bubble,
  }) {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: locale,
      home: Scaffold(
        body: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: bubble,
        ),
      ),
    );
  }

  testWidgets('renders standalone grix card markdown as exec approval card', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval_full_123',
      approvalSlug: 'req_123',
      command: 'echo "Hello, World!"',
      host: 'gateway',
      allowedDecisions: const ['allow-once', 'deny'],
    );

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-grix-card-link',
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
    expect(find.text('echo "Hello, World!"'), findsOneWidget);
    expect(find.textContaining('grix://card/'), findsNothing);
  });

  testWidgets('standalone grix card markdown keeps exec approval actions', (
    WidgetTester tester,
  ) async {
    final envelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval_full_789',
      approvalSlug: 'req_789',
      command: 'pwd',
      host: 'gateway',
      allowedDecisions: const ['allow-once', 'deny'],
    );
    ChatMessageCardAction? tappedAction;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: Scaffold(
          body: MessageBubble(
            msgId: 'msg-grix-card-link-action',
            initialContent: envelope.content,
            messageExtra: envelope.extra,
            onMessageCardAction: (action) async {
              if (action.card is ChatExecApprovalCardData) {
                tappedAction = action;
              }
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
  });

  testWidgets(
    'renders grix card links inside chat markdown messages as cards',
    (WidgetTester tester) async {
      const content = '''
现在路线，先创建远端 API agent。
[已创建远端 Agent](grix://card/egg_install_status?install_id=eggins_204216015836196864&status=running&step=agent_created&target_agent_id=2042126968270360576&summary=%E5%B7%B2%E5%88%9B%E5%BB%BA%E8%BF%9C%E7%AB%AF%20Agent)
远端 agent 已创建成功，开始下载 persona 包。
''';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('en', 'US'),
          home: const Scaffold(
            body: MessageBubble(
              msgId: 'msg-grix-inline-card-link',
              initialContent: content,
              messageExtra: {},
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('chat_message_card_egg_install_status')),
        findsOneWidget,
      );
      expect(find.textContaining('grix://card/'), findsNothing);
    },
  );

  testWidgets(
    'inline grix exec approval card in markdown forwards approval action',
    (WidgetTester tester) async {
      final envelope = ChatMessageCardCodec.buildExecApprovalCard(
        approvalId: 'approval_full_inline_1',
        approvalSlug: 'req_inline_1',
        command: 'pwd',
        host: 'gateway',
        allowedDecisions: const ['allow-once', 'deny'],
      );
      ChatMessageCardAction? tappedAction;
      final content =
          '''
执行前请确认命令。
${envelope.content}
执行完成后会继续后续步骤。
''';

      await tester.pumpWidget(
        buildScrollableBubbleApp(
          locale: const Locale('en', 'US'),
          bubble: MessageBubble(
            msgId: 'msg-grix-inline-exec-approval-action',
            initialContent: content,
            messageExtra: const {},
            onMessageCardAction: (action) async {
              tappedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(
        find.byKey(const Key('chat_message_card_exec_approval_allow_once')),
      );
      await tester.pump();

      expect(tappedAction, isNotNull);
      expect(tappedAction!.card, isA<ChatExecApprovalCardData>());
      expect(tappedAction!.actionId, 'allow-once');
    },
  );

  testWidgets(
    'inline grix Claude question card in markdown forwards quick reply action',
    (WidgetTester tester) async {
      const card = ChatAgentQuestionCardData(
        requestId: 'question-inline-1',
        questions: <ChatAgentQuestionPrompt>[
          ChatAgentQuestionPrompt(
            index: 1,
            header: 'Environment',
            prompt: 'Choose an environment.',
            options: <String>['staging', 'production'],
          ),
        ],
      );
      final envelope = buildEnvelope(card);
      ChatMessageCardAction? tappedAction;
      final content =
          '''
需要你补充一个环境参数。
${envelope.content}
''';

      await tester.pumpWidget(
        buildScrollableBubbleApp(
          locale: const Locale('en', 'US'),
          bubble: MessageBubble(
            msgId: 'msg-grix-inline-agent-question-action',
            initialContent: content,
            messageExtra: const {},
            onMessageCardAction: (action) async {
              tappedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.ensureVisible(
        find.byKey(const Key('chat_message_card_agent_question_option_0')),
      );
      await tester.tap(
        find.byKey(const Key('chat_message_card_agent_question_option_0')),
      );
      await tester.pump();

      expect(tappedAction, isNotNull);
      expect(tappedAction!.card, isA<ChatAgentQuestionCardData>());
      expect(
        tappedAction!.actionId,
        ChatAgentCardActionEncoder.buildQuestionQuickReplyAction(
          card,
          'staging',
        ),
      );
    },
  );

  testWidgets(
    'inline grix Claude open session card in markdown forwards submit action',
    (WidgetTester tester) async {
      const card = ChatAgentOpenSessionCardData(
        summaryText: 'open 缺少目录路径。',
        detailText: '请输入工作目录来启动或恢复 Claude 会话。',
      );
      final envelope = buildEnvelope(card);
      ChatMessageCardAction? tappedAction;
      final content =
          '''
请先补全目录。
${envelope.content}
''';

      await tester.pumpWidget(
        buildScrollableBubbleApp(
          locale: const Locale('zh', 'CN'),
          bubble: MessageBubble(
            msgId: 'msg-grix-inline-agent-open-action',
            initialContent: content,
            messageExtra: const {},
            onMessageCardAction: (action) async {
              tappedAction = action;
              return const ChatMessageCardActionResult.submitted();
            },
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
      await tester.pump();

      expect(tappedAction, isNotNull);
      expect(tappedAction!.card, isA<ChatAgentOpenSessionCardData>());
      expect(
        tappedAction!.actionId,
        ChatAgentCardActionEncoder.buildOpenSessionAction(
          card,
          '/workspace/demo',
        ),
      );
      expect(
        tappedAction!.sourceMessageId,
        'msg-grix-inline-agent-open-action',
      );
    },
  );

  testWidgets(
    'renders egg install card when grix URI uses lenient query format',
    (WidgetTester tester) async {
      const content = '''
现在配置本地 OpenClaw。
[安装中](grix://card/egg_install_status?install_id=eggins_2042145742675513344&amp;status=running&amp;step=installe&amp;summary=%E7%8E%B0%E5%9C%A8%E9%85%8D%E7%BD%AE%E6%9C%AC%E5%9C%B0%20OpenClaw%E3%80%82%E5%AE%89%E8%A3%85%E5%86%85%E5%AE%B9%E5%B7%B2%E8%90%BD%E4%BD%8D%EF%BC%8C%E6%A0%A1%E9%AA%8C%E4%B8%AD%)
安装内容已落位，校验中。
''';

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: const Scaffold(
            body: MessageBubble(
              msgId: 'msg-grix-inline-card-link-lenient',
              initialContent: content,
              messageExtra: {},
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('chat_message_card_egg_install_status')),
        findsOneWidget,
      );
      expect(find.textContaining('grix://card/'), findsNothing);
    },
  );

  testWidgets('does not open malformed grix card links as external URLs', (
    WidgetTester tester,
  ) async {
    const content =
        '[安装状态](grix://card/egg_install_status?install_id=&status=running&summary=)';

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const Scaffold(
          body: MessageBubble(
            msgId: 'msg-grix-inline-card-link-invalid',
            initialContent: content,
            messageExtra: {},
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(tappableLinkSpans(tester), isEmpty);
    expect(find.text('安装状态'), findsOneWidget);
    expect(find.textContaining('grix://card/'), findsNothing);
  });
}
