import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/session_service.dart';
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

class _FakeSessionService extends SessionService {
  _FakeSessionService({this.openFails = false});

  final bool openFails;
  final opened = <String>[];

  @override
  Future<String?> openLatestSession(String peerId, int peerType) async {
    opened.add('$peerId:$peerType');
    if (openFails) throw Exception('open failed');
    return 'session-$peerId';
  }
}

/// 按 op 回放脚本化结果的 ImService。`installedAfterInstall` 用来模拟
/// "install_progress 已经忘了这次安装，但类型其实已经装上了"。
class _ScriptedImService extends ImService {
  _ScriptedImService({
    required this.progressStatus,
    this.installedAfterInstall = false,
    this.progressError = '',
    this.progressOutputTail = '',
  });

  final String progressStatus;
  final bool installedAfterInstall;
  final String progressError;
  final String progressOutputTail;

  bool _installTriggered = false;
  final ops = <String>[];

  /// `<op>@<agentId>`：换通道之后要能看出目录是从新通道重新拉的。
  final adminCalls = <String>[];
  final sent = <({String sessionId, String content})>[];

  @override
  Future<dynamic> requestConnectorAdmin({
    required String agentId,
    required String op,
    Map<String, dynamic>? args,
  }) async {
    ops.add(op);
    adminCalls.add('$op@$agentId');
    switch (op) {
      case 'list_installable':
        return {
          'platform': 'darwin',
          'agents': [
            {
              'agentType': 'codex',
              'label': 'Codex',
              'installed': _installTriggered && installedAfterInstall,
              'installCommand': 'npm install -g @openai/codex',
              'prerequisites': ['node', 'npm'],
            },
          ],
        };
      case 'install':
        _installTriggered = true;
        return {'agentType': 'codex', 'status': 'started'};
      case 'install_progress':
        return {
          'status': progressStatus,
          'error': progressError,
          'outputTail': progressOutputTail,
        };
      default:
        return null;
    }
  }

