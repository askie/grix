/// 技能同步状态（docs/architecture/39）：已同步到库 / 本地改过待更新 / 本地未上传。
/// 系统托管技能（[CommandItemModel.managed]）不带该状态，不可上传。
enum SkillSyncState { synced, modified, unsynced }

/// 技能库某一作用域的启用状态（方案 v2，connector 计算后透传）。
enum LibrarySkillScopeState {
  none,
  link,
  unmanaged,
  conflict,
  broken,
  blocked,
  unavailable,
}

LibrarySkillScopeState _librarySkillScopeStateFromJson(dynamic value) {
  switch (value?.toString().trim()) {
    case 'link':
      return LibrarySkillScopeState.link;
    case 'unmanaged':
      return LibrarySkillScopeState.unmanaged;
    case 'conflict':
      return LibrarySkillScopeState.conflict;
    case 'broken':
      return LibrarySkillScopeState.broken;
    case 'blocked':
      return LibrarySkillScopeState.blocked;
    case 'unavailable':
      return LibrarySkillScopeState.unavailable;
    case 'none':
    default:
      return LibrarySkillScopeState.none;
  }
}

/// 工具栏透传的技能库一项（~/.grix/skills 同步副本 + 启用态）。
class LibrarySkillModel {
  const LibrarySkillModel({
    required this.name,
    this.description = '',
    this.digest = '',
    this.dir = '',
    this.ownerId = '',
    this.system = false,
    this.globalScope = LibrarySkillScopeState.none,
    this.projectScope = LibrarySkillScopeState.none,
  });

  final String name;
  final String description;
  final String digest;
  final String dir;
  final String ownerId;
  final bool system;
  final LibrarySkillScopeState globalScope;
  final LibrarySkillScopeState projectScope;

  bool get isSystem => system || ownerId == '0';

  bool get projectAvailable =>
      projectScope != LibrarySkillScopeState.unavailable;

  /// 当前 Agent 无原生 skills 主根（如 Hermes/Copilot mode=none）：两端均为 unavailable。
  bool get enableUnsupported =>
      globalScope == LibrarySkillScopeState.unavailable &&
      projectScope == LibrarySkillScopeState.unavailable;

  bool get canEnableGlobal =>
      !isSystem &&
      !enableUnsupported &&
      globalScope != LibrarySkillScopeState.blocked &&
      globalScope != LibrarySkillScopeState.unavailable &&
      globalScope != LibrarySkillScopeState.link &&
      globalScope != LibrarySkillScopeState.conflict;

  bool get canEnableProject =>
      !isSystem &&
      !enableUnsupported &&
      projectAvailable &&
      projectScope != LibrarySkillScopeState.blocked &&
      projectScope != LibrarySkillScopeState.link &&
      projectScope != LibrarySkillScopeState.conflict;

  bool get canDisableGlobal =>
      globalScope == LibrarySkillScopeState.link ||
      globalScope == LibrarySkillScopeState.broken;

  bool get canDisableProject =>
      projectScope == LibrarySkillScopeState.link ||
      projectScope == LibrarySkillScopeState.broken;

  factory LibrarySkillModel.fromJson(Map<String, dynamic> json) {
    final scopes = json['enable_scopes'];
    final scopeMap = scopes is Map
        ? Map<String, dynamic>.from(scopes)
        : const <String, dynamic>{};
    return LibrarySkillModel(
      name: json['name']?.toString().trim() ?? '',
      description: json['description']?.toString().trim() ?? '',
      digest: json['digest']?.toString().trim() ?? '',
      dir: json['dir']?.toString().trim() ?? '',
      ownerId: json['owner_id']?.toString().trim() ?? '',
      system: json['system'] == true,
      globalScope: _librarySkillScopeStateFromJson(scopeMap['global']),
      projectScope: _librarySkillScopeStateFromJson(scopeMap['project']),
    );
  }

  bool hasSameContent(LibrarySkillModel other) {
    return name == other.name &&
        description == other.description &&
        digest == other.digest &&
        dir == other.dir &&
        ownerId == other.ownerId &&
        system == other.system &&
        globalScope == other.globalScope &&
        projectScope == other.projectScope;
  }
}

SkillSyncState? _skillSyncStateFromJson(dynamic value) {
  switch (value?.toString().trim()) {
    case 'synced':
      return SkillSyncState.synced;
    case 'modified':
      return SkillSyncState.modified;
    case 'unsynced':
      return SkillSyncState.unsynced;
    default:
      return null;
  }
}

class ToggleItemModel {
  const ToggleItemModel({
    required this.id,
    required this.name,
    this.version = '',
    this.enabled = false,
    this.locked = false,
    this.lockReason = '',
  });

