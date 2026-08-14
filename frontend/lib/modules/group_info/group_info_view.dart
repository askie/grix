import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/themes/app_theme.dart';
import '../../shared/widgets/avatar_network_image.dart';
import '../../shared/widgets/session_avatar.dart';
import 'controllers/group_info_controller.dart';

class GroupInfoView extends GetView<GroupInfoController> {
  const GroupInfoView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(title: Text('group_info_title'.tr)),
      body: Obx(() {
        if (controller.isLoading.value && controller.members.isEmpty) {
          return const Center(child: CircularProgressIndicator());
        }

        final error = controller.loadingError.value.trim();
        if (error.isNotEmpty && controller.members.isEmpty) {
          return _ErrorState(
            message: error,
            onRetry: controller.loadGroupDetail,
          );
        }

        final members = controller.members;

        return ListView(
          padding: const EdgeInsets.fromLTRB(16, 16, 16, 24),
          children: [
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: theme.colorScheme.surface,
                borderRadius: BorderRadius.circular(14),
              ),
              child: Row(
                children: [
                  SessionAvatar(
                    isGroup: true,
                    avatarTitle: controller.avatarTitle,
                    avatarColor: AppTheme.getAvatarColor(controller.avatarSeed),
                    members: controller.groupAvatarMembers,
                    size: 72,
                    borderRadius: 12,
                  ),
                  const SizedBox(width: 14),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          controller.avatarTitle,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.titleMedium?.copyWith(
                            fontWeight: FontWeight.w700,
                            fontSize: 18,
                          ),
                        ),
                        const SizedBox(height: 8),
                        Text(
                          'group_info_member_count'.trParams({
                            'count': '${controller.memberCount}',
                          }),
                          style: theme.textTheme.bodyMedium?.copyWith(
                            color: theme.colorScheme.secondary.withValues(
                              alpha: 0.75,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 18),
            Text(
              'group_info_members'.tr,
              style: theme.textTheme.titleMedium?.copyWith(
                fontWeight: FontWeight.w700,
                fontSize: 16,
              ),
            ),
            const SizedBox(height: 10),
            if (members.isEmpty)
              Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 24,
                ),
                decoration: BoxDecoration(
                  color: theme.colorScheme.surface,
                  borderRadius: BorderRadius.circular(14),
                ),
                child: Center(
                  child: Text(
                    'chat_members_empty'.tr,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: theme.colorScheme.secondary.withValues(alpha: 0.7),
                    ),
                  ),
                ),
              )
            else
              Container(
                decoration: BoxDecoration(
                  color: theme.colorScheme.surface,
                  borderRadius: BorderRadius.circular(14),
                ),
                child: Column(
                  children: [
                    for (var i = 0; i < members.length; i++) ...[
                      _GroupMemberTile(
                        member: members[i],
                        onTap: () => controller.openMemberProfile(members[i]),
                      ),
                      if (i < members.length - 1)
                        Divider(
                          height: 1,
                          indent: 72,
                          endIndent: 12,
                          color: theme.colorScheme.outline.withValues(
                            alpha: 0.15,
                          ),
                        ),
                    ],
                  ],
                ),
              ),
          ],
        );
      }),
    );
  }
}

class _GroupMemberTile extends StatelessWidget {
  const _GroupMemberTile({required this.member, required this.onTap});

  final GroupMemberProfile member;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final canOpenProfile = member.isUser;
    final subtitle = _buildSubtitle();

    return InkWell(
      onTap: canOpenProfile ? onTap : null,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        child: Row(
          children: [
            _MemberAvatar(member: member),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    member.displayName,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.bodyLarge?.copyWith(
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  if (subtitle.isNotEmpty) ...[
                    const SizedBox(height: 3),
                    Text(
                      subtitle,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.secondary.withValues(
                          alpha: 0.8,
                        ),
                      ),
                    ),
                  ],
                ],
              ),
            ),
            if (member.isMe)
              Padding(
                padding: EdgeInsets.only(right: canOpenProfile ? 8 : 0),
                child: Text(
                  'group_info_me'.tr,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.primaryColor,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              )
            else if (member.isFriend)
              Padding(
                padding: EdgeInsets.only(right: canOpenProfile ? 8 : 0),
                child: Text(
                  'group_info_friend'.tr,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: AppTheme.successColor,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
            if (canOpenProfile)
              Icon(
                Icons.chevron_right_rounded,
                color: theme.colorScheme.secondary.withValues(alpha: 0.65),
              ),
          ],
        ),
      ),
    );
  }

  String _buildSubtitle() {
    final username = member.username.trim();
    if (username.isNotEmpty) {
      return '@$username';
    }
    if (!member.isUser) {
      return 'AI';
    }
    return member.memberId;
  }
}

class _MemberAvatar extends StatelessWidget {
  const _MemberAvatar({required this.member});

  final GroupMemberProfile member;

  @override
  Widget build(BuildContext context) {
    final avatarUrl = member.avatarUrl.trim();
    final fallback = SessionAvatar(
      isGroup: false,
      avatarTitle: member.displayName,
      avatarColor: AppTheme.getAvatarColor(member.memberId),
      size: 48,
      borderRadius: 0,
    );

    if (avatarUrl.isEmpty) {
      return fallback;
    }

    return ClipRRect(
      borderRadius: BorderRadius.zero,
      child: SizedBox(
        width: 48,
        height: 48,
        child: AvatarNetworkImage(avatarUrl: avatarUrl, fallback: fallback),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 12),
            ElevatedButton(onPressed: onRetry, child: Text('common_retry'.tr)),
          ],
        ),
      ),
    );
  }
}
