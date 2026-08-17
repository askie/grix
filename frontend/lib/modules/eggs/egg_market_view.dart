import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/themes/app_theme.dart';
import '../../data/providers/agent_service.dart';
import '../../data/providers/egg_market_service.dart';
import '../../shared/widgets/app_dialog_style.dart';
import 'controllers/egg_market_controller.dart';

enum _EggDetailDialogResult { install }

String _formatAgentLabel(AgentModel agent) {
  final agentName = agent.agentName.trim();
  final rawType = agent.agentClientType.trim().toLowerCase();
  final typeKey = 'ai_agent_client_type_$rawType';
  final clientType = typeKey.tr != typeKey ? typeKey.tr : rawType;
  final name = agentName.isEmpty ? '#${agent.id.trim()}' : agentName;
  if (clientType.isEmpty) {
    return name;
  }
  return '$clientType: $name';
}

AgentModel? _findAgent(List<AgentModel> agents, String id) {
  final normalized = id.trim();
  for (final agent in agents) {
    if (agent.id.trim() == normalized) return agent;
  }
  return null;
}

bool _canHatchNewAgent(EggMarketEggModel egg, AgentModel agent) {
  return egg.canCreateAgent && agent.isMain;
}

bool _canInstallToExistingAgent(EggMarketEggModel egg, AgentModel agent) {
  final normalizedClientType = agent.agentClientType.trim().toLowerCase();
  if (normalizedClientType.isEmpty) return false;
  return egg.existingAgentClientTypes.any(
    (clientType) => clientType.trim().toLowerCase() == normalizedClientType,
  );
}

bool _usesExistingAgentInstall(EggMarketEggModel egg, AgentModel agent) {
  return _canInstallToExistingAgent(egg, agent);
}

List<AgentModel> _sortedAgents(List<AgentModel> agents) {
  return [...agents]
    ..sort((a, b) => _formatAgentLabel(a).compareTo(_formatAgentLabel(b)));
}

Future<String?> _showAgentPickerSheet(
  BuildContext context,
  List<AgentModel> agents,
  String selectedID,
) {
  return showModalBottomSheet<String>(
    context: context,
    showDragHandle: true,
    useSafeArea: true,
    builder: (sheetContext) => _AgentPickerSheet(
      agents: _sortedAgents(agents),
      selectedID: selectedID,
    ),
  );
}

class _AgentPickerSheet extends StatefulWidget {
  const _AgentPickerSheet({required this.agents, required this.selectedID});

  final List<AgentModel> agents;
  final String selectedID;

  @override
  State<_AgentPickerSheet> createState() => _AgentPickerSheetState();
}

class _AgentPickerSheetState extends State<_AgentPickerSheet> {
  static const double _itemExtent = 56;

