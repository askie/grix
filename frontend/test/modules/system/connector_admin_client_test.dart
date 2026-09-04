import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/connector_admin_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/system/connector_admin_client.dart';

/// 记录调用并回放预设结果的 ImService；连接器管理只走 requestConnectorAdmin，
/// 这里替掉它就能在不起 WS 的情况下钉住线上契约的解析。
class _FakeImService extends ImService {
  final calls = <Map<String, dynamic>>[];
  Object? nextResult;
  Object? nextError;

  @override
  Future<dynamic> requestConnectorAdmin({
    required String agentId,
    required String op,
    Map<String, dynamic>? args,
  }) async {
    calls.add({'agent_id': agentId, 'op': op, 'args': args});
    if (nextError != null) throw nextError!;
    return nextResult;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeImService im;
  late ConnectorAdminClient client;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    im = _FakeImService();
    Get.put<ImService>(im);
    client = ConnectorAdminClient('agent-channel');
  });

  tearDown(Get.reset);

  // 契约守卫：list_installable 回的是对象 {platform, agents:[...]}（与连接器
  // GET /api/install 一致），不是裸数组。按数组解析会得到空列表，用户看到"没有
  // 可安装的类型"，这里钉住对象形态。
  test('listInstallable parses the {platform, agents} envelope', () async {
    im.nextResult = {
      'platform': 'darwin',
      'agents': [
        {'agentType': 'claude', 'label': 'Claude', 'installed': true},
        {'agent_type': 'codex', 'label': 'Codex'},
        {'label': 'no type, must be dropped'},
      ],
    };

    final list = await client.listInstallable();

    expect(list.platform, 'darwin');
    expect(list.agents.map((a) => a.agentType), ['claude', 'codex']);
    expect(list.agents.first.installed, isTrue);
    expect(list.agents[1].installed, isFalse);
    expect(im.calls.single['op'], 'list_installable');
    expect(im.calls.single['agent_id'], 'agent-channel');
  });

  test('install sends the agent_type arg', () async {
    im.nextResult = {'agentType': 'codex', 'status': 'started'};

    await client.install('codex');

    expect(im.calls.single['op'], 'install');
    expect(im.calls.single['args'], {'agent_type': 'codex'});
  });

  test('installProgress falls back to unknown on a malformed result', () async {
    im.nextResult = 'nonsense';

    final progress = await client.installProgress('codex');

    expect(progress.status, 'unknown');
    expect(progress.isDone, isFalse);
    expect(progress.isError, isFalse);
  });

  test('installProgress surfaces the connector error status', () async {
    im.nextResult = {'status': 'error', 'error': 'npm exited 1'};

    final progress = await client.installProgress('codex');

    expect(progress.isError, isTrue);
    expect(progress.error, 'npm exited 1');
  });

  test('createAgent returns the fields the backend echoes back', () async {
    im.nextResult = {
      'agent_id': '123',
      'agent_name': 'claude-1',
      'client_type': 'claude',
      'session_id': 'sess-1',
    };

    final created = await client.createAgent(
      agentName: 'claude-1',
      clientType: 'claude',
    );

    expect(created.agentId, '123');
    expect(created.sessionId, 'sess-1');
    expect(im.calls.single['op'], 'create_agent');
    expect(im.calls.single['args'], {
      'agent_name': 'claude-1',
      'client_type': 'claude',
    });
  });

  // 升级提示守卫：连接器太老时后端回 unsupported、连接器自己不认识 op 时回
  // unsupported_op，两者都必须被认成"要升级连接器"，否则用户只会看到一句
  // 看不懂的错误并反复重试。
  test('unsupported error codes both mean "upgrade the connector"', () {
    expect(
      const ConnectorAdminException('x', code: 'unsupported').isUnsupported,
      isTrue,
    );
    expect(
      const ConnectorAdminException('x', code: 'unsupported_op').isUnsupported,
      isTrue,
    );
    expect(
      const ConnectorAdminException('x', code: 'internal_error').isUnsupported,
      isFalse,
    );
    expect(
      const ConnectorAdminException('x', code: 'offline').isOffline,
      isTrue,
    );
  });

  // host_name 是后端新加的显式字段；老后端只有 config.host_meta.hostname，
  // 两条路都要能归到同一台机器上，否则升级期间分组会碎掉。
  test('AgentModel reads host name from host_name or config fallback', () {
    final withField = AgentModel.fromJson({
      'id': '1',
      'agent_name': 'a',
      'host_name': 'gcf-mac',
      'supports_connector_admin': true,
      'config': {
        'host_meta': {'hostname': 'stale-name'},
      },
    });
    expect(withField.hostname, 'gcf-mac');
    expect(withField.supportsConnectorAdmin, isTrue);

    final legacy = AgentModel.fromJson({
      'id': '2',
      'agent_name': 'b',
      'config': {
        'host_meta': {'hostname': 'gcf-mac'},
      },
    });
    expect(legacy.hostname, 'gcf-mac');
    expect(legacy.supportsConnectorAdmin, isFalse);
  });
}
