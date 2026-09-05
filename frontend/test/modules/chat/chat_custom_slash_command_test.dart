import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/agent_toolbar_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/modules/ai/models/agent_slash_command_model.dart';
import 'package:grix/modules/chat/chat_view.dart';

/// 只替换自定义命令相关的三个 REST 方法，其余沿用真实实现（测试里不会走到）。
class _FakeAgentService extends AgentService {
  final List<String> deletedCommandIds = [];
  List<AgentSlashCommandEntry> listed = const [
    AgentSlashCommandEntry(
      id: '7001',
      name: '/deploy',
      description: '发布到预发环境',
    ),
  ];

  @override
  Future<ServiceResult<List<AgentSlashCommandEntry>>> getAgentSlashCommands(
    String agentId,
  ) async {
    return ServiceResult<List<AgentSlashCommandEntry>>.success(data: listed);
  }

  final List<String> createdCommandNames = [];

  @override
  Future<ServiceResult<AgentSlashCommandEntry>> createAgentSlashCommand(
    String agentId, {
    required String name,
    String description = '',
  }) async {
    createdCommandNames.add(name);
    return ServiceResult<AgentSlashCommandEntry>.success(
      data: AgentSlashCommandEntry(
        id: '7002',
        name: name,
        description: description,
      ),
    );
  }

  @override
  Future<ServiceResult<void>> deleteAgentSlashCommand(
    String agentId,
    String commandId,
  ) async {
    deletedCommandIds.add(commandId);
    return ServiceResult<void>.success();
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const commands = [
    CommandItemModel(
      id: '/clear',
      name: '/clear',
      description: '清空上下文',
      exec: '/clear',
    ),
    CommandItemModel(
      id: '/deploy',
      name: '/deploy',
      description: '发布到预发环境',
      exec: '/deploy',
      source: 'custom',
    ),
  ];

  late _FakeAgentService agentService;

  void register({required bool ownsAgent}) {
    agentService = _FakeAgentService();
    agentService.hasLoaded.value = true;
    if (ownsAgent) {
      agentService.agents.value = [
        AgentModel(id: 'agent-1', agentName: 'demo'),
      ];
    }
    Get.put<AgentService>(agentService);
  }

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(Get.reset);

  Future<void> pumpSheet(
    WidgetTester tester, {
    String commandListItemId = 'slash_commands',
  }) async {
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.lightTheme,
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              onPressed: () => showChatCommandListSheet(
                context,
                title: '',
                commands: commands,
                commandListItemId: commandListItemId,
                agentId: 'agent-1',
                sessionId: 'session-1',
                onSelected: (_) {},
              ),
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
  }

  testWidgets('主人打开斜杠命令面板：自定义行带标签和删除按钮，底部有添加入口', (tester) async {
    register(ownsAgent: true);
    await pumpSheet(tester);

    expect(find.text('/deploy'), findsOneWidget);
    expect(find.text('自定义'), findsOneWidget);
    expect(
      find.byKey(const Key('chat_slash_command_delete_/deploy')),
      findsOneWidget,
    );
    expect(
      find.byKey(const Key('chat_slash_command_add_entry')),
      findsOneWidget,
    );
    // 内置命令不带自定义标记。
    expect(find.text('/clear'), findsOneWidget);
  });

  testWidgets('非主人：没有添加入口，也没有删除按钮', (tester) async {
    register(ownsAgent: false);
    await pumpSheet(tester);

    expect(find.text('/deploy'), findsOneWidget);
    expect(find.text('自定义'), findsOneWidget);
    expect(
      find.byKey(const Key('chat_slash_command_delete_/deploy')),
      findsNothing,
    );
    expect(find.byKey(const Key('chat_slash_command_add_entry')), findsNothing);
  });

  testWidgets('技能面板不受影响：不显示自定义入口', (tester) async {
    register(ownsAgent: true);
    await pumpSheet(tester, commandListItemId: 'skills');

    expect(find.byKey(const Key('chat_slash_command_add_entry')), findsNothing);
    expect(find.text('自定义'), findsNothing);
  });

  testWidgets('添加弹窗先在本地校验命令名，通过后写入并补一行', (tester) async {
    register(ownsAgent: true);
    await pumpSheet(tester);

    await tester.tap(find.byKey(const Key('chat_slash_command_add_entry')));
    await tester.pumpAndSettle();

    // 缺前导斜杠：本地就拦下，不发请求。
    await tester.enterText(
      find.byKey(const Key('chat_slash_command_name_field')),
      'rollback',
    );
    await tester.tap(find.byKey(const Key('chat_slash_command_submit')));
    await tester.pumpAndSettle();
    expect(agentService.createdCommandNames, isEmpty);
    expect(
      find.text('命令名需以 / 开头，只能用小写字母、数字和 _ : -，最长 32 位'),
      findsOneWidget,
    );

    await tester.enterText(
      find.byKey(const Key('chat_slash_command_name_field')),
      '/Rollback',
    );
    await tester.enterText(
      find.byKey(const Key('chat_slash_command_description_field')),
      '回滚上一次发布',
    );
    await tester.tap(find.byKey(const Key('chat_slash_command_submit')));
    await tester.pumpAndSettle();

    expect(agentService.createdCommandNames, ['/rollback']);
    expect(find.text('/rollback'), findsOneWidget);
    expect(find.text('自定义'), findsNWidgets(2));

    await tester.pump(const Duration(seconds: 5));
  });

  testWidgets('确认删除后调用删除接口，并把该行从面板里去掉', (tester) async {
    register(ownsAgent: true);
    await pumpSheet(tester);

    await tester.tap(
      find.byKey(const Key('chat_slash_command_delete_/deploy')),
    );
    await tester.pumpAndSettle();
    expect(find.text('删除自定义命令 /deploy？'), findsOneWidget);

    await tester.tap(find.widgetWithText(TextButton, '删除'));
    await tester.pumpAndSettle();

    expect(agentService.deletedCommandIds, ['7001']);
    expect(find.text('/deploy'), findsNothing);
    expect(find.text('/clear'), findsOneWidget);

    // 成功提示是带 3s 自动消失定时器的全局 Toast，跑完再结束测试。
    await tester.pump(const Duration(seconds: 5));
  });
}
