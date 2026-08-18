import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/agent_toolbar_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/chat/chat_view.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Future<ImService> pumpSkillSheet(
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
    return imService;
  }

  testWidgets('session skill list shows switches only when opted in', (
    tester,
  ) async {
    await pumpSkillSheet(tester, showToggles: true);
    expect(find.byType(Switch), findsOneWidget);
  });

  testWidgets(
    'session skill switch unlocks immediately when the action cannot be sent',
    (tester) async {
      await pumpSkillSheet(tester, showToggles: true);
      await tester.tap(find.byType(Switch));
      await tester.pump();
      expect(find.byType(CircularProgressIndicator), findsNothing);
      expect(find.byType(Switch), findsOneWidget);
      await tester.pump(const Duration(seconds: 3));
    },
  );

  testWidgets('ordinary agent skill list keeps the command-only UI', (
    tester,
  ) async {
    await pumpSkillSheet(tester, showToggles: false);
    expect(find.byType(Switch), findsNothing);
  });

  testWidgets('skill list groups by scope with counts and copies path', (
    tester,
  ) async {
    final item = AgentToolbarItemModel.fromJson({
      'item_id': 'skills',
      'group_id': 'skills',
      'kind': 'button',
      'action_id': 'skills',
      'local_action': 'client:command_list',
      'commands': [
        {
          'id': 'proj-skill',
          'name': 'proj-skill',
          'description': '',
          'exec': 'proj-skill',
          'source': 'project',
          'path': '.dsh/skills/proj-skill/SKILL.md',
        },
        {
          'id': 'user-skill',
          'name': 'user-skill',
          'description': '',
          'exec': 'user-skill',
          'source': 'global',
          'path': '~/.dsh/skills/user-skill/SKILL.md',
        },
        {
          'id': 'conn-skill',
          'name': 'conn-skill',
          'description': '',
          'exec': 'conn-skill',
          'source': 'connector',
          'path': '~/.grix/skills/conn-skill/SKILL.md',
        },
      ],
    });

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
              showSkillLibrary: true,
              toolbarItem: item,
            ),
            child: const Text('open'),
          ),
        ),
      ),
    );
    final platformCalls = <MethodCall>[];
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        platformCalls.add(call);
        return null;
      },
    );
    addTearDown(
      () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      ),
    );

    await tester.tap(find.text('open'));
    // 技能库 Tab 带常驻动画，pumpAndSettle 不会收敛，这里显式推进到弹窗打开。
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    // 项目组在前、公共组在后，标题各带本组技能数量。
    expect(find.textContaining('(1)'), findsOneWidget);
    expect(find.textContaining('(2)'), findsOneWidget);
    expect(find.text('.dsh/skills/proj-skill/SKILL.md'), findsOneWidget);

    await tester.tap(find.text('~/.dsh/skills/user-skill/SKILL.md'));
    await tester.pump();
    final copied = platformCalls.lastWhere(
      (call) => call.method == 'Clipboard.setData',
    );
    expect(
      (copied.arguments as Map)['text'],
      '~/.dsh/skills/user-skill/SKILL.md',
    );
    // 复制成功会弹一次 toast，等它的定时器跑完，避免 pending timer 断言。
    await tester.pump(const Duration(seconds: 5));
  }, timeout: const Timeout(Duration(seconds: 60)));

  testWidgets('slash command list stays flat without scope headers', (
    tester,
  ) async {
    final item = AgentToolbarItemModel.fromJson({
      'item_id': 'slash_commands',
      'group_id': 'slash_commands',
      'kind': 'button',
      'action_id': 'slash_commands',
      'local_action': 'client:command_list',
      'commands': [
        {
          'id': '/help',
          'name': '/help',
          'description': '',
          'exec': '/help',
          'source': 'global',
        },
      ],
    });

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
              toolbarItem: item,
            ),
            child: const Text('open'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    expect(find.textContaining('(1)'), findsNothing);
    expect(find.text('/help'), findsOneWidget);
  }, timeout: const Timeout(Duration(seconds: 60)));

  testWidgets('open skill sheet removes switches after capability downgrade', (
    tester,
  ) async {
    final imService = await pumpSkillSheet(tester, showToggles: true);
    expect(find.byType(Switch), findsOneWidget);

    imService.agentToolbars['session-1'] = AgentToolbarModel.fromJson({
      'session_id': 'session-1',
      'agent_id': '42',
      'toolbar_id': 'toolbar-1',
      'revision': 2,
      'visible': true,
      'updated_at': 2,
      'items': [
        {
          'item_id': 'skills',
          'group_id': 'skills',
          'kind': 'button',
          'action_id': 'skills',
          'local_action': 'client:command_list',
          'show_toggles': false,
          'commands': [
            {
              'id': 'message-send',
              'name': 'message-send',
              'description': 'Send a message',
              'exec': 'message-send',
            },
          ],
        },
      ],
    });
    await tester.pump();
    await tester.pump();
    expect(find.byType(Switch), findsNothing);
  });
}
