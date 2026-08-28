part of 'chat_view.dart';

Widget buildChatRoundControlButton({
  required IconData icon,
  required VoidCallback onTap,
}) {
  return Material(
    color: Colors.transparent,
    child: InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(6),
      child: Container(
        width: 28,
        height: 28,
        alignment: Alignment.center,
        decoration: BoxDecoration(
          border: Border.all(color: Colors.black.withValues(alpha: 0.08)),
          borderRadius: BorderRadius.circular(6),
        ),
        child: Icon(icon, size: 16),
      ),
    ),
  );
}

Widget buildChatBottomSheetFrame(
  BuildContext context, {
  required double maxHeightFactor,
  required Widget child,
}) {
  final keyboardHeight = MediaQuery.of(context).viewInsets.bottom;
  return SafeArea(
    top: false,
    child: ConstrainedBox(
      constraints: BoxConstraints(
        maxHeight: MediaQuery.of(context).size.height * maxHeightFactor,
      ),
      child: SizedBox(
        width: double.infinity,
        child: Padding(
          padding: EdgeInsets.only(top: 8, bottom: 8 + keyboardHeight),
          child: child,
        ),
      ),
    ),
  );
}

/// 工具栏 select 选项超过该数量时改走可滚动 BottomSheet（避免 PopupMenu 撑满屏幕裁切）。
const int kChatToolbarSelectSheetMinOptions = 8;

@visibleForTesting
bool chatToolbarSelectUsesSheet(int optionCount) =>
    optionCount >= kChatToolbarSelectSheetMinOptions;

class _ChatSheetSearchField extends StatelessWidget {
  const _ChatSheetSearchField({
    required this.controller,
    required this.keyword,
    required this.onChanged,
    required this.onClear,
  });

  final TextEditingController controller;
  final String keyword;
  final ValueChanged<String> onChanged;
  final VoidCallback onClear;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 4),
      child: TextField(
        key: const Key('chat_sheet_search_field'),
        controller: controller,
        onChanged: onChanged,
        textInputAction: TextInputAction.search,
        style: const TextStyle(fontSize: 14),
        decoration: InputDecoration(
          isDense: true,
          prefixIcon: const Icon(Icons.search, size: 18),
          prefixIconConstraints: const BoxConstraints(
            minWidth: 36,
            minHeight: 36,
          ),
          suffixIcon: keyword.isEmpty
              ? null
              : IconButton(
                  icon: const Icon(Icons.close, size: 18),
                  splashRadius: 18,
                  onPressed: onClear,
                ),
          hintText: 'chat_search_keyword_hint'.tr,
          hintStyle: TextStyle(
            fontSize: 14,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.4),
          ),
          contentPadding: const EdgeInsets.symmetric(vertical: 10),
          filled: true,
          fillColor: theme.colorScheme.onSurface.withValues(alpha: 0.04),
          border: OutlineInputBorder(
            borderRadius: BorderRadius.circular(10),
            borderSide: BorderSide.none,
          ),
        ),
      ),
    );
  }
}

/// 选择器「新建」伪选项 id（后端约定，例如 deepseek createProfileOptionID）。
/// 命中后弹输入框，名字作为 option_id，以 create_profile action 发回。
const String kChatToolbarCreateProfileOptionId = '__create__';

/// 「新建 Profile」名字输入对话框。返回 trim 后的名字；取消或空名字返回 null。
Future<String?> showChatToolbarCreateProfileDialog({
  required BuildContext context,
}) {
  return showAppDialog<String>(
    context: context,
    builder: (_) => const _ChatToolbarCreateProfileDialog(),
  );
}

class _ChatToolbarCreateProfileDialog extends StatefulWidget {
  const _ChatToolbarCreateProfileDialog();

  @override
  State<_ChatToolbarCreateProfileDialog> createState() =>
      _ChatToolbarCreateProfileDialogState();
}

class _ChatToolbarCreateProfileDialogState
    extends State<_ChatToolbarCreateProfileDialog> {
  final TextEditingController _nameController = TextEditingController();
  bool _showEmptyError = false;

  @override
  void dispose() {
    _nameController.dispose();
    super.dispose();
  }

  void _submit() {
    final name = _nameController.text.trim();
    if (name.isEmpty) {
      setState(() => _showEmptyError = true);
      return;
    }
    Navigator.of(context).pop(name);
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      scrollable: true,
      title: Text('chat_toolbar_profile_create_title'.tr),
      content: SizedBox(
        width: 420,
        child: TextField(
          controller: _nameController,
          autofocus: true,
          maxLength: 64,
          decoration: InputDecoration(
            hintText: 'chat_toolbar_profile_create_hint'.tr,
            errorText: _showEmptyError
                ? 'chat_toolbar_profile_create_invalid'.tr
                : null,
          ),
          onChanged: (_) {
            if (_showEmptyError) {
              setState(() => _showEmptyError = false);
            }
          },
          onSubmitted: (_) => _submit(),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: Text('common_cancel'.tr),
        ),
        FilledButton(onPressed: _submit, child: Text('common_confirm'.tr)),
      ],
    );
  }
}

Future<void> showChatToolbarSelectSheet({
  required BuildContext context,
  required String title,
  required List<AgentToolbarOptionModel> options,
  required String currentValue,
  required ValueChanged<String> onSelected,
}) {
  return SheetGuard.run<void>(
    'chat_toolbar_select_sheet',
    () => showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      constraints: const BoxConstraints(maxWidth: 400),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) => _ChatToolbarSelectSheet(
        title: title,
        options: options,
        currentValue: currentValue,
        onSelected: (optionId) {
          if (!popSheetOnce(sheetContext)) return;
          onSelected(optionId);
        },
      ),
    ),
  ).then((_) {});
}

class _ChatToolbarSelectSheet extends StatefulWidget {
  const _ChatToolbarSelectSheet({
    required this.title,
    required this.options,
    required this.currentValue,
    required this.onSelected,
  });

  final String title;
  final List<AgentToolbarOptionModel> options;
  final String currentValue;
  final ValueChanged<String> onSelected;

  @override
  State<_ChatToolbarSelectSheet> createState() =>
      _ChatToolbarSelectSheetState();
}

class _ChatToolbarSelectSheetState extends State<_ChatToolbarSelectSheet> {
  final TextEditingController _searchController = TextEditingController();
  String _keyword = '';

  List<AgentToolbarOptionModel> get _visibleOptions {
    if (_keyword.isEmpty) return widget.options;
    final kw = _keyword.toLowerCase();
    return widget.options
        .where(
          (option) =>
              option.label.toLowerCase().contains(kw) ||
              option.optionId.toLowerCase().contains(kw),
        )
        .toList();
  }

  bool _isCurrent(AgentToolbarOptionModel option) {
    final current = widget.currentValue.trim().toLowerCase();
    if (current.isEmpty) return false;
    return current == option.optionId.trim().toLowerCase() ||
        current == option.label.trim().toLowerCase();
  }

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final options = _visibleOptions;
    return Container(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: buildChatBottomSheetFrame(
        context,
        maxHeightFactor: 0.72,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            buildChatBottomSheetHandle(context),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
              child: Text(
                widget.title,
                style: const TextStyle(
                  fontSize: 15,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
            _ChatSheetSearchField(
              controller: _searchController,
              keyword: _keyword,
              onChanged: (value) => setState(() => _keyword = value.trim()),
              onClear: () {
                _searchController.clear();
                setState(() => _keyword = '');
              },
            ),
            const SizedBox(height: 4),
            Flexible(
              child: options.isEmpty
                  ? Padding(
                      padding: const EdgeInsets.symmetric(vertical: 32),
                      child: Text(
                        'chat_skill_search_empty'.tr,
                        style: TextStyle(
                          fontSize: 13,
                          color: theme.colorScheme.onSurface.withValues(
                            alpha: 0.6,
                          ),
                        ),
                      ),
                    )
                  : ListView.builder(
                      shrinkWrap: true,
                      itemCount: options.length,
                      itemBuilder: (context, index) {
                        final option = options[index];
                        final isCurrent = _isCurrent(option);
                        return ListTile(
                          enabled: !option.disabled,
                          leading: Icon(
                            isCurrent
                                ? Icons.check_circle_rounded
                                : Icons.circle_outlined,
                            size: 18,
                            color: option.disabled
                                ? theme.disabledColor
                                : isCurrent
                                ? theme.colorScheme.primary
                                : theme.colorScheme.secondary.withValues(
                                    alpha: 0.52,
                                  ),
                          ),
                          title: Text(
                            option.label,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              fontSize: 14,
                              fontWeight: isCurrent
                                  ? FontWeight.w600
                                  : FontWeight.w500,
                              color: option.disabled
                                  ? theme.disabledColor
                                  : theme.colorScheme.onSurface,
                            ),
                          ),
                          onTap: option.disabled
                              ? null
                              : () => widget.onSelected(option.optionId),
                        );
                      },
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

Widget buildChatBottomSheetHandle(BuildContext context) {
  return Container(
    width: 36,
    height: 4,
    margin: const EdgeInsets.only(bottom: 12),
    decoration: BoxDecoration(
      color: Theme.of(context).colorScheme.outline.withValues(alpha: 0.3),
      borderRadius: BorderRadius.circular(2),
    ),
  );
}

/// 技能在“最近使用”记录中的去重 key：优先用执行命令，其次 id，最后名称。
String chatSkillUsageKey(CommandItemModel cmd) {
  if (cmd.exec.isNotEmpty) return cmd.exec;
  if (cmd.id.isNotEmpty) return cmd.id;
  return cmd.name;
}

void showChatCommandListSheet(
  BuildContext context, {
  required String title,
  required List<CommandItemModel> commands,
  required ValueChanged<CommandItemModel> onSelected,
  String? agentId,
  String? sessionId,
  ImService? imService,

  /// 打开弹窗时点中的工具栏 item_id（`skills` / `slash_commands`），
  /// 下拉刷新后用来从快照里取回同一份列表，避免误取另一个 command_list。
  String commandListItemId = '',
  List<LibrarySkillModel> librarySkills = const <LibrarySkillModel>[],
  ValueChanged<String>? onLibrarySkillInserted,
  AgentToolbarItemModel? toolbarItem,
  AgentToolbarModel? toolbar,

  /// 是否附带「已启用 / 技能库」Tab（技能弹窗专用）。命令弹窗传 false，
  /// 只展示命令列表，避免与技能弹窗重复。
  bool showSkillLibrary = false,
}) {
  showModalBottomSheet(
    context: context,
    isScrollControlled: true,
    constraints: const BoxConstraints(maxWidth: 400),
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
    ),
    builder: (sheetContext) => _ChatCommandListSheet(
      title: title,
      commands: commands,
      librarySkills: librarySkills,
      commandListItemId: commandListItemId,
      agentId: agentId,
      sessionId: sessionId,
      imService: imService,
      onLibrarySkillInserted: onLibrarySkillInserted,
      toolbarItem: toolbarItem,
      toolbar: toolbar,
      showSkillLibrary: showSkillLibrary,
      onSelected: (cmd) {
        // 仅前端记录最近使用，下次打开时置顶。
        if (Get.isRegistered<ChatSkillUsageService>()) {
          Get.find<ChatSkillUsageService>().record(chatSkillUsageKey(cmd));
        }
        Navigator.of(sheetContext).pop();
        onSelected(cmd);
      },
    ),
  );
}

void showChatToggleListSheet(
  BuildContext context, {
  required AgentToolbarItemModel item,
  required AgentToolbarModel toolbar,
  required String sessionId,
  required ImService imService,
}) {
  showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    constraints: const BoxConstraints(maxWidth: 400),
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
    ),
    builder: (sheetContext) => _ChatToggleListSheet(
      itemId: item.itemId,
      sessionId: sessionId,
      toolbar: toolbar,
      imService: imService,
    ),
  );
}

class _ChatToggleListSheet extends StatelessWidget {
  const _ChatToggleListSheet({
    required this.itemId,
    required this.sessionId,
    required this.toolbar,
    required this.imService,
  });

  final String itemId;
  final String sessionId;
  final AgentToolbarModel toolbar;
  final ImService imService;

  AgentToolbarItemModel? _currentItem() {
    final snapshot = imService.getAgentToolbar(sessionId) ?? toolbar;
    for (final candidate in snapshot.items) {
      if (candidate.itemId == itemId) return candidate;
    }
    return null;
  }

  Future<void> _send(String event, {String optionId = ''}) {
    final item = _currentItem();
    if (item == null) return Future.value();
    return imService.sendAgentToolbarAction(
      sessionId: sessionId,
      toolbar: imService.getAgentToolbar(sessionId) ?? toolbar,
      item: item,
      event: event,
      optionId: optionId,
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
        child: Obx(() {
          final item = _currentItem();
          final toggles = item?.toggles ?? const <ToggleItemModel>[];
          final restartRequired = (item?.value ?? '') == 'restart_required';
          return Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      'chat_toolbar_plugins'.tr,
                      style: theme.textTheme.titleMedium,
                    ),
                  ),
                  IconButton(
                    tooltip: 'common_refresh'.tr,
                    onPressed: () => _send('refresh'),
                    icon: const Icon(Icons.refresh, size: 20),
                  ),
                ],
              ),
              if (restartRequired)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Text(
                    'chat_toolbar_restart_required'.tr,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.error,
                    ),
                  ),
                ),
              if (toggles.isEmpty)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 24),
                  child: Text(
                    'chat_toolbar_no_plugins'.tr,
                    style: theme.textTheme.bodyMedium,
                  ),
                )
              else
                ConstrainedBox(
                  constraints: BoxConstraints(
                    maxHeight: MediaQuery.of(context).size.height * 0.5,
                  ),
                  child: ListView.separated(
                    shrinkWrap: true,
                    itemCount: toggles.length,
                    separatorBuilder: (_, __) => const Divider(height: 1),
                    itemBuilder: (context, index) {
                      final toggle = toggles[index];
                      final subtitle = [
                        if (toggle.version.isNotEmpty) toggle.version,
                        if (toggle.locked && toggle.lockReason.isNotEmpty)
                          toggle.lockReason,
                      ].join(' · ');
                      return SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        title: Text(toggle.name),
                        subtitle: subtitle.isEmpty ? null : Text(subtitle),
                        value: toggle.enabled,
                        onChanged: toggle.locked
                            ? null
                            : (value) => _send(
                                value ? 'enable' : 'disable',
                                optionId: toggle.id,
                              ),
                      );
                    },
                  ),
                ),
            ],
          );
        }),
      ),
    );
  }
}

/// 技能列表里的作用域分组标题行（与 [CommandItemModel] 混在同一份 rows 里渲染）。
class _SkillScopeHeader {
  const _SkillScopeHeader({
    required this.label,
    required this.count,
    required this.project,
  });

  final String label;
  final int count;
  final bool project;
}

