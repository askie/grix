import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/system/remote_agent_install_sheet.dart';

class _FakeAuthService extends AuthService {
  @override
  String? get userId => 'owner-1';

  @override
  void attachAuthInterceptor(Dio dio) {}
}

class _FakeAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

/// 按 op 回放脚本化结果的 ImService。`installedAfterInstall` 用来模拟
/// "install_progress 已经忘了这次安装，但类型其实已经装上了"。
class _ScriptedImService extends ImService {
  _ScriptedImService({
    required this.progressStatus,
    this.installedAfterInstall = false,
  });

  final String progressStatus;
  final bool installedAfterInstall;

  bool _installTriggered = false;
  final ops = <String>[];

  @override
  Future<dynamic> requestConnectorAdmin({
    required String agentId,
    required String op,
    Map<String, dynamic>? args,
  }) async {
    ops.add(op);
    switch (op) {
      case 'list_installable':
        return {
          'platform': 'darwin',
          'agents': [
            {
              'agentType': 'codex',
              'label': 'Codex',
              'installed': _installTriggered && installedAfterInstall,
            },
          ],
        };
      case 'install':
        _installTriggered = true;
        return {'agentType': 'codex', 'status': 'started'};
      case 'install_progress':
        return {'status': progressStatus};
      default:
        return null;
    }
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  AgentModel channelAgent() => AgentModel(
    id: 'channel-1',
    agentName: 'Alpha',
    providerType: 3,
    hostname: 'gcf-mac',
    online: true,
    supportsConnectorAdmin: true,
  );

  Future<void> openSheet(WidgetTester tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: ElevatedButton(
                onPressed: () => showRemoteAgentInstallSheet(
                  context: context,
                  hostLabel: 'gcf-mac',
                  channelCandidates: [channelAgent()],
                ),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
  }

  setUp(() {
    Get.testMode = true;
    Get.reset();
    Get.put<AuthService>(_FakeAuthService());
    Get.put<AgentService>(_FakeAgentService());
  });

  tearDown(Get.reset);

  testWidgets('lists installable types from the {platform, agents} envelope', (
    tester,
  ) async {
    Get.put<ImService>(_ScriptedImService(progressStatus: 'done'));

    await openSheet(tester);

    expect(find.byKey(const Key('remote-install-codex')), findsOneWidget);
    expect(find.text('Not installed'), findsOneWidget);
  });

  // 回归守卫：连接器装完会清掉进度记录，之后 install_progress 恒回 unknown。
  // 只看进度就会一路轮到超时并报"安装超时"，尽管其实已经装好了。
  // 必须用可安装列表交叉确认，确认已装就收口并进入起名字环节。
  testWidgets('unknown progress settles as done once the type reports installed', (
    tester,
  ) async {
    Get.put<ImService>(
      _ScriptedImService(
        progressStatus: 'unknown',
        installedAfterInstall: true,
      ),
    );

    await openSheet(tester);
    await tester.tap(find.byKey(const Key('remote-install-codex')));
    await tester.pump();
    // 第一次轮询在 2s 之后。
    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 400));

    expect(find.text('Create'), findsOneWidget);
  });

  // 反向对照：类型始终没装上时，unknown 不能被当成装完，必须继续轮询，
  // 不能弹出起名字对话框。
  testWidgets('unknown progress keeps polling while the type is not installed', (
    tester,
  ) async {
    Get.put<ImService>(
      _ScriptedImService(
        progressStatus: 'unknown',
        installedAfterInstall: false,
      ),
    );

    await openSheet(tester);
    await tester.tap(find.byKey(const Key('remote-install-codex')));
    await tester.pump();
    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 400));

    expect(find.text('Create'), findsNothing);

    // 关掉弹窗让轮询链自行终止；再排空最后一次已经排好的 2s 延时。
    await tester.tap(find.byIcon(Icons.close));
    await tester.pump(const Duration(seconds: 1));
    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 400));
  });
}