  final ScrollController _scrollController = ScrollController();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _revealSelected());
  }

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
  }

  void _revealSelected() {
    if (!_scrollController.hasClients) return;
    final index = widget.agents.indexWhere(
      (agent) => agent.id.trim() == widget.selectedID.trim(),
    );
    if (index < 0) return;
    final position = _scrollController.position;
    final centered =
        index * _itemExtent - (position.viewportDimension - _itemExtent) / 2;
    _scrollController.jumpTo(centered.clamp(0.0, position.maxScrollExtent));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: Text(
              'eggs_pond_select_agent_label'.tr,
              style: theme.textTheme.titleSmall,
            ),
          ),
          Flexible(
            child: ListView.builder(
              controller: _scrollController,
              shrinkWrap: true,
              itemExtent: _itemExtent,
              itemCount: widget.agents.length,
              itemBuilder: (itemContext, index) {
                final agent = widget.agents[index];
                final isSelected = agent.id.trim() == widget.selectedID.trim();
                return ListTile(
                  key: ValueKey('egg_market_agent_option_${agent.id}'),
                  selected: isSelected,
                  title: Text(
                    _formatAgentLabel(agent),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  trailing: isSelected
                      ? Icon(
                          Icons.check_rounded,
                          color: theme.colorScheme.primary,
                        )
                      : null,
                  onTap: () => Navigator.of(context).pop(agent.id),
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

String _formatEggVersion(EggMarketEggModel egg) {
  return 'v${egg.version}';
}

Widget _buildEggChip(BuildContext context, String text) {
  return Container(
    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
    decoration: BoxDecoration(
      color: Theme.of(context).colorScheme.surfaceContainerHighest,
      borderRadius: BorderRadius.circular(6),
    ),
    child: Text(
      text,
      style: const TextStyle(fontSize: 11, fontWeight: FontWeight.w600),
    ),
  );
}

Widget _buildEggSectionTitle(BuildContext context, String text) {
  return Text(
    text,
    style: Theme.of(
      context,
    ).textTheme.bodyMedium?.copyWith(fontWeight: FontWeight.w700),
  );
}

Color _parseEggColor(String rawColor, Color fallback) {
  final text = rawColor.trim();
  if (!text.startsWith('#')) {
    return fallback;
  }
  final hex = text.substring(1);
  final value = int.tryParse(hex, radix: 16);
  if (value == null) {
    return fallback;
  }
  if (hex.length == 6) {
    return Color(0xFF000000 | value);
  }
  if (hex.length == 8) {
    return Color(value);
  }
  return fallback;
}

class EggMarketView extends StatelessWidget {
  EggMarketView({super.key});

  final EggMarketController controller = Get.find<EggMarketController>();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(
        centerTitle: false,
        title: InkWell(
          borderRadius: BorderRadius.circular(10),
          onTap: controller.scrollToTop,
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 8),
            child: Text(
              'eggs_pond_title'.tr,
              style: theme.textTheme.titleLarge?.copyWith(fontSize: 18),
            ),
          ),
        ),
        actions: [
          IconButton(
            onPressed: controller.refreshAll,
            icon: const Icon(Icons.refresh_rounded),
          ),
        ],
      ),
      body: Column(
        children: [
          Material(
            color: theme.scaffoldBackgroundColor,
            elevation: 1,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 6, 12, 6),
              child: _buildSearchCard(context),
            ),
          ),
          Expanded(
            child: Obx(() {
              return RefreshIndicator(
                onRefresh: controller.refreshAll,
                child: ListView(
                  key: const PageStorageKey<String>('home_eggs_pond_scroll'),
                  controller: controller.scrollController,
                  physics: const AlwaysScrollableScrollPhysics(),
                  padding: const EdgeInsets.fromLTRB(12, 12, 12, 20),
                  children: [
                    if (controller.searched.value) ...[
                      _buildSectionTitle(
                        'eggs_pond_results'.trParams({
                          'keyword': controller.currentKeyword.value,
                        }),
                      ),
                      const SizedBox(height: 8),
                      _buildEggList(
                        context,
                        eggs: controller.resultEggs,
                        emptyKey: 'eggs_pond_empty_result',
                      ),
                    ] else ...[
                      _buildSectionTitle('eggs_pond_hot'.tr),
                      const SizedBox(height: 8),
                      _buildEggList(
                        context,
                        eggs: controller.hotEggs,
                        emptyKey: 'eggs_pond_empty_hot',
                      ),
                    ],
                    if (controller.isLoading.value) ...[
                      const SizedBox(height: 16),
                      _buildLoadingText(theme),
                    ],
                    if (controller.isLoadingMore.value) ...[
                      const SizedBox(height: 16),
                      _buildLoadingText(theme),
                    ],
                  ],
                ),
              );
            }),
          ),
        ],
      ),
    );
  }

  Widget _buildSearchCard(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
        child: ValueListenableBuilder<TextEditingValue>(
          valueListenable: controller.keywordController,
          builder: (context, value, _) {
            final hasKeyword = value.text.trim().isNotEmpty;
            return Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: controller.keywordController,
                    focusNode: controller.searchFocusNode,
                    onSubmitted: (_) {
                      controller.submitSearch();
                    },
                    onTapOutside: (_) => controller.dismissSearchKeyboard(),
                    textInputAction: TextInputAction.search,
                    decoration: InputDecoration(
                      hintText: 'eggs_pond_search_placeholder'.tr,
                      prefixIcon: const Icon(Icons.search_rounded),
                      contentPadding: const EdgeInsets.symmetric(
                        horizontal: 12,
                        vertical: 10,
                      ),
                      suffixIcon: hasKeyword
                          ? IconButton(
                              key: const ValueKey(
                                'egg_market_clear_search_button',
                              ),
                              tooltip: 'common_clear'.tr,
                              onPressed: controller.clearSearch,
                              icon: const Icon(Icons.close_rounded),
                            )
                          : null,
                      border: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(10),
                      ),
                      isDense: true,
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                ElevatedButton(
                  onPressed: () {
                    controller.submitSearch();
                  },
                  style: ElevatedButton.styleFrom(
                    minimumSize: const Size(72, 40),
                    padding: const EdgeInsets.symmetric(
                      horizontal: 14,
                      vertical: 10,
                    ),
                    backgroundColor: theme.colorScheme.primary,
                    foregroundColor: theme.colorScheme.onPrimary,
                  ),
                  child: Text('eggs_pond_search'.tr),
                ),
              ],
            );
          },
        ),
      ),
    );
  }

  Widget _buildSectionTitle(String title) {
    return Text(
      title,
      style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
    );
  }

  Widget _buildLoadingText(ThemeData theme) {
    return Center(
      child: Text(
        'eggs_pond_loading'.tr,
        style: TextStyle(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
        ),
      ),
    );
  }

  Widget _buildEggList(
    BuildContext context, {
    required List<EggMarketEggModel> eggs,
    required String emptyKey,
  }) {
    if (eggs.isEmpty) {
      return Container(
        width: double.infinity,
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 22),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(12),
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
        ),
        child: Text(
          emptyKey.tr,
          textAlign: TextAlign.center,
          style: TextStyle(
            color: Theme.of(
              context,
            ).colorScheme.onSurface.withValues(alpha: 0.7),
          ),
        ),
      );
    }

    return Column(
      children: eggs.map((egg) {
        return Padding(
          padding: const EdgeInsets.only(bottom: 10),
          child: _EggCard(
            egg: egg,
            onOpenDetails: () => _showEggDetailsDialog(context, egg),
          ),
        );
      }).toList(),
    );
  }

  Future<void> _showEggDetailsDialog(
    BuildContext context,
    EggMarketEggModel egg,
  ) async {
    final result = await showAppDialog<_EggDetailDialogResult>(
      context: context,
      builder: (dialogContext) => _EggDetailDialog(
        egg: egg,
        installing: controller.isInstalling.value,
        onInstall: () {
          Navigator.of(dialogContext).pop(_EggDetailDialogResult.install);
        },
      ),
    );
    if (result != _EggDetailDialogResult.install || !context.mounted) {
      return;
    }
    await _openInstallDialog(context, egg);
  }

  Future<void> _openInstallDialog(
    BuildContext context,
    EggMarketEggModel egg,
  ) async {
    final agents = _collectCompatibleAgents(egg);
    if (agents.isEmpty) return;

    AgentModel agent;
    if (agents.length == 1) {
      agent = agents.first;
    } else {
      final picked = await _showAgentPickerDialog(context, egg, agents);
      if (picked == null || !context.mounted) return;
      agent = picked;
    }

    final isExistingAgentInstall = _usesExistingAgentInstall(egg, agent);

    await controller.installEgg(
      egg: egg,
      installMode: isExistingAgentInstall
          ? EggInstallMode.existingAgent
          : EggInstallMode.createNew,
      targetAgentID: isExistingAgentInstall ? agent.id : null,
      executorAgentID: isExistingAgentInstall ? null : agent.id,
      isSkillInstall: isExistingAgentInstall,
    );
  }

  List<AgentModel> _collectCompatibleAgents(EggMarketEggModel egg) {
    final agents = <AgentModel>[];
    final seenIDs = <String>{};

    final hasAgentMode = egg.canCreateAgent;
    final hasExistingMode = egg.existingAgentClientTypes.isNotEmpty;

    if (hasAgentMode) {
      for (final agent in controller.agentsForHatchType(EggHatchType.agent)) {
        if (seenIDs.add(agent.id.trim())) agents.add(agent);
      }
    }
    if (hasExistingMode) {
      for (final agent in controller.agentsForHatchType(EggHatchType.skill)) {
        if (_canInstallToExistingAgent(egg, agent) &&
            seenIDs.add(agent.id.trim())) {
          agents.add(agent);
        }
      }
    }
    return agents;
  }

  Future<AgentModel?> _showAgentPickerDialog(
    BuildContext context,
    EggMarketEggModel egg,
    List<AgentModel> agents,
  ) async {
    return showAppDialog<AgentModel>(
      context: context,
      builder: (dialogContext) {
        var selectedID = agents.first.id;

        return StatefulBuilder(
          builder: (context, setState) {
            final theme = Theme.of(context);
            final selectedAgent = _findAgent(agents, selectedID);
            final isExistingAgentInstall =
                selectedAgent != null &&
                _usesExistingAgentInstall(egg, selectedAgent);

            return AlertDialog(
              title: Text(
                isExistingAgentInstall
                    ? 'eggs_pond_install_dialog_title_skill'.tr
                    : 'eggs_pond_install_dialog_title'.tr,
              ),
              content: SizedBox(
                width: resolveDialogConstraints(
                  context,
                  size: AppDialogSize.standard,
                ).maxWidth,
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      egg.name,
                      style: const TextStyle(
                        fontWeight: FontWeight.w700,
                        fontSize: 15,
                      ),
                    ),
                    const SizedBox(height: 12),
                    Text(
                      'eggs_pond_select_agent_label'.tr,
                      style: const TextStyle(fontWeight: FontWeight.w600),
                    ),
                    const SizedBox(height: 8),
                    InkWell(
                      key: const ValueKey('egg_market_agent_picker_field'),
                      borderRadius: BorderRadius.circular(10),
                      onTap: () async {
                        final picked = await _showAgentPickerSheet(
                          context,
                          agents,
                          selectedID,
                        );
                        if (picked != null) {
                          setState(() => selectedID = picked);
                        }
                      },
                      child: InputDecorator(
                        decoration: InputDecoration(
                          border: OutlineInputBorder(
                            borderRadius: BorderRadius.circular(10),
                          ),
                          isDense: true,
                          suffixIcon: const Icon(Icons.arrow_drop_down_rounded),
                        ),
                        child: Text(
                          selectedAgent == null
                              ? ''
                              : _formatAgentLabel(selectedAgent),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                    ),
                    if (selectedAgent != null) ...[
                      const SizedBox(height: 8),
                      Text(
                        isExistingAgentInstall
                            ? 'eggs_pond_install_hint_skill_target'.trParams({
                                'agent': _formatAgentLabel(selectedAgent),
                              })
                            : 'eggs_pond_install_hint_hatch_new'.trParams({
                                'agent': _formatAgentLabel(selectedAgent),
                              }),
                        style: TextStyle(
                          fontSize: 12,
                          color: theme.colorScheme.onSurface.withValues(
                            alpha: 0.6,
                          ),
                        ),
                      ),
                    ],
                  ],
                ),
              ),
              actions: [
                TextButton(
                  onPressed: () => Navigator.of(dialogContext).pop(),
                  child: Text('common_cancel'.tr),
                ),
                ElevatedButton(
                  onPressed: selectedAgent == null
                      ? null
                      : () => Navigator.of(dialogContext).pop(selectedAgent),
                  child: Text(
                    isExistingAgentInstall
                        ? 'eggs_pond_install_confirm_skill'.tr
                        : 'eggs_pond_install_confirm'.tr,
                  ),
                ),
              ],
            );
          },
        );
      },
    );
  }
}

