import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../app/themes/app_theme.dart';
import '../../../data/providers/agent_service.dart';
import '../../../data/providers/friend_service.dart';
import '../../../shared/widgets/session_avatar.dart';
import 'contact_agent_pick_result.dart';

export 'contact_agent_pick_result.dart';

/// 弹出可搜索的「联系人 + Agent」选择面板：列出当前账户的好友与全部可用
/// agent（自己持有 + 别人共享），支持按名称 / ID 过滤，点选后通过返回值
/// 带回选中条目的 id + 展示名（取消返回 null）。
/// 用于「在介绍中插入联系人」等需要快速挑选联系人 / agent 的场景。
Future<ContactAgentPickResult?> showContactAgentPickerSheet(
  BuildContext context, {
  bool agentsOnly = false,
}) {
  // 各触发一次加载（服务内部去重/吞错），列表数据由 Obx 响应式刷新。
  Get.find<AgentService>().loadAgents();
  if (!agentsOnly) {
    Get.find<FriendService>().loadFriendList();
  }
  return showModalBottomSheet<ContactAgentPickResult>(
    context: context,
    isScrollControlled: true,
    backgroundColor: Theme.of(context).scaffoldBackgroundColor,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
    ),
    builder: (_) => _ContactAgentPickerSheet(agentsOnly: agentsOnly),
  );
}

class _ContactAgentPickerSheet extends StatefulWidget {
  const _ContactAgentPickerSheet({required this.agentsOnly});

  final bool agentsOnly;

  @override
  State<_ContactAgentPickerSheet> createState() =>
      _ContactAgentPickerSheetState();
}

class _ContactAgentPickerSheetState extends State<_ContactAgentPickerSheet> {
  final AgentService _agentService = Get.find<AgentService>();
  FriendService? _friendService;
  final TextEditingController _searchController = TextEditingController();
  String _keyword = '';

  @override
  void initState() {
    super.initState();
    if (!widget.agentsOnly) {
      _friendService = Get.find<FriendService>();
    }
  }

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  List<FriendItem> _filteredFriends() {
    final all = _friendService?.friendList ?? const <FriendItem>[];
    final keyword = _keyword.trim().toLowerCase();
    if (keyword.isEmpty) {
      return all;
    }
    return all
        .where(
          (friend) =>
              friend.nickname.toLowerCase().contains(keyword) ||
              friend.username.toLowerCase().contains(keyword) ||
              friend.remarkName.toLowerCase().contains(keyword) ||
              friend.userId.toLowerCase().contains(keyword),
        )
        .toList(growable: false);
  }

  List<AgentModel> _filteredAgents() {
    final all = _agentService.allAccessibleAgents;
    final keyword = _keyword.trim().toLowerCase();
    if (keyword.isEmpty) {
      return all;
    }
    return all
        .where(
          (agent) =>
              agent.agentName.toLowerCase().contains(keyword) ||
              agent.id.toLowerCase().contains(keyword),
        )
        .toList(growable: false);
  }

  // 与聊天艾特一致：备注 > 昵称 > 用户名。
  String _friendDisplayName(FriendItem friend) {
    final remark = friend.remarkName.trim();
    if (remark.isNotEmpty) {
      return remark;
    }
    final nickname = friend.nickname.trim();
    if (nickname.isNotEmpty) {
      return nickname;
    }
    return friend.username.trim();
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: BoxConstraints(
        maxHeight: MediaQuery.of(context).size.height * 0.7,
      ),
      padding: const EdgeInsets.only(top: 16),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Text(
              widget.agentsOnly
                  ? 'ai_agents_title'.tr
                  : 'ai_agent_insert_id_picker_title'.tr,
              style: const TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 8),
            child: TextField(
              key: const Key('contact_agent_picker_search_field'),
              controller: _searchController,
              autofocus: true,
              onChanged: (value) => setState(() => _keyword = value),
              decoration: InputDecoration(
                hintText: 'ai_agent_insert_id_search_hint'.tr,
                prefixIcon: const Icon(Icons.search_rounded),
                isDense: true,
                suffixIcon: _keyword.isEmpty
                    ? null
                    : IconButton(
                        icon: const Icon(Icons.close_rounded, size: 18),
                        onPressed: () {
                          _searchController.clear();
                          setState(() => _keyword = '');
                        },
                      ),
              ),
            ),
          ),
          const Divider(height: 1),
          Flexible(
            child: Obx(() {
              final friends = _filteredFriends();
              final agents = _filteredAgents();
              if (friends.isEmpty && agents.isEmpty) {
                return Padding(
                  padding: const EdgeInsets.all(32),
                  child: Text(
                    _keyword.trim().isEmpty
                        ? 'ai_agent_insert_id_empty'.tr
                        : 'conversations_no_match'.tr,
                  ),
                );
              }
              return ListView(
                shrinkWrap: true,
                children: [
                  if (friends.isNotEmpty) ...[
                    _buildSectionHeader(context, 'contacts_friends'.tr),
                    ...friends.map(_buildFriendTile),
                  ],
                  if (agents.isNotEmpty) ...[
                    _buildSectionHeader(context, 'ai_agents_title'.tr),
                    ...agents.map(_buildAgentTile),
                  ],
                ],
              );
            }),
          ),
          SizedBox(height: MediaQuery.of(context).padding.bottom),
        ],
      ),
    );
  }

  Widget _buildSectionHeader(BuildContext context, String title) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 10, 16, 2),
      child: Text(
        title,
        style: TextStyle(
          fontSize: 12,
          fontWeight: FontWeight.w600,
          color: theme.colorScheme.secondary,
          letterSpacing: 0.5,
        ),
      ),
    );
  }

  Widget _buildFriendTile(FriendItem friend) {
    final theme = Theme.of(context);
    final displayName = _friendDisplayName(friend);
    return ListTile(
      key: Key('contact_picker_item_${friend.userId}'),
      leading: SessionAvatar(
        isGroup: false,
        avatarTitle: displayName,
        avatarColor: AppTheme.getAvatarColor(friend.userId),
        avatarUrl: friend.avatarUrl,
        size: 40,
        borderRadius: AppTheme.listAvatarCornerRadius(40),
      ),
      title: Text(displayName),
      subtitle: Text(
        friend.userId,
        style: theme.textTheme.bodySmall?.copyWith(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
        ),
      ),
      onTap: () => Get.back(
        result: ContactAgentPickResult(
          id: friend.userId,
          displayName: displayName,
          avatarUrl: friend.avatarUrl,
        ),
      ),
    );
  }

  Widget _buildAgentTile(AgentModel agent) {
    final theme = Theme.of(context);
    final displayName = agent.agentName.trim().isNotEmpty
        ? agent.agentName.trim()
        : agent.id;
    return ListTile(
      key: Key('agent_picker_item_${agent.id}'),
      leading: SessionAvatar(
        isGroup: false,
        avatarTitle: displayName,
        avatarColor: AppTheme.getAvatarColor(agent.id),
        avatarUrl: agent.avatarUrl,
        size: 40,
        borderRadius: AppTheme.listAvatarCornerRadius(40),
      ),
      title: Text(displayName),
      subtitle: Text(
        agent.id,
        style: theme.textTheme.bodySmall?.copyWith(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
        ),
      ),
      onTap: () => Get.back(
        result: ContactAgentPickResult(
          id: agent.id,
          displayName: displayName,
          avatarUrl: agent.avatarUrl,
        ),
      ),
    );
  }
}
