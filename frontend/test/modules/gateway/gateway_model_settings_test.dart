import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/gateway_service.dart';
import 'package:grix/modules/gateway/gateway_agents_relay_view.dart';
import 'package:grix/modules/gateway/gateway_model_settings_view.dart';

class _FakeAuthService extends AuthService {
  @override
  void attachAuthInterceptor(Dio dio) {}
}

/// 移动端「模型设置」测试专用 GatewayService：模型清单/兜底设置/Agent 列表可控，
/// PUT relay-settings 与 POST relay 的入参全部记录供断言，不走真实 HTTP。
class _FakeGatewayService extends GatewayService {
  List<GatewayModelModel> modelsToReturn = const [
    GatewayModelModel(
      provider: 'deepseek',
      model: 'deepseek-v4-flash',
      inputPricePerM: '0.07',
      outputPricePerM: '0.11',
    ),
    GatewayModelModel(
      provider: 'deepseek',
      model: 'deepseek-v4-pro',
      inputPricePerM: '0.28',
      outputPricePerM: '0.42',
    ),
  ];

  GatewayRelaySettingsModel? settingsToReturn = const GatewayRelaySettingsModel(
    defaultModel: 'deepseek-v4-flash',
    modelMap: {'claude-opus-4-8': 'deepseek-v4-pro'},
  );

  List<GatewayAgentRelayStateModel> agentsToReturn = const [];
  int listAgentCalls = 0;

  final putRelaySettingsCalls = <Map<String, Object?>>[];
  bool putRelaySettingsResult = true;

  final setRelayCalls = <Map<String, Object?>>[];
  GatewaySetAgentRelayResult setRelayResult = const GatewaySetAgentRelayResult(
    GatewaySetRelayStatus.failed,
  );

  /// 需要按调用次序返回不同结果（如 needModel 后重试成功）时压入队列，优先于
  /// [setRelayResult] 按序消费。
  final setRelayResultQueue = <GatewaySetAgentRelayResult>[];

  /// 模拟服务端落库：setAgentRelay 成功后，listAgents 按写入的 model 回显
  /// （审查意见：换模型用例要断言刷新后列表显示新模型，而不是只看 POST 参数）。
  final _appliedModels = <String, String>{};

  @override
  Future<List<GatewayModelModel>> listModels() async => modelsToReturn;

  @override
  Future<GatewayRelaySettingsModel?> getRelaySettings() async =>
      settingsToReturn;

  @override
  Future<bool> putRelaySettings({
    required String defaultModel,
    required Map<String, String> modelMap,
  }) async {
    putRelaySettingsCalls.add({
      'defaultModel': defaultModel,
      'modelMap': modelMap,
    });
    return putRelaySettingsResult;
  }

  @override
  Future<List<GatewayAgentRelayStateModel>> listAgents() async {
    listAgentCalls++;
    return agentsToReturn.map((a) {
      final appliedModel = _appliedModels[a.agentId];
      if (appliedModel == null) return a;
      return GatewayAgentRelayStateModel(
        agentId: a.agentId,
        agentName: a.agentName,
        clientType: a.clientType,
        supported: a.supported,
        configured: a.configured,
        relayModel: appliedModel,
        enabled: a.enabled,
        applied: a.applied,
        appliedAt: a.appliedAt,
        stateKnown: a.stateKnown,
      );
    }).toList();
  }

  @override
  Future<GatewaySetAgentRelayResult> setAgentRelay(
    String agentId, {
    required bool enabled,
    String? model,
    int? expectedRevision,
  }) async {
    setRelayCalls.add({
      'agentId': agentId,
      'enabled': enabled,
      'model': model,
      'expectedRevision': expectedRevision,
    });
    final result = setRelayResultQueue.isNotEmpty
        ? setRelayResultQueue.removeAt(0)
        : setRelayResult;
    if (result.status == GatewaySetRelayStatus.ok &&
        model != null &&
        model.isNotEmpty) {
      _appliedModels[agentId] = model;
    }
    return result;
  }
}

