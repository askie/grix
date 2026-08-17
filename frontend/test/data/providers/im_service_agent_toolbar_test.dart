import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/agent_toolbar_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';

  @override
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero}) => true;

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async => TokenRefreshStatus.ready;
}

class _FakeSessionService extends SessionService {
  @override
  Future<SessionSnapshotFetchResult> fetchSessionSnapshotsResult({
    int limit = 200,
    int maxPages = 5,
  }) async {
    return const SessionSnapshotFetchResult(snapshots: [], success: true);
  }
}

class _RecordingSink implements WebSocketSink {
  final List<Map<String, dynamic>> packets = [];

  @override
  void add(dynamic data) {
    packets.add(jsonDecode(data as String) as Map<String, dynamic>);
  }

  @override
  Future<void> close([int? closeCode, String? closeReason]) async {}

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _FakeWebSocketChannel implements WebSocketChannel {
  _FakeWebSocketChannel({
    required this.ready,
    required Stream<dynamic> stream,
    required WebSocketSink sink,
  }) : _stream = stream,
       _sink = sink;

  @override
  final Future<void> ready;

  final Stream<dynamic> _stream;
  final WebSocketSink _sink;

  @override
  Stream<dynamic> get stream => _stream;

  @override
  WebSocketSink get sink => _sink;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _ToolbarEnv {
  _ToolbarEnv(this.service, this.sink, this.downstream);

  final ImService service;
  final _RecordingSink sink;
  final StreamController<dynamic> downstream;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    SharedPreferences.setMockInitialValues(<String, Object>{});
    Get.put<AuthService>(_FakeAuthService());
    Get.put<SessionService>(_FakeSessionService());
  });

  tearDown(() {
    ImService.channelConnectorForTest = null;
    Get.reset();
  });

  SessionModel buildSession(String sessionId) {
    return SessionModel(
      sessionId: sessionId,
      title: 'Gemini Session',
      type: 'private',
      peerType: 2,
      updatedAt: 0,
      lastMessageTime: 0,
    );
  }

  SessionModel buildGroupSession(String sessionId) {
    return SessionModel(
      sessionId: sessionId,
      title: 'Group Session',
      type: 'group',
      peerType: 0,
      updatedAt: 0,
      lastMessageTime: 0,
    );
  }

  AgentToolbarModel buildToolbar({
    required String sessionId,
    String modelValue = '',
    String modelBadge = '',
    int revision = 3,
  }) {
    return AgentToolbarModel(
      sessionId: sessionId,
      agentId: 'agent-gemini',
      toolbarId: 'toolbar-gemini',
      revision: revision,
      visible: true,
      updatedAt: 100,
      items: <AgentToolbarItemModel>[
        const AgentToolbarItemModel(
          itemId: 'session_control',
          groupId: 'session_control',
          kind: 'select',
          actionId: 'session_control',
          label: 'Gemini 工作区',
          icon: 'folder',
          variant: 'secondary',
          disabled: false,
          loading: false,
          selected: false,
          tooltip: '',
          badgeText: 'demo',
          confirmTitle: '',
          confirmText: '',
          value: '',
          placeholder: '选择工作区操作',
          options: <AgentToolbarOptionModel>[
            AgentToolbarOptionModel(
              optionId: 'status',
              label: '查看状态',
              disabled: false,
            ),
          ],
          percent: 0,
          centerText: '',
          progressDesc: '',
          progressDetail: '',
          localAction: '',
          commands: <CommandItemModel>[],
        ),
        AgentToolbarItemModel(
          itemId: 'select_model',
          groupId: 'model_control',
          kind: 'select',
          actionId: 'select_model',
          label: '模型',
          icon: 'cpu',
          variant: 'secondary',
          disabled: false,
          loading: false,
          selected: false,
          tooltip: '',
          badgeText: modelBadge,
          confirmTitle: '',
          confirmText: '',
          value: modelValue,
          placeholder: '选择模型',
          options: const <AgentToolbarOptionModel>[
            AgentToolbarOptionModel(
              optionId: 'gemini-2.5-flash',
              label: 'Gemini 2.5 Flash',
              disabled: false,
            ),
            AgentToolbarOptionModel(
              optionId: 'gemini-2.5-pro',
              label: 'Gemini 2.5 Pro',
              disabled: false,
            ),
          ],
          percent: 0,
          centerText: '',
          progressDesc: '',
          progressDetail: '',
          localAction: '',
          commands: const <CommandItemModel>[],
        ),
      ],
    );
  }

