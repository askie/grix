import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/modules/chat/private_chat_creating_status.dart';
import 'package:grix/modules/chat/private_chat_creating_view.dart';
import 'package:grix/modules/chat/services/chat_route_navigator.dart';

/// Holds the creating shell open (≥3s of fake time) and asserts the ellipsis
/// label changes — the local repro gate from the zero-base plan.
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    ChatRouteNavigator.debugHoldBeforeCreateSession = Duration.zero;
  });

  tearDown(() {
    ChatRouteNavigator.debugHoldBeforeCreateSession = Duration.zero;
    Get.reset();
  });

  testWidgets('creating status ellipsis advances while shell is held open', (
    tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: _HoldTestTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const PrivateChatCreatingView(),
      ),
    );
    await tester.pump();

    expect(find.textContaining('创建中'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);

    final state = tester.state<PrivateChatCreatingStatusState>(
      find.byType(PrivateChatCreatingStatus),
    );
    final labels = <String>{state.label};

    // Simulate a slow createSession window (≥3s).
    for (var i = 0; i < 8; i++) {
      await tester.pump(const Duration(milliseconds: 400));
      labels.add(state.label);
    }

    expect(
      labels.length,
      greaterThan(1),
      reason: 'ellipsis must change while the creating shell stays open',
    );
    expect(
      find.byKey(const Key('private_chat_creating_status')),
      findsOneWidget,
    );
  });
}

class _HoldTestTranslations extends Translations {
  @override
  Map<String, Map<String, String>> get keys => const {
    'zh_CN': {
      'chat_send_placeholder': '输入消息...',
      'chat_creating_session': '创建中',
    },
  };
}
