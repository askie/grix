import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/modules/chat/private_chat_creating_route.dart';
import 'package:grix/modules/chat/private_chat_creating_status.dart';
import 'package:grix/modules/chat/private_chat_creating_view.dart';
import 'package:grix/modules/chat/services/chat_route_navigator.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(Get.reset);

  testWidgets('shows a usable composer and keeps text in creation draft', (
    tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: _CreatingTestTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('en', 'US'),
        home: const Scaffold(body: Text('source')),
      ),
    );
    final draft = PrivateChatCreationDraft();
    final routeFuture = Get.to<void>(
      () => const PrivateChatCreatingView(),
      arguments: <String, dynamic>{
        'title': 'OpenCode',
        'creation_draft': draft,
      },
      transition: Transition.noTransition,
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 1));

    expect(find.text('OpenCode'), findsOneWidget);
    expect(
      find.byKey(const Key('private_chat_creating_status')),
      findsOneWidget,
    );
    expect(find.textContaining('创建中'), findsOneWidget);
    expect(find.byType(LinearProgressIndicator), findsNothing);
    expect(find.byType(CircularProgressIndicator), findsNothing);

    await tester.enterText(
      find.byKey(const Key('private_chat_creating_input')),
      '先写好这条消息',
    );
    expect(draft.text, '先写好这条消息');

    Get.back<void>();
    await tester.pump();
    await routeFuture;
    expect(draft.text, '先写好这条消息');
  });

  testWidgets('creating status ellipsis advances across frames', (
    tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: _CreatingTestTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const PrivateChatCreatingView(),
      ),
    );
    await tester.pump();

    final state = tester.state<PrivateChatCreatingStatusState>(
      find.byType(PrivateChatCreatingStatus),
    );
    final first = state.label;

    await tester.pump(const Duration(milliseconds: 500));

    expect(
      state.label,
      isNot(first),
      reason: 'wall-clock ellipsis must advance across frames',
    );
    expect(find.textContaining('创建中'), findsOneWidget);
  });

  testWidgets('creating status advances when an ancestor disables tickers', (
    tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: _CreatingTestTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const TickerMode(
          enabled: false,
          child: PrivateChatCreatingView(),
        ),
      ),
    );
    await tester.pump();

    final state = tester.state<PrivateChatCreatingStatusState>(
      find.byType(PrivateChatCreatingStatus),
    );
    final first = state.label;

    await tester.pump(const Duration(milliseconds: 500));

    expect(
      state.label,
      isNot(first),
      reason: 'status must keep moving when route/overlay TickerMode is muted',
    );
  });

  test('creating route disables snapshotting and transition window', () {
    final route = PrivateChatCreatingRoute(
      arguments: const <String, dynamic>{'title': 't'},
    );

    expect(route.settings.name, AppRoutes.privateChatCreating);
    expect(route.allowSnapshotting, isFalse);
    expect(route.transitionDuration, Duration.zero);
    expect(route.reverseTransitionDuration, Duration.zero);
    expect(route.opaque, isTrue);

    final named = AppRoutes.routes.singleWhere(
      (candidate) => candidate.name == AppRoutes.privateChatCreating,
    );
    expect(named.transition, Transition.noTransition);
    expect(named.transitionDuration, Duration.zero);
    expect(named.opaque, isTrue);
    expect(named.showCupertinoParallax, isFalse);
  });

  test('creating shell is replaced by chat without a second transition', () {
    final route = ChatRouteNavigator.buildCreatingToChatReplacementRoute<void>(
      arguments: const <String, dynamic>{
        'session_id': 'session-1',
        'title': 'OpenCode',
        'type': 'private',
      },
      parameters: const <String, String>{
        'session_id': 'session-1',
        'title': 'OpenCode',
        'type': 'private',
      },
    );

    expect(route.transition, Transition.noTransition);
    expect(route.transitionDuration, Duration.zero);
    expect(route.showCupertinoParallax, isFalse);
    expect(route.opaque, isFalse);
    expect(route.settings.name, contains('/chat?'));
    expect(route.parameter?['session_id'], 'session-1');
  });
}

class _CreatingTestTranslations extends Translations {
  @override
  Map<String, Map<String, String>> get keys => const {
    'zh_CN': {
      'chat_send_placeholder': '输入消息...',
      'chat_creating_session': '创建中',
    },
    'en_US': {
      'chat_send_placeholder': 'Type a message...',
      'chat_creating_session': 'Creating',
    },
  };
}