  Map<String, dynamic> buildToolbarSnapshotPayload({
    required String sessionId,
    String modelValue = '',
    String modelBadge = '',
    int revision = 4,
  }) {
    return <String, dynamic>{
      'session_id': sessionId,
      'agent_id': 'agent-gemini',
      'toolbar_id': 'toolbar-gemini',
      'revision': revision,
      'visible': true,
      'updated_at': 200,
      'items': <Map<String, dynamic>>[
        <String, dynamic>{
          'item_id': 'session_control',
          'group_id': 'session_control',
          'kind': 'select',
          'action_id': 'session_control',
          'label': 'Gemini 工作区',
          'icon': 'folder',
          'variant': 'secondary',
          'disabled': false,
          'loading': false,
          'selected': false,
          'tooltip': '',
          'badge_text': 'demo',
          'confirm_title': '',
          'confirm_text': '',
          'value': '',
          'placeholder': '选择工作区操作',
          'options': <Map<String, dynamic>>[
            <String, dynamic>{
              'option_id': 'status',
              'label': '查看状态',
              'disabled': false,
            },
          ],
        },
        <String, dynamic>{
          'item_id': 'select_model',
          'group_id': 'model_control',
          'kind': 'select',
          'action_id': 'select_model',
          'label': '模型',
          'icon': 'cpu',
          'variant': 'secondary',
          'disabled': false,
          'loading': false,
          'selected': false,
          'tooltip': '',
          'badge_text': modelBadge,
          'confirm_title': '',
          'confirm_text': '',
          'value': modelValue,
          'placeholder': '选择模型',
          'options': <Map<String, dynamic>>[
            <String, dynamic>{
              'option_id': 'gemini-2.5-flash',
              'label': 'Gemini 2.5 Flash',
              'disabled': false,
            },
            <String, dynamic>{
              'option_id': 'gemini-2.5-pro',
              'label': 'Gemini 2.5 Pro',
              'disabled': false,
            },
          ],
        },
      ],
    };
  }

  String packet(String cmd, Map<String, dynamic> payload) {
    return jsonEncode(<String, dynamic>{
      'cmd': cmd,
      'seq': 1,
      'payload': payload,
    });
  }

  Future<void> expectEventually(
    bool Function() condition, {
    String? reason,
  }) async {
    final deadline = DateTime.now().add(const Duration(seconds: 5));
    while (DateTime.now().isBefore(deadline)) {
      if (condition()) return;
      await Future<void>.delayed(const Duration(milliseconds: 25));
    }
    expect(condition(), isTrue, reason: reason);
  }

  Future<_ToolbarEnv> connectAuthenticatedService() async {
    final sink = _RecordingSink();
    final downstream = StreamController<dynamic>();
    ImService.channelConnectorForTest = (uri) => _FakeWebSocketChannel(
      ready: Future<void>.value(),
      stream: downstream.stream,
      sink: sink,
    );

    final service = ImService();
    service.connect('ws://127.0.0.1:1/ws');
    await expectEventually(
      () => sink.packets.any((p) => p['cmd'] == 'auth'),
      reason: '连接后应发出 auth 包',
    );
    downstream.add(
      jsonEncode(<String, dynamic>{
        'cmd': 'auth_ack',
        'payload': <String, dynamic>{'code': 0, 'user_id': '1001'},
      }),
    );
    await expectEventually(
      () => service.isAuthenticated,
      reason: 'auth_ack code=0 后应进入已鉴权态',
    );
    return _ToolbarEnv(service, sink, downstream);
  }

