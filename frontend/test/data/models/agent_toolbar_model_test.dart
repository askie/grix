import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/agent_toolbar_model.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('skill command toggles are opt-in and participate in equality', () {
    final item = AgentToolbarItemModel.fromJson({
      'item_id': 'skills',
      'group_id': 'skills',
      'kind': 'button',
      'action_id': 'dsh_skills',
      'show_toggles': true,
      'toggles': [
        {'id': 'message-send', 'name': 'message-send', 'enabled': true},
      ],
    });
    final changed = AgentToolbarItemModel.fromJson({
      'item_id': 'skills',
      'group_id': 'skills',
      'kind': 'button',
      'action_id': 'dsh_skills',
      'show_toggles': true,
      'toggles': [
        {'id': 'message-send', 'name': 'message-send', 'enabled': false},
      ],
    });
    final defaultItem = AgentToolbarItemModel.fromJson({
      'item_id': 'skills',
      'group_id': 'skills',
      'kind': 'button',
      'action_id': 'skills',
    });

    expect(item.showToggles, isTrue);
    expect(item.toggles.single.enabled, isTrue);
    expect(item.hasSameContent(changed), isFalse);
    expect(defaultItem.showToggles, isFalse);
  });

  group('AgentToolbarItemModel progress fields', () {
    test('isProgress returns true when kind is progress', () {
      const item = AgentToolbarItemModel(
        itemId: 'usage_5h',
        groupId: 'usage',
        kind: 'progress',
        actionId: 'usage_5h',
        label: '',
        icon: '',
        variant: 'warning',
        disabled: false,
        loading: false,
        selected: false,
        tooltip: '',
        badgeText: '',
        confirmTitle: '',
        confirmText: '',
        value: '',
        placeholder: '',
        options: [],
        percent: 73.5,
        centerText: '5H',
        progressDesc: '5小时Token额度',
        progressDetail: '已用 73.5%',
      );

      expect(item.isProgress, isTrue);
      expect(item.isButton, isFalse);
      expect(item.isSelect, isFalse);
    });

    test('fromJson parses progress fields from snake_case JSON', () {
      final json = <String, dynamic>{
        'item_id': 'usage_7d',
        'group_id': 'usage',
        'kind': 'progress',
        'action_id': 'usage_7d',
        'label': '',
        'icon': '',
        'variant': 'primary',
        'disabled': false,
        'loading': false,
        'selected': false,
        'tooltip': '',
        'badge_text': '',
        'confirm_title': '',
        'confirm_text': '',
        'value': '',
        'placeholder': '',
        'options': [],
        'percent': 45.2,
        'center_text': '7D',
        'progress_desc': '7天Token额度',
        'progress_detail': '已用 45.2% · 剩余 3天18h',
        'progress_window_minutes': 10080,
      };

      final item = AgentToolbarItemModel.fromJson(json);

      expect(item.kind, 'progress');
      expect(item.isProgress, isTrue);
      expect(item.percent, 45.2);
      expect(item.centerText, '7D');
      expect(item.progressDesc, '7天Token额度');
      expect(item.progressDetail, '已用 45.2% · 剩余 3天18h');
      expect(item.progressWindowMinutes, 10080);
    });

    test('fromJson handles missing progress fields with defaults', () {
      final json = <String, dynamic>{
        'item_id': 'test',
        'group_id': '',
        'kind': 'button',
        'action_id': 'test',
        'label': 'Click',
        'icon': '',
        'variant': '',
        'disabled': false,
        'loading': false,
        'selected': false,
        'tooltip': '',
        'badge_text': '',
        'confirm_title': '',
        'confirm_text': '',
        'value': '',
        'placeholder': '',
        'options': [],
      };

      final item = AgentToolbarItemModel.fromJson(json);

      expect(item.isProgress, isFalse);
      expect(item.percent, 0.0);
      expect(item.centerText, '');
      expect(item.progressDesc, '');
      expect(item.progressDetail, '');
      expect(item.progressWindowMinutes, 0);
    });

    test('fromJson parses percent from int', () {
      final json = <String, dynamic>{
        'item_id': 't',
        'group_id': '',
        'kind': 'progress',
        'action_id': 't',
        'label': '',
        'icon': '',
        'variant': '',
        'disabled': false,
        'loading': false,
        'selected': false,
        'tooltip': '',
        'badge_text': '',
        'confirm_title': '',
        'confirm_text': '',
        'value': '',
        'placeholder': '',
        'options': [],
        'percent': 80,
      };

      final item = AgentToolbarItemModel.fromJson(json);
      expect(item.percent, 80.0);
    });

    test('fromJson parses percent from string', () {
      final json = <String, dynamic>{
        'item_id': 't',
        'group_id': '',
        'kind': 'progress',
        'action_id': 't',
        'label': '',
        'icon': '',
        'variant': '',
        'disabled': false,
        'loading': false,
        'selected': false,
        'tooltip': '',
        'badge_text': '',
        'confirm_title': '',
        'confirm_text': '',
        'value': '',
        'placeholder': '',
        'options': [],
        'percent': '62.8',
      };

      final item = AgentToolbarItemModel.fromJson(json);
      expect(item.percent, 62.8);
    });

    test('copyWith preserves progress fields when not overridden', () {
      const item = AgentToolbarItemModel(
        itemId: 't',
        groupId: '',
        kind: 'progress',
        actionId: 't',
        label: '',
        icon: '',
        variant: 'success',
        disabled: false,
        loading: false,
        selected: false,
        tooltip: '',
        badgeText: '',
        confirmTitle: '',
        confirmText: '',
        value: '',
        placeholder: '',
        options: [],
        percent: 50.0,
        centerText: '压缩',
        progressDesc: '会话上下文',
        progressDetail: '已用 50%',
      );

      final copied = item.copyWith(disabled: true);

      expect(copied.percent, 50.0);
      expect(copied.centerText, '压缩');
      expect(copied.progressDesc, '会话上下文');
      expect(copied.progressDetail, '已用 50%');
      expect(copied.disabled, isTrue);
    });

    test('copyWith overrides progress fields', () {
      const item = AgentToolbarItemModel(
        itemId: 't',
        groupId: '',
        kind: 'progress',
        actionId: 't',
        label: '',
        icon: '',
        variant: '',
        disabled: false,
        loading: false,
        selected: false,
        tooltip: '',
        badgeText: '',
        confirmTitle: '',
        confirmText: '',
        value: '',
        placeholder: '',
        options: [],
        percent: 30.0,
        centerText: '5H',
        progressDesc: '',
        progressDetail: '',
      );

      final updated = item.copyWith(
        percent: 90.0,
        centerText: '7D',
        progressDesc: '7天额度',
        progressDetail: '快满了',
      );

      expect(updated.percent, 90.0);
      expect(updated.centerText, '7D');
      expect(updated.progressDesc, '7天额度');
      expect(updated.progressDetail, '快满了');
    });

    test('hasSameContent returns false when progress fields differ', () {
      const a = AgentToolbarItemModel(
        itemId: 't',
        groupId: '',
        kind: 'progress',
        actionId: 't',
        label: '',
        icon: '',
        variant: '',
        disabled: false,
        loading: false,
        selected: false,
        tooltip: '',
        badgeText: '',
        confirmTitle: '',
        confirmText: '',
        value: '',
        placeholder: '',
        options: [],
        percent: 50.0,
        centerText: '5H',
        progressDesc: 'desc',
        progressDetail: 'detail',
      );

      final b = a.copyWith(percent: 60.0);
      expect(a.hasSameContent(b), isFalse);

      final c = a.copyWith(centerText: '7D');
      expect(a.hasSameContent(c), isFalse);

      final d = a.copyWith(progressDesc: 'other');
      expect(a.hasSameContent(d), isFalse);

      final e = a.copyWith(progressWindowMinutes: 300);
      expect(a.hasSameContent(e), isFalse);
    });

    test('hasSameContent returns true when all fields match', () {
      const a = AgentToolbarItemModel(
        itemId: 't',
        groupId: 'g',
        kind: 'progress',
        actionId: 't',
        label: '',
        icon: '',
        variant: 'warning',
        disabled: false,
        loading: false,
        selected: false,
        tooltip: '',
        badgeText: '',
        confirmTitle: '',
        confirmText: '',
        value: '',
        placeholder: '',
        options: [],
        percent: 73.5,
        centerText: '5H',
        progressDesc: '5H额度',
        progressDetail: '已用 73.5%',
      );

      const b = AgentToolbarItemModel(
        itemId: 't',
        groupId: 'g',
        kind: 'progress',
        actionId: 't',
        label: '',
        icon: '',
        variant: 'warning',
        disabled: false,
        loading: false,
        selected: false,
        tooltip: '',
        badgeText: '',
        confirmTitle: '',
        confirmText: '',
        value: '',
        placeholder: '',
        options: [],
        percent: 73.5,
        centerText: '5H',
        progressDesc: '5H额度',
        progressDetail: '已用 73.5%',
      );

      expect(a.hasSameContent(b), isTrue);
    });

    test('CJK centerText is preserved correctly', () {
      final json = <String, dynamic>{
        'item_id': 't',
        'group_id': '',
        'kind': 'progress',
        'action_id': 't',
        'label': '',
        'icon': '',
        'variant': '',
        'disabled': false,
        'loading': false,
        'selected': false,
        'tooltip': '',
        'badge_text': '',
        'confirm_title': '',
        'confirm_text': '',
        'value': '',
        'placeholder': '',
        'options': [],
        'percent': 60.0,
        'center_text': '压缩',
        'progress_desc': '会话压缩空间',
        'progress_detail': '剩余 40%',
      };

      final item = AgentToolbarItemModel.fromJson(json);
      expect(item.centerText, '压缩');
      expect(item.centerText.length, 2);
    });
  });

  group('CommandItemModel skill sync state', () {
    test('fromJson parses managed and sync_state', () {
      final cmd = CommandItemModel.fromJson({
        'id': 'a',
        'name': 'a',
        'description': 'd',
        'exec': 'a',
        'managed': true,
        'sync_state': 'synced',
      });
      expect(cmd.managed, true);
      expect(cmd.syncState, SkillSyncState.synced);
    });

    test('fromJson defaults managed=false and syncState=null when absent', () {
      final cmd = CommandItemModel.fromJson({
        'id': 'a',
        'name': 'a',
        'description': '',
        'exec': 'a',
      });
      expect(cmd.managed, false);
      expect(cmd.syncState, isNull);
    });

    test('fromJson ignores unknown sync_state values', () {
      final cmd = CommandItemModel.fromJson({
        'id': 'a',
        'name': 'a',
        'description': '',
        'exec': 'a',
        'sync_state': 'garbage',
      });
      expect(cmd.syncState, isNull);
    });

    test('canDelete is false for managed skills', () {
      const cmd = CommandItemModel(
        id: 'a',
        name: 'a',
        description: '',
        exec: 'a',
        managed: true,
      );
      expect(cmd.canDelete, isFalse);
    });

    test('canDelete is true for non-managed skills', () {
      const cmd = CommandItemModel(
        id: 'a',
        name: 'a',
        description: '',
        exec: 'a',
        managed: false,
      );
      expect(cmd.canDelete, isTrue);
    });

    test('canUpload is false for managed skills regardless of sync_state', () {
      const cmd = CommandItemModel(
        id: 'a',
        name: 'a',
        description: '',
        exec: 'a',
        managed: true,
        syncState: SkillSyncState.unsynced,
      );
      expect(cmd.canUpload, false);
    });

    test('canUpload is false when syncState is null (unknown/unparsed)', () {
      const cmd = CommandItemModel(
        id: 'a',
        name: 'a',
        description: '',
        exec: 'a',
        managed: false,
        syncState: null,
      );
      expect(cmd.canUpload, false);
    });

    test(
      'canUpload is true for a non-managed skill with a known sync state',
      () {
        const cmd = CommandItemModel(
          id: 'a',
          name: 'a',
          description: '',
          exec: 'a',
          managed: false,
          syncState: SkillSyncState.modified,
        );
        expect(cmd.canUpload, true);
      },
    );

    test('fromJson parses source and path, project scope only for project', () {
      final project = CommandItemModel.fromJson({
        'id': 'a',
        'name': 'a',
        'description': '',
        'exec': 'a',
        'source': 'project',
        'path': '.dsh/skills/a/SKILL.md',
      });
      final global = CommandItemModel.fromJson({
        'id': 'b',
        'name': 'b',
        'description': '',
        'exec': 'b',
        'source': 'global',
        'path': '~/.dsh/skills/b/SKILL.md',
      });
      final legacy = CommandItemModel.fromJson({
        'id': 'c',
        'name': 'c',
        'description': '',
        'exec': 'c',
      });

      expect(project.isProjectScope, isTrue);
      expect(project.path, '.dsh/skills/a/SKILL.md');
      expect(global.isProjectScope, isFalse);
      expect(global.path, '~/.dsh/skills/b/SKILL.md');
      // 旧 connector 不带这两个字段：降级为空，UI 不显示路径行。
      expect(legacy.source, '');
      expect(legacy.path, '');
      expect(legacy.isProjectScope, isFalse);
    });

    test('hasSameContent compares source and path', () {
      const a = CommandItemModel(
        id: 'x',
        name: 'x',
        description: '',
        exec: 'x',
        source: 'global',
        path: '~/.dsh/skills/x/SKILL.md',
      );
      const movedPath = CommandItemModel(
        id: 'x',
        name: 'x',
        description: '',
        exec: 'x',
        source: 'global',
        path: '~/.agents/skills/x/SKILL.md',
      );
      const movedScope = CommandItemModel(
        id: 'x',
        name: 'x',
        description: '',
        exec: 'x',
        source: 'project',
        path: '~/.dsh/skills/x/SKILL.md',
      );
      expect(a.hasSameContent(movedPath), isFalse);
      expect(a.hasSameContent(movedScope), isFalse);
    });

    test('hasSameContent compares managed and syncState too', () {
      const a = CommandItemModel(
        id: 'x',
        name: 'x',
        description: '',
        exec: 'x',
        syncState: SkillSyncState.synced,
      );
      const b = CommandItemModel(
        id: 'x',
        name: 'x',
        description: '',
        exec: 'x',
        syncState: SkillSyncState.modified,
      );
      expect(a.hasSameContent(b), false);
    });
  });

  group('LibrarySkillModel', () {
    test('fromJson parses enable_scopes and owner_id', () {
      final skill = LibrarySkillModel.fromJson({
        'name': 'demo',
        'description': 'd',
        'digest': 'abc',
        'dir': 'demo',
        'owner_id': '42',
        'system': false,
        'enable_scopes': {'global': 'none', 'project': 'unavailable'},
      });
      expect(skill.name, 'demo');
      expect(skill.ownerId, '42');
      expect(skill.globalScope, LibrarySkillScopeState.none);
      expect(skill.projectScope, LibrarySkillScopeState.unavailable);
      expect(skill.projectAvailable, isFalse);
      expect(skill.canEnableGlobal, isTrue);
      expect(skill.canEnableProject, isFalse);
      expect(skill.enableUnsupported, isFalse);
    });

    test('owner_id 0 is treated as system and blocked from enable', () {
      final skill = LibrarySkillModel.fromJson({
        'name': 'grix-log',
        'owner_id': '0',
        'system': false,
        'enable_scopes': {'global': 'none', 'project': 'none'},
      });
      expect(skill.isSystem, isTrue);
      expect(skill.canEnableGlobal, isFalse);
      expect(skill.canEnableProject, isFalse);
    });

    test('both scopes unavailable means enableUnsupported (mode=none)', () {
      final skill = LibrarySkillModel.fromJson({
        'name': 'demo',
        'owner_id': '1',
        'enable_scopes': {'global': 'unavailable', 'project': 'unavailable'},
      });
      expect(skill.enableUnsupported, isTrue);
      expect(skill.canEnableGlobal, isFalse);
      expect(skill.canEnableProject, isFalse);
    });

    test('conflict cannot be enabled', () {
      final skill = LibrarySkillModel.fromJson({
        'name': 'demo',
        'owner_id': '1',
        'enable_scopes': {'global': 'conflict', 'project': 'conflict'},
      });
      expect(skill.canEnableGlobal, isFalse);
      expect(skill.canEnableProject, isFalse);
      expect(skill.enableUnsupported, isFalse);
    });

    test('canDisable includes broken links', () {
      final skill = LibrarySkillModel.fromJson({
        'name': 'demo',
        'owner_id': '1',
        'enable_scopes': {'global': 'broken', 'project': 'link'},
      });
      expect(skill.canDisableGlobal, isTrue);
      expect(skill.canDisableProject, isTrue);
      // broken 允许再次 enable（connector 会清悬空链再挂）
      expect(skill.canEnableGlobal, isTrue);
      expect(skill.canEnableProject, isFalse);
    });
  });

  group('AgentToolbarModel.commandListCommands', () {
    AgentToolbarItemModel commandListItem({
      required String itemId,
      required List<CommandItemModel> commands,
    }) {
      return AgentToolbarItemModel(
        itemId: itemId,
        groupId: itemId,
        kind: 'button',
        actionId: itemId,
        label: itemId,
        icon: '',
        variant: 'secondary',
        disabled: false,
        loading: false,
        selected: false,
        tooltip: '',
        badgeText: '',
        confirmTitle: '',
        confirmText: '',
        value: '',
        placeholder: '',
        options: const [],
        percent: 0,
        centerText: '',
        progressDesc: '',
        progressDetail: '',
        localAction: 'client:command_list',
        commands: commands,
      );
    }

    test('prefers preferredItemId over earlier slash_commands', () {
      const slashHelp = CommandItemModel(
        id: '/help',
        name: '/help',
        description: '显示可用命令列表',
        exec: '/help',
      );
      const skillEgg = CommandItemModel(
        id: 'egg-smoke',
        name: 'egg-smoke',
        description: '虾蛋联调',
        exec: 'egg-smoke',
      );
      final toolbar = AgentToolbarModel(
        sessionId: 's1',
        agentId: 'a1',
        toolbarId: 't1',
        revision: 1,
        visible: true,
        updatedAt: 1,
        // 与后端一致：slash_commands 排在 skills 前面
        items: [
          commandListItem(
            itemId: 'slash_commands',
            commands: const [slashHelp],
          ),
          commandListItem(itemId: 'skills', commands: const [skillEgg]),
        ],
      );

      final cmds = toolbar.commandListCommands(preferredItemId: 'skills');
      expect(cmds, hasLength(1));
      expect(cmds.single.name, 'egg-smoke');
    });

    test('falls back to skills when preferredItemId missing', () {
      const slashHelp = CommandItemModel(
        id: '/help',
        name: '/help',
        description: '显示可用命令列表',
        exec: '/help',
      );
      const skillEgg = CommandItemModel(
        id: 'egg-smoke',
        name: 'egg-smoke',
        description: '虾蛋联调',
        exec: 'egg-smoke',
      );
      final toolbar = AgentToolbarModel(
        sessionId: 's1',
        agentId: 'a1',
        toolbarId: 't1',
        revision: 1,
        visible: true,
        updatedAt: 1,
        items: [
          commandListItem(
            itemId: 'slash_commands',
            commands: const [slashHelp],
          ),
          commandListItem(itemId: 'skills', commands: const [skillEgg]),
        ],
      );

      final cmds = toolbar.commandListCommands();
      expect(cmds.single.name, 'egg-smoke');
    });

    test('keeps slash_commands when that item was opened', () {
      const slashHelp = CommandItemModel(
        id: '/help',
        name: '/help',
        description: '显示可用命令列表',
        exec: '/help',
      );
      const skillEgg = CommandItemModel(
        id: 'egg-smoke',
        name: 'egg-smoke',
        description: '虾蛋联调',
        exec: 'egg-smoke',
      );
      final toolbar = AgentToolbarModel(
        sessionId: 's1',
        agentId: 'a1',
        toolbarId: 't1',
        revision: 1,
        visible: true,
        updatedAt: 1,
        items: [
          commandListItem(
            itemId: 'slash_commands',
            commands: const [slashHelp],
          ),
          commandListItem(itemId: 'skills', commands: const [skillEgg]),
        ],
      );

      final cmds = toolbar.commandListCommands(
        preferredItemId: 'slash_commands',
      );
      expect(cmds.single.name, '/help');
    });
  });
  group('AgentToolbarModel audit_enabled', () {
    Map<String, dynamic> snapshotJson({bool? auditEnabled}) {
      final json = <String, dynamic>{
        'session_id': 's1',
        'agent_id': '8101',
        'toolbar_id': 't1',
        'revision': 1,
        'visible': true,
        'updated_at': 1,
        'items': <dynamic>[],
      };
      if (auditEnabled != null) {
        json['audit_enabled'] = auditEnabled;
      }
      return json;
    }

    test('parses audit_enabled when present and treats absence as null', () {
      expect(
        AgentToolbarModel.fromJson(
          snapshotJson(auditEnabled: true),
        ).auditEnabled,
        isTrue,
      );
      expect(
        AgentToolbarModel.fromJson(
          snapshotJson(auditEnabled: false),
        ).auditEnabled,
        isFalse,
      );
      // 旧后端不下发该字段：保持 null，前端按本地回退。
      expect(AgentToolbarModel.fromJson(snapshotJson()).auditEnabled, isNull);
    });

    test('hasSameContent compares audit_enabled', () {
      final enabled = AgentToolbarModel.fromJson(
        snapshotJson(auditEnabled: true),
      );
      final disabled = AgentToolbarModel.fromJson(
        snapshotJson(auditEnabled: false),
      );
      final absent = AgentToolbarModel.fromJson(snapshotJson());
      expect(enabled.hasSameContent(disabled), isFalse);
      expect(enabled.hasSameContent(absent), isFalse);
      expect(
        enabled.hasSameContent(
          AgentToolbarModel.fromJson(snapshotJson(auditEnabled: true)),
        ),
        isTrue,
      );
    });
  });
}