  final String id;
  final String name;
  final String version;
  final bool enabled;
  final bool locked;
  final String lockReason;

  factory ToggleItemModel.fromJson(Map<String, dynamic> json) {
    return ToggleItemModel(
      id: json['id']?.toString().trim() ?? '',
      name: json['name']?.toString().trim() ?? '',
      version: json['version']?.toString().trim() ?? '',
      enabled: json['enabled'] == true,
      locked: json['locked'] == true,
      lockReason: json['lock_reason']?.toString().trim() ?? '',
    );
  }
}

class CommandItemModel {
  const CommandItemModel({
    required this.id,
    required this.name,
    required this.description,
    required this.exec,
    this.managed = false,
    this.syncState,
  });

  final String id;
  final String name;
  final String description;
  final String exec;
  final bool managed;
  final SkillSyncState? syncState;

  /// 系统托管技能（connector 投影/装的插件/CLI 系统缓存）一律不可一键上传。
  bool get canUpload => !managed && syncState != null;

  factory CommandItemModel.fromJson(Map<String, dynamic> json) {
    return CommandItemModel(
      id: json['id']?.toString().trim() ?? '',
      name: json['name']?.toString().trim() ?? '',
      description: json['description']?.toString().trim() ?? '',
      exec: json['exec']?.toString().trim() ?? '',
      managed: json['managed'] == true,
      syncState: _skillSyncStateFromJson(json['sync_state']),
    );
  }

  bool hasSameContent(CommandItemModel other) {
    return id == other.id &&
        name == other.name &&
        description == other.description &&
        exec == other.exec &&
        managed == other.managed &&
        syncState == other.syncState;
  }
}

class AgentToolbarOptionModel {
  const AgentToolbarOptionModel({
    required this.optionId,
    required this.label,
    required this.disabled,
  });

  final String optionId;
  final String label;
  final bool disabled;

  factory AgentToolbarOptionModel.fromJson(Map<String, dynamic> json) {
    return AgentToolbarOptionModel(
      optionId: json['option_id']?.toString().trim() ?? '',
      label: json['label']?.toString().trim() ?? '',
      disabled: json['disabled'] == true,
    );
  }

  bool hasSameContent(AgentToolbarOptionModel other) {
    return optionId == other.optionId &&
        label == other.label &&
        disabled == other.disabled;
  }
}

class AgentToolbarItemModel {
  const AgentToolbarItemModel({
    required this.itemId,
    required this.groupId,
    required this.kind,
    required this.actionId,
    required this.label,
    required this.icon,
    required this.variant,
    required this.disabled,
    required this.loading,
    required this.selected,
    required this.tooltip,
    required this.badgeText,
    required this.confirmTitle,
    required this.confirmText,
    required this.value,
    required this.placeholder,
    required this.options,
    required this.percent,
    required this.centerText,
    required this.progressDesc,
    required this.progressDetail,
    this.localAction = '',
    this.commands = const <CommandItemModel>[],
    this.toggles = const <ToggleItemModel>[],
  });

  final String itemId;
  final String groupId;
  final String kind;
  final String actionId;
  final String label;
  final String icon;
  final String variant;
  final bool disabled;
  final bool loading;
  final bool selected;
  final String tooltip;
  final String badgeText;
  final String confirmTitle;
  final String confirmText;
  final String value;
  final String placeholder;
  final List<AgentToolbarOptionModel> options;

  // Progress-specific fields (kind == 'progress')
  final double percent;
  final String centerText;
  final String progressDesc;
  final String progressDetail;

  // Client-side local action fields
  final String localAction;
  final List<CommandItemModel> commands;
  final List<ToggleItemModel> toggles;

  bool get isButton => kind == 'button';
  bool get isSelect => kind == 'select';
  bool get isProgress => kind == 'progress';
  bool get isClientCommandList => localAction == 'client:command_list';
  bool get isClientToggleList =>
      localAction == 'client:toggle_list' || kind == 'toggle_list';

  /// 「技能」命令列表（区别于 `slash_commands`）：技能弹窗才附带技能库 Tab，
  /// 命令弹窗只展示命令本身。
  bool get isSkillsCommandList =>
      isClientCommandList && (itemId == 'skills' || actionId == 'skills');