  String latestToolbarClientActionId(_RecordingSink sink) {
    final packet = sink.packets.lastWhere(
      (candidate) => candidate['cmd'] == 'agent_toolbar_action',
    );
    final payload = packet['payload'] as Map<String, dynamic>;
    return payload['client_action_id']?.toString() ?? '';
  }

  test(
    'accepted ack clears toolbar item loading before snapshot arrives',
    () async {
      final env = await connectAuthenticatedService();
      final service = env.service;
      const sessionId = 'sess-gemini-toolbar';
      service.sessions
        ..clear()
        ..add(buildSession(sessionId));
      final toolbar = buildToolbar(sessionId: sessionId);
      service.agentToolbars[sessionId] = toolbar;

      bool? accepted;
      await service.sendAgentToolbarAction(
        sessionId: sessionId,
        toolbar: toolbar,
        item: toolbar.items[1],
        event: 'select',
        optionId: 'gemini-2.5-pro',
        onAck: (value) => accepted = value,
      );

      var displayItem = service.getToolbarItemForDisplay(
        sessionId,
        toolbar.items[1],
      );
      expect(displayItem.loading, isTrue);
      expect(displayItem.disabled, isTrue);
      expect(displayItem.label, 'Gemini 2.5 Pro');
      expect(displayItem.value, 'gemini-2.5-pro');

      await service.handleDownstreamForTest(
        packet('agent_toolbar_action_ack', <String, dynamic>{
          'session_id': sessionId,
          'client_action_id': latestToolbarClientActionId(env.sink),
          'accepted': true,
          'code': 'accepted',
          'msg': '',
        }),
      );

      displayItem = service.getToolbarItemForDisplay(
        sessionId,
        toolbar.items[1],
      );
      expect(displayItem.loading, isFalse);
      expect(displayItem.disabled, isFalse);
      expect(displayItem.label, 'Gemini 2.5 Pro');
      expect(displayItem.value, 'gemini-2.5-pro');
      expect(accepted, isTrue);

      await service.handleDownstreamForTest(
        packet(
          'agent_toolbar_sync',
          buildToolbarSnapshotPayload(
            sessionId: sessionId,
            modelValue: 'gemini-2.5-pro',
            modelBadge: 'Gemini 2.5 Pro',
          ),
        ),
      );

      final updatedToolbar = service.getAgentToolbar(sessionId);
      expect(updatedToolbar, isNotNull);
      expect(updatedToolbar!.items.first.actionId, 'session_control');
      expect(updatedToolbar.items[1].value, 'gemini-2.5-pro');
      expect(updatedToolbar.items[1].badgeText, 'Gemini 2.5 Pro');

      displayItem = service.getToolbarItemForDisplay(
        sessionId,
        updatedToolbar.items[1],
      );
      expect(displayItem.loading, isFalse);
      expect(displayItem.disabled, isFalse);
    },
  );

  test(
    'accepted select ack does not revert display on newer stale snapshot',
    () async {
      final env = await connectAuthenticatedService();
      final service = env.service;
      const sessionId = 'sess-gemini-toolbar-stale-select';
      service.sessions
        ..clear()
        ..add(buildSession(sessionId));
      final toolbar = buildToolbar(
        sessionId: sessionId,
        modelValue: 'gemini-2.5-flash',
        modelBadge: 'Gemini 2.5 Flash',
      );
      service.agentToolbars[sessionId] = toolbar;

      await service.sendAgentToolbarAction(
        sessionId: sessionId,
        toolbar: toolbar,
        item: toolbar.items[1],
        event: 'select',
        optionId: 'gemini-2.5-pro',
      );

      await service.handleDownstreamForTest(
        packet('agent_toolbar_action_ack', <String, dynamic>{
          'session_id': sessionId,
          'client_action_id': latestToolbarClientActionId(env.sink),
          'accepted': true,
          'code': 'accepted',
          'msg': '',
        }),
      );

      await service.handleDownstreamForTest(
        packet(
          'agent_toolbar_sync',
          buildToolbarSnapshotPayload(
            sessionId: sessionId,
            modelValue: 'gemini-2.5-flash',
            modelBadge: 'Gemini 2.5 Flash',
            revision: toolbar.revision + 1,
          ),
        ),
      );

      final staleToolbar = service.getAgentToolbar(sessionId);
      expect(staleToolbar, isNotNull);
      final displayItem = service.getToolbarItemForDisplay(
        sessionId,
        staleToolbar!.items[1],
      );
      expect(displayItem.loading, isFalse);
      expect(displayItem.disabled, isFalse);
      expect(displayItem.label, 'Gemini 2.5 Pro');
      expect(displayItem.value, 'gemini-2.5-pro');
      expect(displayItem.badgeText, 'Gemini 2.5 Pro');
    },
  );

