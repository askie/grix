import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/agent_toolbar_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/chat/chat_view.dart';

/// 删除技能后必须用连接器重扫快照重建列表，而不是只在本地抹掉一行：
/// 否则「已启用」和「技能库」两个 Tab 会各说各话。
class _DeleteFakeImService extends ImService {
  _DeleteFakeImService({this.refreshError});

  final Object? refreshError;
  final List<String> deleted = <String>[];
  int refreshCalls = 0;

  @override
  Future<void> requestSkillDelete({
    required String agentId,
    required String sessionId,
    required String name,
  }) async {
    deleted.add(name);
  }

  @override
  Future<AgentToolbarModel> requestSkillRefresh({
    required String agentId,
    required String sessionId,
  }) async {
    refreshCalls++;
    if (refreshError != null) throw refreshError!;
    return AgentToolbarModel.fromJson({
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
          'commands': [
            {
              'id': 'keep-skill',
              'name': 'keep-skill',
              'description': '',
              'exec': 'keep-skill',
              'source': 'global',
              'path': '~/.dsh/skills/keep-skill/SKILL.md',
            },
          ],
        },
      ],
    });
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  AgentToolbarItemModel skillsItem() => AgentToolbarItemModel.fromJson({
    'item_id': 'skills',
    'group_id': 'skills',
    'kind': 'button',
    'action_id': 'skills',
    'local_action': 'client:command_list',
    'commands': [
      {
        'id': 'doomed-skill',
        'name': 'doomed-skill',
        'description': '',
        'exec': 'doomed-skill',
        'source': 'global',
        'path': '~/.dsh/skills/doomed-skill/SKILL.md',
        'sync_state': 'synced',
      },
      {
        'id': 'keep-skill',
        'name': 'keep-skill',
        'description': '',
        'exec': 'keep-skill',
        'source': 'global',
        'path': '~/.dsh/skills/keep-skill/SKILL.md',
        'sync_state': 'synced',
      },
    ],
  });

  Future<void> openSheet(WidgetTester tester, ImService imService) async {
    final item = skillsItem();
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
              agentId: '42',
              sessionId: 'session-1',
              imService: imService,
              showSkillLibrary: true,
              toolbarItem: item,
            ),
            child: const Text('open'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    // 技能库 Tab 带常驻动画，pumpAndSettle 不会收敛，显式推进到弹窗打开。
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
  }

  Future<void> confirmDelete(WidgetTester tester) async {
    await tester.tap(find.byIcon(Icons.delete_outline).first);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    await tester.tap(find.text('chat_skill_delete_confirm'.tr));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
  }

  testWidgets(
    'deleting a skill reloads the list from the connector',
    (tester) async {
      final imService = _DeleteFakeImService();
      await openSheet(tester, imService);
      expect(find.text('doomed-skill'), findsOneWidget);

      await confirmDelete(tester);

      expect(imService.deleted, ['doomed-skill']);
      expect(imService.refreshCalls, 1);
      // 重扫快照里只剩 keep-skill，被删的那条从列表消失。
      expect(find.text('doomed-skill'), findsNothing);
      expect(find.text('keep-skill'), findsOneWidget);
      await tester.pump(const Duration(seconds: 5));
    },
    timeout: const Timeout(Duration(seconds: 60)),
  );

  testWidgets(
    'refresh failure after delete does not report delete failure',
    (tester) async {
      final imService = _DeleteFakeImService(
        refreshError: Exception('offline'),
      );
      await openSheet(tester, imService);

      await confirmDelete(tester);

      expect(imService.deleted, ['doomed-skill']);
      expect(imService.refreshCalls, 1);
      // 磁盘已删干净：刷新失败只提示刷新失败，并降级为本地移除该行。
      expect(find.textContaining('chat_skill_delete_failed'), findsNothing);
      expect(find.text('doomed-skill'), findsNothing);
      expect(find.text('keep-skill'), findsOneWidget);
      await tester.pump(const Duration(seconds: 5));
    },
    timeout: const Timeout(Duration(seconds: 60)),
  );
}
