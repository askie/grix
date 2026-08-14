import 'dart:async';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_open_session_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_message_card_action.dart';
import 'package:grix/modules/chat/message_cards/services/chat_agent_card_action_encoder.dart';
import 'package:grix/modules/chat/message_cards/services/chat_open_session_draft_store.dart';
import 'package:grix/modules/chat/message_cards/widgets/chat_agent_open_session_card_view.dart';

void main() {
  testWidgets('Windows open session card shows browse button and fills input', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatAgentOpenSessionCardView(
            card: const ChatAgentOpenSessionCardData(
              cardInstanceId: 'card-open-1',
              summaryText: 'open 缺少目录路径。',
            ),
            isMine: false,
            fontScale: 1,
            platform: TargetPlatform.windows,
            pickRemoteDirectory: () async => r'C:\work\demo',
            onSubmit: (_) async =>
                const ChatMessageCardActionResult.submitted(),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_open_session_browse')),
      findsOneWidget,
    );

    await tester.tap(
      find.byKey(const Key('chat_message_card_agent_open_session_browse')),
    );
    await tester.pumpAndSettle();

    final input = tester.widget<TextField>(
      find.byKey(const Key('chat_message_card_agent_open_session_input')),
    );
    expect(input.controller?.text, r'C:\work\demo');
  });

  testWidgets('Android open session card hides browse button', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatAgentOpenSessionCardView(
            card: const ChatAgentOpenSessionCardData(
              cardInstanceId: 'card-open-1',
              summaryText: 'open 缺少目录路径。',
            ),
            isMine: false,
            fontScale: 1,
            platform: TargetPlatform.android,
            onSubmit: (_) async =>
                const ChatMessageCardActionResult.submitted(),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_open_session_browse')),
      findsNothing,
    );
    expect(
      find.byKey(const Key('chat_message_card_agent_open_session_input')),
      findsOneWidget,
    );
  });

  testWidgets('desktop open session card keeps focus on mouse outside tap', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatAgentOpenSessionCardView(
            card: const ChatAgentOpenSessionCardData(
              cardInstanceId: 'card-open-1',
              summaryText: 'open 缺少目录路径。',
            ),
            isMine: false,
            fontScale: 1,
            platform: TargetPlatform.macOS,
            onSubmit: (_) async =>
                const ChatMessageCardActionResult.submitted(),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final input = tester.widget<TextField>(
      find.byKey(const Key('chat_message_card_agent_open_session_input')),
    );
    final focusNode = input.focusNode;
    expect(focusNode, isNotNull);
    focusNode!.requestFocus();
    await tester.pump();
    expect(focusNode.hasFocus, isTrue);

    final onTapOutside = input.onTapOutside;
    expect(onTapOutside, isNotNull);
    onTapOutside!(const PointerDownEvent(kind: ui.PointerDeviceKind.mouse));
    await tester.pump();

    expect(
      focusNode.hasFocus,
      isTrue,
      reason: 'Desktop paste menu clicks should not steal focus.',
    );
  });

  testWidgets('open session card renders submitted state from card payload', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const Scaffold(
          body: ChatAgentOpenSessionCardView(
            card: ChatAgentOpenSessionCardData(
              summaryText: 'open 缺少目录路径。',
              submittedPath: '/workspace/demo',
            ),
            isMine: false,
            fontScale: 1,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_open_session_input')),
      findsNothing,
    );
    expect(
      find.byKey(const Key('chat_message_card_agent_open_session_submitted')),
      findsOneWidget,
    );
    expect(find.text('已提交工作目录：/workspace/demo'), findsOneWidget);
  });

  testWidgets('open session card submits grix action link', (
    WidgetTester tester,
  ) async {
    String? submittedAction;

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatAgentOpenSessionCardView(
            card: const ChatAgentOpenSessionCardData(
              cardInstanceId: 'card-open-1',
              summaryText: 'open 缺少目录路径。',
            ),
            isMine: false,
            fontScale: 1,
            onSubmit: (action) async {
              submittedAction = action;
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
    await tester.pump(const Duration(milliseconds: 300));

    expect(
      submittedAction,
      ChatAgentCardActionEncoder.buildOpenSessionAction(
        const ChatAgentOpenSessionCardData(
          cardInstanceId: 'card-open-1',
          summaryText: 'open 缺少目录路径。',
        ),
        '/workspace/demo',
      ),
    );
    expect(
      find.byKey(const Key('chat_message_card_agent_open_session_input')),
      findsNothing,
    );
    expect(find.text('已提交工作目录：/workspace/demo'), findsOneWidget);
  });

  testWidgets(
    'open session card locks submit button while request is pending',
    (WidgetTester tester) async {
      final completer = Completer<ChatMessageCardActionResult>();
      var submitCount = 0;

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: Scaffold(
            body: ChatAgentOpenSessionCardView(
              card: const ChatAgentOpenSessionCardData(
                summaryText: 'open 缺少目录路径。',
              ),
              isMine: false,
              fontScale: 1,
              onSubmit: (_) {
                submitCount += 1;
                return completer.future;
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
      await tester.pump();

      expect(submitCount, 1);
      expect(find.text('提交中'), findsOneWidget);

      final submitButton = tester.widget<FilledButton>(
        find.byKey(const Key('chat_message_card_agent_open_session_submit')),
      );
      expect(submitButton.onPressed, isNull);

      await tester.tap(
        find.byKey(const Key('chat_message_card_agent_open_session_submit')),
        warnIfMissed: false,
      );
      await tester.pump();
      expect(submitCount, 1);

      completer.complete(const ChatMessageCardActionResult.submitted());
      await tester.pumpAndSettle();
      await tester.pump(const Duration(milliseconds: 300));

      expect(find.text('已提交工作目录：/workspace/demo'), findsOneWidget);
    },
  );

  testWidgets('open session card renders mapped result on the same card', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const Scaffold(
          body: ChatAgentOpenSessionCardView(
            card: ChatAgentOpenSessionCardData(
              summaryText: 'open 缺少目录路径。',
              submittedPath: '/workspace/demo',
              submissionStatus: ChatAgentStatusCardData(
                category: 'session',
                status: 'success',
                summary: 'Codex session opened for /workspace/demo.',
                detailText: 'Workspace: /workspace/demo\nWorker: starting',
                referenceId: 'session-1',
              ),
            ),
            isMine: false,
            fontScale: 1,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_message_card_agent_open_session_result')),
      findsOneWidget,
    );
    expect(
      find.text('Codex session opened for /workspace/demo.'),
      findsOneWidget,
    );
  });

  testWidgets(
    'open session card recovers from race condition when backend retry card arrives before onSubmit completes',
    (WidgetTester tester) async {
      // Simulates the race condition: server processes the failure so fast
      // that the card edit arrives while _handleSubmit is still awaiting
      // onSubmit, i.e. _isSubmitting is still true and _hasPendingSubmission
      // is still false.
      final completer = Completer<ChatMessageCardActionResult>();
      var card = const ChatAgentOpenSessionCardData(
        cardInstanceId: 'card-race-1',
        summaryText: '当前对话还没有打开工作目录。',
        detailText: '先提交一个工作目录。',
      );

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: Scaffold(
            body: StatefulBuilder(
              builder: (context, setState) {
                return ChatAgentOpenSessionCardView(
                  card: card,
                  isMine: false,
                  fontScale: 1,
                  onSubmit: (_) => completer.future,
                  key: const ValueKey('test-card'),
                );
              },
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Enter path and click submit — onSubmit is now awaiting (completer not completed).
      await tester.enterText(
        find.byKey(const Key('chat_message_card_agent_open_session_input')),
        r'D:\go\src\grix-connector',
      );
      await tester.tap(
        find.byKey(const Key('chat_message_card_agent_open_session_submit')),
      );
      await tester.pump(); // process setState({ _isSubmitting = true })

      // While onSubmit is pending, backend edits the card to a retry form
      // (simulating the race condition where local_action_result is processed
      // before the chat message acknowledgment returns).
      card = const ChatAgentOpenSessionCardData(
        cardInstanceId: 'card-race-1',
        summaryText: 'Claude workspace path is invalid.',
        detailText: 'Specified path is not valid on this host: D:\\go\\src\\grix-connector',
        initialCwd: r'D:\go\src\grix-connector',
      );
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: Scaffold(
            body: StatefulBuilder(
              builder: (context, setState) {
                return ChatAgentOpenSessionCardView(
                  card: card,
                  isMine: false,
                  fontScale: 1,
                  onSubmit: (_) => completer.future,
                  key: const ValueKey('test-card'),
                );
              },
            ),
          ),
        ),
      );
      await tester.pump(); // process didUpdateWidget

      // Now onSubmit completes with submitted.
      completer.complete(const ChatMessageCardActionResult.submitted());
      await tester.pumpAndSettle();
      await tester.pump(const Duration(milliseconds: 300));

      // The card should show the retry input form, NOT the pending/submitted state.
      expect(
        find.byKey(const Key('chat_message_card_agent_open_session_input')),
        findsOneWidget,
        reason: 'Input form should be visible after backend reset (race condition recovery)',
      );
      expect(
        find.byKey(const Key('chat_message_card_agent_open_session_submit')),
        findsOneWidget,
        reason: 'Submit button should be re-enabled after backend reset',
      );
      expect(
        // zh_CN 环境下后端英文模板被本地化。
        find.text('Claude 工作目录路径无效。'),
        findsOneWidget,
        reason: 'Backend error message should be visible',
      );
    },
  );

  testWidgets(
    'open session card shows retry form when backend edit arrives after submission pending',
    (WidgetTester tester) async {
      // Normal timing: onSubmit completes first (setting _hasPendingSubmission),
      // then the backend card edit arrives.
      var card = const ChatAgentOpenSessionCardData(
        cardInstanceId: 'card-normal-1',
        summaryText: '当前对话还没有打开工作目录。',
        detailText: '先提交一个工作目录。',
      );

      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: Scaffold(
            body: StatefulBuilder(
              builder: (context, setState) {
                return ChatAgentOpenSessionCardView(
                  card: card,
                  isMine: false,
                  fontScale: 1,
                  onSubmit: (_) async =>
                      const ChatMessageCardActionResult.submitted(),
                  key: const ValueKey('test-card'),
                );
              },
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Submit — onSubmit returns immediately with submitted.
      await tester.enterText(
        find.byKey(const Key('chat_message_card_agent_open_session_input')),
        r'D:\go\src\grix-connector',
      );
      await tester.tap(
        find.byKey(const Key('chat_message_card_agent_open_session_submit')),
      );
      await tester.pumpAndSettle();
      await tester.pump(const Duration(milliseconds: 300));

      // Card should be in pending state (input hidden, submitted text shown).
      expect(
        find.byKey(const Key('chat_message_card_agent_open_session_input')),
        findsNothing,
        reason: 'Input should be hidden in pending state',
      );

      // Backend edits card to retry form.
      card = const ChatAgentOpenSessionCardData(
        cardInstanceId: 'card-normal-1',
        summaryText: 'Claude workspace path is invalid.',
        detailText: 'Specified path is not valid on this host: D:\\go\\src\\grix-connector',
        initialCwd: r'D:\go\src\grix-connector',
      );
      await tester.pumpWidget(
        GetMaterialApp(
          translations: AppTranslations(),
          locale: const Locale('zh', 'CN'),
          home: Scaffold(
            body: StatefulBuilder(
              builder: (context, setState) {
                return ChatAgentOpenSessionCardView(
                  card: card,
                  isMine: false,
                  fontScale: 1,
                  onSubmit: (_) async =>
                      const ChatMessageCardActionResult.submitted(),
                  key: const ValueKey('test-card'),
                );
              },
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      // Card should recover: input form visible again.
      expect(
        find.byKey(const Key('chat_message_card_agent_open_session_input')),
        findsOneWidget,
        reason: 'Input form should reappear after backend retry card (normal timing)',
      );
      expect(
        // zh_CN 环境下后端英文模板被本地化。
        find.text('Claude 工作目录路径无效。'),
        findsOneWidget,
        reason: 'Backend error message should be visible',
      );
    },
  );

  testWidgets(
    'selected directory survives card state recreation before submit',
    (WidgetTester tester) async {
      const card = ChatAgentOpenSessionCardData(
        cardInstanceId: 'card-open-draft-1',
        summaryText: 'open 缺少目录路径。',
      );

      Widget buildCard() => GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: ChatAgentOpenSessionCardView(
            card: card,
            isMine: false,
            fontScale: 1,
            platform: TargetPlatform.windows,
            pickRemoteDirectory: () async => r'C:\work\demo',
            onSubmit: (_) async =>
                const ChatMessageCardActionResult.submitted(),
          ),
        ),
      );

      // 选中目录但不提交。
      await tester.pumpWidget(buildCard());
      await tester.pumpAndSettle();
      await tester.tap(
        find.byKey(const Key('chat_message_card_agent_open_session_browse')),
      );
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<TextField>(
              find.byKey(
                const Key('chat_message_card_agent_open_session_input'),
              ),
            )
            .controller
            ?.text,
        r'C:\work\demo',
      );

      // 用一个不同的 widget 强制销毁卡片 State（模拟在屏内重建/列表刷新），
      // 再挂回同实例卡片。修复前路径会丢，修复后应从草稿恢复。
      await tester.pumpWidget(
        const MaterialApp(home: Scaffold(body: SizedBox.shrink())),
      );
      await tester.pumpAndSettle();
      await tester.pumpWidget(buildCard());
      await tester.pumpAndSettle();

      expect(
        tester
            .widget<TextField>(
              find.byKey(
                const Key('chat_message_card_agent_open_session_input'),
              ),
            )
            .controller
            ?.text,
        r'C:\work\demo',
        reason: '未提交的目录路径应在卡片 State 重建后从草稿恢复',
      );

      // 清理草稿，避免污染其他用例。
      ChatOpenSessionDraftStore.clear('card-open-draft-1');
    },
  );
}