  test('action-menu select does not enter optimistic value display', () async {
    final env = await connectAuthenticatedService();
    final service = env.service;
    const sessionId = 'sess-gemini-toolbar-action-menu';
    service.sessions
      ..clear()
      ..add(buildSession(sessionId));
    final toolbar = buildToolbar(sessionId: sessionId);
    service.agentToolbars[sessionId] = toolbar;

    await service.sendAgentToolbarAction(
      sessionId: sessionId,
      toolbar: toolbar,
      item: toolbar.items[0],
      event: 'select',
      optionId: 'status',
    );

    var displayItem = service.getToolbarItemForDisplay(
      sessionId,
      toolbar.items[0],
    );
    expect(displayItem.loading, isTrue);
    expect(displayItem.label, 'Gemini 工作区');
    expect(displayItem.value, isEmpty);

    await service.handleDownstreamForTest(
      packet('agent_toolbar_action_ack', <String, dynamic>{
        'session_id': sessionId,
        'client_action_id': latestToolbarClientActionId(env.sink),
        'accepted': true,
        'code': 'accepted',
        'msg': '',
      }),
    );

    displayItem = service.getToolbarItemForDisplay(sessionId, toolbar.items[0]);
    expect(displayItem.loading, isFalse);
    expect(displayItem.disabled, isFalse);
    expect(displayItem.label, 'Gemini 工作区');
    expect(displayItem.value, isEmpty);
    expect(
      env.sink.packets.where((p) => p['cmd'] == 'agent_toolbar_get'),
      isNotEmpty,
    );
  });

  test('rejected ack after stale snapshot clears optimistic select', () async {
    final env = await connectAuthenticatedService();
    final service = env.service;
    const sessionId = 'sess-gemini-toolbar-reject-after-snapshot';
    service.sessions
      ..clear()
      ..add(buildSession(sessionId));
    final toolbar = buildToolbar(
      sessionId: sessionId,
      modelValue: 'gemini-2.5-flash',
      modelBadge: 'Gemini 2.5 Flash',
    );
    service.agentToolbars[sessionId] = toolbar;

    bool? accepted;
    await service.sendAgentToolbarAction(
      sessionId: sessionId,
      toolbar: toolbar,
      item: toolbar.items[1],
      event: 'select',
      optionId: 'gemini-2.5-pro',
      onAck: (value) => accepted = value,
    );
    final clientActionId = latestToolbarClientActionId(env.sink);

    await service.handleDownstreamForTest(
      packet(
        'agent_toolbar_sync',
        buildToolbarSnapshotPayload(
          sessionId: sessionId,
          modelValue: 'gemini-2.5-flash',
          modelBadge: 'Gemini 2.5 Flash',
          revision: toolbar.revision + 1,
        ),
      ),
    );
    var displayItem = service.getToolbarItemForDisplay(
      sessionId,
      service.getAgentToolbar(sessionId)!.items[1],
    );
    expect(displayItem.value, 'gemini-2.5-pro');
    expect(displayItem.badgeText, 'Gemini 2.5 Pro');

    await service.handleDownstreamForTest(
      packet('agent_toolbar_action_ack', <String, dynamic>{
        'session_id': sessionId,
        'client_action_id': clientActionId,
        'accepted': false,
        'code': 'action_failed',
        'msg': '',
      }),
    );

    displayItem = service.getToolbarItemForDisplay(
      sessionId,
      service.getAgentToolbar(sessionId)!.items[1],
    );
    expect(displayItem.loading, isFalse);
    expect(displayItem.disabled, isFalse);
    expect(displayItem.value, 'gemini-2.5-flash');
    expect(displayItem.badgeText, 'Gemini 2.5 Flash');
    expect(accepted, isFalse);
  });