class _ChatCommandListSheet extends StatefulWidget {
  const _ChatCommandListSheet({
    required this.title,
    required this.commands,
    required this.onSelected,
    this.librarySkills = const <LibrarySkillModel>[],
    this.commandListItemId = '',
    this.agentId,
    this.sessionId,
    this.imService,
    this.onLibrarySkillInserted,
    this.showSkillLibrary = false,
    this.toolbarItem,
    this.toolbar,
  });

  final String title;
  final List<CommandItemModel> commands;
  final List<LibrarySkillModel> librarySkills;
  final String commandListItemId;
  final ValueChanged<CommandItemModel> onSelected;
  final String? agentId;
  final String? sessionId;
  final ImService? imService;
  final ValueChanged<String>? onLibrarySkillInserted;
  final AgentToolbarItemModel? toolbarItem;
  final AgentToolbarModel? toolbar;

  /// 是否展示「已启用 / 技能库」Tab。仅技能弹窗为 true；命令弹窗为
  /// false，只渲染命令列表（技能库入口与技能弹窗重复）。
  final bool showSkillLibrary;

  @override
  State<_ChatCommandListSheet> createState() => _ChatCommandListSheetState();
}

class _ChatCommandListSheetState extends State<_ChatCommandListSheet>
    with SingleTickerProviderStateMixin {
  final TextEditingController _searchController = TextEditingController();
  String _keyword = '';

  /// 仅技能弹窗（showSkillLibrary）需要 TabController。
  TabController? _tabController;

  /// 上传成功后本地即时标记为已同步，不等下一轮工具栏快照刷新才反馈。
  final Set<String> _justUploaded = {};
  final Set<String> _uploading = {};
  final Set<String> _deleting = {};
  final Set<String> _libraryBusy = {};
  final Set<String> _skillToggleBusy = {};

  /// 进入弹窗时固定一次最近使用排序，避免边用边跳动。
  /// 下拉刷新时按新清单重建一次（用户显式触发，不算"边用边跳动"）。
  late List<CommandItemModel> _orderedCommands;
  late List<LibrarySkillModel> _librarySkills = List<LibrarySkillModel>.from(
    widget.librarySkills,
  );
  late Map<String, ToggleItemModel> _skillToggles;
  late int _skillToolbarRevision;
  late final int _initialSkillToolbarRevision;
  late bool _sessionSkillTogglesEnabled;

  @override
  void initState() {
    super.initState();
    if (widget.showSkillLibrary) {
      _tabController = TabController(length: 2, vsync: this);
    }
    _orderedCommands = _buildOrderedCommands();
    _skillToggles = {
      for (final toggle
          in widget.toolbarItem?.toggles ?? const <ToggleItemModel>[])
        toggle.id: toggle,
    };
    _skillToolbarRevision = widget.toolbar?.revision ?? 0;
    _initialSkillToolbarRevision = _skillToolbarRevision;
    _sessionSkillTogglesEnabled = widget.toolbarItem?.showToggles == true;
  }

  bool get _showSkillToggles => _sessionSkillTogglesEnabled;

  AgentToolbarItemModel? _currentToolbarItem() {
    final sessionId = widget.sessionId;
    final imService = widget.imService;
    final fallback = widget.toolbarItem;
    if (sessionId == null || imService == null) return fallback;
    final snapshot = imService.getAgentToolbar(sessionId) ?? widget.toolbar;
    if (snapshot == null) return fallback;
    for (final item in snapshot.items) {
      if (item.itemId == widget.commandListItemId) return item;
    }
    if (snapshot.revision > _initialSkillToolbarRevision) return null;
    return fallback;
  }

  void _syncSessionSkillsFromToolbar() {
    final sessionId = widget.sessionId;
    final snapshot = sessionId == null
        ? widget.toolbar
        : widget.imService?.getAgentToolbar(sessionId) ?? widget.toolbar;
    if (snapshot == null || snapshot.revision <= _skillToolbarRevision) return;
    AgentToolbarItemModel? item;
    for (final candidate in snapshot.items) {
      if (candidate.itemId == widget.commandListItemId) {
        item = candidate;
        break;
      }
    }
    if (item == null || !item.showToggles) {
      _sessionSkillTogglesEnabled = false;
      _orderedCommands = item == null
          ? <CommandItemModel>[]
          : _buildOrderedCommands(item.commands);
      _skillToggles.clear();
      _skillToggleBusy.clear();
      _skillToolbarRevision = snapshot.revision;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
      return;
    }
    _sessionSkillTogglesEnabled = true;
    _orderedCommands = _buildOrderedCommands(item.commands);
    _skillToggles = {for (final toggle in item.toggles) toggle.id: toggle};
    _skillToggleBusy.clear();
    _skillToolbarRevision = snapshot.revision;
  }

  Future<void> _handleSessionSkillToggle(
    CommandItemModel command,
    bool enabled,
  ) async {
    final item = _currentToolbarItem();
    final sessionId = widget.sessionId;
    final toolbar = sessionId == null
        ? widget.toolbar
        : widget.imService?.getAgentToolbar(sessionId) ?? widget.toolbar;
    final toggle = _skillToggles[command.id];
    if (item == null || toolbar == null || toggle == null || toggle.locked) {
      return;
    }
    setState(() {
      _skillToggleBusy.add(command.id);
      _skillToggles[command.id] = ToggleItemModel(
        id: toggle.id,
        name: toggle.name,
        version: toggle.version,
        enabled: enabled,
        locked: toggle.locked,
        lockReason: toggle.lockReason,
      );
    });
    Timer? rebuildDeadlineTimer;
    try {
      final actionAck = Completer<bool>();
      final sent = await widget.imService!.sendAgentToolbarAction(
        sessionId: widget.sessionId!,
        toolbar: toolbar,
        item: item,
        event: enabled ? 'enable' : 'disable',
        optionId: command.id,
        onAck: (accepted) {
          if (!actionAck.isCompleted) actionAck.complete(accepted);
        },
      );
      if (!sent) throw StateError('agent toolbar action was not sent');
      final deadline = Completer<void>();
      rebuildDeadlineTimer = Timer(const Duration(seconds: 75), () {
        if (!deadline.isCompleted) deadline.complete();
      });
      final accepted = await Future.any<bool?>([
        actionAck.future,
        deadline.future.then<bool?>((_) => null),
      ]);
      if (!mounted || !_skillToggleBusy.contains(command.id)) {
        rebuildDeadlineTimer.cancel();
        return;
      }
      if (accepted != true) {
        rebuildDeadlineTimer.cancel();
        setState(() {
          _skillToggleBusy.remove(command.id);
          _skillToggles[command.id] = toggle;
        });
        return;
      }
      // Connector may need the full 60s Profile Bridge session/create window
      // before it can publish the authoritative toolbar revision. Keep the
      // optimistic state pending through that window instead of reverting a
      // successful switch early.
      await deadline.future;
      if (!mounted || !_skillToggleBusy.contains(command.id)) return;
      setState(() {
        _skillToggleBusy.remove(command.id);
        _skillToggles[command.id] = toggle;
      });
    } catch (e) {
      rebuildDeadlineTimer?.cancel();
      if (!mounted) return;
      setState(() {
        _skillToggleBusy.remove(command.id);
        _skillToggles[command.id] = toggle;
      });
      CustomToast.show(
        'chat_skill_session_toggle_failed'.trParams({
          'error': userFacingError(e),
        }),
        isError: true,
      );
    }
  }

  bool _canUploadItem(CommandItemModel cmd) {
    return widget.agentId != null &&
        widget.sessionId != null &&
        widget.imService != null &&
        cmd.canUpload &&
        !_justUploaded.contains(chatSkillUsageKey(cmd));
  }

  bool _canDeleteItem(CommandItemModel cmd) {
    return widget.showSkillLibrary &&
        widget.agentId != null &&
        widget.sessionId != null &&
        widget.imService != null &&
        cmd.canDelete;
  }

  bool get _canUseLibraryActions =>
      widget.agentId != null &&
      widget.sessionId != null &&
      widget.imService != null;

  void _recordSkillUsage(String key) {
    final normalized = key.trim();
    if (normalized.isEmpty) return;
    if (Get.isRegistered<ChatSkillUsageService>()) {
      Get.find<ChatSkillUsageService>().record(normalized);
    }
  }

  Future<void> _handleDelete(CommandItemModel cmd) async {
    final confirmed = await showAppConfirmDialog(
      context: context,
      title: 'chat_skill_delete_title'.trParams({'name': cmd.name}),
      message: 'chat_skill_delete_body'.trParams({
        'path': cmd.path.isNotEmpty ? cmd.path : cmd.name,
      }),
      cancelText: 'chat_skill_delete_cancel'.tr,
      confirmText: 'chat_skill_delete_confirm'.tr,
      isDestructive: true,
    );
    if (confirmed != true || !mounted) return;
    final key = chatSkillUsageKey(cmd);
    setState(() => _deleting.add(key));
    try {
      await widget.imService!.requestSkillDelete(
        agentId: widget.agentId!,
        sessionId: widget.sessionId!,
        name: cmd.name,
      );
    } catch (e) {
      if (!mounted) return;
      setState(() => _deleting.remove(key));
      CustomToast.show(
        'chat_skill_delete_failed'.trParams({'error': userFacingError(e)}),
        isError: true,
      );
      return;
    }
    if (!mounted) return;
    setState(() => _deleting.remove(key));
    CustomToast.show('chat_skill_delete_success'.trParams({'name': cmd.name}));
    // 磁盘已删干净，刷新只影响列表展示：单独兜住异常，否则会误报删除失败。
    try {
      await _reloadFromConnector();
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _orderedCommands = _orderedCommands
            .where((item) => item.id != cmd.id)
            .toList();
      });
      CustomToast.show(
        'chat_skill_refresh_failed'.trParams({'error': userFacingError(e)}),
        isError: true,
      );
    }
  }

  /// 让连接器重扫本地技能与技能库，用回执快照重建命令列表与技能库两个 Tab。
  /// 删除成功与下拉刷新共用，保证列表始终与磁盘真实技能集一致。
  Future<void> _reloadFromConnector() async {
    if (!_canUseLibraryActions) return;
    final toolbar = await widget.imService!.requestSkillRefresh(
      agentId: widget.agentId!,
      sessionId: widget.sessionId!,
    );
    if (!mounted) return;
    setState(() {
      _librarySkills = List<LibrarySkillModel>.from(toolbar.librarySkills);
      _orderedCommands = _buildOrderedCommands(
        toolbar.commandListCommands(
          preferredItemId: widget.commandListItemId,
        ),
      );
    });
  }

  Future<void> _handleUpload(CommandItemModel cmd) async {
    final key = chatSkillUsageKey(cmd);
    if (cmd.syncState == SkillSyncState.modified) {
      final confirmed = await showAppConfirmDialog(
        context: context,
        title: 'chat_skill_upload_overwrite_title'.trParams({'name': cmd.name}),
        message: 'chat_skill_upload_overwrite_body'.tr,
        cancelText: 'chat_skill_upload_cancel'.tr,
        confirmText: 'chat_skill_upload_confirm'.tr,
      );
      if (confirmed != true) return;
    }
    if (!mounted) return;
    setState(() => _uploading.add(key));
    try {
      await widget.imService!.requestSkillUpload(
        agentId: widget.agentId!,
        sessionId: widget.sessionId!,
        name: cmd.name,
      );
      if (!mounted) return;
      _recordSkillUsage(key);
      setState(() {
        _uploading.remove(key);
        _justUploaded.add(key);
        _orderedCommands = _buildOrderedCommands();
      });
      CustomToast.show(
        'chat_skill_upload_success'.trParams({'name': cmd.name}),
        isError: false,
      );
    } catch (e) {
      if (!mounted) return;
      setState(() => _uploading.remove(key));
      CustomToast.show(
        'chat_skill_upload_failed'.trParams({'error': userFacingError(e)}),
        isError: true,
      );
    }
  }

  List<CommandItemModel> _buildOrderedCommands([
    List<CommandItemModel>? source,
  ]) {
    final usage = Get.isRegistered<ChatSkillUsageService>()
        ? Get.find<ChatSkillUsageService>()
        : null;
    final indexed = (source ?? widget.commands)
        .asMap()
        .entries
        .map((e) => (cmd: e.value, origin: e.key))
        .toList();
    indexed.sort((a, b) {
      final rankA = usage?.rankOf(chatSkillUsageKey(a.cmd)) ?? 0;
      final rankB = usage?.rankOf(chatSkillUsageKey(b.cmd)) ?? 0;
      if (rankA != rankB) return rankA.compareTo(rankB);
      // 名次相同保持原始顺序，保证排序稳定。
      return a.origin.compareTo(b.origin);
    });
    return indexed.map((e) => e.cmd).toList();
  }

  List<CommandItemModel> get _visibleCommands {
    if (_keyword.isEmpty) return _orderedCommands;
    final kw = _keyword.toLowerCase();
    return _orderedCommands
        .where(
          (cmd) =>
              cmd.name.toLowerCase().contains(kw) ||
              cmd.description.toLowerCase().contains(kw) ||
              cmd.exec.toLowerCase().contains(kw),
        )
        .toList();
  }

  /// 已启用列表 trailing：上传（未同步时）+ 删除（非托管）。
  /// 系统托管技能不渲染任何操作按钮。
  Widget? _buildSkillSyncTrailing(CommandItemModel cmd) {
    final key = chatSkillUsageKey(cmd);
    if (_uploading.contains(key) || _deleting.contains(key)) {
      return const SizedBox(
        width: 16,
        height: 16,
        child: CircularProgressIndicator(strokeWidth: 2),
      );
    }
    final children = <Widget>[];
    if (!cmd.managed && cmd.syncState != null) {
      final synced =
          _justUploaded.contains(key) || cmd.syncState == SkillSyncState.synced;
      if (!synced && _canUploadItem(cmd)) {
        children.add(
          IconButton(
            icon: const Icon(Icons.cloud_upload_outlined, size: 18),
            tooltip: 'chat_skill_upload_tooltip'.tr,
            visualDensity: VisualDensity.compact,
            onPressed: () => _handleUpload(cmd),
          ),
        );
      }
    }
    if (_canDeleteItem(cmd)) {
      children.add(
        IconButton(
          icon: const Icon(Icons.delete_outline, size: 18),
          tooltip: 'chat_skill_delete_tooltip'.tr,
          visualDensity: VisualDensity.compact,
          onPressed: () => _handleDelete(cmd),
        ),
      );
    }
    if (children.isEmpty) return null;
    return Row(mainAxisSize: MainAxisSize.min, children: children);
  }

  Widget? _buildCommandTrailing(ThemeData theme, CommandItemModel command) {
    if (!_showSkillToggles) return _buildSkillSyncTrailing(command);
    final toggle = _skillToggles[command.id];
    if (toggle == null) return null;
    if (_skillToggleBusy.contains(command.id)) {
      return const SizedBox(
        width: 20,
        height: 20,
        child: CircularProgressIndicator(strokeWidth: 2),
      );
    }
    return Switch.adaptive(
      value: toggle.enabled,
      onChanged:
          toggle.locked ||
              _skillToggleBusy.isNotEmpty ||
              _currentToolbarItem()?.disabled == true
          ? null
          : (enabled) => _handleSessionSkillToggle(command, enabled),
    );
  }

  /// 下拉刷新：让 agent 的 connector/插件重扫本地技能与技能库，回执快照
  /// 一次性刷新两个 Tab；快照同时已喂回 imService.agentToolbars（见 downstream）。
  Future<void> _handleRefresh() async {
    if (!_canUseLibraryActions) return;
    try {
      if (_showSkillToggles) {
        final item = _currentToolbarItem();
        final toolbar =
            widget.imService!.getAgentToolbar(widget.sessionId!) ??
            widget.toolbar;
        if (item == null || toolbar == null) return;
        await widget.imService!.sendAgentToolbarAction(
          sessionId: widget.sessionId!,
          toolbar: toolbar,
          item: item,
          event: 'refresh',
        );
        return;
      }
      await _reloadFromConnector();
    } catch (e) {
      // 技能弹窗是 modal bottom sheet，页面级消息条经常挂不到可见
      // Scaffold；统一使用全局 CustomToast。
      CustomToast.show(
        'chat_skill_refresh_failed'.trParams({'error': userFacingError(e)}),
        isError: true,
      );
    }
  }

  /// 空态也包一层可滚动 ListView：保证空列表时下拉刷新手势仍可用。
  Widget _emptyTabView(String message, ThemeData theme) {
    return ListView(
      physics: const AlwaysScrollableScrollPhysics(),
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 32),
          child: Center(
            child: Text(
              message,
              textAlign: TextAlign.center,
              style: TextStyle(
                fontSize: 13,
                color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
              ),
            ),
          ),
        ),
      ],
    );
  }

  List<LibrarySkillModel> get _visibleLibrarySkills {
    if (_keyword.isEmpty) return _librarySkills;
    final kw = _keyword.toLowerCase();
    return _librarySkills
        .where(
          (s) =>
              s.name.toLowerCase().contains(kw) ||
              s.description.toLowerCase().contains(kw),
        )
        .toList();
  }

  String _scopeLabel(LibrarySkillScopeState state) {
    switch (state) {
      case LibrarySkillScopeState.link:
        return 'chat_skill_library_scope_link'.tr;
      case LibrarySkillScopeState.unmanaged:
        return 'chat_skill_library_scope_unmanaged'.tr;
      case LibrarySkillScopeState.conflict:
        return 'chat_skill_library_scope_conflict'.tr;
      case LibrarySkillScopeState.broken:
        return 'chat_skill_library_scope_broken'.tr;
      case LibrarySkillScopeState.blocked:
        return 'chat_skill_library_scope_blocked'.tr;
      case LibrarySkillScopeState.unavailable:
        return 'chat_skill_library_scope_unavailable'.tr;
      case LibrarySkillScopeState.none:
        return 'chat_skill_library_scope_none'.tr;
    }
  }

  LibrarySkillModel _copyLibrarySkill(
    LibrarySkillModel s, {
    LibrarySkillScopeState? globalScope,
    LibrarySkillScopeState? projectScope,
  }) {
    return LibrarySkillModel(
      name: s.name,
      description: s.description,
      digest: s.digest,
      dir: s.dir,
      ownerId: s.ownerId,
      system: s.system,
      globalScope: globalScope ?? s.globalScope,
      projectScope: projectScope ?? s.projectScope,
    );
  }

  void _insertLibrarySkillCommand(String name) {
    final trimmed = name.trim();
    if (trimmed.isEmpty) return;
    final cmd = trimmed.startsWith('/') ? trimmed : '/$trimmed';
    widget.onLibrarySkillInserted?.call('$cmd ');
  }

  /// mode=none（无原生 skills 主根）：不发 enable，只插使用说明模板。
  void _handleLibraryInsertGuide(LibrarySkillModel skill) {
    if (skill.isSystem) return;
    final dirHint = skill.dir.isNotEmpty
        ? '~/.grix/skills/${skill.dir}'
        : '~/.grix/skills/${skill.name}';
    final buffer = StringBuffer()
      ..writeln('chat_skill_library_guide_line1'.trParams({'name': skill.name}))
      ..writeln('chat_skill_library_guide_line2'.trParams({'path': dirHint}));
    if (skill.description.isNotEmpty) {
      buffer.writeln(
        'chat_skill_library_guide_line3'.trParams({
          'description': skill.description,
        }),
      );
    }
    widget.onLibrarySkillInserted?.call(buffer.toString());
    if (!mounted) return;
    CustomToast.show(
      'chat_skill_library_guide_inserted'.trParams({'name': skill.name}),
      isError: false,
    );
  }

  Future<void> _handleLibraryEnable(LibrarySkillModel skill) async {
    if (skill.isSystem) return;
    if (skill.enableUnsupported) {
      _handleLibraryInsertGuide(skill);
      return;
    }
    if (!_canUseLibraryActions) return;

    final projectAvailable = skill.projectAvailable;
    final choice = await showModalBottomSheet<String>(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              title: Text('chat_skill_library_enable_global'.tr),
              subtitle: Text(_scopeLabel(skill.globalScope)),
              enabled:
                  skill.canEnableGlobal ||
                  skill.globalScope == LibrarySkillScopeState.unmanaged ||
                  skill.globalScope == LibrarySkillScopeState.broken,
              onTap: () => Navigator.pop(ctx, 'global'),
            ),
            ListTile(
              title: Text('chat_skill_library_enable_project'.tr),
              subtitle: Text(
                projectAvailable
                    ? _scopeLabel(skill.projectScope)
                    : 'chat_skill_library_project_disabled'.tr,
              ),
              enabled:
                  projectAvailable &&
                  (skill.canEnableProject ||
                      skill.projectScope == LibrarySkillScopeState.unmanaged ||
                      skill.projectScope == LibrarySkillScopeState.broken),
              onTap: () => Navigator.pop(ctx, 'project'),
            ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
    if (choice == null || !mounted) return;

    final current = choice == 'global' ? skill.globalScope : skill.projectScope;
    String? force;
    if (current == LibrarySkillScopeState.conflict) {
      if (!mounted) return;
      CustomToast.show(
        'chat_skill_library_conflict_blocked'.trParams({'name': skill.name}),
        isError: true,
      );
      return;
    } else if (current == LibrarySkillScopeState.unmanaged) {
      final ok = await showAppConfirmDialog(
        context: context,
        title: 'chat_skill_library_replace_dir_title'.trParams({
          'name': skill.name,
        }),
        message: 'chat_skill_library_replace_dir_body'.tr,
        cancelText: 'chat_skill_upload_cancel'.tr,
        confirmText: 'chat_skill_library_replace_dir_confirm'.tr,
      );
      if (ok != true) return;
      force = 'replace_with_link';
    }

    final busyKey = '${skill.name}:$choice';
    setState(() => _libraryBusy.add(busyKey));
    try {
      await widget.imService!.requestSkillEnable(
        agentId: widget.agentId!,
        sessionId: widget.sessionId!,
        name: skill.name,
        scope: choice,
        force: force,
      );
      if (!mounted) return;
      setState(() {
        _libraryBusy.remove(busyKey);
        _librarySkills = _librarySkills
            .map(
              (s) => s.name != skill.name
                  ? s
                  : _copyLibrarySkill(
                      s,
                      globalScope: choice == 'global'
                          ? LibrarySkillScopeState.link
                          : null,
                      projectScope: choice == 'project'
                          ? LibrarySkillScopeState.link
                          : null,
                    ),
            )
            .toList();
      });
      _insertLibrarySkillCommand(skill.name);
      _recordSkillUsage(skill.name);
      if (!mounted) return;
      CustomToast.show(
        'chat_skill_library_enable_success'.trParams({'name': skill.name}),
        isError: false,
      );
    } catch (e) {
      if (!mounted) return;
      setState(() => _libraryBusy.remove(busyKey));
      CustomToast.show(
        'chat_skill_library_enable_failed'.trParams({
          'error': userFacingError(e),
        }),
        isError: true,
      );
    }
  }

  Future<void> _handleLibraryDisable(
    LibrarySkillModel skill,
    String scope,
  ) async {
    if (!_canUseLibraryActions) return;
    final busyKey = '${skill.name}:disable:$scope';
    setState(() => _libraryBusy.add(busyKey));
    try {
      await widget.imService!.requestSkillDisable(
        agentId: widget.agentId!,
        sessionId: widget.sessionId!,
        name: skill.name,
        scope: scope,
      );
      if (!mounted) return;
      setState(() {
        _libraryBusy.remove(busyKey);
        _librarySkills = _librarySkills
            .map(
              (s) => s.name != skill.name
                  ? s
                  : _copyLibrarySkill(
                      s,
                      globalScope: scope == 'global'
                          ? LibrarySkillScopeState.none
                          : null,
                      projectScope: scope == 'project'
                          ? LibrarySkillScopeState.none
                          : null,
                    ),
            )
            .toList();
      });
      CustomToast.show(
        'chat_skill_library_disable_success'.trParams({'name': skill.name}),
        isError: false,
      );
    } catch (e) {
      if (!mounted) return;
      setState(() => _libraryBusy.remove(busyKey));
      CustomToast.show(
        'chat_skill_library_disable_failed'.trParams({
          'error': userFacingError(e),
        }),
        isError: true,
      );
    }
  }

  Widget _buildLibraryTrailing(ThemeData theme, LibrarySkillModel skill) {
    final busy = _libraryBusy.any((k) => k.startsWith('${skill.name}:'));
    if (busy) {
      return const SizedBox(
        width: 16,
        height: 16,
        child: CircularProgressIndicator(strokeWidth: 2),
      );
    }
    if (skill.isSystem) {
      return Text(
        'chat_skill_library_system'.tr,
        style: TextStyle(
          fontSize: 11,
          color: theme.colorScheme.onSurface.withValues(alpha: 0.4),
        ),
      );
    }
    if (skill.enableUnsupported) {
      return IconButton(
        icon: const Icon(Icons.notes, size: 18),
        tooltip: 'chat_skill_library_guide_tooltip'.tr,
        visualDensity: VisualDensity.compact,
        onPressed: () => _handleLibraryInsertGuide(skill),
      );
    }
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (skill.canDisableGlobal || skill.canDisableProject)
          IconButton(
            icon: const Icon(Icons.link_off, size: 18),
            tooltip: 'chat_skill_library_disable_tooltip'.tr,
            visualDensity: VisualDensity.compact,
            onPressed: () async {
              if (skill.canDisableGlobal && skill.canDisableProject) {
                final picked = await showModalBottomSheet<String>(
                  context: context,
                  builder: (ctx) => SafeArea(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        ListTile(
                          title: Text('chat_skill_library_disable_global'.tr),
                          onTap: () => Navigator.pop(ctx, 'global'),
                        ),
                        ListTile(
                          title: Text('chat_skill_library_disable_project'.tr),
                          onTap: () => Navigator.pop(ctx, 'project'),
                        ),
                      ],
                    ),
                  ),
                );
                if (picked == null) return;
                await _handleLibraryDisable(skill, picked);
              } else {
                final scope = skill.canDisableGlobal ? 'global' : 'project';
                await _handleLibraryDisable(skill, scope);
              }
            },
          ),
        IconButton(
          icon: const Icon(Icons.link, size: 18),
          tooltip: 'chat_skill_library_enable_tooltip'.tr,
          visualDensity: VisualDensity.compact,
          onPressed: () => _handleLibraryEnable(skill),
        ),
      ],
    );
  }

  @override
  void dispose() {
    _searchController.dispose();
    _tabController?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final commands = _visibleCommands;
    final library = _visibleLibrarySkills;
    final tabController = _tabController;
    final showTabs = widget.showSkillLibrary && tabController != null;
    return Container(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: buildChatBottomSheetFrame(
        context,
        maxHeightFactor: 0.7,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            buildChatBottomSheetHandle(context),
            // 标题在技能弹窗且 title 非空时展示；命令弹窗无标题、无 Tab。
            // 搜索框紧接拖动手柄，命令列表直接展示、不留间隙。
            if (showTabs && widget.title.isNotEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 4,
                ),
                child: Text(
                  widget.title,
                  style: const TextStyle(
                    fontSize: 15,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
            if (showTabs)
              TabBar(
                controller: tabController,
                tabs: [
                  if (_showSkillToggles)
                    Tab(text: 'chat_skill_tab_session'.tr)
                  else
                    Tab(text: 'chat_skill_tab_enabled'.tr),
                  Tab(text: 'chat_skill_tab_library'.tr),
                ],
              ),
            _ChatSheetSearchField(
              controller: _searchController,
              keyword: _keyword,
              onChanged: (value) => setState(() => _keyword = value.trim()),
              onClear: () {
                _searchController.clear();
                setState(() => _keyword = '');
              },
            ),
            Flexible(
              child: showTabs
                  ? TabBarView(
                      controller: tabController,
                      children: [
                        _buildCommandsList(theme, commands),
                        _buildLibraryList(theme, library),
                      ],
                    )
                  : _buildCommandsList(theme, commands),
            ),
            SizedBox(height: MediaQuery.of(context).padding.bottom),
          ],
        ),
      ),
    );
  }

  Widget _buildCommandsList(ThemeData theme, List<CommandItemModel> commands) {
    if (_showSkillToggles) {
      return Obx(() {
        _syncSessionSkillsFromToolbar();
        return _buildCommandsListBody(theme, _visibleCommands);
      });
    }
    return _buildCommandsListBody(theme, commands);
  }

  Widget _buildCommandsListBody(
    ThemeData theme,
    List<CommandItemModel> commands,
  ) {
    final rows = _buildSkillRows(commands);
    return RefreshIndicator(
      onRefresh: _handleRefresh,
      child: commands.isEmpty
          ? _emptyTabView('chat_skill_search_empty'.tr, theme)
          : ListView.builder(
              physics: const AlwaysScrollableScrollPhysics(),
              shrinkWrap: true,
              itemCount: rows.length,
              itemBuilder: (context, index) {
                final row = rows[index];
                if (row is _SkillScopeHeader) {
                  return _buildSkillScopeHeader(theme, row);
                }
                final cmd = row as CommandItemModel;
                return ListTile(
                  title: Text(
                    cmd.name,
                    style: TextStyle(
                      fontSize: 14,
                      fontWeight: FontWeight.w500,
                      color: theme.colorScheme.onSurface,
                    ),
                  ),
                  subtitle: _buildCommandSubtitle(theme, cmd),
                  trailing: _buildCommandTrailing(theme, cmd),
                  onTap: _skillToggles[cmd.id]?.enabled == false
                      ? null
                      : () => widget.onSelected(cmd),
                );
              },
            ),
    );
  }

  /// 技能列表按作用域分组：项目级在前、公共在后，组标题带技能数量，方便用户
  /// 一眼看出「公共装了多少、项目级有哪些」。斜杠命令列表与不带 source 的旧
  /// connector 上报不分组，保持原有平铺。
  List<Object> _buildSkillRows(List<CommandItemModel> commands) {
    if (!widget.showSkillLibrary ||
        !commands.any((cmd) => cmd.source.isNotEmpty)) {
      return List<Object>.from(commands);
    }
    final project = commands.where((cmd) => cmd.isProjectScope).toList();
    final global = commands.where((cmd) => !cmd.isProjectScope).toList();
    return [
      for (final group in [
        (label: 'chat_skill_scope_project'.tr, items: project, project: true),
        (label: 'chat_skill_scope_global'.tr, items: global, project: false),
      ])
        if (group.items.isNotEmpty) ...[
          _SkillScopeHeader(
            label: group.label,
            count: group.items.length,
            project: group.project,
          ),
          ...group.items,
        ],
    ];
  }

  Widget _buildSkillScopeHeader(ThemeData theme, _SkillScopeHeader header) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
      child: Row(
        children: [
          Icon(
            header.project ? Icons.folder_outlined : Icons.public,
            size: 14,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.55),
          ),
          const SizedBox(width: 6),
          Text(
            '${header.label} (${header.count})',
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w600,
              color: theme.colorScheme.onSurface.withValues(alpha: 0.55),
            ),
          ),
        ],
      ),
    );
  }

  Widget? _buildCommandSubtitle(ThemeData theme, CommandItemModel cmd) {
    final rows = <Widget>[
      if (cmd.description.isNotEmpty)
        Text(
          cmd.description,
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(
            fontSize: 12,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
          ),
        ),
      if (cmd.path.isNotEmpty) _buildSkillPathRow(theme, cmd),
    ];
    if (rows.isEmpty) return null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: rows,
    );
  }

  /// 技能路径单行展示：左侧作用域图标，点击整行复制路径到剪贴板。
  Widget _buildSkillPathRow(ThemeData theme, CommandItemModel cmd) {
    final color = theme.colorScheme.onSurface.withValues(alpha: 0.45);
    return Padding(
      padding: const EdgeInsets.only(top: 2),
      child: InkWell(
        onTap: () => _copySkillPath(cmd.path),
        child: Row(
          children: [
            Icon(
              cmd.isProjectScope ? Icons.folder_outlined : Icons.public,
              size: 12,
              color: color,
            ),
            const SizedBox(width: 4),
            Expanded(
              child: Text(
                cmd.path,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(fontSize: 11, color: color),
              ),
            ),
            const SizedBox(width: 4),
            Icon(Icons.copy_outlined, size: 12, color: color),
          ],
        ),
      ),
    );
  }

  Future<void> _copySkillPath(String path) async {
    await Clipboard.setData(ClipboardData(text: path));
    CustomToast.show('chat_skill_path_copied'.tr);
  }

  Widget _buildLibraryList(ThemeData theme, List<LibrarySkillModel> library) {
    return RefreshIndicator(
      onRefresh: _handleRefresh,
      child: library.isEmpty
          ? _emptyTabView('chat_skill_library_empty'.tr, theme)
          : ListView.builder(
              physics: const AlwaysScrollableScrollPhysics(),
              shrinkWrap: true,
              itemCount: library.length,
              itemBuilder: (context, index) {
                final skill = library[index];
                return ListTile(
                  title: Text(
                    skill.name,
                    style: TextStyle(
                      fontSize: 14,
                      fontWeight: FontWeight.w500,
                      color: theme.colorScheme.onSurface,
                    ),
                  ),
                  subtitle: skill.description.isNotEmpty
                      ? Text(
                          skill.description,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 12,
                            color: theme.colorScheme.onSurface.withValues(
                              alpha: 0.55,
                            ),
                          ),
                        )
                      : null,
                  trailing: _buildLibraryTrailing(theme, skill),
                  onTap: skill.isSystem
                      ? null
                      : () => _handleLibraryEnable(skill),
                );
              },
            ),
    );
  }
}

