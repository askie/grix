import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../data/providers/agent_service.dart';
import '../../../data/providers/friend_service.dart';
import '../../../shared/utils/toast_util.dart';

/// 弹出某个 agent 的「共享管理」面板：展示已共享账户、可移除、可从好友中添加。
/// 仅 agent 主人调用（被共享的 agent 不能再共享）。
Future<void> showAgentShareSheet(
  BuildContext context,
  AgentModel agent,
) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    backgroundColor: Theme.of(context).cardColor,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
    ),
    builder: (_) => _AgentShareSheet(agent: agent),
  );
}

class _AgentShareSheet extends StatefulWidget {
  const _AgentShareSheet({required this.agent});

  final AgentModel agent;

  @override
  State<_AgentShareSheet> createState() => _AgentShareSheetState();
}

class _AgentShareSheetState extends State<_AgentShareSheet> {
  final AgentService _agentService = Get.find<AgentService>();
  final FriendService _friendService = Get.find<FriendService>();

  bool _loading = true;
  bool _busy = false;
  List<String> _sharedIds = [];

  @override
  void initState() {
    super.initState();
    _reload();
  }

  Future<void> _reload() async {
    setState(() => _loading = true);
    final ids = await _agentService.listAgentShares(widget.agent.id);
    if (!mounted) return;
    await _friendService.ensureUserProfiles(ids);
    if (!mounted) return;
    setState(() {
      _sharedIds = ids;
      _loading = false;
    });
  }

  String _displayName(String userId) {
    final nickname = _friendService.getUserNickname(userId);
    if (nickname != null && nickname.trim().isNotEmpty) return nickname.trim();
    return userId;
  }

  Future<void> _remove(String userId) async {
    if (_busy) return;
    setState(() => _busy = true);
    final ok = await _agentService.revokeAgentShare(widget.agent.id, userId);
    setState(() => _busy = false);
    if (ok) {
      await _reload();
    } else {
      CustomToast.show('ai_agent_share_revoke_failed'.tr, isError: true);
    }
  }

  Future<void> _add(String userId) async {
    if (_busy) return;
    setState(() => _busy = true);
    final ok = await _agentService.shareAgentTo(widget.agent.id, userId);
    setState(() => _busy = false);
    if (ok) {
      await _reload();
    } else {
      CustomToast.show('ai_agent_share_grant_failed'.tr, isError: true);
    }
  }

  void _openAddPicker() {
    final shared = _sharedIds.toSet();
    final candidates = _friendService.friendList
        .where((f) => !shared.contains(f.userId))
        .toList();
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Theme.of(context).cardColor,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (pickCtx) {
        return SafeArea(
          child: ConstrainedBox(
            constraints: BoxConstraints(
              maxHeight: MediaQuery.of(pickCtx).size.height * 0.7,
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Padding(
                  padding: const EdgeInsets.all(16),
                  child: Text('ai_agent_share_picker_title'.tr,
                      style: const TextStyle(
                          fontSize: 16, fontWeight: FontWeight.w600)),
                ),
                if (candidates.isEmpty)
                  Padding(
                    padding: const EdgeInsets.all(24),
                    child: Text('ai_agent_share_picker_empty'.tr),
                  )
                else
                  Flexible(
                    child: ListView.builder(
                      shrinkWrap: true,
                      itemCount: candidates.length,
                      itemBuilder: (_, i) {
                        final f = candidates[i];
                        final name = f.remarkName.trim().isNotEmpty
                            ? f.remarkName.trim()
                            : (f.nickname.trim().isNotEmpty
                                ? f.nickname.trim()
                                : f.userId);
                        return ListTile(
                          leading: CircleAvatar(
                            backgroundImage: f.avatarUrl.trim().isNotEmpty
                                ? NetworkImage(f.avatarUrl.trim())
                                : null,
                            child: f.avatarUrl.trim().isEmpty
                                ? Text(name.isNotEmpty ? name[0] : '?')
                                : null,
                          ),
                          title: Text(name),
                          onTap: () {
                            Navigator.of(pickCtx).pop();
                            _add(f.userId);
                          },
                        );
                      },
                    ),
                  ),
                const SizedBox(height: 8),
              ],
            ),
          ),
        );
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.of(context).size.height * 0.75,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      'ai_agent_share_title'
                          .trParams({'name': widget.agent.agentName}),
                      style: const TextStyle(
                          fontSize: 16, fontWeight: FontWeight.w600),
                    ),
                  ),
                  TextButton.icon(
                    onPressed: _busy ? null : _openAddPicker,
                    icon: const Icon(Icons.person_add_alt_1, size: 18),
                    label: Text('ai_agent_share_add'.tr),
                  ),
                ],
              ),
            ),
            // 共享边界提示：告知主人共享后会向被共享者开放本机能力，并且对方与 agent 的
            // 对话历史也会落在本机。这是设计上的"宿主机能力完全开放"契约，需要主人知情。
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
              child: Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: theme.colorScheme.surfaceContainerHighest
                      .withOpacity(0.6),
                  borderRadius: BorderRadius.circular(8),
                  border: Border.all(
                    color: theme.colorScheme.outlineVariant.withOpacity(0.5),
                  ),
                ),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(
                      Icons.info_outline,
                      size: 18,
                      color: theme.colorScheme.primary,
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        'ai_agent_share_notice'.tr,
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
            const Divider(height: 1),
            if (_loading)
              const Padding(
                padding: EdgeInsets.all(32),
                child: CircularProgressIndicator(),
              )
            else if (_sharedIds.isEmpty)
              Padding(
                padding: const EdgeInsets.all(32),
                child: Text('ai_agent_share_empty'.tr),
              )
            else
              Flexible(
                child: ListView.builder(
                  shrinkWrap: true,
                  itemCount: _sharedIds.length,
                  itemBuilder: (_, i) {
                    final id = _sharedIds[i];
                    final avatar = _friendService.getUserAvatarUrl(id) ?? '';
                    final name = _displayName(id);
                    return ListTile(
                      leading: CircleAvatar(
                        backgroundImage: avatar.trim().isNotEmpty
                            ? NetworkImage(avatar.trim())
                            : null,
                        child: avatar.trim().isEmpty
                            ? Text(name.isNotEmpty ? name[0] : '?')
                            : null,
                      ),
                      title: Text(name),
                      trailing: IconButton(
                        icon: const Icon(Icons.remove_circle_outline,
                            color: Colors.redAccent),
                        onPressed: _busy ? null : () => _remove(id),
                      ),
                    );
                  },
                ),
              ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
  }
}