  @override
  Future<void> sendMessage(
    String content,
    String sessionId, {
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool updateCurrentSessionUi = true,
  }) async {
    sent.add((sessionId: sessionId, content: content));
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  AgentModel channelAgent({
    String id = 'channel-1',
    String name = 'Alpha',
    String clientType = 'claude',
    bool online = true,
  }) => AgentModel(
    id: id,
    agentName: name,
    providerType: 3,
    agentClientType: clientType,
    hostname: 'gcf-mac',
    online: online,
    supportsConnectorAdmin: true,
  );

  Future<void> openSheet(
    WidgetTester tester, {
    List<AgentModel>? candidates,
  }) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        // 求助流程会跳到聊天页，测试里用占位页接住这条路由。
        getPages: [
          GetPage(
            name: AppRoutes.chat,
            page: () => const Scaffold(body: Text('chat page')),
          ),
        ],
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: ElevatedButton(
                onPressed: () => showRemoteAgentInstallSheet(
                  context: context,
                  hostLabel: 'gcf-mac',
                  channelCandidates: candidates ?? [channelAgent()],
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
  testWidgets(
    'unknown progress settles as done once the type reports installed',
    (tester) async {
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
    },
  );

  // 反向对照：类型始终没装上时，unknown 不能被当成装完，必须继续轮询，
  // 不能弹出起名字对话框。
  testWidgets(
    'unknown progress keeps polling while the type is not installed',
    (tester) async {
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
    },
  );

  // 通道选择器：离线候选当不了通道，选出来只会等到下发才失败，不能出现在列表里。
  testWidgets('channel picker lists only online candidates with their type', (
    tester,
  ) async {
    Get.put<ImService>(_ScriptedImService(progressStatus: 'done'));

    await openSheet(
      tester,
      candidates: [
        channelAgent(),
        channelAgent(id: 'channel-2', name: 'Beta', clientType: 'codex'),
        channelAgent(id: 'channel-3', name: 'Ghost', online: false),
      ],
    );
    await tester.tap(find.byKey(const Key('remote-install-channel-picker')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('remote-install-channel-channel-1')), findsOne);
    expect(find.byKey(const Key('remote-install-channel-channel-2')), findsOne);
    expect(
      find.byKey(const Key('remote-install-channel-channel-3')),
      findsNothing,
    );
    // 行上带客户端类型文案，光看名字分不出这是哪个客户端。
    expect(
      find.descendant(
        of: find.byKey(const Key('remote-install-channel-channel-1')),
        matching: find.text('Claude'),
      ),
      findsOne,
    );
    expect(
      find.descendant(
        of: find.byKey(const Key('remote-install-channel-channel-2')),
        matching: find.text('Codex'),
      ),
      findsOne,
    );
  });

  testWidgets('picking another channel reloads the catalog from it', (
    tester,
  ) async {
    final im = _ScriptedImService(progressStatus: 'done');
    Get.put<ImService>(im);

    await openSheet(
      tester,
      candidates: [
        channelAgent(),
        channelAgent(id: 'channel-2', name: 'Beta', clientType: 'codex'),
      ],
    );
    await tester.tap(find.byKey(const Key('remote-install-channel-picker')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('remote-install-channel-channel-2')));
    await tester.pumpAndSettle();

    expect(im.adminCalls, contains('list_installable@channel-2'));
    // 选中项换成了 Beta：再打开选择器时勾在 Beta 那行。
    await tester.tap(find.byKey(const Key('remote-install-channel-picker')));
    await tester.pumpAndSettle();
    expect(
      find.descendant(
        of: find.byKey(const Key('remote-install-channel-channel-2')),
        matching: find.byIcon(Icons.check_rounded),
      ),
      findsOne,
    );
  });

  // 安装失败后手机端用户什么也做不了：只有那台机器上的通道 agent 能动手。
  // 先问一句，确认了才跳过去，并把现场一次说清。
  testWidgets('install failure asks to hand the job to the channel agent', (
    tester,
  ) async {
    final im = _ScriptedImService(
      progressStatus: 'error',
      progressError: 'npm exited with code 1',
      progressOutputTail: 'EACCES: permission denied',
    );
    Get.put<ImService>(im);
    final sessions = _FakeSessionService();
    Get.put<SessionService>(sessions);

    await openSheet(tester);
    await tester.tap(find.byKey(const Key('remote-install-codex')));
    await tester.pump();
    await tester.pump(const Duration(seconds: 3));
    await tester.pumpAndSettle();

    expect(find.text('Installation failed'), findsOne);
    await tester.tap(find.byKey(const Key('remote-install-help-confirm')));
    await tester.pumpAndSettle();

    expect(sessions.opened, ['channel-1:2']);
    expect(im.sent.single.sessionId, 'session-channel-1');
    final text = im.sent.single.content;
    expect(text, contains('gcf-mac'));
    expect(text, contains('Codex'));
    expect(text, contains('install_error'));
    expect(text, contains('npm exited with code 1'));
    expect(text, contains('npm install -g @openai/codex'));
    expect(text, contains('node, npm'));
    expect(text, contains('EACCES: permission denied'));

    // 失败时弹过 toast，排空它的 3s 定时器再收尾。
    await tester.pump(const Duration(seconds: 4));
  });

  testWidgets('declining the help prompt neither opens nor sends anything', (
    tester,
  ) async {
    final im = _ScriptedImService(
      progressStatus: 'error',
      progressError: 'npm exited with code 1',
    );
    Get.put<ImService>(im);
    final sessions = _FakeSessionService();
    Get.put<SessionService>(sessions);

    await openSheet(tester);
    await tester.tap(find.byKey(const Key('remote-install-codex')));
    await tester.pump();
    await tester.pump(const Duration(seconds: 3));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();

    expect(sessions.opened, isEmpty);
    expect(im.sent, isEmpty);
    // 取消只是不求助，安装弹窗还在，用户可以自己重试。
    expect(find.byKey(const Key('remote-install-codex')), findsOne);

    await tester.pump(const Duration(seconds: 4));
  });

  // 反向对照：弹窗这时已经关了，开会话再失败必须被吞掉并提示，
  // 不能变成一次没人看得见的未捕获异常。
  testWidgets('a failing session handoff surfaces instead of throwing', (
    tester,
  ) async {
    final im = _ScriptedImService(
      progressStatus: 'error',
      progressError: 'npm exited with code 1',
    );
    Get.put<ImService>(im);
    final sessions = _FakeSessionService(openFails: true);
    Get.put<SessionService>(sessions);

    await openSheet(tester);
    await tester.tap(find.byKey(const Key('remote-install-codex')));
    await tester.pump();
    await tester.pump(const Duration(seconds: 3));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('remote-install-help-confirm')));
    await tester.pumpAndSettle();

    expect(sessions.opened, ['channel-1:2']);
    expect(im.sent, isEmpty);

    await tester.pump(const Duration(seconds: 4));
  });
}