void showChatQueueSheet(
  BuildContext context, {
  required ImService imService,
  required String sessionId,
  ChatController? controller,
}) {
  final theme = Theme.of(context);
  showModalBottomSheet(
    context: context,
    isScrollControlled: true,
    constraints: const BoxConstraints(maxWidth: 480),
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
    ),
    builder: (sheetContext) {
      // 统一拖动手势的状态（每次开面板重建）：insertGap 是排序插入间隙
      // 指示（展示序 0..len，-1＝不指示）；rowKeys 用于拖动中按 eventId
      // 量取各行的屏幕位置。
      final insertGap = (-1).obs;
      final mergeTargetEventId = ''.obs;
      const sortGapHeight = 14.0;
      Offset? dragPointer;
      final rowKeys = <String, GlobalKey>{};
      GlobalKey rowKeyOf(String eventId) => rowKeys.putIfAbsent(
        eventId,
        () => GlobalKey(debugLabel: 'queue_row_$eventId'),
      );

      List<EventLifecycleQueueItem> currentItems() =>
          orderQueueItemsForDisplay(imService.queueItemsForSession(sessionId));

      // 指针落点对应的排序插入间隙；落在某行中心带（合并区）时返回 -1。
      int gapAt(Offset globalPos) {
        final items = currentItems();
        for (var i = 0; i < items.length; i++) {
          final box =
              rowKeys[items[i].eventId]?.currentContext?.findRenderObject()
                  as RenderBox?;
          if (box == null || !box.attached) {
            continue;
          }
          final top = box.localToGlobal(Offset.zero).dy;
          final h = box.size.height;
          final rel = globalPos.dy - top;
          if (rel < 0) {
            return i;
          }
          if (rel <= h) {
            if (rel >= h * 0.25 && rel <= h * 0.75) {
              return -1; // 中心带＝合并落点，不做排序插入
            }
            return rel < h * 0.25 ? i : i + 1;
          }
        }
        return items.length;
      }

      // 统一拖动中：实时更新插入间隙指示（与被拖项原位相邻时不显示）。
      void updateGapIndicator(EventLifecycleQueueItem dragged, Offset pos) {
        final items = currentItems();
        final oldIndex = items.indexWhere((e) => e.eventId == dragged.eventId);
        var gap = gapAt(pos);
        if (gap == oldIndex || gap == oldIndex + 1) {
          gap = -1;
        }
        if (insertGap.value != gap) {
          insertGap.value = gap;
        }
      }

      bool isMergeCenter(EventLifecycleQueueItem target, Offset globalPos) {
        final box =
            rowKeys[target.eventId]?.currentContext?.findRenderObject()
                as RenderBox?;
        if (box == null || !box.attached) {
          return false;
        }
        final top = box.localToGlobal(Offset.zero).dy;
        final rel = globalPos.dy - top;
        return rel >= box.size.height * 0.25 && rel <= box.size.height * 0.75;
      }

      String mergeTargetAt(EventLifecycleQueueItem dragged, Offset globalPos) {
        if (!dragged.canCancel || dragged.fullContent.trim().isEmpty) {
          return '';
        }
        for (final target in currentItems()) {
          if (target.queuePosition > 0 &&
              target.eventId != dragged.eventId &&
              isMergeCenter(target, globalPos)) {
            return target.eventId;
          }
        }
        return '';
      }

      // 松手（未命中合并中心带）：按最终落点计算插入间隙并提交排序。
      void finishUnifiedDrag(EventLifecycleQueueItem dragged, Offset pos) {
        final hoverGap = insertGap.value;
        insertGap.value = -1;
        mergeTargetEventId.value = '';
        final items = currentItems();
        final oldIndex = items.indexWhere((e) => e.eventId == dragged.eventId);
        final target = hoverGap >= 0 ? hoverGap : gapAt(pos);
        if (oldIndex < 0 ||
            target < 0 ||
            target == oldIndex ||
            target == oldIndex + 1) {
          return;
        }
        final orderedIds = computeReorderedQueueIds(
          displayItems: items,
          oldIndex: oldIndex,
          newIndex: target,
        );
        if (orderedIds == null) {
          return;
        }
        imService.sendQueueReorder(
          sessionId: sessionId,
          orderedEventIds: orderedIds,
        );
      }

      Widget buildSortGap(int gap) {
        // ListView.builder 的 itemBuilder 在 Obx 的同步构建阶段之后才执行，
        // 因此排序指示器需要自己订阅 insertGap，才能在拖动中即时变色。
        return Obx(() {
          final active = insertGap.value == gap;
          return Container(
            key: ValueKey('queue_sort_gap_$gap'),
            height: sortGapHeight,
            alignment: Alignment.center,
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: AnimatedContainer(
              key: ValueKey('queue_sort_indicator_$gap'),
              duration: const Duration(milliseconds: 80),
              curve: Curves.easeOut,
              height: active ? 3 : 1,
              decoration: BoxDecoration(
                color: active
                    ? theme.colorScheme.primary
                    : theme.colorScheme.outline.withValues(alpha: 0.14),
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          );
        });
      }

      return Obx(() {
        final queueItems = currentItems();
        return buildChatBottomSheetFrame(
          context,
          maxHeightFactor: 0.75,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              buildChatBottomSheetHandle(context),
              Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 4,
                ),
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        '${'chat_queue_title'.tr} (${queueItems.length})',
                        style: const TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                    TextButton(
                      onPressed: queueItems.isEmpty
                          ? null
                          : () {
                              imService.sendQueueClear(sessionId: sessionId);
                            },
                      child: Text('chat_queue_clear'.tr),
                    ),
                  ],
                ),
              ),
              const Divider(height: 1),
              Flexible(
                child: queueItems.isEmpty
                    ? Center(
                        child: Text(
                          'chat_queue_empty'.tr,
                          style: TextStyle(
                            fontSize: 13,
                            color: theme.colorScheme.onSurface.withValues(
                              alpha: 0.62,
                            ),
                          ),
                        ),
                      )
                    // 展示序为倒序（最新排队在上，running 沉底）。长按任意
                    // 排队任务整行后拖动：落到其他任务行中心带＝合并，落到
                    // 行上/下沿或间隙＝排序。running 项不可拖动。
                    : ListView.builder(
                        shrinkWrap: true,
                        itemCount: queueItems.length,
                        itemBuilder: (context, index) {
                          final item = queueItems[index];
                          final preview = item.contentPreview.isNotEmpty
                              ? item.contentPreview
                              : (item.clientMsgId.isNotEmpty
                                    ? item.clientMsgId
                                    : (item.messageId.isNotEmpty
                                          ? item.messageId
                                          : item.eventId));
                          final previewText = preview.length > 48
                              ? '${preview.substring(0, 48)}...'
                              : preview;
                          final position = item.queuePosition > 0
                              ? '#${item.queuePosition}'
                              : '-';
                          final draggable = item.queuePosition > 0;
                          // running 项（position 0）暂停/编辑照常渲染但置灰
                          // 禁用（与拖拽手柄置灰同款处理）。
                          final holdEditEnabled = item.queuePosition > 0;
                          final heldBadge = !item.held
                              ? ''
                              : '  ${item.heldReason == 'editing' ? 'chat_queue_editing_badge'.tr : 'chat_queue_held_badge'.tr}';
                          // 操作按钮统一 34x34 格；拖动改为整行长按，不再
                          // 显示难以点中的独立拖动手柄。
                          Widget actionCell({
                            required IconData icon,
                            required Color color,
                            VoidCallback? onTap,
                            double iconSize = 20,
                          }) {
                            final cell = Container(
                              width: 34,
                              height: 34,
                              color: Colors.transparent,
                              alignment: Alignment.center,
                              child: Icon(icon, size: iconSize, color: color),
                            );
                            return onTap == null
                                ? cell
                                : InkWell(
                                    onTap: onTap,
                                    borderRadius: BorderRadius.circular(6),
                                    child: cell,
                                  );
                          }

                          final actionButtons = Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Tooltip(
                                triggerMode: TooltipTriggerMode.manual,
                                message: item.held
                                    ? 'chat_queue_resume'.tr
                                    : 'chat_queue_hold'.tr,
                                child: actionCell(
                                  icon: item.held
                                      ? Icons.play_arrow_rounded
                                      : Icons.pause_rounded,
                                  color: holdEditEnabled
                                      ? theme.colorScheme.onSurface.withValues(
                                          alpha: 0.65,
                                        )
                                      : theme.disabledColor,
                                  onTap: holdEditEnabled
                                      ? () {
                                          imService.sendEventHold(
                                            sessionId: sessionId,
                                            eventId: item.eventId,
                                            hold: !item.held,
                                            reason: 'manual',
                                          );
                                        }
                                      : null,
                                ),
                              ),
                              Tooltip(
                                triggerMode: TooltipTriggerMode.manual,
                                message: 'chat_queue_edit'.tr,
                                child: actionCell(
                                  icon: Icons.edit_outlined,
                                  iconSize: 18,
                                  color: (holdEditEnabled && controller != null)
                                      ? theme.colorScheme.onSurface.withValues(
                                          alpha: 0.65,
                                        )
                                      : theme.disabledColor,
                                  onTap: (holdEditEnabled && controller != null)
                                      ? () async {
                                          final entered = await controller
                                              .startQueueTaskEdit(item);
                                          if (entered && sheetContext.mounted) {
                                            Navigator.of(sheetContext).pop();
                                          }
                                        }
                                      : null,
                                ),
                              ),
                              Tooltip(
                                triggerMode: TooltipTriggerMode.manual,
                                message: 'chat_queue_delete'.tr,
                                child: actionCell(
                                  icon: Icons.delete_outline_rounded,
                                  iconSize: 19,
                                  color: item.canCancel
                                      ? theme.colorScheme.error
                                      : theme.disabledColor,
                                  onTap: item.canCancel
                                      ? () {
                                          imService.sendEventCancel(
                                            sessionId: sessionId,
                                            item: item,
                                          );
                                        }
                                      : null,
                                ),
                              ),
                            ],
                          );
                          final textBlock = Padding(
                            padding: const EdgeInsets.only(
                              left: 16,
                              top: 8,
                              bottom: 8,
                            ),
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  previewText,
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: const TextStyle(
                                    fontSize: 14,
                                    fontWeight: FontWeight.w500,
                                  ),
                                ),
                                Text(
                                  '${item.state}  $position$heldBadge',
                                  style: TextStyle(
                                    fontSize: 12,
                                    color: theme.colorScheme.onSurface
                                        .withValues(alpha: 0.65),
                                  ),
                                ),
                              ],
                            ),
                          );
                          final tile = Row(
                            children: [
                              Expanded(child: textBlock),
                              Padding(
                                padding: const EdgeInsets.only(right: 16),
                                child: actionButtons,
                              ),
                            ],
                          );
                          return Column(
                            key: ValueKey(item.eventId),
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              // 每两行之间保留固定的排序落点。此前这里只有 1px
                              // 分隔线，真机手指几乎无法命中，容易直接进入行中心
                              // 的合并区。固定高度也避免插入线出现时推动列表跳动。
                              buildSortGap(index),
                              DragTarget<EventLifecycleQueueItem>(
                                key: rowKeyOf(item.eventId),
                                onWillAcceptWithDetails: (details) {
                                  final draggedItem = details.data;
                                  return draggable &&
                                      draggedItem.queuePosition > 0 &&
                                      draggedItem.canCancel &&
                                      draggedItem.eventId != item.eventId &&
                                      draggedItem.fullContent.trim().isNotEmpty;
                                },
                                onAcceptWithDetails: (details) {
                                  final pointer = dragPointer ?? details.offset;
                                  final merge = isMergeCenter(item, pointer);
                                  insertGap.value = -1;
                                  mergeTargetEventId.value = '';
                                  if (merge) {
                                    _confirmQueueTaskMerge(
                                      sheetContext,
                                      imService: imService,
                                      sessionId: sessionId,
                                      dragged: details.data,
                                      target: item,
                                    );
                                  } else {
                                    finishUnifiedDrag(details.data, pointer);
                                  }
                                },
                                builder: (context, candidateData, rejectedData) {
                                  // 行边缘虽然仍位于 DragTarget 内，但语义是排序。
                                  // 只有指针处于中心合并带时才着色，确保任务高亮
                                  // 与间隙插入线不会同时出现。
                                  final row = Obx(() {
                                    final activeMergeTarget =
                                        mergeTargetEventId.value;
                                    final merging =
                                        candidateData.isNotEmpty &&
                                        activeMergeTarget == item.eventId;
                                    return AnimatedContainer(
                                      key: ValueKey(
                                        'queue_merge_target_${item.eventId}',
                                      ),
                                      duration: const Duration(
                                        milliseconds: 80,
                                      ),
                                      curve: Curves.easeOut,
                                      decoration: BoxDecoration(
                                        color: merging
                                            ? theme.colorScheme.primary
                                                  .withValues(alpha: 0.24)
                                            : null,
                                        border: merging
                                            ? Border.all(
                                                color: theme.colorScheme.primary
                                                    .withValues(alpha: 0.9),
                                                width: 2,
                                              )
                                            : null,
                                        borderRadius: BorderRadius.circular(8),
                                      ),
                                      child: tile,
                                    );
                                  });
                                  if (!draggable) {
                                    return row;
                                  }
                                  final feedbackWidth =
                                      (MediaQuery.sizeOf(sheetContext).width -
                                              32)
                                          .clamp(0.0, 448.0)
                                          .toDouble();
                                  return LongPressDraggable<
                                    EventLifecycleQueueItem
                                  >(
                                    data: item,
                                    // 让反馈行以手指为中心；业务命中使用单独记录
                                    // 的 dragPointer，不再依赖反馈行左上角坐标。
                                    dragAnchorStrategy:
                                        (draggable, context, position) =>
                                            Offset(feedbackWidth / 2, 28),
                                    feedback: Material(
                                      key: ValueKey(
                                        'queue_drag_feedback_${item.eventId}',
                                      ),
                                      color: Colors.transparent,
                                      child: SizedBox(
                                        width: feedbackWidth,
                                        child: Container(
                                          constraints: const BoxConstraints(
                                            minHeight: 56,
                                          ),
                                          padding: const EdgeInsets.symmetric(
                                            horizontal: 16,
                                            vertical: 8,
                                          ),
                                          decoration: BoxDecoration(
                                            color: theme.colorScheme.surface
                                                .withValues(alpha: 0.88),
                                            borderRadius: BorderRadius.circular(
                                              8,
                                            ),
                                            boxShadow: const [
                                              BoxShadow(
                                                color: Color(0x33000000),
                                                blurRadius: 12,
                                                offset: Offset(0, 4),
                                              ),
                                            ],
                                          ),
                                          child: Column(
                                            mainAxisSize: MainAxisSize.min,
                                            crossAxisAlignment:
                                                CrossAxisAlignment.start,
                                            children: [
                                              Text(
                                                previewText,
                                                maxLines: 1,
                                                overflow: TextOverflow.ellipsis,
                                                style: const TextStyle(
                                                  fontSize: 14,
                                                  fontWeight: FontWeight.w500,
                                                ),
                                              ),
                                              Text(
                                                '${item.state}  $position$heldBadge',
                                                style: TextStyle(
                                                  fontSize: 12,
                                                  color: theme
                                                      .colorScheme
                                                      .onSurface
                                                      .withValues(alpha: 0.65),
                                                ),
                                              ),
                                            ],
                                          ),
                                        ),
                                      ),
                                    ),
                                    onDragUpdate: (details) {
                                      dragPointer = details.globalPosition;
                                      mergeTargetEventId.value = mergeTargetAt(
                                        item,
                                        details.globalPosition,
                                      );
                                      updateGapIndicator(
                                        item,
                                        details.globalPosition,
                                      );
                                    },
                                    onDraggableCanceled: (velocity, offset) {
                                      finishUnifiedDrag(
                                        item,
                                        dragPointer ?? offset,
                                      );
                                      dragPointer = null;
                                      mergeTargetEventId.value = '';
                                    },
                                    onDragCompleted: () {
                                      insertGap.value = -1;
                                      dragPointer = null;
                                      mergeTargetEventId.value = '';
                                    },
                                    childWhenDragging: Opacity(
                                      opacity: 0.25,
                                      child: row,
                                    ),
                                    child: row,
                                  );
                                },
                              ),
                              // 最后一行之后也保留同样大小的排序落点，可将任务
                              // 拖到展示列表末端。
                              if (index == queueItems.length - 1)
                                buildSortGap(queueItems.length),
                            ],
                          );
                        },
                      ),
              ),
              // 手势提示小字：整行长按拖动，落到任务上=合并，落到间隙=排序。
              if (queueItems.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Text(
                    'chat_queue_sheet_hint'.tr,
                    style: TextStyle(
                      fontSize: 11,
                      color: theme.colorScheme.onSurface.withValues(
                        alpha: 0.45,
                      ),
                    ),
                  ),
                ),
              SizedBox(height: MediaQuery.of(context).padding.bottom),
            ],
          ),
        );
      });
    },
  );
}