class _EggCard extends StatelessWidget {
  const _EggCard({required this.egg, required this.onOpenDetails});

  final EggMarketEggModel egg;
  final VoidCallback onOpenDetails;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final color = _parseEggColor(egg.color, theme.colorScheme.primary);

    return Card(
      margin: EdgeInsets.zero,
      child: InkWell(
        borderRadius: BorderRadius.circular(12),
        onTap: onOpenDetails,
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Container(
                    width: 42,
                    height: 42,
                    alignment: Alignment.center,
                    decoration: BoxDecoration(
                      color: color.withValues(alpha: 0.15),
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: Text(
                      egg.emoji,
                      style: const TextStyle(fontSize: 20),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          egg.name,
                          style: const TextStyle(
                            fontSize: 15,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        const SizedBox(height: 2),
                        Text(
                          _formatEggVersion(egg),
                          style: TextStyle(
                            color: theme.colorScheme.onSurface.withValues(
                              alpha: 0.62,
                            ),
                            fontSize: 12,
                          ),
                        ),
                      ],
                    ),
                  ),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 4,
                    ),
                    decoration: BoxDecoration(
                      color: AppTheme.successColor.withValues(alpha: 0.12),
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: Text(
                      '🔥 ${egg.installCount}',
                      style: const TextStyle(
                        fontWeight: FontWeight.w600,
                        fontSize: 12,
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 10),
              Text(
                egg.description,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  color: theme.colorScheme.onSurface.withValues(alpha: 0.75),
                ),
              ),
              if (egg.vibe.trim().isNotEmpty) ...[
                const SizedBox(height: 10),
                Wrap(
                  spacing: 8,
                  runSpacing: 6,
                  children: [_buildEggChip(context, egg.vibe)],
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _EggDetailDialog extends StatelessWidget {
  const _EggDetailDialog({
    required this.egg,
    required this.installing,
    required this.onInstall,
  });

  final EggMarketEggModel egg;
  final bool installing;
  final VoidCallback onInstall;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final color = _parseEggColor(egg.color, theme.colorScheme.primary);

    return AlertDialog(
      title: Row(
        children: [
          Expanded(child: Text('eggs_pond_detail_title'.tr)),
          IconButton(
            tooltip: 'common_cancel'.tr,
            onPressed: () => Navigator.of(context).pop(),
            icon: const Icon(Icons.close_rounded),
          ),
        ],
      ),
      content: SizedBox(
        width: resolveDialogConstraints(
          context,
          size: AppDialogSize.standard,
        ).maxWidth,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Container(
                    width: 52,
                    height: 52,
                    alignment: Alignment.center,
                    decoration: BoxDecoration(
                      color: color.withValues(alpha: 0.15),
                      borderRadius: BorderRadius.circular(14),
                    ),
                    child: Text(
                      egg.emoji,
                      style: const TextStyle(fontSize: 26),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          egg.name,
                          style: const TextStyle(
                            fontSize: 16,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        const SizedBox(height: 4),
                        Text(
                          'eggs_pond_detail_version'.trParams({
                            'version': egg.version.toString(),
                          }),
                          style: TextStyle(
                            color: theme.colorScheme.onSurface.withValues(
                              alpha: 0.68,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 4,
                    ),
                    decoration: BoxDecoration(
                      color: AppTheme.successColor.withValues(alpha: 0.12),
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: Text(
                      'eggs_pond_detail_install_count'.trParams({
                        'count': egg.installCount.toString(),
                      }),
                      style: const TextStyle(
                        fontWeight: FontWeight.w600,
                        fontSize: 12,
                      ),
                    ),
                  ),
                ],
              ),
              if (egg.vibe.trim().isNotEmpty) ...[
                const SizedBox(height: 16),
                _buildEggSectionTitle(context, 'eggs_pond_detail_vibe'.tr),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 6,
                  children: [_buildEggChip(context, egg.vibe)],
                ),
              ],
              const SizedBox(height: 16),
              _buildEggSectionTitle(context, 'eggs_pond_detail_description'.tr),
              const SizedBox(height: 8),
              Text(
                egg.description,
                style: TextStyle(
                  color: theme.colorScheme.onSurface.withValues(alpha: 0.78),
                  height: 1.45,
                ),
              ),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: Text('common_cancel'.tr),
        ),
        ElevatedButton.icon(
          onPressed: installing ? null : onInstall,
          icon: const Icon(Icons.bolt_rounded, size: 18),
          label: Text('eggs_pond_install'.tr),
        ),
      ],
    );
  }
}
