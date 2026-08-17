import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/agent_toolbar_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/chat/chat_view.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<void> pumpSkillSheet(
    WidgetTester tester, {
    required bool showToggles,
  }) async {
    final item = AgentToolbarItemModel.fromJson({
      'item_id': 'skills',
      'group_id': 'skills',
      'kind': 'button',
      'action_id': showToggles ? 'dsh_skills' : 'skills',
      'local_action': 'client:command_list',
      'show_toggles': showToggles,
      'commands': [
        {
          'id': 'message-send',
          'name': 'message-send',
          'description': 'Send a message',
          'exec': 'message-send',
        },
      ],
      'toggles': [
        {'id': 'message-send', 'name': 'message-send', 'enabled': true},
      ],
    });
    final toolbar = AgentToolbarModel.fromJson({
      'session_id': 'session-1',
      'agent_id': '42',
      'toolbar_id': 'toolbar-1',
      'revision': 1,
      'visible': true,
      'updated_at': 1,
      'items': [],
    });
    final imService = ImService();
    imService.agentToolbars['session-1'] = toolbar;

    await tester.pumpWidget(
      GetMaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () => showChatCommandListSheet(
              context,
              title: '',
              commands: item.commands,
              onSelected: (_) {},
              commandListItemId: item.itemId,
              sessionId: 'session-1',
              imService: imService,
              toolbarItem: item,
              toolbar: toolbar,
            ),
            child: const Text('open'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
  }

  testWidgets('session skill list shows switches only when opted in', (
    tester,
  ) async {
    await pumpSkillSheet(tester, showToggles: true);
    expect(find.byType(Switch), findsOneWidget);
  });

  testWidgets(
    'session skill switch stays pending through runtime rebuild window',
    (tester) async {
      await pumpSkillSheet(tester, showToggles: true);
      await tester.tap(find.byType(Switch));
      await tester.pump();
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      await tester.pump(const Duration(seconds: 21));
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      await tester.pump(const Duration(seconds: 54));
      expect(find.byType(CircularProgressIndicator), findsNothing);
      expect(find.byType(Switch), findsOneWidget);
    },
  );

  testWidgets('ordinary agent skill list keeps the command-only UI', (
    tester,
  ) async {
    await pumpSkillSheet(tester, showToggles: false);
    expect(find.byType(Switch), findsNothing);
  });
}