/// 队列任务合并确认 + 执行（纯前端组合，无新协议）：
/// 先 queue_edit 把目标任务全文改写为「被拖任务全文 + 换行 + 目标任务原
/// 全文」，成功后 event_cancel 移除被拖任务；任一步失败不动队列并 toast。
Future<void> _confirmQueueTaskMerge(
  BuildContext context, {
  required ImService imService,
  required String sessionId,
  required EventLifecycleQueueItem dragged,
  required EventLifecycleQueueItem target,
}) async {
  String briefOf(EventLifecycleQueueItem e) {
    final text = e.fullContent.trim();
    if (text.isEmpty) {
      return e.eventId;
    }
    return text.length > 20 ? '${text.substring(0, 20)}...' : text;
  }

  final confirmed = await showAppConfirmDialog(
    context: context,
    title: 'chat_queue_merge_title'.tr,
    message: 'chat_queue_merge_confirm'.trParams({
      'dragged': briefOf(dragged),
      'target': briefOf(target),
    }),
    confirmText: 'chat_queue_merge'.tr,
    isDestructive: true,
  );
  if (!confirmed) {
    return;
  }
  final merged = '${dragged.fullContent.trim()}\n${target.fullContent.trim()}'
      .trim();
  final result = await imService.sendQueueEdit(
    sessionId: sessionId,
    eventId: target.eventId,
    content: merged,
  );
  if (!result.ok) {
    CustomToast.show('chat_queue_merge_failed'.tr);
    return;
  }
  // queue_edit 命中后 connector 会自动解除该任务的 hold（协议约定）；
  // 目标若原本处于暂停状态，合并不应顺带把它恢复执行，这里补一次
  // hold 还原（失败有 TTL/快照兜底，无需等回执）。
  if (target.held) {
    unawaited(
      imService.sendEventHold(
        sessionId: sessionId,
        eventId: target.eventId,
        hold: true,
        reason: target.heldReason.isNotEmpty ? target.heldReason : 'manual',
      ),
    );
  }
  // event_cancel 无回执（fire-and-forget）：极端情况下（恰好在取消前
  // 开跑）可能取消失败，两条任务会带着重复内容并存，由权威
  // queue_snapshot 收敛后用户可见，不会静默丢任务。
  imService.sendEventCancel(sessionId: sessionId, item: dragged);
}