  factory AgentToolbarItemModel.fromJson(Map<String, dynamic> json) {
    final rawOptions = json['options'];
    final rawCommands = json['commands'];
    final rawToggles = json['toggles'];
    return AgentToolbarItemModel(
      itemId: json['item_id']?.toString().trim() ?? '',
      groupId: json['group_id']?.toString().trim() ?? '',
      kind: json['kind']?.toString().trim() ?? '',
      actionId: json['action_id']?.toString().trim() ?? '',
      label: json['label']?.toString().trim() ?? '',
      icon: json['icon']?.toString().trim() ?? '',
      variant: json['variant']?.toString().trim() ?? '',
      disabled: json['disabled'] == true,
      loading: json['loading'] == true,
      selected: json['selected'] == true,
      tooltip: json['tooltip']?.toString().trim() ?? '',
      badgeText: json['badge_text']?.toString().trim() ?? '',
      confirmTitle: json['confirm_title']?.toString().trim() ?? '',
      confirmText: json['confirm_text']?.toString().trim() ?? '',
      value: json['value']?.toString().trim() ?? '',
      placeholder: json['placeholder']?.toString().trim() ?? '',
      options:
          (rawOptions as List?)
              ?.map(
                (item) => AgentToolbarOptionModel.fromJson(
                  Map<String, dynamic>.from(item as Map),
                ),
              )
              .toList() ??
          const <AgentToolbarOptionModel>[],
      percent: _readToolbarDouble(json['percent']),
      centerText: json['center_text']?.toString().trim() ?? '',
      progressDesc: json['progress_desc']?.toString().trim() ?? '',
      progressDetail: json['progress_detail']?.toString().trim() ?? '',
      localAction: json['local_action']?.toString().trim() ?? '',
      commands:
          (rawCommands as List?)
              ?.map(
                (item) => CommandItemModel.fromJson(
                  Map<String, dynamic>.from(item as Map),
                ),
              )
              .toList() ??
          const <CommandItemModel>[],
      toggles:
          (rawToggles as List?)
              ?.map(
                (item) => ToggleItemModel.fromJson(
                  Map<String, dynamic>.from(item as Map),
                ),
              )
              .toList() ??
          const <ToggleItemModel>[],
    );
  }

  AgentToolbarItemModel copyWith({
    String? label,
    String? badgeText,
    bool? disabled,
    bool? loading,
    bool? selected,
    String? value,
    double? percent,
    String? centerText,
    String? progressDesc,
    String? progressDetail,
  }) {
    return AgentToolbarItemModel(
      itemId: itemId,
      groupId: groupId,
      kind: kind,
      actionId: actionId,
      label: label ?? this.label,
      icon: icon,
      variant: variant,
      disabled: disabled ?? this.disabled,
      loading: loading ?? this.loading,
      selected: selected ?? this.selected,
      tooltip: tooltip,
      badgeText: badgeText ?? this.badgeText,
      confirmTitle: confirmTitle,
      confirmText: confirmText,
      value: value ?? this.value,
      placeholder: placeholder,
      options: options,
      percent: percent ?? this.percent,
      centerText: centerText ?? this.centerText,
      progressDesc: progressDesc ?? this.progressDesc,
      progressDetail: progressDetail ?? this.progressDetail,
      localAction: localAction,
      commands: commands,
      toggles: toggles,
    );
  }

  bool hasSameContent(AgentToolbarItemModel other) {
    return itemId == other.itemId &&
        groupId == other.groupId &&
        kind == other.kind &&
        actionId == other.actionId &&
        label == other.label &&
        icon == other.icon &&
        variant == other.variant &&
        disabled == other.disabled &&
        loading == other.loading &&
        selected == other.selected &&
        tooltip == other.tooltip &&
        badgeText == other.badgeText &&
        confirmTitle == other.confirmTitle &&
        confirmText == other.confirmText &&
        value == other.value &&
        placeholder == other.placeholder &&
        percent == other.percent &&
        centerText == other.centerText &&
        progressDesc == other.progressDesc &&
        progressDetail == other.progressDetail &&
        localAction == other.localAction &&
        _toolbarListsHaveSameContent(options, other.options) &&
        _toolbarListsHaveSameContent(commands, other.commands);
  }
}

class AgentToolbarModel {
  const AgentToolbarModel({
    required this.sessionId,
    required this.agentId,
    required this.toolbarId,
    required this.revision,
    required this.visible,
    required this.updatedAt,
    required this.items,
    this.librarySkills = const <LibrarySkillModel>[],
    this.auditEnabled,
  });

  final String sessionId;
  final String agentId;
  final String toolbarId;
  final int revision;
  final bool visible;
  final int updatedAt;
  final List<AgentToolbarItemModel> items;

  /// 技能库全集 + 各作用域启用状态（方案 v2，不受 visible 影响）。
  final List<LibrarySkillModel> librarySkills;

  /// 对话审计开关的服务端状态。null 表示后端不接管该场景（Feature Gate 未开 /
  /// 访客会话），非 null 时以服务端为准；前端不做本地持久化。
  final bool? auditEnabled;