  test('failed send settles the toolbar action callback immediately', () async {
    final service = ImService();
    const sessionId = 'sess-toolbar-send-failed';
    final toolbar = buildToolbar(sessionId: sessionId);
    bool? accepted;

    final sent = await service.sendAgentToolbarAction(
      sessionId: sessionId,
      toolbar: toolbar,
      item: toolbar.items[1],
      event: 'select',
      optionId: 'gemini-2.5-pro',
      onAck: (value) => accepted = value,
    );

    expect(sent, isFalse);
    expect(accepted, isFalse);
  });

  test(
    'duplicate toolbar snapshot does not notify toolbar observers',
    () async {
      final service = ImService();
      const sessionId = 'sess-gemini-toolbar-duplicate';
      service.sessions
        ..clear()
        ..add(buildSession(sessionId));
      service.agentToolbars[sessionId] = buildToolbar(sessionId: sessionId);

      var notifyCount = 0;
      final subscription = service.agentToolbars.listen((_) {
        notifyCount++;
      });
      addTearDown(subscription.cancel);

      await service.handleDownstreamForTest(
        packet(
          'agent_toolbar_sync',
          buildToolbarSnapshotPayload(
            sessionId: sessionId,
            modelValue: '',
            modelBadge: '',
          ),
        ),
      );

      expect(notifyCount, 1);

      notifyCount = 0;
      await service.handleDownstreamForTest(
        packet(
          'agent_toolbar_sync',
          buildToolbarSnapshotPayload(
            sessionId: sessionId,
            modelValue: '',
            modelBadge: '',
          ),
        ),
      );

      expect(notifyCount, 0);
    },
  );

  test('toolbar snapshot updates do not notify message observers', () async {
    final service = ImService();
    const sessionId = 'sess-gemini-toolbar-message-isolation';
    service.sessions
      ..clear()
      ..add(buildSession(sessionId));

    var messageNotifyCount = 0;
    final subscription = service.currentMessages.listen((_) {
      messageNotifyCount++;
    });
    addTearDown(subscription.cancel);

    await service.handleDownstreamForTest(
      packet(
        'agent_toolbar_sync',
        buildToolbarSnapshotPayload(
          sessionId: sessionId,
          modelValue: 'gemini-2.5-pro',
          modelBadge: 'Gemini 2.5 Pro',
        ),
      ),
    );

    expect(messageNotifyCount, 0);
    expect(service.currentMessages, isEmpty);
    expect(service.getAgentToolbar(sessionId), isNotNull);
  });

  test('rejected ack clears toolbar loading immediately', () async {
    final env = await connectAuthenticatedService();
    final service = env.service;
    const sessionId = 'sess-gemini-toolbar-rejected';
    service.sessions
      ..clear()
      ..add(buildSession(sessionId));
    final toolbar = buildToolbar(sessionId: sessionId);
    service.agentToolbars[sessionId] = toolbar;

    await service.sendAgentToolbarAction(
      sessionId: sessionId,
      toolbar: toolbar,
      item: toolbar.items[1],
      event: 'select',
      optionId: 'gemini-2.5-pro',
    );

    var displayItem = service.getToolbarItemForDisplay(
      sessionId,
      toolbar.items[1],
    );
    expect(displayItem.loading, isTrue);

    await service.handleDownstreamForTest(
      packet('agent_toolbar_action_ack', <String, dynamic>{
        'session_id': sessionId,
        'client_action_id': latestToolbarClientActionId(env.sink),
        'accepted': false,
        'code': 'action_failed',
        'msg': '',
      }),
    );

    displayItem = service.getToolbarItemForDisplay(sessionId, toolbar.items[1]);
    expect(displayItem.loading, isFalse);
    expect(displayItem.disabled, isFalse);
  });