void showChatAgentPicker(
  ChatController controller,
  BuildContext context, {
  required double fontScale,
  bool voiceOnly = false,
  void Function(String agentId)? onPick,
}) {
  controller.loadAgents();
  showModalBottomSheet(
    context: context,
    isScrollControlled: true,
    constraints: const BoxConstraints(maxWidth: 400),
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
    ),
    builder: (context) => buildChatBottomSheetFrame(
      context,
      maxHeightFactor: 0.8,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          buildChatBottomSheetHandle(context),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
            child: Text(
              'ai_delegate_pick_agent'.tr,
              style: TextStyle(
                fontSize: 15 * fontScale,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
          Flexible(
            child: Obx(() {
              final accessible = controller.agentService.allAccessibleAgents;
              final agents = voiceOnly
                  ? accessible.where((a) => a.providerType == 4).toList()
                  : accessible;
              if (agents.isEmpty) {
                return Padding(
                  padding: const EdgeInsets.all(32),
                  child: Text('ai_agents_empty'.tr),
                );
              }
              return ListView.builder(
                itemCount: agents.length,
                itemBuilder: (context, index) {
                  final agent = agents[index];
                  return ListTile(
                    leading: const Icon(Icons.smart_toy_rounded),
                    title: Text(agent.agentName),
                    subtitle: Text(
                      agent.providerType == 3
                          ? 'ai_provider_agent_api'.tr
                          : agent.providerType == 2
                          ? 'ai_provider_local'.tr
                          : agent.modelProvider.isNotEmpty
                          ? agent.modelProvider
                          : 'ai_provider_remote'.tr,
                    ),
                    onTap: () {
                      Get.back();
                      if (onPick != null) {
                        onPick(agent.id);
                      } else {
                        controller.startDelegate(agent.id);
                      }
                    },
                  );
                },
              );
            }),
          ),
          SizedBox(height: MediaQuery.of(context).padding.bottom),
        ],
      ),
    ),
  );
}

Future<void> showChatMenu(
  ChatController controller,
  BuildContext pageContext, {
  required double fontScale,
}) {
  // 从触发瞬间起守卫，覆盖查收藏的异步窗口，防止连点叠出多层菜单。
  return SheetGuard.run<void>(
    'chat_menu',
    () => _showChatMenuSheet(controller, pageContext, fontScale: fontScale),
  );
}

