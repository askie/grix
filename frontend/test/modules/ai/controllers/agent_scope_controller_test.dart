import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/modules/ai/controllers/agent_scope_controller.dart';

class _FakeAgentService extends AgentService {
  ServiceResult<AgentScopeConfig> getScopesResult =
      ServiceResult<AgentScopeConfig>.success(data: const AgentScopeConfig());
  ServiceResult<AgentScopeConfig> replaceScopesResult =
      ServiceResult<AgentScopeConfig>.success(data: const AgentScopeConfig());
  String? getAgentScopesAgentId;
  String? replaceScopesAgentId;
  List<String>? replaceScopesPayload;

  @override
  Future<ServiceResult<AgentScopeConfig>> getAgentScopes(String agentId) async {
    getAgentScopesAgentId = agentId;
    return getScopesResult;
  }

  @override
  Future<ServiceResult<AgentScopeConfig>> replaceAgentScopes(
    String agentId,
    List<String> scopes,
  ) async {
    replaceScopesAgentId = agentId;
    replaceScopesPayload = List<String>.from(scopes);
    return replaceScopesResult;
  }
}

void main() {
  setUp(() {
    Get.reset();
    Get.testMode = true;
    Get.addTranslations(AppTranslations().keys);
    Get.locale = const Locale('zh', 'CN');
    Get.fallbackLocale = const Locale('en', 'US');
  });

  tearDown(() {
    Get.reset();
  });

  AgentScopeController buildController(
    _FakeAgentService service, {
    AgentScopeToastShower? showToast,
  }) {
    Get.put<AgentService>(service);
    final controller = AgentScopeController(showToast: showToast);
    controller.agentId.value = '9992';
    controller.providerType.value = 3;
    return controller;
  }

  test(
    'loadScopes uses backend returned scopes and available_scopes',
    () async {
      final service = _FakeAgentService()
        ..getScopesResult = ServiceResult<AgentScopeConfig>.success(
          data: const AgentScopeConfig(
            scopes: ['group.member.add'],
            availableScopes: ['group.create', 'group.member.add'],
          ),
        );
      final controller = buildController(service);

      await controller.loadScopes();

      expect(service.getAgentScopesAgentId, '9992');
      expect(controller.selectedScopes, ['group.member.add']);
      expect(controller.availableScopes, ['group.create', 'group.member.add']);
      expect(controller.scopeOptions.map((item) => item.scope), [
        'group.create',
        'group.member.add',
      ]);
    },
  );

  test('scopeOptions keeps selected unknown scope visible', () async {
    final service = _FakeAgentService()
      ..getScopesResult = ServiceResult<AgentScopeConfig>.success(
        data: const AgentScopeConfig(
          scopes: ['group.member.add', 'group.unknown.custom'],
          availableScopes: ['group.member.add'],
        ),
      );
    final controller = buildController(service);

    await controller.loadScopes();

    expect(controller.selectedScopes, [
      'group.member.add',
      'group.unknown.custom',
    ]);
    expect(controller.scopeOptions.map((item) => item.scope), [
      'group.member.add',
      'group.unknown.custom',
    ]);
  });

  test('scopeOptions uses backend text for unknown scopes', () async {
    final service = _FakeAgentService()
      ..getScopesResult = ServiceResult<AgentScopeConfig>.success(
        data: const AgentScopeConfig(
          scopes: ['future.scope'],
          availableScopes: ['future.scope'],
          availableScopeItems: [
            AgentScopeItem(
              scope: 'future.scope',
              label: '未来权限',
              description: '服务端下发的新权限说明。',
            ),
          ],
        ),
      );
    final controller = buildController(service);

    await controller.loadScopes();

    expect(controller.scopeOptions.single.label, '未来权限');
    expect(controller.scopeOptions.single.description, '服务端下发的新权限说明。');
  });

  test('scopeOptions translates session and contact search scopes', () async {
    final service = _FakeAgentService()
      ..getScopesResult = ServiceResult<AgentScopeConfig>.success(
        data: const AgentScopeConfig(
          scopes: ['session.search'],
          availableScopes: ['session.search', 'contact.search'],
        ),
      );
    final controller = buildController(service);

    await controller.loadScopes();

    expect(controller.scopeOptions.map((item) => item.label), [
      '搜索会话',
      '搜索联系人',
    ]);
    expect(controller.scopeOptions.map((item) => item.description), [
      '允许搜索会话。',
      '允许搜索联系人。',
    ]);
  });

  test('scopeOptions translates agent category scopes', () async {
    final service = _FakeAgentService()
      ..getScopesResult = ServiceResult<AgentScopeConfig>.success(
        data: const AgentScopeConfig(
          scopes: ['agent.category.assign'],
          availableScopes: [
            'agent.category.list',
            'agent.category.create',
            'agent.category.update',
            'agent.category.assign',
          ],
        ),
      );
    final controller = buildController(service);

    await controller.loadScopes();

    expect(controller.scopeOptions.map((item) => item.label), [
      '查看 Agent 分类',
      '创建 Agent 分类',
      '修改 Agent 分类',
      '设置 Agent 分类',
    ]);
    expect(controller.scopeOptions.map((item) => item.description), [
      '允许查看当前账号下的 Agent 分类列表。',
      '允许创建新的 Agent 分类。',
      '允许修改已有 Agent 分类。',
      '允许为 Agent 设置或清空分类。',
    ]);
  });

  test('selectAll and clearScopes follow available scopes', () {
    final controller = buildController(_FakeAgentService());
    controller.availableScopes.value = ['group.create', 'group.member.add'];

    controller.selectAllScopes();
    expect(controller.selectedScopes, ['group.create', 'group.member.add']);

    controller.clearScopes();
    expect(controller.selectedScopes, isEmpty);
  });

  test(
    'selectAll does not use local fallback when available list is empty',
    () {
      final controller = buildController(_FakeAgentService());
      controller.selectedScopes.value = ['group.member.add'];

      controller.selectAllScopes();

      expect(controller.selectedScopes, isEmpty);
    },
  );

  test(
    'saveScopes sends payload and refreshes local state from response',
    () async {
      final service = _FakeAgentService()
        ..replaceScopesResult = ServiceResult<AgentScopeConfig>.success(
          data: const AgentScopeConfig(
            scopes: ['group.create'],
            availableScopes: ['group.create', 'group.member.add'],
          ),
        );
      String? toastMessage;
      bool? toastIsError;
      final controllerWithToast = buildController(
        service,
        showToast: (message, {isError = true}) {
          toastMessage = message;
          toastIsError = isError;
        },
      );
      controllerWithToast.selectedScopes.value = ['group.member.add'];

      await controllerWithToast.saveScopes();

      expect(service.replaceScopesAgentId, '9992');
      expect(service.replaceScopesPayload, ['group.member.add']);
      expect(controllerWithToast.selectedScopes, ['group.create']);
      expect(controllerWithToast.availableScopes, [
        'group.create',
        'group.member.add',
      ]);
      expect(toastMessage, 'ai_agent_scope_save_success'.tr);
      expect(toastIsError, isFalse);
    },
  );
}