  bool get hasVisibleItems => visible && items.isNotEmpty;

  /// 从工具栏快照取出「命令列表」类 item 的 commands。
  ///
  /// `skills` 与 `slash_commands` 都用 `local_action=client:command_list`，
  /// 且多数 agent 会把 `slash_commands` 插在 items 最前；刷新时若只取
  /// 「第一个 command_list」会把已启用技能列表错换成内置斜杠命令。
  /// [preferredItemId] 应传打开弹窗时点中的 item_id（如 `skills`）。
  List<CommandItemModel> commandListCommands({String preferredItemId = ''}) {
    final preferred = preferredItemId.trim();
    if (preferred.isNotEmpty) {
      for (final item in items) {
        if (item.isClientCommandList && item.itemId == preferred) {
          return item.commands;
        }
      }
    }
    for (final item in items) {
      if (item.isSkillsCommandList) {
        return item.commands;
      }
    }
    for (final item in items) {
      if (item.isClientCommandList) return item.commands;
    }
    return const <CommandItemModel>[];
  }

  factory AgentToolbarModel.empty(String sessionId) {
    return AgentToolbarModel(
      sessionId: sessionId.trim(),
      agentId: '',
      toolbarId: '',
      revision: 0,
      visible: false,
      updatedAt: 0,
      items: const <AgentToolbarItemModel>[],
      librarySkills: const <LibrarySkillModel>[],
    );
  }

  factory AgentToolbarModel.fromJson(Map<String, dynamic> json) {
    final rawItems = json['items'];
    final rawLibrary = json['library_skills'];
    return AgentToolbarModel(
      sessionId: json['session_id']?.toString().trim() ?? '',
      agentId: json['agent_id']?.toString().trim() ?? '',
      toolbarId: json['toolbar_id']?.toString().trim() ?? '',
      revision: _readToolbarInt(json['revision']),
      visible: json['visible'] == true,
      updatedAt: _readToolbarInt(json['updated_at']),
      items:
          (rawItems as List?)
              ?.map(
                (item) => AgentToolbarItemModel.fromJson(
                  Map<String, dynamic>.from(item as Map),
                ),
              )
              .toList() ??
          const <AgentToolbarItemModel>[],
      librarySkills:
          (rawLibrary as List?)
              ?.whereType<Map>()
              .map(
                (item) =>
                    LibrarySkillModel.fromJson(Map<String, dynamic>.from(item)),
              )
              .where((s) => s.name.isNotEmpty)
              .toList() ??
          const <LibrarySkillModel>[],
      auditEnabled: json['audit_enabled'] is bool
          ? json['audit_enabled'] as bool
          : null,
    );
  }

  bool hasSameContent(AgentToolbarModel other) {
    return sessionId == other.sessionId &&
        agentId == other.agentId &&
        toolbarId == other.toolbarId &&
        revision == other.revision &&
        visible == other.visible &&
        updatedAt == other.updatedAt &&
        auditEnabled == other.auditEnabled &&
        _toolbarListsHaveSameContent(items, other.items) &&
        _toolbarListsHaveSameContent(librarySkills, other.librarySkills);
  }
}

bool _toolbarListsHaveSameContent<T>(List<T> left, List<T> right) {
  if (identical(left, right)) {
    return true;
  }
  if (left.length != right.length) {
    return false;
  }
  for (var index = 0; index < left.length; index++) {
    final leftItem = left[index];
    final rightItem = right[index];
    if (leftItem is AgentToolbarItemModel &&
        rightItem is AgentToolbarItemModel) {
      if (!leftItem.hasSameContent(rightItem)) {
        return false;
      }
      continue;
    }
    if (leftItem is AgentToolbarOptionModel &&
        rightItem is AgentToolbarOptionModel) {
      if (!leftItem.hasSameContent(rightItem)) {
        return false;
      }
      continue;
    }
    if (leftItem is CommandItemModel && rightItem is CommandItemModel) {
      if (!leftItem.hasSameContent(rightItem)) {
        return false;
      }
      continue;
    }
    if (leftItem is LibrarySkillModel && rightItem is LibrarySkillModel) {
      if (!leftItem.hasSameContent(rightItem)) {
        return false;
      }
      continue;
    }
    if (leftItem != rightItem) {
      return false;
    }
  }
  return true;
}

int _readToolbarInt(dynamic value) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString().trim() ?? '') ?? 0;
}

double _readToolbarDouble(dynamic value) {
  if (value is double) return value;
  if (value is num) return value.toDouble();
  return double.tryParse(value?.toString().trim() ?? '') ?? 0.0;
}