Future<void> _showChatMenuSheet(
  ChatController controller,
  BuildContext pageContext, {
  required double fontScale,
}) async {
  final favoriteService = UserSessionFavoriteService();
  final favIds = await favoriteService.listIds();
  final isFavorited = favIds.contains(controller.sessionId);

  if (!pageContext.mounted) return;

  final theme = Theme.of(pageContext);
  await showModalBottomSheet<void>(
    context: pageContext,
    isScrollControlled: true,
    constraints: const BoxConstraints(maxWidth: 400),
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
    ),
    builder: (sheetContext) => buildChatBottomSheetFrame(
      sheetContext,
      maxHeightFactor: 0.65,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          buildChatBottomSheetHandle(sheetContext),
          Flexible(
            child: Theme(
              data: theme.copyWith(
                listTileTheme: const ListTileThemeData(
                  contentPadding: EdgeInsets.symmetric(horizontal: 16),
                  minVerticalPadding: 2,
                ),
              ),
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    if (controller.chatType == 'group')
                      ListTile(
                        leading: const Icon(Icons.group_rounded),
                        title: Obx(
                          () => Text(
                            controller.groupMemberCount > 0
                                ? '${controller.groupMemberCount} ${'chat_members'.tr}'
                                : 'chat_group'.tr,
                          ),
                        ),
                        onTap: () {
                          if (!popSheetOnce(sheetContext)) return;
                          showChatGroupMembersSheet(
                            controller,
                            pageContext,
                            fontScale: fontScale,
                          );
                        },
                      ),
                    if (controller.chatType == 'group')
                      ListTile(
                        leading: const Icon(Icons.badge_outlined),
                        title: Text('chat_set_group_nickname'.tr),
                        onTap: () async {
                          if (!popSheetOnce(sheetContext)) return;
                          await showChatSetGroupNicknameDialog(
                            controller,
                            pageContext,
                          );
                        },
                      ),
                    if (controller.chatType == 'group' &&
                        controller.canManageGroupMembers)
                      ListTile(
                        leading: const Icon(Icons.tune_rounded),
                        title: Text('chat_group_runtime_settings'.tr),
                        onTap: () async {
                          if (!popSheetOnce(sheetContext)) return;
                          await controller.refreshSessionDetail();
                          if (!pageContext.mounted) {
                            return;
                          }
                          await showChatGroupRuntimeSettingsDialog(
                            controller,
                            pageContext,
                          );
                        },
                      ),
                    if (controller.chatType == 'group' &&
                        controller.canManageGroupMembers)
                      ListTile(
                        leading: const Icon(Icons.qr_code_2_rounded),
                        title: Text('chat_group_qr_menu'.tr),
                        onTap: () {
                          if (!popSheetOnce(sheetContext)) return;
                          Get.to<void>(
                            () => GroupChatQrView(
                              sessionId: controller.sessionId,
                              groupName: controller.displayChatTitle,
                            ),
                          );
                        },
                      ),
                    if (controller.canForwardConversationCard)
                      ListTile(
                        leading: const Icon(Icons.forward_rounded),
                        title: Text('chat_forward_conversation_card'.tr),
                        onLongPress: () {
                          Clipboard.setData(
                            ClipboardData(text: controller.sessionId),
                          );
                          CustomToast.show(
                            'chat_session_id_copied'.tr,
                            isError: false,
                          );
                        },
                        onTap: () async {
                          if (!popSheetOnce(sheetContext)) return;
                          final target = await pickChatForwardTarget(
                            controller,
                            pageContext,
                            onSendToAgent: () {
                              unawaited(
                                showSendMessageToAgentDialog(
                                  pageContext,
                                  initialMessage:
                                      buildChatConversationCardAgentDraft(
                                        controller,
                                      ),
                                ),
                              );
                            },
                          );
                          if (target == null || !pageContext.mounted) {
                            return;
                          }
                          final accompanyingMessage =
                              await showChatForwardConversationDialog(
                                context: pageContext,
                                sourceTitle: controller.displayChatTitle,
                                targetTitle: target.title,
                                sourceIsGroup: controller.isGroupChat,
                                sourceAvatarTitle: controller.headerAvatarTitle,
                                sourceAvatarUrl:
                                    controller.privatePeerAvatarUrl,
                                sourceAvatarMembers:
                                    controller.groupAvatarMembers,
                              );
                          if (accompanyingMessage == null ||
                              !pageContext.mounted) {
                            return;
                          }
                          await forwardChatConversationCard(
                            controller: controller,
                            context: pageContext,
                            targetSessionId: target.sessionId,
                            accompanyingMessage: accompanyingMessage,
                          );
                        },
                      ),
                    if (controller.chatType == 'private')
                      ListTile(
                        leading: const Icon(Icons.group_add_rounded),
                        title: Text('chat_convert_to_group'.tr),
                        onTap: () async {
                          if (!popSheetOnce(sheetContext)) return;
                          final confirmed = await showAppConfirmDialog(
                            context: pageContext,
                            title: 'chat_convert_to_group'.tr,
                            message: 'chat_convert_to_group_confirm'.tr,
                            confirmText: 'chat_convert_to_group'.tr,
                          );
                          if (confirmed != true || !pageContext.mounted) {
                            return;
                          }
                          final ok = await controller.convertToGroup();
                          CustomToast.show(
                            ok
                                ? 'chat_convert_to_group_success'.tr
                                : 'chat_convert_to_group_failed'.tr,
                            isError: !ok,
                          );
                        },
                      ),
                    ListTile(
                      leading: const Icon(
                        Icons.drive_file_rename_outline_rounded,
                      ),
                      title: Text('chat_rename'.tr),
                      onTap: () async {
                        if (!popSheetOnce(sheetContext)) return;
                        await showChatRenameDialog(controller, pageContext);
                      },
                    ),
                    ListTile(
                      leading: Icon(
                        isFavorited
                            ? Icons.bookmark_rounded
                            : Icons.bookmark_border_rounded,
                        color: isFavorited ? AppTheme.primaryColor : null,
                      ),
                      title: Text(
                        isFavorited
                            ? 'conversations_unfavorite'.tr
                            : 'conversations_favorite'.tr,
                      ),
                      onTap: () async {
                        if (!popSheetOnce(sheetContext)) return;
                        if (isFavorited) {
                          await favoriteService.remove(controller.sessionId);
                        } else {
                          await favoriteService.add(controller.sessionId);
                        }
                        if (Get.isRegistered<ConversationsController>()) {
                          Get.find<ConversationsController>()
                              .reloadFavoriteIds();
                        }
                      },
                    ),
                    Obx(() {
                      final isDelegated =
                          controller.imService.delegateStates[controller
                              .sessionId] !=
                          null;
                      if (isDelegated) {
                        return ListTile(
                          leading: const Icon(
                            Icons.smart_toy_rounded,
                            color: AppTheme.errorColor,
                          ),
                          title: Text(
                            'ai_delegate_cancel'.tr,
                            style: const TextStyle(color: AppTheme.errorColor),
                          ),
                          onTap: () {
                            if (!popSheetOnce(sheetContext)) return;
                            controller.stopDelegate();
                          },
                        );
                      }
                      return ListTile(
                        leading: const Icon(Icons.smart_toy_outlined),
                        title: Text('ai_delegate_start'.tr),
                        onTap: () {
                          if (!popSheetOnce(sheetContext)) return;
                          showChatAgentPicker(
                            controller,
                            pageContext,
                            fontScale: fontScale,
                          );
                        },
                      );
                    }),
                    if (!PlatformCapability.isMobile)
                      Obx(() {
                        final voiceDelegated =
                            controller.imService.voiceDelegateStates[controller
                                .sessionId] !=
                            null;
                        if (voiceDelegated) {
                          return ListTile(
                            leading: const Icon(
                              Icons.support_agent_rounded,
                              color: AppTheme.errorColor,
                            ),
                            title: Text(
                              'chat_voice_delegate_cancel'.tr,
                              style: const TextStyle(
                                color: AppTheme.errorColor,
                              ),
                            ),
                            onTap: () {
                              if (!popSheetOnce(sheetContext)) return;
                              controller.imService.stopVoiceDelegate(
                                controller.sessionId,
                              );
                            },
                          );
                        }
                        return ListTile(
                          leading: const Icon(Icons.support_agent_outlined),
                          title: Text('chat_voice_delegate_start'.tr),
                          onTap: () {
                            if (!popSheetOnce(sheetContext)) return;
                            showChatAgentPicker(
                              controller,
                              pageContext,
                              fontScale: fontScale,
                              voiceOnly: true,
                              onPick: (agentId) =>
                                  controller.imService.startVoiceDelegate(
                                    controller.sessionId,
                                    agentId,
                                  ),
                            );
                          },
                        );
                      }),
                    ListTile(
                      leading: const Icon(Icons.notifications_outlined),
                      title: Text('me_notification'.tr),
                      onTap: () {
                        if (!popSheetOnce(sheetContext)) return;
                        showChatNotificationSettingSheet(
                          controller,
                          pageContext,
                        );
                      },
                    ),
                    if (controller.canReportGroup)
                      ListTile(
                        leading: const Icon(
                          Icons.flag_outlined,
                          color: AppTheme.errorColor,
                        ),
                        title: Text(
                          'chat_report_group'.tr,
                          style: const TextStyle(color: AppTheme.errorColor),
                        ),
                        onTap: () {
                          if (!popSheetOnce(sheetContext)) return;
                          controller.openGroupReportPage();
                        },
                      ),
                    if (controller.canDissolveGroup)
                      ListTile(
                        leading: const Icon(
                          Icons.group_off_rounded,
                          color: AppTheme.errorColor,
                        ),
                        title: Text(
                          'chat_dissolve_group'.tr,
                          style: const TextStyle(color: AppTheme.errorColor),
                        ),
                        onTap: () async {
                          if (!popSheetOnce(sheetContext)) return;
                          final confirmed = await showChatDissolveGroupConfirm(
                            controller,
                            pageContext,
                          );
                          if (!confirmed) return;

                          final ok = await controller.dissolveGroup();
                          if (!ok) {
                            CustomToast.show('chat_dissolve_group_failed'.tr);
                            return;
                          }
                          CustomToast.show(
                            'chat_group_dissolved'.tr,
                            isError: false,
                          );
                          if (Get.key.currentState?.canPop() ?? false) {
                            Get.back();
                          }
                        },
                      ),
                    ListTile(
                      leading: const Icon(Icons.hub_outlined),
                      title: Text('chat_webhook_manage'.tr),
                      onTap: () async {
                        if (!popSheetOnce(sheetContext)) return;
                        await showAppDialog<void>(
                          context: pageContext,
                          builder: (_) => WebhookManagerDialog(
                            sessionId: controller.sessionId,
                          ),
                        );
                      },
                    ),
                    ListTile(
                      leading: const Icon(
                        Icons.delete_outline_rounded,
                        color: AppTheme.errorColor,
                      ),
                      title: Text(
                        'common_delete'.tr,
                        style: const TextStyle(color: AppTheme.errorColor),
                      ),
                      onTap: () async {
                        if (!popSheetOnce(sheetContext)) return;
                        final confirmed = await showChatDeleteConfirm(
                          controller,
                          pageContext,
                        );
                        if (!confirmed) return;

                        await controller.deleteCurrentConversation();
                        if (Get.key.currentState?.canPop() ?? false) {
                          Get.back();
                        }
                      },
                    ),
                  ],
                ),
              ),
            ),
          ),
          SizedBox(height: MediaQuery.of(sheetContext).padding.bottom),
        ],
      ),
    ),
  );
}

Future<bool> showChatVisitorSessionActionConfirm(
  BuildContext context, {
  required String title,
  required String content,
  required String confirmText,
  bool destructive = false,
}) {
  return showAppConfirmDialog(
    context: context,
    title: title,
    message: content,
    confirmText: confirmText,
    isDestructive: destructive,
  );
}

Future<void> showChatVisitorInfoDialog(
  BuildContext context,
  ChatController controller,
) async {
  final rows = <MapEntry<String, String>>[
    MapEntry('chat_visitor_info_site'.tr, controller.visitorSiteName),
    MapEntry('chat_visitor_info_name'.tr, controller.visitorName),
    MapEntry('chat_visitor_info_email'.tr, controller.visitorEmail),
    MapEntry('chat_visitor_info_last_page'.tr, controller.visitorLastPageUrl),
  ].where((entry) => entry.value.trim().isNotEmpty).toList(growable: false);

  await showAppContentDialog<void>(
    context: context,
    title: 'chat_visitor_info_title'.tr,
    size: AppDialogSize.compact,
    content: rows.isEmpty
        ? Text('chat_visitor_info_empty'.tr)
        : Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              for (final row in rows) ...[
                Builder(
                  builder: (ctx) => Text(
                    row.key,
                    style: Theme.of(ctx).textTheme.labelMedium?.copyWith(
                      color: Theme.of(
                        ctx,
                      ).textTheme.bodySmall?.color?.withValues(alpha: 0.7),
                    ),
                  ),
                ),
                const SizedBox(height: 4),
                Builder(
                  builder: (ctx) => SelectableText(
                    row.value,
                    style: Theme.of(ctx).textTheme.bodyMedium,
                  ),
                ),
                const SizedBox(height: 12),
              ],
            ],
          ),
    actions: [
      Builder(
        builder: (ctx) => TextButton(
          onPressed: () => Navigator.of(ctx).pop(),
          child: Text('common_close'.tr),
        ),
      ),
    ],
  );
}