void main() {
  late _FakeGatewayService service;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    // GatewayService.onInit 会向 AuthService 注册鉴权拦截器，测试环境同样要提供。
    Get.put<AuthService>(_FakeAuthService());
    service = _FakeGatewayService();
    Get.put<GatewayService>(service);
  });

  tearDown(() {
    Get.reset();
  });

  Future<void> pumpPage(WidgetTester tester, Widget page) async {
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.lightTheme,
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('zh', 'CN'),
        home: page,
      ),
    );
    await tester.pumpAndSettle();
  }

  GatewayAgentRelayStateModel agent({
    required String id,
    required String name,
    String clientType = 'claude',
    String relayModel = '',
    bool? enabled,
    bool? applied,
    bool? stateKnown,
  }) {
    return GatewayAgentRelayStateModel(
      agentId: id,
      agentName: name,
      clientType: clientType,
      supported: true,
      configured: enabled == true,
      relayModel: relayModel,
      enabled: enabled,
      applied: applied,
      stateKnown: stateKnown,
    );
  }

  group('模型设置主页', () {
    testWidgets('两组入口 + 副标题回显，页面树无任何充值/余额文案', (tester) async {
      service.agentsToReturn = [
        agent(
          id: '1',
          name: 'Claude',
          enabled: true,
          applied: true,
          stateKnown: true,
        ),
        agent(
          id: '2',
          name: 'Codex',
          enabled: false,
          applied: false,
          stateKnown: true,
        ),
        // 不支持中转的类型不计入"共 Y 个"。
        const GatewayAgentRelayStateModel(
          agentId: '3',
          agentName: 'Gemini',
          clientType: 'gemini',
          supported: false,
          configured: false,
        ),
      ];

      await pumpPage(tester, const GatewayModelSettingsView());

      expect(find.text('模型设置'), findsOneWidget);
      expect(find.text('默认模型'), findsOneWidget);
      // 副标题 = 当前模型（价格已隐藏，72bdf7e2）。
      expect(find.textContaining('deepseek-v4-flash'), findsOneWidget);
      expect(find.textContaining('输入 \$0.07 / 输出 \$0.11'), findsNothing);
      expect(find.text('Agent 模型设置'), findsNWidgets(2)); // 组标题 + 入口行
      expect(find.text('已开启 1 / 共 2 个'), findsOneWidget);

      // 合规红线（设计 §3.0）：无充值/余额/流水类文案与按钮。
      expect(find.textContaining('充值'), findsNothing);
      expect(find.textContaining('余额'), findsNothing);
      expect(find.textContaining('消费流水'), findsNothing);
      expect(find.textContaining('Top Up', skipOffstage: false), findsNothing);
    });

    testWidgets('默认模型点选即保存：PUT 整体保存且保留现有 model_map', (tester) async {
      await pumpPage(tester, const GatewayModelSettingsView());

      await tester.tap(find.text('默认模型'));
      await tester.pumpAndSettle();
      expect(find.text('deepseek-v4-pro'), findsOneWidget);

      await tester.tap(find.text('deepseek-v4-pro'));
      await tester.pumpAndSettle();

      expect(service.putRelaySettingsCalls.length, 1);
      expect(
        service.putRelaySettingsCalls.first['defaultModel'],
        'deepseek-v4-pro',
      );
      // 关键断言：整体保存必须原样带上已有映射表。
      expect(service.putRelaySettingsCalls.first['modelMap'], {
        'claude-opus-4-8': 'deepseek-v4-pro',
      });
      expect(find.textContaining('已保存'), findsOneWidget);
      // 保存成功后返回主页。
      expect(find.text('Agent 模型设置'), findsNWidgets(2));

      await tester.pump(const Duration(seconds: 4));
    });

    testWidgets('保存失败：toast 提示并还原选中态，停留在选择页', (tester) async {
      service.putRelaySettingsResult = false;

      await pumpPage(tester, const GatewayModelSettingsView());
      await tester.tap(find.text('默认模型'));
      await tester.pumpAndSettle();

      await tester.tap(find.text('deepseek-v4-pro'));
      await tester.pumpAndSettle();

      expect(service.putRelaySettingsCalls.length, 1);
      expect(find.textContaining('保存失败'), findsOneWidget);
      // 选中态还原：唯一的勾仍在原默认模型 deepseek-v4-flash 那一行。
      final flashTile = tester.widget<ListTile>(
        find.ancestor(
          of: find.text('deepseek-v4-flash'),
          matching: find.byType(ListTile),
        ),
      );
      expect(flashTile.trailing, isA<Icon>());
      final proTile = tester.widget<ListTile>(
        find.ancestor(
          of: find.text('deepseek-v4-pro'),
          matching: find.byType(ListTile),
        ),
      );
      expect(proTile.trailing, isNull);

      await tester.pump(const Duration(seconds: 4));
    });

    testWidgets('价目表为空：展示空态文案', (tester) async {
      service.modelsToReturn = const [];

      await pumpPage(tester, const GatewayModelSettingsView());
      await tester.tap(find.text('默认模型'));
      await tester.pumpAndSettle();

      expect(find.textContaining('暂无可用模型'), findsOneWidget);
    });
  });

  group('Agent 模型设置列表页', () {
    testWidgets('状态行四态：已开启/待生效/设备离线/已关闭', (tester) async {
      service.agentsToReturn = [
        agent(
          id: '1',
          name: '已开启',
          enabled: true,
          applied: true,
          stateKnown: true,
        ),
        agent(
          id: '2',
          name: '待生效',
          enabled: true,
          applied: false,
          stateKnown: true,
        ),
        agent(
          id: '3',
          name: '离线',
          enabled: true,
          applied: false,
          stateKnown: false,
        ),
        agent(
          id: '4',
          name: '已关闭',
          enabled: false,
          applied: false,
          stateKnown: true,
        ),
      ];

      await pumpPage(tester, const GatewayAgentsRelayView());

      expect(find.textContaining('正在使用 Grix 中转'), findsOneWidget);
      expect(find.textContaining('处理中，稍后自动生效'), findsOneWidget);
      expect(find.textContaining('设备离线，上线后自动生效'), findsOneWidget);
      expect(find.textContaining('走你自己的账号'), findsOneWidget);

      // Switch 真值 = 服务端 enabled。
      final switches = tester.widgetList<Switch>(find.byType(Switch)).toList();
      expect(switches.map((s) => s.value), [true, true, true, false]);

      // 本页同样无充值文案。
      expect(find.textContaining('充值'), findsNothing);
    });

    testWidgets('服务端扩展字段缺席（flag 关）：开关禁用并如实说明，不把未知当成关', (tester) async {
      service.agentsToReturn = [agent(id: '1', name: 'Claude')];

      await pumpPage(tester, const GatewayAgentsRelayView());

      final sw = tester.widget<Switch>(find.byType(Switch));
      expect(sw.onChanged, isNull);
      expect(find.textContaining('服务端版本暂不支持该操作'), findsOneWidget);
    });

    testWidgets('开关乐观更新：写服务端 desired 成功后刷新，提示稍后自动生效', (tester) async {
      service.agentsToReturn = [
        agent(
          id: '1',
          name: 'Claude',
          enabled: false,
          applied: false,
          stateKnown: true,
        ),
      ];
      service.setRelayResult = const GatewaySetAgentRelayResult(
        GatewaySetRelayStatus.ok,
        GatewayRelayWriteStateModel(
          agentId: '1',
          enabled: true,
          relayModel: '',
          revision: 1,
          applied: false,
        ),
      );

      await pumpPage(tester, const GatewayAgentsRelayView());
      expect(service.listAgentCalls, 1);

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();

      expect(service.setRelayCalls.length, 1);
      expect(service.setRelayCalls.first['agentId'], '1');
      expect(service.setRelayCalls.first['enabled'], isTrue);
      // 首次写没有服务端 revision（GET 不返回），走 last-write-wins。
      expect(service.setRelayCalls.first['expectedRevision'], isNull);
      expect(find.textContaining('稍后自动生效'), findsOneWidget);
      // 成功后刷新列表。
      expect(service.listAgentCalls, 2);

      await tester.pump(const Duration(seconds: 4));
    });

    testWidgets('写失败：toast 提示并刷新回滚乐观更新', (tester) async {
      service.agentsToReturn = [
        agent(
          id: '1',
          name: 'Claude',
          enabled: false,
          applied: false,
          stateKnown: true,
        ),
      ];
      service.setRelayResult = const GatewaySetAgentRelayResult(
        GatewaySetRelayStatus.failed,
      );

      await pumpPage(tester, const GatewayAgentsRelayView());

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();

      expect(find.textContaining('开启中转失败'), findsOneWidget);
      // 失败路径刷新一次，Switch 回到服务端真值（关）。
      expect(service.listAgentCalls, 2);
      expect(tester.widget<Switch>(find.byType(Switch)).value, isFalse);

      await tester.pump(const Duration(seconds: 4));
    });

    testWidgets('409 冲突：自动刷新最新 state 并提示重试', (tester) async {
      service.agentsToReturn = [
        agent(
          id: '1',
          name: 'Claude',
          enabled: false,
          applied: false,
          stateKnown: true,
        ),
      ];
      service.setRelayResult = const GatewaySetAgentRelayResult(
        GatewaySetRelayStatus.conflict,
        GatewayRelayWriteStateModel(
          agentId: '1',
          enabled: true,
          relayModel: '',
          revision: 2,
          applied: false,
        ),
      );

      await pumpPage(tester, const GatewayAgentsRelayView());
      expect(service.listAgentCalls, 1);

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();

      expect(service.listAgentCalls, 2);
      expect(find.textContaining('开关状态已被其他端修改'), findsOneWidget);

      await tester.pump(const Duration(seconds: 4));
    });

    testWidgets('已启用换模型：进同款选择页，选完即 POST {enabled:true, model:新值}', (
      tester,
    ) async {
      service.agentsToReturn = [
        agent(
          id: '2',
          name: '本机Qwen',
          clientType: 'qwen',
          relayModel: 'deepseek-v4-flash',
          enabled: true,
          applied: true,
          stateKnown: true,
        ),
      ];
      service.setRelayResult = const GatewaySetAgentRelayResult(
        GatewaySetRelayStatus.ok,
        GatewayRelayWriteStateModel(
          agentId: '2',
          enabled: true,
          relayModel: 'deepseek-v4-pro',
          revision: 2,
          applied: false,
        ),
      );

      await pumpPage(tester, const GatewayAgentsRelayView());

      await tester.tap(find.textContaining('模型：deepseek-v4-flash'));
      await tester.pumpAndSettle();
      // 同款选择页：当前模型打勾，点另一项即保存。
      await tester.tap(find.text('deepseek-v4-pro'));
      await tester.pumpAndSettle();

      expect(service.setRelayCalls.length, 1);
      expect(service.setRelayCalls.first['enabled'], isTrue);
      expect(service.setRelayCalls.first['model'], 'deepseek-v4-pro');
      // 换模型成功并刷新后，列表回显新模型（fake 按 setAgentRelay 写库回显）。
      expect(find.textContaining('模型：deepseek-v4-pro'), findsOneWidget);

      await tester.pump(const Duration(seconds: 4));
    });

    testWidgets('服务端 400 needModel：弹选择页引导，选完带 model 自动重试成功', (tester) async {
      service.agentsToReturn = [
        agent(
          id: '2',
          name: '本机Qwen',
          clientType: 'qwen',
          relayModel: 'deepseek-v4-flash',
          enabled: false,
          applied: false,
          stateKnown: true,
        ),
      ];
      // 第一次写被服务端按 needModel 拒掉，带模型重试后放行。
      service.setRelayResultQueue.addAll([
        const GatewaySetAgentRelayResult(GatewaySetRelayStatus.needModel),
        const GatewaySetAgentRelayResult(
          GatewaySetRelayStatus.ok,
          GatewayRelayWriteStateModel(
            agentId: '2',
            enabled: true,
            relayModel: 'deepseek-v4-pro',
            revision: 1,
            applied: false,
          ),
        ),
      ]);

      await pumpPage(tester, const GatewayAgentsRelayView());

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();
      // needModel 后自动弹出同款模型选择页。
      expect(find.text('deepseek-v4-pro'), findsOneWidget);

      await tester.tap(find.text('deepseek-v4-pro'));
      await tester.pumpAndSettle();

      expect(service.setRelayCalls.length, 2);
      expect(service.setRelayCalls[0]['enabled'], isTrue);
      expect(service.setRelayCalls[0]['model'], 'deepseek-v4-flash');
      // 重试带的是选择页里新选的模型。
      expect(service.setRelayCalls[1]['enabled'], isTrue);
      expect(service.setRelayCalls[1]['model'], 'deepseek-v4-pro');
      expect(find.textContaining('稍后自动生效'), findsOneWidget);

      await tester.pump(const Duration(seconds: 4));
    });

    testWidgets('服务端 503 disabled（flag 关）：如实提示并刷新回落，不崩溃', (tester) async {
      service.agentsToReturn = [
        agent(
          id: '1',
          name: 'Claude',
          enabled: false,
          applied: false,
          stateKnown: true,
        ),
      ];
      service.setRelayResult = const GatewaySetAgentRelayResult(
        GatewaySetRelayStatus.disabled,
      );

      await pumpPage(tester, const GatewayAgentsRelayView());
      expect(service.listAgentCalls, 1);

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();

      expect(find.textContaining('服务端版本暂不支持该操作'), findsOneWidget);
      // disabled 路径刷新一次列表（GET 回落旧语义），页面正常渲染不崩溃。
      expect(service.listAgentCalls, 2);
      expect(tester.widget<Switch>(find.byType(Switch)).value, isFalse);

      await tester.pump(const Duration(seconds: 4));
    });

    testWidgets('未启用时点模型行：只本地暂存，不发服务端写', (tester) async {
      service.agentsToReturn = [
        agent(
          id: '2',
          name: '本机Qwen',
          clientType: 'qwen',
          relayModel: 'deepseek-v4-flash',
          enabled: false,
          applied: false,
          stateKnown: true,
        ),
      ];

      await pumpPage(tester, const GatewayAgentsRelayView());

      await tester.tap(find.textContaining('模型：deepseek-v4-flash'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('deepseek-v4-pro'));
      await tester.pumpAndSettle();

      expect(service.setRelayCalls, isEmpty);
      // 暂存回显在列表行。
      expect(find.textContaining('模型：deepseek-v4-pro'), findsOneWidget);
    });
  });
}