  test('group chat ignores toolbar snapshot without target agent', () async {
    final service = ImService();
    const sessionId = 'sess-group-toolbar-no-target';
    service.sessions
      ..clear()
      ..add(buildGroupSession(sessionId));

    await service.handleDownstreamForTest(
      packet(
        'agent_toolbar_sync',
        buildToolbarSnapshotPayload(
          sessionId: sessionId,
          modelValue: 'gemini-2.5-pro',
          modelBadge: 'Gemini 2.5 Pro',
        ),
      ),
    );

    expect(service.getAgentToolbar(sessionId), isNull);
  });

  test(
    'group chat accepts toolbar snapshot only for selected target agent',
    () async {
      final service = ImService();
      const sessionId = 'sess-group-toolbar-target';
      service.sessions
        ..clear()
        ..add(buildGroupSession(sessionId));

      service.setGroupToolbarTargetAgent(sessionId, agentId: '10086');

      await service.handleDownstreamForTest(
        packet(
          'agent_toolbar_sync',
          buildToolbarSnapshotPayload(
            sessionId: sessionId,
            modelValue: 'gemini-2.5-pro',
            modelBadge: 'Gemini 2.5 Pro',
          )..['agent_id'] = '10086',
        ),
      );

      final toolbar = service.getAgentToolbar(sessionId);
      expect(toolbar, isNotNull);
      expect(toolbar!.agentId, '10086');
    },
  );

  test(
    'group chat drops snapshot from non-target agent after target selected',
    () async {
      final service = ImService();
      const sessionId = 'sess-group-toolbar-switch-target';
      service.sessions
        ..clear()
        ..add(buildGroupSession(sessionId));

      service.setGroupToolbarTargetAgent(sessionId, agentId: '20001');
      await service.handleDownstreamForTest(
        packet(
          'agent_toolbar_sync',
          buildToolbarSnapshotPayload(sessionId: sessionId)
            ..['agent_id'] = '20001',
        ),
      );
      expect(service.getAgentToolbar(sessionId), isNotNull);

      service.setGroupToolbarTargetAgent(sessionId, agentId: '20002');
      await service.handleDownstreamForTest(
        packet(
          'agent_toolbar_sync',
          buildToolbarSnapshotPayload(sessionId: sessionId)
            ..['agent_id'] = '20001',
        ),
      );
      expect(service.getAgentToolbar(sessionId), isNull);
    },
  );

  test('group chat clears toolbar when target agent cleared', () async {
    final service = ImService();
    const sessionId = 'sess-group-toolbar-clear';
    service.sessions
      ..clear()
      ..add(buildGroupSession(sessionId));

    service.setGroupToolbarTargetAgent(sessionId, agentId: '30001');
    await service.handleDownstreamForTest(
      packet(
        'agent_toolbar_sync',
        buildToolbarSnapshotPayload(sessionId: sessionId)
          ..['agent_id'] = '30001',
      ),
    );
    expect(service.getAgentToolbar(sessionId), isNotNull);

    service.setGroupToolbarTargetAgent(sessionId, agentId: '');
    expect(service.getAgentToolbar(sessionId), isNull);
  });

  test(
    'getAgentToolbar returns toolbar when session not yet in sessions list',
    () async {
      final service = ImService();
      const sessionId = 'sess-new-agent-not-in-list';
      // 不将 session 加入 sessions 列表，模拟新建对话时 session 元数据尚未同步
      service.sessions.clear();
      final toolbar = buildToolbar(sessionId: sessionId);
      service.agentToolbars[sessionId] = toolbar;

      // 即使 findSessionById 返回 null，也应返回已存储的 toolbar
      final result = service.getAgentToolbar(sessionId);
      expect(result, isNotNull);
      expect(result!.sessionId, sessionId);
      expect(result.items.length, 2);
    },
  );

