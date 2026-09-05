import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/routes/app_route_observer.dart';
import 'controllers/agents_controller.dart';
import '../../data/providers/agent_category_service.dart';
import '../../data/providers/agent_service.dart';
import '../../data/providers/feature_flag_service.dart';
import '../../app/routes/app_routes.dart';
import '../../app/themes/app_theme.dart';
import '../../shared/widgets/app_dialog_style.dart';
import '../../shared/widgets/session_avatar.dart';
import '../../shared/utils/toast_util.dart';
import '../chat/services/chat_route_navigator.dart';
import '../system/agent_session_handoff.dart';
import '../system/remote_agent_install_sheet.dart';
import 'controllers/agent_category_manage_controller.dart';
import 'widgets/agent_quick_access_button.dart';

/// hermes 走后端 agentadapter 接入，不带 grix-connector，永远不会是通道候选。
const String _hermesClientType = 'hermes';

// --- Drag data type for category blocks ---

class _CategoryDragData {
  const _CategoryDragData(this.node, this.parentCategoryId);
  final CategoryNode node;
  final String parentCategoryId;
}

class AgentsView extends StatefulWidget {
  const AgentsView({super.key});

  @override
  State<AgentsView> createState() => _AgentsViewState();
}

class _AgentsViewState extends State<AgentsView> with RouteAware {
  late final AgentsController controller = Get.find<AgentsController>();
  late final AgentCategoryManageController _categoryManageController =
      Get.isRegistered<AgentCategoryManageController>()
      ? Get.find<AgentCategoryManageController>()
      : Get.put(AgentCategoryManageController());
  ModalRoute<dynamic>? _route;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final nextRoute = ModalRoute.of(context);
    if (nextRoute == null || identical(_route, nextRoute)) return;
    if (_route != null) appRouteObserver.unsubscribe(this);
    _route = nextRoute;
    appRouteObserver.subscribe(this, nextRoute);
  }

  @override
  void dispose() {
    appRouteObserver.unsubscribe(this);
    super.dispose();
  }

  @override
  void didPopNext() => controller.triggerRefreshForVisiblePage();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        centerTitle: false,
        title: Text(
          'ai_agents_title'.tr,
          style: theme.textTheme.titleLarge?.copyWith(fontSize: 18),
        ),
        actions: [
          Obx(
            () => IconButton(
              icon: Icon(
                controller.viewMode.value == 0
                    ? Icons.computer_outlined
                    : Icons.category_outlined,
              ),
              tooltip: controller.viewMode.value == 0
                  ? 'ai_agents_view_by_host'.tr
                  : 'ai_agents_view_by_category'.tr,
              onPressed: controller.toggleViewMode,
            ),
          ),
          IconButton(
            icon: const Icon(Icons.create_new_folder_outlined),
            onPressed: () => Get.toNamed(AppRoutes.agentCategoryManage),
          ),
          IconButton(
            key: const Key('agents-add-button'),
            icon: const Icon(Icons.add_rounded),
            onPressed: () => _showCreateMenu(context),
          ),
        ],
      ),
      body: Obx(() {
        // 自己的 agent + 「分享给我的」一并展示：被共享者像用自己的 agent 一样使用，
        // 管理类操作在底部菜单里已按 owner 隐藏。
        final agentList = <AgentModel>[
          ...controller.agentService.agents,
          ...controller.agentService.sharedAgents,
        ];
        final hasAgents = agentList.isNotEmpty;
        final currentViewMode = controller.viewMode.value;
        // Every Rx must be read here, inside the Obx closure. LayoutBuilder's
        // builder runs during layout, after this Obx has finished collecting
        // dependencies, so a read down there is invisible to it — the page
        // would stop rebuilding on category changes while the agent list itself
        // is unchanged.
        final categoryList = List<AgentCategoryModel>.of(
          controller.categoryService.categories,
        );
        final tree = List<CategoryNode>.of(_categoryManageController.treeNodes);
        return LayoutBuilder(
          builder: (context, constraints) {
            if (!hasAgents) {
              return RefreshIndicator(
                onRefresh: controller.refreshAgents,
                child: ListView(
                  physics: const AlwaysScrollableScrollPhysics(),
                  padding: EdgeInsets.zero,
                  children: [
                    SizedBox(
                      height: constraints.maxHeight,
                      child: _buildEmpty(theme),
                    ),
                  ],
                ),
              );
            }

            // 按电脑分组视图
            if (currentViewMode == 1) {
              final hostnameGroups = controller.groupByHostname(agentList);
              return RefreshIndicator(
                onRefresh: controller.refreshAgents,
                child: _buildHostnameGrid(context, hostnameGroups),
              );
            }

            final grouped = _groupAgentsByCategory(agentList);
            final uncategorized = _uncategorizedAgents(agentList, categoryList);

            final visibleRoots = <_CategoryWithAgents>[];
            for (final node in tree) {
              visibleRoots.add(_CategoryWithAgents(node: node));
            }

            return RefreshIndicator(
              onRefresh: controller.refreshAgents,
              child: _buildCategoryGrid(
                context,
                visibleRoots: visibleRoots,
                grouped: grouped,
                uncategorized: uncategorized,
              ),
            );
          },
        );
      }),
    );
  }

  // --- Helpers ---

  Map<String, List<AgentModel>> _groupAgentsByCategory(
    List<AgentModel> agents,
  ) {
    final map = <String, List<AgentModel>>{};
    for (final agent in agents) {
      final cid =
          (agent.categoryId.trim().isEmpty || agent.categoryId.trim() == '0')
          ? '_uncategorized_'
          : agent.categoryId.trim();
      map.putIfAbsent(cid, () => []).add(agent);
    }
    for (final list in map.values) {
      list.sort((a, b) {
        final cmp = a.sortOrder.compareTo(b.sortOrder);
        return cmp != 0 ? cmp : a.createdAt.compareTo(b.createdAt);
      });
    }
    return map;
  }

  List<AgentModel> _uncategorizedAgents(
    List<AgentModel> agents,
    List<AgentCategoryModel> categories,
  ) {
    final knownIds = categories.map((c) => c.id.trim()).toSet();
    final list = agents.where((a) {
      final cid = a.categoryId.trim();
      return cid.isEmpty || cid == '0' || !knownIds.contains(cid);
    }).toList();
    list.sort((a, b) {
      final cmp = a.sortOrder.compareTo(b.sortOrder);
      return cmp != 0 ? cmp : a.createdAt.compareTo(b.createdAt);
    });
    return list;
  }

  // --- Layout ---

  Widget _buildHostnameGrid(
    BuildContext context,
    Map<String, List<AgentModel>> hostnameGroups,
  ) {
    const gap = 8.0;

    // 有名字的主机按名字排，"未知主机"（老 connector 没上报过 host_meta）永远垫底。
    final namedHosts =
        hostnameGroups.keys.where((key) => key.isNotEmpty).toList()..sort();
    final orderedHosts = <String>[
      ...namedHosts,
      if (hostnameGroups.containsKey('')) '',
    ];

    final boxes = <Widget>[
      for (final host in orderedHosts)
        Padding(
          padding: const EdgeInsets.only(top: 10, bottom: gap),
          child: _buildHostBox(context, host, hostnameGroups[host]!),
        ),
    ];

    return ListView(
      key: const PageStorageKey<String>('home_agents_hostname_scroll'),
      physics: const AlwaysScrollableScrollPhysics(),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      children: boxes,
    );
  }

  Widget _buildHostBox(
    BuildContext context,
    String host,
    List<AgentModel> agents,
  ) {
    final theme = Theme.of(context);
    final borderColor = theme.colorScheme.outline.withValues(alpha: 0.3);
    final label = host.isEmpty ? 'ai_agents_unknown_host'.tr : host;

    return Stack(
      clipBehavior: Clip.none,
      alignment: Alignment.topCenter,
      children: [
        Container(
          width: double.infinity,
          margin: const EdgeInsets.only(top: 6),
          decoration: BoxDecoration(
            color: theme.brightness == Brightness.light ? Colors.white : null,
            border: Border.all(color: borderColor),
            borderRadius: BorderRadius.circular(12),
          ),
          // 顶部留够 20：胶囊从 y=-3 起、连边框 26 高，内容从 y=26 开始才不压标签。
          padding: const EdgeInsets.fromLTRB(8, 20, 8, 8),
          child: _buildAgentWrap(
            context,
            agents,
            '',
            spacing: 8,
            runSpacing: 8,
            byHost: true,
          ),
        ),
        Positioned(
          top: -3,
          left: 16,
          right: 16,
          child: Center(
            child: Container(
              padding: const EdgeInsets.only(left: 10, right: 2),
              decoration: BoxDecoration(
                color: theme.brightness == Brightness.dark
                    ? const Color(0xFF1C1B1F)
                    : Colors.white,
                borderRadius: BorderRadius.circular(8),
                border: Border.all(color: borderColor),
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(
                    Icons.computer,
                    size: 12,
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
                  ),
                  const SizedBox(width: 4),
                  Text(
                    label,
                    style: TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.w600,
                      color: theme.colorScheme.onSurface,
                      height: 1.3,
                    ),
                  ),
                  _buildHostInstallButton(context, host),
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }

  /// 主机分组头上的「安装 agent」入口：手机端没有本机 connector 的 127 admin API，
  /// 借这台机器上一个自己的、在线的、连接器声明了 connector_admin 的 agent 当通道。
  ///
  /// 四种情况，按这个顺序判断：
  /// - 有通道候选：走远程安装弹窗。
  /// - 没有通道，但这台机器上有在线的连接器 agent：连接器装了、只是版本太老
  ///   （< 4.8.1 不声明 connector_admin），提示升级连接器。
  /// - 连一个在线连接器 agent 都没有，但有在线的 hermes：这台机器根本没装连接器
  ///   （hermes 走后端适配器接入，只上报主机名），让 hermes 自己把连接器装上，
  ///   比让用户去"升级连接器"实在。
  /// - 一个在线自有 agent 都没有：置灰，说清是没装连接器或连接器离线。
  Widget _buildHostInstallButton(BuildContext context, String host) {
    final theme = Theme.of(context);
    final onlineOwned = controller.agentService.agents
        .where(
          (agent) =>
              agent.hostname == host && controller.isApiAgentOnline(agent),
        )
        .toList();
    final channels = onlineOwned
        .where((agent) => agent.supportsConnectorAdmin)
        .toList();
    // 连接器在线但没声明 connector_admin，说明连接器装了、只是版本太老。
    // 这种主机不能走 hermes 路线：连接器已经在了，要的是升级而不是重装。
    final hasLegacyConnector =
        channels.isEmpty &&
        onlineOwned.any(
          (agent) =>
              agent.providerType == 3 &&
              agent.agentClientType != _hermesClientType,
        );
    // 多个 hermes 在线时取第一个：它们都在同一台机器上，谁装都一样。
    final hermesCandidates = channels.isNotEmpty || hasLegacyConnector
        ? const <AgentModel>[]
        : onlineOwned
              .where(
                (agent) =>
                    agent.agentClientType == _hermesClientType &&
                    controller.agentService.isOwnedByMe(agent),
              )
              .toList();
    final hermes = hermesCandidates.isEmpty ? null : hermesCandidates.first;
    final enabled = onlineOwned.isNotEmpty;
    final hostLabel = host.isEmpty ? 'ai_agents_unknown_host'.tr : host;

    return Tooltip(
      message: !enabled
          ? 'remote_install_no_connector'.tr
          : hermes != null
          ? 'remote_install_hermes_action'.trParams({'agent': hermes.agentName})
          : channels.isEmpty
          ? 'remote_install_upgrade_connector'.tr
          : 'remote_install_action'.tr,
      child: IconButton(
        key: Key('host-install-${host.isEmpty ? '_unknown_' : host}'),
        // 胶囊只有二十几像素高，IconButton 默认的 padded 热区会把布局盒撑到 48，
        // constraints 拦不住它，撑高的胶囊会盖住下面瓦片左上角的类型标签。
        style: IconButton.styleFrom(
          tapTargetSize: MaterialTapTargetSize.shrinkWrap,
          minimumSize: const Size(24, 24),
        ),
        padding: const EdgeInsets.symmetric(horizontal: 4),
        constraints: const BoxConstraints(minWidth: 24, minHeight: 24),
        iconSize: 14,
        icon: Icon(
          hermes != null ? Icons.support_agent : Icons.add_circle_outline,
          color: enabled
              ? theme.colorScheme.primary
              : theme.colorScheme.onSurface.withValues(alpha: 0.3),
        ),
        onPressed: !enabled
            ? null
            : () {
                if (hermes != null) {
                  unawaited(_askHermesToInstallConnector(
                    context,
                    hermes: hermes,
                    hostLabel: hostLabel,
                  ));
                  return;
                }
                if (channels.isEmpty) {
                  CustomToast.show('remote_install_upgrade_connector'.tr);
                  return;
                }
                showRemoteAgentInstallSheet(
                  context: context,
                  hostLabel: hostLabel,
                  channelCandidates: channels,
                );
              },
      ),
    );
  }

  /// 让这台机器上的 hermes 去装 Grix 连接器：先确认，再打开与它的会话，
  /// 以主人身份把要做的事说清。装完之后用户回列表刷新，就有通道候选了。
  Future<void> _askHermesToInstallConnector(
    BuildContext context, {
    required AgentModel hermes,
    required String hostLabel,
  }) async {
    final confirmed = await showAppDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('remote_install_hermes_title'.tr),
        content: Text(
          'remote_install_hermes_confirm'.trParams({
            'agent': hermes.agentName,
            'host': hostLabel,
          }),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: Text('common_cancel'.tr),
          ),
          FilledButton(
            key: const Key('host-install-hermes-confirm'),
            onPressed: () => Navigator.pop(ctx, true),
            child: Text('remote_install_help_action'.tr),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    await openAgentSessionAndSend(
      agent: hermes,
      message: 'remote_install_hermes_message'.trParams({'host': hostLabel}),
    );
  }

  Widget _buildCategoryGrid(
    BuildContext context, {
    required List<_CategoryWithAgents> visibleRoots,
    required Map<String, List<AgentModel>> grouped,
    required List<AgentModel> uncategorized,
  }) {
    final rootIds = <String>[];
    final allBoxes = <Widget>[];

    for (final item in visibleRoots) {
      final id = item.node.model.id.trim();
      rootIds.add(id);
      allBoxes.add(
        _buildCategoryBox(
          context,
          name: item.node.model.name,
          node: item.node,
          grouped: grouped,
          categoryId: id,
          allRootIds: rootIds,
        ),
      );
    }
    if (uncategorized.isNotEmpty) {
      allBoxes.add(
        _buildCategoryBox(
          context,
          name: 'ai_agents_uncategorized'.tr,
          agents: uncategorized,
          categoryId: '0',
          allRootIds: rootIds,
        ),
      );
    }

    const gap = 8.0;

    return ListView(
      key: const PageStorageKey<String>('home_agents_scroll'),
      physics: const AlwaysScrollableScrollPhysics(),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      children: allBoxes
          .map(
            (box) => Padding(
              padding: const EdgeInsets.only(bottom: gap),
              child: box,
            ),
          )
          .toList(),
    );
  }

  /// Category box: accepts both category-drag (for reordering) and agent-drag
  /// (for cross-category move / within-category reorder).
  Widget _buildCategoryBox(
    BuildContext context, {
    required String name,
    CategoryNode? node,
    List<AgentModel>? agents,
    Map<String, List<AgentModel>>? grouped,
    String categoryId = '0',
    List<String>? allRootIds,
  }) {
    final theme = Theme.of(context);
    final directAgents =
        agents ?? (grouped?[node!.model.id.trim()] ?? <AgentModel>[]);
    final borderColor = theme.colorScheme.outline.withValues(alpha: 0.3);

    final hasSubcategories = node?.children.isNotEmpty ?? false;
    final hasContent = directAgents.isNotEmpty || hasSubcategories;

    // Only add DragTarget<AgentModel> when no subcategories exist,
    // otherwise subcategory DragTargets handle agent drops.
    Widget buildInnerContainer({Color? agentHoverBorderColor}) {
      return Container(
        width: double.infinity,
        margin: const EdgeInsets.only(top: 6),
        decoration: BoxDecoration(
          color: theme.brightness == Brightness.light ? Colors.white : null,
          border: Border.all(color: agentHoverBorderColor ?? borderColor),
          borderRadius: BorderRadius.circular(12),
        ),
        padding: const EdgeInsets.fromLTRB(8, 14, 8, 8),
        constraints: hasContent ? null : const BoxConstraints(minHeight: 60),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.center,
          mainAxisSize: MainAxisSize.min,
          children: [
            if (directAgents.isNotEmpty)
              _buildAgentWrap(
                context,
                directAgents,
                categoryId,
                spacing: 8,
                runSpacing: 8,
              ),
            if (node != null)
              for (final sub in node.children) ...[
                const SizedBox(height: 18),
                _buildSubcategoryBox(
                  context,
                  node: sub,
                  grouped: grouped ?? {},
                  parentCategoryId: categoryId,
                  siblingIds: node.children
                      .map((c) => c.model.id.trim())
                      .toList(),
                ),
              ],
          ],
        ),
      );
    }

    Widget innerContent;
    if (hasSubcategories) {
      innerContent = buildInnerContainer();
    } else {
      innerContent = DragTarget<AgentModel>(
        onWillAcceptWithDetails: (details) =>
            details.data.categoryId != categoryId,
        onAcceptWithDetails: (details) {
          controller.moveAgentToCategory(details.data, categoryId);
        },
        builder: (context, candidateData, _) {
          return buildInnerContainer(
            agentHoverBorderColor: candidateData.isNotEmpty
                ? Colors.blue
                : null,
          );
        },
      );
    }

    // Outer: DragTarget<_CategoryDragData> for category reordering
    Widget categoryDropTarget = DragTarget<_CategoryDragData>(
      onWillAcceptWithDetails: (details) {
        if (node == null) return false;
        return details.data.node.model.id.trim() != categoryId;
      },
      onAcceptWithDetails: (details) {
        _handleCategoryDropOnCategory(
          details.data,
          categoryId,
          allRootIds ?? [],
        );
      },
      builder: (context, catCandidateData, _) {
        final catHovering = catCandidateData.isNotEmpty;
        return Stack(
          clipBehavior: Clip.none,
          children: [
            innerContent,
            if (catHovering)
              Positioned(
                bottom: -2,
                left: 8,
                right: 8,
                child: Container(
                  height: 3,
                  decoration: BoxDecoration(
                    color: Colors.blue,
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
              ),
          ],
        );
      },
    );

    // Title capsule
    Widget titleCapsule;
    if (node != null) {
      titleCapsule = LongPressDraggable<_CategoryDragData>(
        data: _CategoryDragData(node, '0'),
        feedback: _buildCategoryDragFeedback(theme, name),
        childWhenDragging: Opacity(
          opacity: 0.4,
          child: _buildCapsuleLabel(theme, name, borderColor, fontSize: 11),
        ),
        child: GestureDetector(
          onDoubleTap: () => _showCategoryNameEditDialog(
            context,
            node.model.id,
            node.model.name,
          ),
          child: _buildCapsuleLabel(
            theme,
            name,
            borderColor,
            fontSize: 11,
            showDragHandle: true,
          ),
        ),
      );
    } else {
      titleCapsule = _buildCapsuleLabel(theme, name, borderColor, fontSize: 11);
    }

    return Padding(
      padding: const EdgeInsets.only(top: 10),
      child: Stack(
        clipBehavior: Clip.none,
        alignment: Alignment.topCenter,
        children: [
          categoryDropTarget,
          Positioned(
            top: -3,
            left: 16,
            right: 16,
            child: Center(child: titleCapsule),
          ),
        ],
      ),
    );
  }

  Widget _buildSubcategoryBox(
    BuildContext context, {
    required CategoryNode node,
    required Map<String, List<AgentModel>> grouped,
    required String parentCategoryId,
    required List<String> siblingIds,
  }) {
    final theme = Theme.of(context);
    final subAgents = grouped[node.model.id.trim()] ?? <AgentModel>[];
    final borderColor = theme.colorScheme.outline.withValues(alpha: 0.4);
    final categoryId = node.model.id.trim();

    Widget buildInnerBox({Color? agentHoverBorderColor}) {
      return SizedBox(
        width: double.infinity,
        child: Container(
          decoration: BoxDecoration(
            color: theme.brightness == Brightness.dark
                ? const Color(0xFF1C1B1F).withValues(alpha: 0.5)
                : Colors.white.withValues(alpha: 0.7),
            border: Border.all(color: agentHoverBorderColor ?? borderColor),
            borderRadius: BorderRadius.circular(8),
          ),
          padding: const EdgeInsets.fromLTRB(6, 14, 6, 6),
          constraints: const BoxConstraints(minHeight: 40),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.center,
            mainAxisSize: MainAxisSize.min,
            children: [
              if (subAgents.isNotEmpty)
                _buildAgentWrap(
                  context,
                  subAgents,
                  categoryId,
                  spacing: 6,
                  runSpacing: 6,
                ),
              for (final child in node.children) ...[
                const SizedBox(height: 16),
                _buildSubcategoryBox(
                  context,
                  node: child,
                  grouped: grouped,
                  parentCategoryId: categoryId,
                  siblingIds: node.children
                      .map((c) => c.model.id.trim())
                      .toList(),
                ),
              ],
            ],
          ),
        ),
      );
    }

    Widget innerContent;
    if (node.children.isNotEmpty) {
      innerContent = buildInnerBox();
    } else {
      innerContent = DragTarget<AgentModel>(
        onWillAcceptWithDetails: (details) =>
            details.data.categoryId != categoryId,
        onAcceptWithDetails: (details) {
          controller.moveAgentToCategory(details.data, categoryId);
        },
        builder: (context, candidateData, _) {
          return buildInnerBox(
            agentHoverBorderColor: candidateData.isNotEmpty
                ? Colors.blue
                : null,
          );
        },
      );
    }

    // Wrap with DragTarget for subcategory reordering
    Widget dropTarget = DragTarget<_CategoryDragData>(
      onWillAcceptWithDetails: (details) {
        return details.data.node.model.id.trim() != categoryId;
      },
      onAcceptWithDetails: (details) {
        _handleSubcategoryDrop(
          details.data,
          categoryId,
          parentCategoryId,
          siblingIds,
        );
      },
      builder: (context, catCandidateData, _) {
        final catHovering = catCandidateData.isNotEmpty;
        return Stack(
          clipBehavior: Clip.none,
          children: [
            innerContent,
            if (catHovering)
              Positioned(
                bottom: -2,
                left: 6,
                right: 6,
                child: Container(
                  height: 3,
                  decoration: BoxDecoration(
                    color: Colors.blue,
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
              ),
          ],
        );
      },
    );

    final titleCapsule = LongPressDraggable<_CategoryDragData>(
      data: _CategoryDragData(node, parentCategoryId),
      feedback: _buildCategoryDragFeedback(theme, node.model.name, isSub: true),
      childWhenDragging: Opacity(
        opacity: 0.4,
        child: _buildCapsuleLabel(
          theme,
          node.model.name,
          borderColor,
          fontSize: 10,
        ),
      ),
      child: GestureDetector(
        onDoubleTap: () => _showCategoryNameEditDialog(
          context,
          node.model.id,
          node.model.name,
        ),
        child: _buildCapsuleLabel(
          theme,
          node.model.name,
          borderColor,
          fontSize: 10,
          showDragHandle: true,
        ),
      ),
    );

    return Stack(
      clipBehavior: Clip.none,
      children: [
        dropTarget,
        Positioned(
          top: -8,
          left: 12,
          right: 12,
          child: Center(child: titleCapsule),
        ),
      ],
    );
  }

  Widget _buildAgentWrap(
    BuildContext context,
    List<AgentModel> agents,
    String categoryId, {
    double spacing = 8,
    double runSpacing = 8,
    bool byHost = false,
  }) {
    return Wrap(
      spacing: spacing,
      runSpacing: runSpacing,
      alignment: WrapAlignment.center,
      children: [
        for (var i = 0; i < agents.length; i++)
          _AgentBlock(
            agent: agents[i],
            insertIndex: i,
            categoryId: categoryId,
            onTap: () => _showAvatarActionSheet(context, agents[i]),
            isOnline: controller.isApiAgentOnline(agents[i]),
            providerLabel: byHost && agents[i].providerType == 4
                ? 'ai_provider_voice_short'.tr
                : controller.providerDisplayLabel(agents[i]),
            onDropAgent: (draggedAgent, targetIndex) {
              controller.reorderAgentInCategory(
                draggedAgent,
                categoryId,
                targetIndex,
              );
            },
          ),
      ],
    );
  }

  void _handleCategoryDropOnCategory(
    _CategoryDragData dragData,
    String targetCategoryId,
    List<String> rootIds,
  ) {
    final movedId = dragData.node.model.id.trim();
    final targetIdx = rootIds.indexOf(targetCategoryId);
    if (targetIdx < 0) return;

    if (dragData.parentCategoryId != '0') {
      controller.moveCategoryToParent(movedId, '0', targetIdx + 1);
    } else {
      final reordered = rootIds.where((id) => id != movedId).toList();
      final newTargetIdx = reordered.indexOf(targetCategoryId);
      if (newTargetIdx < 0) return;
      reordered.insert(newTargetIdx + 1, movedId);
      controller.reorderCategories(reordered);
    }
  }

  void _handleSubcategoryDrop(
    _CategoryDragData dragData,
    String targetSubcategoryId,
    String parentCategoryId,
    List<String> siblingIds,
  ) {
    final movedId = dragData.node.model.id.trim();

    if (dragData.parentCategoryId == parentCategoryId) {
      // Same parent: reorder siblings
      final reordered = siblingIds.where((id) => id != movedId).toList();
      final targetIdx = reordered.indexOf(targetSubcategoryId);
      if (targetIdx < 0) return;
      reordered.insert(targetIdx + 1, movedId);
      controller.reorderCategories(reordered);
    } else {
      // Cross-parent: move to new parent at the end
      controller.moveCategoryToParent(
        movedId,
        parentCategoryId,
        siblingIds.length,
      );
    }
  }

  // --- Shared UI ---

  Widget _buildCapsuleLabel(
    ThemeData theme,
    String name,
    Color borderColor, {
    double fontSize = 11,
    bool showDragHandle = false,
  }) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 1),
      decoration: BoxDecoration(
        color: theme.brightness == Brightness.dark
            ? const Color(0xFF1C1B1F)
            : Colors.white,
        borderRadius: BorderRadius.circular(fontSize >= 11 ? 8 : 6),
        border: Border.all(color: borderColor),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (showDragHandle) ...[
            Icon(
              Icons.drag_handle,
              size: fontSize + 1,
              color: theme.colorScheme.onSurface.withValues(alpha: 0.3),
            ),
            const SizedBox(width: 3),
          ],
          Text(
            name,
            style: TextStyle(
              fontSize: fontSize,
              fontWeight: FontWeight.w600,
              color: theme.colorScheme.onSurface,
              height: 1.3,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildCategoryDragFeedback(
    ThemeData theme,
    String name, {
    bool isSub = false,
  }) {
    return Material(
      elevation: 4,
      borderRadius: BorderRadius.circular(isSub ? 8 : 12),
      child: Container(
        padding: EdgeInsets.symmetric(
          horizontal: isSub ? 12 : 16,
          vertical: isSub ? 8 : 10,
        ),
        decoration: BoxDecoration(
          color: theme.colorScheme.surfaceContainerHighest.withValues(
            alpha: 0.95,
          ),
          borderRadius: BorderRadius.circular(isSub ? 8 : 12),
          border: Border.all(
            color: theme.colorScheme.primary.withValues(alpha: 0.4),
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              isSub ? Icons.folder_open : Icons.folder,
              size: isSub ? 14 : 16,
              color: theme.colorScheme.primary,
            ),
            const SizedBox(width: 6),
            Text(
              name,
              style: TextStyle(
                fontSize: isSub ? 12 : 13,
                fontWeight: FontWeight.w600,
                color: theme.colorScheme.onSurface,
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildEmpty(ThemeData theme) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 80,
            height: 80,
            decoration: BoxDecoration(
              color: theme.primaryColor.withValues(alpha: 0.08),
              shape: BoxShape.circle,
            ),
            child: Icon(
              Icons.smart_toy_outlined,
              size: 36,
              color: theme.primaryColor.withValues(alpha: 0.4),
            ),
          ),
          const SizedBox(height: 16),
          Text(
            'ai_agents_empty'.tr,
            style: TextStyle(
              fontSize: 15,
              fontWeight: FontWeight.w600,
              color: theme.colorScheme.onSurface.withValues(alpha: 0.5),
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'ai_agents_empty_hint'.tr,
            style: TextStyle(
              fontSize: 13,
              color: theme.colorScheme.secondary.withValues(alpha: 0.5),
            ),
          ),
          const SizedBox(height: 12),
          AgentQuickAccessButton(onPressed: controller.openAgentQuickOnboard),
        ],
      ),
    );
  }

  /// The "+" offers both paths: the one-question quick onboarding (default for
  /// CLI agents) and the full wizard with every provider type.
  void _showCreateMenu(BuildContext context) {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (sheetContext) => SafeArea(
        top: false,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              key: const Key('agents-menu-quick-onboard'),
              leading: const Icon(Icons.bolt_rounded),
              title: Text('ai_agent_quick_entry'.tr),
              subtitle: Text('ai_agent_quick_entry_hint'.tr),
              onTap: () {
                Navigator.of(sheetContext).pop();
                controller.openAgentQuickOnboard();
              },
            ),
            ListTile(
              key: const Key('agents-menu-full-create'),
              leading: const Icon(Icons.tune_rounded),
              title: Text('ai_agents_create'.tr),
              subtitle: Text('ai_agent_quick_entry_full_hint'.tr),
              onTap: () {
                Navigator.of(sheetContext).pop();
                controller.openAgentCreate();
              },
            ),
          ],
        ),
      ),
    );
  }

  void _showAvatarActionSheet(BuildContext context, AgentModel agent) {
    final isAPI = agent.providerType == 3;
    final isOnline = controller.isApiAgentOnline(agent);
    final typeLabel = controller.providerDisplayLabel(agent);
    final theme = Theme.of(context);
    showModalBottomSheet<void>(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 12),
              child: Container(
                width: 36,
                height: 4,
                decoration: BoxDecoration(
                  color: theme.colorScheme.outline.withValues(alpha: 0.3),
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
            ),
            // Title row: name + tags
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 6),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      agent.agentName,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w700,
                        color: theme.colorScheme.onSurface,
                      ),
                    ),
                  ),
                  const SizedBox(width: 6),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 8,
                      vertical: 3,
                    ),
                    decoration: BoxDecoration(
                      color: theme.colorScheme.primary.withValues(alpha: 0.12),
                      borderRadius: BorderRadius.circular(10),
                    ),
                    child: Text(
                      typeLabel,
                      style: TextStyle(
                        fontSize: 11,
                        fontWeight: FontWeight.w600,
                        color: theme.colorScheme.primary,
                      ),
                    ),
                  ),
                  if (agent.isMain) ...[
                    const SizedBox(width: 6),
                    Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 8,
                        vertical: 3,
                      ),
                      decoration: BoxDecoration(
                        color: Colors.orange.withValues(alpha: 0.15),
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: Text(
                        'ai_agent_status_main'.tr,
                        style: const TextStyle(
                          fontSize: 11,
                          fontWeight: FontWeight.w600,
                          color: Colors.orange,
                        ),
                      ),
                    ),
                  ],
                  if (isAPI) ...[
                    const SizedBox(width: 6),
                    Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 8,
                        vertical: 3,
                      ),
                      decoration: BoxDecoration(
                        color:
                            (isOnline
                                    ? AppTheme.successColor
                                    : AppTheme.warningColor)
                                .withValues(alpha: 0.15),
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Container(
                            width: 6,
                            height: 6,
                            decoration: BoxDecoration(
                              color: isOnline
                                  ? AppTheme.successColor
                                  : AppTheme.warningColor,
                              shape: BoxShape.circle,
                            ),
                          ),
                          const SizedBox(width: 4),
                          Text(
                            isOnline
                                ? 'ai_agent_status_online'.tr
                                : 'ai_agent_status_offline'.tr,
                            style: TextStyle(
                              fontSize: 11,
                              fontWeight: FontWeight.w600,
                              color: isOnline
                                  ? AppTheme.successColor
                                  : AppTheme.warningColor,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ],
              ),
            ),
            // Introduction (2 lines, tap to see full)
            if (agent.introduction.trim().isNotEmpty)
              GestureDetector(
                onTap: () => _showIntroductionDialog(context, agent),
                child: Padding(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 20,
                    vertical: 4,
                  ),
                  child: Text(
                    agent.introduction.trim(),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 13,
                      color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
                    ),
                  ),
                ),
              ),
            // 被共享 agent 提示：告知使用者该 agent 运行在主人机器上，
            // 你与它的对话历史会保存在那台机器，本地能力也是主人机器的能力。
            if (!controller.agentService.isOwnedByMe(agent))
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 8, 20, 0),
                child: Container(
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: theme.colorScheme.surfaceContainerHighest.withValues(
                      alpha: 0.6,
                    ),
                    borderRadius: BorderRadius.circular(8),
                    border: Border.all(
                      color: theme.colorScheme.outlineVariant.withValues(
                        alpha: 0.5,
                      ),
                    ),
                  ),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Icon(
                        Icons.info_outline,
                        size: 16,
                        color: theme.colorScheme.primary,
                      ),
                      const SizedBox(width: 6),
                      Expanded(
                        child: Text(
                          'ai_agent_shared_to_me_notice'.tr,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                            height: 1.4,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            const SizedBox(height: 12),
            // Action buttons row
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                children: [
                  _buildActionButton(
                    context: context,
                    icon: Icons.add_comment_outlined,
                    label: 'ai_agent_action_new_chat'.tr,
                    onTap: () {
                      Navigator.of(sheetContext).pop();
                      _createNewChat(agent);
                    },
                  ),
                  _buildActionButton(
                    context: context,
                    icon: Icons.person_outline,
                    label: 'ai_agent_action_profile'.tr,
                    onTap: () {
                      Navigator.of(sheetContext).pop();
                      controller.openAgentProfile(agent);
                    },
                  ),
                  // 管理类（编辑配置）：仅 agent 主人可见；被共享者只能使用不能管理。
                  if (controller.agentService.isOwnedByMe(agent))
                    _buildActionButton(
                      context: context,
                      icon: Icons.settings_outlined,
                      label: 'ai_agent_action_config'.tr,
                      onTap: () {
                        Navigator.of(sheetContext).pop();
                        controller.openAgentEdit(agent);
                      },
                    ),
                  // 共享：受 feature gate `agent_share` 控制（默认关闭，塘主按白名单/全量开放）；
                  // 仅主人可共享 agent-api 类 agent（被共享的 agent 不能再共享）。
                  if (isAPI &&
                      controller.agentService.isOwnedByMe(agent) &&
                      Get.find<FeatureFlagService>().isEnabled('agent_share'))
                    _buildActionButton(
                      context: context,
                      icon: Icons.share_outlined,
                      label: 'ai_agent_action_share'.tr,
                      onTap: () {
                        Navigator.of(sheetContext).pop();
                        controller.openAgentShare(agent);
                      },
                    ),
                  if (isAPI && controller.agentService.isOwnedByMe(agent))
                    _buildActionButton(
                      context: context,
                      icon: Icons.shield_outlined,
                      label: 'ai_agent_action_scope'.tr,
                      onTap: () {
                        Navigator.of(sheetContext).pop();
                        controller.openAgentScopes(agent);
                      },
                    ),
                  // 连接安全：查看登录历史 + 把某个登录 IP 加入黑名单，仅 API 类且主人可见。
                  if (isAPI && controller.agentService.isOwnedByMe(agent))
                    _buildActionButton(
                      context: context,
                      icon: Icons.security_outlined,
                      label: 'ai_agent_action_conn_security'.tr,
                      onTap: () {
                        Navigator.of(sheetContext).pop();
                        controller.openAgentConnSecurity(agent);
                      },
                    ),
                ],
              ),
            ),
            const SizedBox(height: 16),
          ],
        ),
      ),
    );
  }

  void _showIntroductionDialog(BuildContext context, AgentModel agent) {
    showAppMessageDialog(
      context: context,
      title: agent.agentName,
      message: agent.introduction.trim(),
      dismissText: 'common_confirm'.tr,
    );
  }

  Widget _buildActionButton({
    required BuildContext context,
    required IconData icon,
    required String label,
    required VoidCallback onTap,
  }) {
    final theme = Theme.of(context);
    return Expanded(
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(12),
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 10, horizontal: 4),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(icon, size: 24, color: theme.colorScheme.primary),
              const SizedBox(height: 6),
              // 部分语言的标签很长（如德文 Berechtigungen），空间不够时
              // 自动等比缩小以保持单行，避免换行破坏整行按钮的布局。
              FittedBox(
                fit: BoxFit.scaleDown,
                child: Text(
                  label,
                  maxLines: 1,
                  softWrap: false,
                  style: TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w500,
                    color: theme.colorScheme.onSurface,
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _createNewChat(AgentModel agent) async {
    final agentId = agent.id.trim();
    if (agentId.isEmpty) return;
    await ChatRouteNavigator.createAndOpenPrivateChat(
      peerId: agentId,
      peerType: 2,
      fallbackTitle: agent.agentName,
    );
  }

  void _showCategoryNameEditDialog(
    BuildContext context,
    String categoryId,
    String currentName,
  ) async {
    final newName = await showAppInputDialog(
      context: context,
      title: 'ai_agent_category_rename'.tr,
      initialValue: currentName,
      hintText: 'ai_agent_category_name_hint'.tr,
    );
    if (newName == null) return;
    final trimmed = newName.trim();
    if (trimmed.isNotEmpty && trimmed != currentName) {
      controller.updateCategoryName(categoryId, trimmed);
    }
  }
}

// --- Data classes ---

class _CategoryWithAgents {
  _CategoryWithAgents({required this.node});
  final CategoryNode node;
}

// --- Agent block: both Draggable AND DragTarget ---

class _AgentBlock extends StatelessWidget {
  const _AgentBlock({
    required this.agent,
    required this.insertIndex,
    required this.categoryId,
    required this.onTap,
    required this.isOnline,
    required this.providerLabel,
    required this.onDropAgent,
  });

  final AgentModel agent;
  final int insertIndex;
  final String categoryId;
  final VoidCallback onTap;
  final bool isOnline;
  final String providerLabel;
  final void Function(AgentModel draggedAgent, int targetIndex) onDropAgent;

  static const double blockSize = 64.0;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final avatarColor = AppTheme.getAvatarColor(agent.agentName);

    return DragTarget<AgentModel>(
      onWillAcceptWithDetails: (details) => details.data.id != agent.id,
      onAcceptWithDetails: (details) => onDropAgent(details.data, insertIndex),
      builder: (context, candidateData, _) {
        final isDropTarget = candidateData.isNotEmpty;
        return Stack(
          clipBehavior: Clip.none,
          children: [
            // Blue vertical line on the left when another agent hovers
            if (isDropTarget)
              Positioned(
                left: -6,
                top: 0,
                bottom: 14,
                child: Container(
                  width: 4,
                  decoration: BoxDecoration(
                    color: Colors.blue,
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
              ),
            LongPressDraggable<AgentModel>(
              data: agent,
              feedback: Material(
                elevation: 4,
                borderRadius: BorderRadius.circular(8),
                child: SizedBox(
                  width: blockSize,
                  height: blockSize,
                  child: Opacity(
                    opacity: 0.8,
                    child: SessionAvatar(
                      isGroup: false,
                      avatarTitle: agent.agentName,
                      avatarColor: avatarColor,
                      avatarUrl: agent.avatarUrl,
                      size: blockSize,
                      borderRadius: AppTheme.listAvatarCornerRadius(blockSize),
                    ),
                  ),
                ),
              ),
              childWhenDragging: Opacity(
                opacity: 0.3,
                child: _buildAvatarContent(theme, avatarColor),
              ),
              child: GestureDetector(
                onTap: onTap,
                child: _buildAvatarContent(theme, avatarColor),
              ),
            ),
          ],
        );
      },
    );
  }

  Widget _buildAvatarContent(ThemeData theme, Color avatarColor) {
    return SizedBox(
      width: blockSize,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          SizedBox(
            width: blockSize,
            height: blockSize,
            child: Stack(
              clipBehavior: Clip.none,
              children: [
                Positioned.fill(
                  child: SessionAvatar(
                    isGroup: false,
                    avatarTitle: agent.agentName,
                    avatarColor: avatarColor,
                    avatarUrl: agent.avatarUrl,
                    size: blockSize,
                    borderRadius: AppTheme.listAvatarCornerRadius(blockSize),
                  ),
                ),
                Positioned(
                  left: 0,
                  top: 0,
                  child: Container(
                    padding: const EdgeInsets.fromLTRB(4, 2, 5, 2),
                    decoration: BoxDecoration(
                      color: theme.colorScheme.surfaceContainerHighest
                          .withValues(alpha: 0.9),
                      borderRadius: const BorderRadius.only(
                        bottomRight: Radius.circular(6),
                      ),
                      border: Border.all(
                        color: theme.colorScheme.outline.withValues(alpha: 0.3),
                      ),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        if (agent.providerType == 3)
                          Container(
                            width: 5,
                            height: 5,
                            margin: const EdgeInsets.only(right: 3),
                            decoration: BoxDecoration(
                              color: isOnline
                                  ? const Color(0xFF00E676)
                                  : AppTheme.warningColor,
                              shape: BoxShape.circle,
                            ),
                          ),
                        Text(
                          providerLabel,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 8,
                            fontWeight: FontWeight.w600,
                            color: theme.colorScheme.onSurface,
                            height: 1.2,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
                if (agent.isMain)
                  Positioned(
                    right: 0,
                    bottom: 0,
                    child: Container(
                      width: 18,
                      height: 18,
                      decoration: BoxDecoration(
                        color: Colors.orange,
                        borderRadius: const BorderRadius.only(
                          topLeft: Radius.circular(2),
                        ),
                        border: Border(
                          left: BorderSide(
                            color: theme.scaffoldBackgroundColor,
                            width: 1,
                          ),
                          top: BorderSide(
                            color: theme.scaffoldBackgroundColor,
                            width: 1,
                          ),
                          right: BorderSide.none,
                          bottom: BorderSide.none,
                        ),
                      ),
                      child: FittedBox(
                        child: Padding(
                          padding: const EdgeInsets.all(2),
                          child: Text(
                            'ai_agent_status_main'.tr,
                            style: const TextStyle(
                              fontSize: 9,
                              fontWeight: FontWeight.w700,
                              color: Colors.white,
                              height: 1,
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
              ],
            ),
          ),
          const SizedBox(height: 2),
          Text(
            agent.agentName,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            textAlign: TextAlign.center,
            style: TextStyle(
              fontSize: 11,
              fontWeight: FontWeight.w500,
              color: theme.colorScheme.onSurface,
              height: 1.2,
            ),
          ),
        ],
      ),
    );
  }
}