void showChatGroupMembersSheet(
  ChatController controller,
  BuildContext pageContext, {
  required double fontScale,
}) {
  controller.refreshSessionDetail();
  var keyword = '';
  final searchController = TextEditingController();
  showModalBottomSheet(
    context: pageContext,
    isScrollControlled: true,
    constraints: const BoxConstraints(maxWidth: 420),
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
    ),
    builder: (sheetContext) {
      return StatefulBuilder(
        builder: (context, setModalState) {
          return buildChatBottomSheetFrame(
            sheetContext,
            maxHeightFactor: 0.72,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                buildChatBottomSheetHandle(sheetContext),
                Obx(() {
                  final members = controller.groupMembers;
                  final count = controller.groupMemberCount > 0
                      ? controller.groupMemberCount
                      : members.length;
                  return Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 16,
                      vertical: 6,
                    ),
                    child: Row(
                      children: [
                        const Icon(Icons.group_rounded, size: 18),
                        const SizedBox(width: 8),
                        Text(
                          '$count ${'chat_members'.tr}',
                          style: TextStyle(
                            fontSize: 15 * fontScale,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        if (controller.allMembersMuted) ...[
                          const SizedBox(width: 8),
                          Container(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 8,
                              vertical: 4,
                            ),
                            decoration: BoxDecoration(
                              color: Theme.of(
                                sheetContext,
                              ).colorScheme.secondary.withValues(alpha: 0.12),
                              borderRadius: BorderRadius.circular(999),
                            ),
                            child: Text(
                              'chat_group_all_members_muted_badge'.tr,
                              style: Theme.of(
                                sheetContext,
                              ).textTheme.labelSmall,
                            ),
                          ),
                        ],
                        const Spacer(),
                        if (controller.canInviteGroupMembers)
                          TextButton.icon(
                            onPressed: () {
                              Navigator.of(sheetContext).pop();
                              showChatInviteFriendsSheet(
                                controller,
                                pageContext,
                                fontScale: fontScale,
                              );
                            },
                            icon: const Icon(
                              Icons.person_add_alt_1_rounded,
                              size: 16,
                            ),
                            label: Text('chat_add_members'.tr),
                          ),
                      ],
                    ),
                  );
                }),
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
                  child: TextField(
                    controller: searchController,
                    onChanged: (v) {
                      setModalState(
                        () => keyword = searchController.text.trim(),
                      );
                    },
                    textInputAction: TextInputAction.search,
                    onSubmitted: (_) {
                      setModalState(
                        () => keyword = searchController.text.trim(),
                      );
                      WidgetsBinding.instance.addPostFrameCallback((_) {
                        FocusManager.instance.primaryFocus?.unfocus();
                      });
                    },
                    decoration: InputDecoration(
                      hintText: 'conversations_search'.tr,
                      prefixIcon: const Icon(Icons.search, size: 20),
                      isDense: true,
                      border: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(10),
                        borderSide: BorderSide.none,
                      ),
                      filled: true,
                      contentPadding: const EdgeInsets.symmetric(
                        vertical: 8,
                        horizontal: 12,
                      ),
                    ),
                  ),
                ),
                Obx(() {
                  final allMembers = controller.groupMembers;
                  if (allMembers.isEmpty) {
                    return Padding(
                      padding: const EdgeInsets.symmetric(vertical: 24),
                      child: Text('chat_members_empty'.tr),
                    );
                  }
                  final kw = keyword.toLowerCase();
                  final members = kw.isEmpty
                      ? allMembers
                      : allMembers.where((m) {
                          final name = controller
                              .resolveGroupMemberDisplayName(m)
                              .toLowerCase();
                          final account = controller
                              .resolveGroupMemberAccount(m)
                              .toLowerCase();
                          return name.contains(kw) || account.contains(kw);
                        }).toList();
                  if (members.isEmpty) {
                    return Padding(
                      padding: const EdgeInsets.symmetric(vertical: 24),
                      child: Text('chat_members_empty'.tr),
                    );
                  }
                  return Flexible(
                    child: ListView.builder(
                      shrinkWrap: true,
                      itemCount: members.length,
                      itemBuilder: (context, index) {
                        final member = members[index];
                        final memberType = (member['member_type'] ?? 1) as int;
                        final displayName = controller
                            .resolveGroupMemberDisplayName(member);
                        final account = controller
                            .resolveGroupMemberAccount(member)
                            .trim();
                        final canPromote = controller.canPromoteGroupMember(
                          member,
                        );
                        final canDemote = controller.canDemoteGroupMember(
                          member,
                        );
                        final canRemove = controller.canRemoveGroupMember(
                          member,
                        );
                        final canLeave = controller.canLeaveGroupMember(member);
                        final canTransfer = controller.canTransferGroupOwner(
                          member,
                        );
                        final canUpdateSpeaking = controller
                            .canUpdateGroupMemberSpeaking(member);
                        final canUpdateAgentReceive = controller
                            .canUpdateGroupMemberAgentReceive(member);
                        final canToggleWhitelist = controller
                            .canToggleGroupMemberSpeakWhitelist(member);
                        final isSpeakMuted = controller.isGroupMemberSpeakMuted(
                          member,
                        );
                        final canSpeakWhenAllMuted = controller
                            .canGroupMemberSpeakWhenAllMuted(member);
                        final subtitleParts = <String>[
                          memberType == 2
                              ? 'nav_ai'.tr
                              : '@${account.isNotEmpty ? account : displayName}',
                        ];
                        if (canUpdateAgentReceive) {
                          subtitleParts.add(
                            '${'chat_agent_receive_label'.tr}: ${describeChatAgentReceiveMode(controller, member)}',
                          );
                        }
                        if (isSpeakMuted) {
                          subtitleParts.add('chat_group_member_muted_badge'.tr);
                        } else if (controller.allMembersMuted &&
                            canSpeakWhenAllMuted) {
                          subtitleParts.add(
                            'chat_group_member_whitelisted_badge'.tr,
                          );
                        }
                        final avatarUrl = controller
                            .resolveGroupMemberAvatarUrl(member)
                            .trim();
                        final avatarFallback = SessionAvatar(
                          isGroup: false,
                          avatarTitle: displayName,
                          avatarColor: AppTheme.getAvatarColor(
                            (member['member_id'] ?? '').toString(),
                          ),
                          size: 40,
                        );
                        return ListTile(
                          leading: SizedBox(
                            width: 40,
                            height: 40,
                            child: avatarUrl.isNotEmpty
                                ? ClipRRect(
                                    borderRadius: BorderRadius.zero,
                                    child: AvatarNetworkImage(
                                      avatarUrl: avatarUrl,
                                      fallback: avatarFallback,
                                      width: 40,
                                      height: 40,
                                    ),
                                  )
                                : avatarFallback,
                          ),
                          title: Text(displayName),
                          subtitle: Text(subtitleParts.join(' · ')),
                          trailing:
                              (!canPromote &&
                                  !canDemote &&
                                  !canRemove &&
                                  !canLeave &&
                                  !canTransfer &&
                                  !canUpdateAgentReceive &&
                                  !canUpdateSpeaking &&
                                  !canToggleWhitelist)
                              ? null
                              : PopupMenuButton<String>(
                                  onSelected: (value) async {
                                    if (value == 'agent_receive_normal' ||
                                        value == 'agent_receive_mention_only') {
                                      var mode = 1;
                                      if (value ==
                                          'agent_receive_mention_only') {
                                        mode = 3;
                                      }
                                      final ok = await controller
                                          .updateGroupMemberAgentReceive(
                                            member,
                                            mode: mode,
                                          );
                                      if (!ok) {
                                        CustomToast.show(
                                          'chat_agent_receive_save_failed'.tr,
                                        );
                                        return;
                                      }
                                      CustomToast.show(
                                        'chat_agent_receive_saved'.tr,
                                        isError: false,
                                      );
                                      return;
                                    }
                                    if (value == 'promote') {
                                      final ok = await controller
                                          .updateGroupMemberRole(
                                            member,
                                            role: 2,
                                          );
                                      if (!ok) {
                                        CustomToast.show(
                                          'chat_member_action_failed'.tr,
                                        );
                                        return;
                                      }
                                      CustomToast.show(
                                        'chat_member_role_updated'.tr,
                                        isError: false,
                                      );
                                      return;
                                    }
                                    if (value == 'demote') {
                                      final ok = await controller
                                          .updateGroupMemberRole(
                                            member,
                                            role: 1,
                                          );
                                      if (!ok) {
                                        CustomToast.show(
                                          'chat_member_action_failed'.tr,
                                        );
                                        return;
                                      }
                                      CustomToast.show(
                                        'chat_member_role_updated'.tr,
                                        isError: false,
                                      );
                                      return;
                                    }
                                    if (value == 'remove') {
                                      final confirmed =
                                          await showChatRemoveMemberConfirm(
                                            controller,
                                            sheetContext,
                                            displayName,
                                          );
                                      if (!confirmed) return;
                                      final removedCount = await controller
                                          .removeGroupMember(member);
                                      if (removedCount <= 0) {
                                        CustomToast.show(
                                          'chat_member_action_failed'.tr,
                                        );
                                        return;
                                      }
                                      CustomToast.show(
                                        'chat_member_removed'.tr,
                                        isError: false,
                                      );
                                      return;
                                    }
                                    if (value == 'leave') {
                                      final confirmed =
                                          await showChatLeaveGroupConfirm(
                                            controller,
                                            sheetContext,
                                          );
                                      if (!confirmed) return;
                                      final ok = await controller.leaveGroup();
                                      if (!ok) {
                                        CustomToast.show(
                                          'chat_leave_group_failed'.tr,
                                        );
                                        return;
                                      }
                                      if (sheetContext.mounted) {
                                        CustomToast.show(
                                          'chat_left_group'.tr,
                                          isError: false,
                                        );
                                        Navigator.of(sheetContext).pop();
                                        if (Get.key.currentState?.canPop() ??
                                            false) {
                                          Get.back();
                                        }
                                      }
                                      return;
                                    }
                                    if (value == 'transfer') {
                                      final confirmed =
                                          await showChatTransferOwnerConfirm(
                                            controller,
                                            sheetContext,
                                            displayName,
                                          );
                                      if (!confirmed) return;
                                      final ok = await controller
                                          .transferGroupOwner(member);
                                      if (!ok) {
                                        CustomToast.show(
                                          'chat_member_action_failed'.tr,
                                        );
                                        return;
                                      }
                                      CustomToast.show(
                                        'chat_owner_transferred'.tr,
                                        isError: false,
                                      );
                                      return;
                                    }
                                    if (value == 'mute' || value == 'unmute') {
                                      final ok = await controller
                                          .updateGroupMemberSpeaking(
                                            member,
                                            isSpeakMuted: value == 'mute',
                                          );
                                      if (!ok) {
                                        CustomToast.show(
                                          'chat_member_action_failed'.tr,
                                        );
                                        return;
                                      }
                                      CustomToast.show(
                                        'chat_group_member_speaking_updated'.tr,
                                        isError: false,
                                      );
                                      return;
                                    }
                                    if (value == 'allow_speaking' ||
                                        value == 'remove_allow_speaking') {
                                      final ok = await controller
                                          .updateGroupMemberSpeaking(
                                            member,
                                            canSpeakWhenAllMuted:
                                                value == 'allow_speaking',
                                          );
                                      if (!ok) {
                                        CustomToast.show(
                                          'chat_member_action_failed'.tr,
                                        );
                                        return;
                                      }
                                      CustomToast.show(
                                        'chat_group_member_speaking_updated'.tr,
                                        isError: false,
                                      );
                                    }
                                  },
                                  itemBuilder: (_) => [
                                    if (canUpdateAgentReceive)
                                      PopupMenuItem<String>(
                                        value: 'agent_receive_normal',
                                        child: Text(
                                          'chat_agent_receive_mode_full'.tr,
                                        ),
                                      ),
                                    if (canUpdateAgentReceive)
                                      PopupMenuItem<String>(
                                        value: 'agent_receive_mention_only',
                                        child: Text(
                                          'chat_agent_receive_mode_mention_only'
                                              .tr,
                                        ),
                                      ),
                                    if (canTransfer)
                                      PopupMenuItem<String>(
                                        value: 'transfer',
                                        child: Text('chat_transfer_owner'.tr),
                                      ),
                                    if (canPromote)
                                      PopupMenuItem<String>(
                                        value: 'promote',
                                        child: Text('chat_set_admin'.tr),
                                      ),
                                    if (canDemote)
                                      PopupMenuItem<String>(
                                        value: 'demote',
                                        child: Text('chat_cancel_admin'.tr),
                                      ),
                                    if (canUpdateSpeaking)
                                      PopupMenuItem<String>(
                                        value: isSpeakMuted ? 'unmute' : 'mute',
                                        child: Text(
                                          isSpeakMuted
                                              ? 'chat_group_member_unmute'.tr
                                              : 'chat_group_member_mute'.tr,
                                        ),
                                      ),
                                    if (canToggleWhitelist && !isSpeakMuted)
                                      PopupMenuItem<String>(
                                        value: canSpeakWhenAllMuted
                                            ? 'remove_allow_speaking'
                                            : 'allow_speaking',
                                        child: Text(
                                          canSpeakWhenAllMuted
                                              ? 'chat_group_member_whitelist_remove'
                                                    .tr
                                              : 'chat_group_member_whitelist_add'
                                                    .tr,
                                        ),
                                      ),
                                    if (canRemove)
                                      PopupMenuItem<String>(
                                        value: 'remove',
                                        child: Text('chat_remove_member'.tr),
                                      ),
                                    if (canLeave)
                                      PopupMenuItem<String>(
                                        value: 'leave',
                                        child: Text(
                                          'chat_leave_group'.tr,
                                          style: const TextStyle(
                                            color: AppTheme.errorColor,
                                          ),
                                        ),
                                      ),
                                  ],
                                ),
                        );
                      },
                    ),
                  );
                }),
              ],
            ),
          );
        },
      );
    },
  );
}

String describeChatAgentReceiveMode(
  ChatController controller,
  Map<String, dynamic> member,
) {
  final mode = controller.groupMemberAgentReceiveMode(member);
  switch (mode) {
    case 2:
      // ModeAll：群内有问必答（私聊转群后原 agent 的状态），与"仅@触发"语义相反。
      return 'chat_agent_receive_mode_all'.tr;
    case 3:
      return 'chat_agent_receive_mode_mention_only'.tr;
    default:
      return 'chat_agent_receive_mode_full'.tr;
  }
}

void showChatInviteFriendsSheet(
  ChatController controller,
  BuildContext pageContext, {
  required double fontScale,
}) {
  if (!controller.canInviteGroupMembers) {
    return;
  }
  controller.ensureFriendListLoaded();
  controller.loadAgents();
  final selectedUserIds = <String>{};
  final selectedAgentIds = <String>{};
  var submitting = false;
  var keyword = '';
  final searchController = TextEditingController();
  showModalBottomSheet(
    context: pageContext,
    isScrollControlled: true,
    constraints: const BoxConstraints(maxWidth: 420),
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
    ),
    builder: (sheetContext) {
      return StatefulBuilder(
        builder: (context, setModalState) {
          Future<void> submitInvite() async {
            if ((selectedUserIds.isEmpty && selectedAgentIds.isEmpty) ||
                submitting) {
              return;
            }
            setModalState(() {
              submitting = true;
            });
            final addedCount = await controller.inviteToGroup(
              userIds: selectedUserIds.toList(),
              agentIds: selectedAgentIds.toList(),
            );
            if (sheetContext.mounted) {
              setModalState(() {
                submitting = false;
              });
              if (addedCount < 0) {
                final errorMessage = controller.lastInviteToGroupErrorMessage
                    .trim();
                CustomToast.show(
                  errorMessage.isNotEmpty
                      ? errorMessage
                      : 'chat_add_members_failed'.tr,
                );
                return;
              }
              CustomToast.show(
                'chat_add_members_success'.trParams({'count': '$addedCount'}),
                isError: false,
              );
              Navigator.of(sheetContext).pop();
            }
          }

          return buildChatBottomSheetFrame(
            sheetContext,
            maxHeightFactor: 0.76,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                buildChatBottomSheetHandle(sheetContext),
                Padding(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 16,
                    vertical: 6,
                  ),
                  child: Row(
                    children: [
                      const Icon(Icons.person_add_alt_1_rounded, size: 18),
                      const SizedBox(width: 8),
                      Text(
                        'chat_invite_friends'.tr,
                        style: TextStyle(
                          fontSize: 15 * fontScale,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const Spacer(),
                      TextButton(
                        onPressed:
                            (selectedUserIds.isEmpty &&
                                    selectedAgentIds.isEmpty) ||
                                submitting
                            ? null
                            : submitInvite,
                        child: submitting
                            ? const SizedBox(
                                width: 16,
                                height: 16,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                ),
                              )
                            : Text('common_confirm'.tr),
                      ),
                    ],
                  ),
                ),
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 4, 16, 8),
                  child: TextField(
                    controller: searchController,
                    onChanged: (v) {
                      final actual = searchController.text.trim();
                      setModalState(() => keyword = actual);
                    },
                    textInputAction: TextInputAction.search,
                    onSubmitted: (_) {
                      final actual = searchController.text.trim();
                      setModalState(() => keyword = actual);
                      WidgetsBinding.instance.addPostFrameCallback((_) {
                        FocusManager.instance.primaryFocus?.unfocus();
                      });
                    },
                    decoration: InputDecoration(
                      hintText: 'conversations_search'.tr,
                      prefixIcon: const Icon(Icons.search, size: 20),
                      isDense: true,
                      border: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(10),
                        borderSide: BorderSide.none,
                      ),
                      filled: true,
                      contentPadding: const EdgeInsets.symmetric(
                        vertical: 8,
                        horizontal: 12,
                      ),
                    ),
                  ),
                ),
                Flexible(
                  child: Obx(() {
                    final kw = keyword.toLowerCase();
                    final friends = controller.invitableFriends
                        .where(
                          (f) =>
                              kw.isEmpty ||
                              f.nickname.toLowerCase().contains(kw) ||
                              f.username.toLowerCase().contains(kw),
                        )
                        .toList();
                    final agents = controller.invitableAgents
                        .where(
                          (a) =>
                              kw.isEmpty ||
                              a.agentName.toLowerCase().contains(kw),
                        )
                        .toList();
                    if (friends.isEmpty && agents.isEmpty) {
                      return Padding(
                        padding: const EdgeInsets.symmetric(vertical: 24),
                        child: Text('chat_no_invitable_friends'.tr),
                      );
                    }

                    return ListView(
                      shrinkWrap: true,
                      children: [
                        if (agents.isNotEmpty) ...[
                          Padding(
                            padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
                            child: Text(
                              'ai_agents_title'.tr,
                              style: TextStyle(
                                fontSize: 12 * fontScale,
                                fontWeight: FontWeight.w600,
                                color: Theme.of(context).colorScheme.primary,
                              ),
                            ),
                          ),
                          ...agents.map((agent) {
                            final agentId = agent.id.trim();
                            final selected = selectedAgentIds.contains(agentId);
                            return CheckboxListTile(
                              value: selected,
                              onChanged: submitting
                                  ? null
                                  : (checked) {
                                      setModalState(() {
                                        if (checked == true) {
                                          selectedAgentIds.add(agentId);
                                        } else {
                                          selectedAgentIds.remove(agentId);
                                        }
                                      });
                                    },
                              title: Text(agent.agentName),
                              subtitle: Text(
                                agent.providerType == 3
                                    ? 'ai_provider_agent_api'.tr
                                    : agent.providerType == 2
                                    ? 'ai_provider_local'.tr
                                    : agent.modelProvider.isNotEmpty
                                    ? agent.modelProvider
                                    : 'ai_provider_remote'.tr,
                              ),
                              secondary: SizedBox(
                                width: 40,
                                height: 40,
                                child: agent.avatarUrl.isNotEmpty
                                    ? ClipRRect(
                                        borderRadius: BorderRadius.zero,
                                        child: AvatarNetworkImage(
                                          avatarUrl: agent.avatarUrl,
                                          fallback: SessionAvatar(
                                            isGroup: false,
                                            avatarTitle: agent.agentName,
                                            avatarColor:
                                                AppTheme.getAvatarColor(
                                                  agentId,
                                                ),
                                            size: 40,
                                          ),
                                          width: 40,
                                          height: 40,
                                        ),
                                      )
                                    : SessionAvatar(
                                        isGroup: false,
                                        avatarTitle: agent.agentName,
                                        avatarColor: AppTheme.getAvatarColor(
                                          agentId,
                                        ),
                                        size: 40,
                                      ),
                              ),
                              controlAffinity: ListTileControlAffinity.leading,
                            );
                          }),
                        ],
                        if (friends.isNotEmpty) ...[
                          Padding(
                            padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
                            child: Text(
                              'contacts_friends'.tr,
                              style: TextStyle(
                                fontSize: 12 * fontScale,
                                fontWeight: FontWeight.w600,
                                color: Theme.of(context).colorScheme.primary,
                              ),
                            ),
                          ),
                          ...friends.map((friend) {
                            final userId = friend.userId.trim();
                            final selected = selectedUserIds.contains(userId);
                            final displayName =
                                friend.nickname.trim().isNotEmpty
                                ? friend.nickname.trim()
                                : friend.username.trim();
                            return CheckboxListTile(
                              value: selected,
                              onChanged: submitting
                                  ? null
                                  : (checked) {
                                      setModalState(() {
                                        if (checked == true) {
                                          selectedUserIds.add(userId);
                                        } else {
                                          selectedUserIds.remove(userId);
                                        }
                                      });
                                    },
                              title: Text(displayName),
                              subtitle: Text('@${friend.username}'),
                              secondary: SizedBox(
                                width: 40,
                                height: 40,
                                child: friend.avatarUrl.isNotEmpty
                                    ? ClipRRect(
                                        borderRadius: BorderRadius.zero,
                                        child: AvatarNetworkImage(
                                          avatarUrl: friend.avatarUrl,
                                          fallback: SessionAvatar(
                                            isGroup: false,
                                            avatarTitle: displayName,
                                            avatarColor:
                                                AppTheme.getAvatarColor(userId),
                                            size: 40,
                                          ),
                                          width: 40,
                                          height: 40,
                                        ),
                                      )
                                    : SessionAvatar(
                                        isGroup: false,
                                        avatarTitle: displayName,
                                        avatarColor: AppTheme.getAvatarColor(
                                          userId,
                                        ),
                                        size: 40,
                                      ),
                              ),
                              controlAffinity: ListTileControlAffinity.leading,
                            );
                          }),
                        ],
                      ],
                    );
                  }),
                ),
              ],
            ),
          );
        },
      );
    },
  );
}