  test(
    'out-of-order toolbar snapshot with lower revision does not overwrite newer state',
    () async {
      final service = ImService();
      const sessionId = 'sess-toolbar-revision-race';
      service.sessions
        ..clear()
        ..add(buildSession(sessionId));

      // 当前已持有新模型状态（revision=5）
      service.agentToolbars[sessionId] = buildToolbar(
        sessionId: sessionId,
        modelValue: 'k2.7-coding-highspeed',
        modelBadge: 'K2.7 Coding Highspeed',
        revision: 5,
      );

      // 乱序到达的旧快照（revision=4），模型为旧值 k3
      await service.handleDownstreamForTest(
        packet(
          'agent_toolbar_sync',
          buildToolbarSnapshotPayload(
            sessionId: sessionId,
            modelValue: 'k3',
            modelBadge: 'k3',
            revision: 4,
          ),
        ),
      );

      final toolbar = service.getAgentToolbar(sessionId);
      expect(toolbar, isNotNull);
      expect(toolbar!.revision, 5);
      expect(toolbar.items[1].value, 'k2.7-coding-highspeed');
      expect(toolbar.items[1].badgeText, 'K2.7 Coding Highspeed');
    },
  );

  test(
    'sendAgentToolbarAction honors actionId override (create_profile)',
    () async {
      final env = await connectAuthenticatedService();
      final service = env.service;
      const sessionId = 'sess-dsh-profile-create';
      service.sessions
        ..clear()
        ..add(buildSession(sessionId));
      final toolbar = buildToolbar(sessionId: sessionId);
      service.agentToolbars[sessionId] = toolbar;

      // DeepSeek Profile 选择器项：item.actionId 是 select_profile，
      // 「新建 Profile」对话框确认后由调用方覆盖成 create_profile。
      const profileItem = AgentToolbarItemModel(
        itemId: 'dsh_profile',
        groupId: 'profile_control',
        kind: 'select',
        actionId: 'select_profile',
        label: '',
        icon: 'profile',
        variant: 'secondary',
        disabled: false,
        loading: false,
        selected: false,
        tooltip: '',
        badgeText: 'web（插件托管）',
        confirmTitle: '',
        confirmText: '',
        value: 'web',
        placeholder: '选择 Profile',
        options: <AgentToolbarOptionModel>[
          AgentToolbarOptionModel(
            optionId: 'web',
            label: 'web（插件托管）',
            disabled: false,
          ),
          AgentToolbarOptionModel(
            optionId: '__create__',
            label: '＋ 新建 Profile…',
            disabled: false,
          ),
        ],
        percent: 0,
        centerText: '',
        progressDesc: '',
        progressDetail: '',
        localAction: '',
        commands: <CommandItemModel>[],
      );

      await service.sendAgentToolbarAction(
        sessionId: sessionId,
        toolbar: toolbar,
        item: profileItem,
        event: 'select',
        optionId: 'team-alpha',
        actionId: 'create_profile',
      );
      var payload =
          env.sink.packets.lastWhere(
                (p) => p['cmd'] == 'agent_toolbar_action',
              )['payload']
              as Map<String, dynamic>;
      expect(payload['action_id'], 'create_profile');
      expect(payload['item_id'], 'dsh_profile');
      expect(payload['option_id'], 'team-alpha');

      // 不覆盖时仍用 item.actionId。
      await service.sendAgentToolbarAction(
        sessionId: sessionId,
        toolbar: toolbar,
        item: profileItem,
        event: 'select',
        optionId: 'web',
      );
      payload =
          env.sink.packets.lastWhere(
                (p) => p['cmd'] == 'agent_toolbar_action',
              )['payload']
              as Map<String, dynamic>;
      expect(payload['action_id'], 'select_profile');
    },
  );
}