Future<String?> showChatForwardConversationDialog({
  required BuildContext context,
  required String sourceTitle,
  required String targetTitle,
  required bool sourceIsGroup,
  required String sourceAvatarTitle,
  String sourceAvatarUrl = '',
  List<SessionAvatarMember> sourceAvatarMembers = const <SessionAvatarMember>[],
}) {
  return showAppDialog<String>(
    context: context,
    builder: (_) => _ChatForwardConversationDialog(
      sourceTitle: sourceTitle,
      targetTitle: targetTitle,
      sourceIsGroup: sourceIsGroup,
      sourceAvatarTitle: sourceAvatarTitle,
      sourceAvatarUrl: sourceAvatarUrl,
      sourceAvatarMembers: sourceAvatarMembers,
    ),
  );
}

class _ChatForwardConversationDialog extends StatefulWidget {
  const _ChatForwardConversationDialog({
    required this.sourceTitle,
    required this.targetTitle,
    required this.sourceIsGroup,
    required this.sourceAvatarTitle,
    required this.sourceAvatarUrl,
    required this.sourceAvatarMembers,
  });

  final String sourceTitle;
  final String targetTitle;
  final bool sourceIsGroup;
  final String sourceAvatarTitle;
  final String sourceAvatarUrl;
  final List<SessionAvatarMember> sourceAvatarMembers;

  @override
  State<_ChatForwardConversationDialog> createState() =>
      _ChatForwardConversationDialogState();
}

class _ChatForwardConversationDialogState
    extends State<_ChatForwardConversationDialog> {
  static const int _maxMessageLength = 100000;

  final TextEditingController _messageController = TextEditingController();

  @override
  void dispose() {
    _messageController.dispose();
    super.dispose();
  }

  void _cancel() => Navigator.of(context).pop();

  void _forward() => Navigator.of(context).pop(_messageController.text);

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final sourceTitle = widget.sourceTitle.trim().isEmpty
        ? 'chat_empty'.tr
        : widget.sourceTitle.trim();

    return AlertDialog(
      scrollable: true,
      title: Text('chat_forward_conversation_card'.tr),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              'chat_forward_conversation_recipient'.trParams({
                'name': widget.targetTitle,
              }),
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 12),
            Container(
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: theme.colorScheme.surfaceContainerHighest.withValues(
                  alpha: 0.55,
                ),
                borderRadius: BorderRadius.circular(12),
                border: Border.all(
                  color: theme.colorScheme.outlineVariant.withValues(
                    alpha: 0.7,
                  ),
                ),
              ),
              child: Row(
                children: [
                  SessionAvatar(
                    isGroup: widget.sourceIsGroup,
                    avatarTitle: widget.sourceAvatarTitle,
                    avatarColor: theme.colorScheme.primary,
                    memberFallbackColor: theme.colorScheme.primary,
                    avatarUrl: widget.sourceIsGroup
                        ? ''
                        : widget.sourceAvatarUrl,
                    members: widget.sourceAvatarMembers,
                    size: 40,
                    borderRadius: 8,
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          sourceTitle,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.titleSmall,
                        ),
                        const SizedBox(height: 2),
                        Text(
                          'chat_forward_conversation_card'.tr,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 16),
            TextField(
              key: const ValueKey('chat_forward_conversation_message_input'),
              controller: _messageController,
              autofocus: true,
              minLines: 3,
              maxLines: 7,
              keyboardType: TextInputType.multiline,
              textInputAction: TextInputAction.newline,
              inputFormatters: [
                LengthLimitingTextInputFormatter(_maxMessageLength),
              ],
              decoration: InputDecoration(
                alignLabelWithHint: true,
                hintText: 'chat_forward_conversation_message_hint'.tr,
              ),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(onPressed: _cancel, child: Text('common_cancel'.tr)),
        FilledButton(
          key: const ValueKey('chat_forward_conversation_confirm'),
          onPressed: _forward,
          child: Text('chat_forward'.tr),
        ),
      ],
    );
  }
}

Future<void> showChatRenameDialog(
  ChatController controller,
  BuildContext context,
) async {
  final nextTitle = await showAppInputDialog(
    context: context,
    title: 'chat_rename'.tr,
    initialValue: controller.displayChatTitle,
    hintText: 'chat_rename_input_hint'.tr,
    helperText: 'chat_rename_empty_hint'.tr,
    maxLength: 255,
  );

  if (nextTitle == null) return;
  final ok = await controller.renameCurrentSession(nextTitle);
  if (ok) {
    CustomToast.show('chat_rename_success'.tr, isError: false);
    return;
  }
  CustomToast.show('chat_rename_failed'.tr);
}

Future<void> showChatSetGroupNicknameDialog(
  ChatController controller,
  BuildContext context,
) async {
  final nextNickname = await showAppInputDialog(
    context: context,
    title: 'chat_set_group_nickname'.tr,
    initialValue: controller.myGroupNickname,
    hintText: 'chat_group_nickname_input_hint'.tr,
    helperText: 'chat_group_nickname_empty_hint'.tr,
    maxLength: 255,
  );

  if (nextNickname == null) return;
  final ok = await controller.setMyGroupNickname(nextNickname);
  if (ok) {
    CustomToast.show('chat_group_nickname_set_success'.tr, isError: false);
    return;
  }
  CustomToast.show('chat_group_nickname_set_failed'.tr);
}

Future<void> showChatGroupRuntimeSettingsDialog(
  ChatController controller,
  BuildContext context,
) async {
  var allMembersMuted = controller.allMembersMuted;
  var allowMemberInvite = controller.allowMemberInvite;
  var submitting = false;

  await showAppDialog<void>(
    context: context,
    builder: (dialogContext) => StatefulBuilder(
      builder: (context, setState) => AlertDialog(
        title: Text('chat_group_runtime_settings'.tr),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SwitchListTile.adaptive(
              contentPadding: EdgeInsets.zero,
              value: allMembersMuted,
              onChanged: submitting
                  ? null
                  : (value) {
                      setState(() {
                        allMembersMuted = value;
                      });
                    },
              title: Text('chat_group_all_members_muted'.tr),
              subtitle: Text('chat_group_all_members_muted_desc'.tr),
            ),
            if (allMembersMuted) ...[
              const SizedBox(height: 8),
              Text(
                'chat_group_all_members_muted_hint'.tr,
                style: Theme.of(dialogContext).textTheme.bodySmall,
              ),
              const SizedBox(height: 8),
            ],
            SwitchListTile.adaptive(
              contentPadding: EdgeInsets.zero,
              value: allowMemberInvite,
              onChanged: submitting
                  ? null
                  : (value) {
                      setState(() {
                        allowMemberInvite = value;
                      });
                    },
              title: Text('chat_group_allow_member_invite'.tr),
              subtitle: Text('chat_group_allow_member_invite_desc'.tr),
            ),
            const SizedBox(height: 8),
            Text(
              'chat_group_member_invite_threshold_desc'.trParams({
                'count': '${controller.memberInviteThreshold}',
              }),
              style: Theme.of(dialogContext).textTheme.bodySmall,
            ),
            if (controller.groupMemberCount >
                    controller.memberInviteThreshold &&
                controller.memberInviteThreshold > 0) ...[
              const SizedBox(height: 8),
              Text(
                'chat_member_invite_threshold_reached'.trParams({
                  'count': '${controller.memberInviteThreshold}',
                }),
                style: Theme.of(dialogContext).textTheme.bodySmall?.copyWith(
                  color: Theme.of(dialogContext).colorScheme.error,
                ),
              ),
            ],
          ],
        ),
        actions: [
          TextButton(
            onPressed: submitting
                ? null
                : () => Navigator.of(dialogContext).pop(),
            child: Text('common_cancel'.tr),
          ),
          TextButton(
            onPressed: submitting
                ? null
                : () async {
                    setState(() {
                      submitting = true;
                    });
                    var ok = true;
                    if (allMembersMuted != controller.allMembersMuted) {
                      ok = await controller.updateGroupAllMembersMuted(
                        allMembersMuted,
                      );
                    }
                    if (ok &&
                        allowMemberInvite != controller.allowMemberInvite) {
                      ok = await controller.updateGroupInviteSetting(
                        allowMemberInvite,
                      );
                    }
                    if (!dialogContext.mounted) {
                      return;
                    }
                    setState(() {
                      submitting = false;
                    });
                    if (!ok) {
                      CustomToast.show(
                        'chat_group_runtime_settings_save_failed'.tr,
                      );
                      return;
                    }
                    Navigator.of(dialogContext).pop();
                    CustomToast.show(
                      'chat_group_runtime_settings_saved'.tr,
                      isError: false,
                    );
                  },
            child: submitting
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Text('common_save'.tr),
          ),
        ],
      ),
    ),
  );
}

Future<void> showChatNotificationSettingSheet(
  ChatController controller,
  BuildContext context,
) async {
  await showModalBottomSheet<void>(
    context: context,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
    ),
    builder: (sheetContext) {
      final currentlyMuted = controller.isCurrentSessionMuted;
      return buildChatBottomSheetFrame(
        sheetContext,
        maxHeightFactor: 0.72,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            buildChatBottomSheetHandle(sheetContext),
            ListTile(
              leading: Icon(
                Icons.notifications_active_outlined,
                color: currentlyMuted
                    ? null
                    : Theme.of(sheetContext).colorScheme.primary,
              ),
              title: Text('chat_notification_enable'.tr),
              trailing: currentlyMuted ? null : const Icon(Icons.check_rounded),
              onTap: () async {
                Navigator.of(sheetContext).pop();
                if (!currentlyMuted) {
                  return;
                }
                final ok = await controller.setCurrentSessionMuted(false);
                if (ok) {
                  CustomToast.show(
                    'chat_notification_enable_success'.tr,
                    isError: false,
                  );
                  return;
                }
                CustomToast.show('chat_notification_update_failed'.tr);
              },
            ),
            ListTile(
              leading: Icon(
                Icons.notifications_off_outlined,
                color: currentlyMuted
                    ? Theme.of(sheetContext).colorScheme.primary
                    : null,
              ),
              title: Text('chat_notification_disable'.tr),
              subtitle: Text('chat_notification_disable_hint'.tr),
              trailing: currentlyMuted ? const Icon(Icons.check_rounded) : null,
              onTap: () async {
                Navigator.of(sheetContext).pop();
                if (currentlyMuted) {
                  return;
                }
                final ok = await controller.setCurrentSessionMuted(true);
                if (ok) {
                  CustomToast.show(
                    'chat_notification_disable_success'.tr,
                    isError: false,
                  );
                  return;
                }
                CustomToast.show('chat_notification_update_failed'.tr);
              },
            ),
            SizedBox(height: MediaQuery.of(sheetContext).padding.bottom),
          ],
        ),
      );
    },
  );
}

Future<bool> showChatDeleteConfirm(
  ChatController controller,
  BuildContext context,
) {
  return showAppConfirmDialog(
    context: context,
    title: 'common_confirm'.tr,
    message:
        '${'conversations_delete_confirm'.tr}\n${'conversations_delete_local_only'.tr}',
    confirmText: 'common_delete'.tr,
    isDestructive: true,
  );
}

Future<bool> showChatRemoveMemberConfirm(
  ChatController controller,
  BuildContext context,
  String displayName,
) {
  return showAppConfirmDialog(
    context: context,
    title: 'common_confirm'.tr,
    message: 'chat_remove_member_confirm'.trParams({'name': displayName}),
    confirmText: 'common_delete'.tr,
    isDestructive: true,
  );
}

Future<bool> showChatTransferOwnerConfirm(
  ChatController controller,
  BuildContext context,
  String displayName,
) {
  return showAppConfirmDialog(
    context: context,
    title: 'common_confirm'.tr,
    message: 'chat_transfer_owner_confirm'.trParams({'name': displayName}),
    confirmText: 'common_confirm'.tr,
    isDestructive: true,
  );
}

Future<bool> showChatDissolveGroupConfirm(
  ChatController controller,
  BuildContext context,
) {
  return showAppConfirmDialog(
    context: context,
    title: 'common_confirm'.tr,
    message: 'chat_dissolve_group_confirm'.tr,
    confirmText: 'chat_dissolve_group'.tr,
    isDestructive: true,
  );
}

Future<bool> showChatLeaveGroupConfirm(
  ChatController controller,
  BuildContext context,
) {
  return showAppConfirmDialog(
    context: context,
    title: 'common_confirm'.tr,
    message: 'chat_leave_group_confirm'.tr,
    confirmText: 'chat_leave_group'.tr,
    isDestructive: true,
  );
}
