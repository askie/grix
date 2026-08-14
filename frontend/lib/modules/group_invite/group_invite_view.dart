import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/themes/app_theme.dart';
import '../../shared/widgets/session_avatar.dart';
import 'controllers/group_invite_controller.dart';

class GroupInviteView extends GetView<GroupInviteController> {
  const GroupInviteView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(title: Text('group_invite_title'.tr)),
      body: Obx(() {
        final error = controller.loadingError.value.trim();
        if (error.isNotEmpty && controller.groupName.value.trim().isEmpty) {
          return _ErrorState(
            message: error,
            onRetry: controller.loadGroupPreview,
          );
        }

        return ListView(
          padding: const EdgeInsets.fromLTRB(16, 20, 16, 24),
          children: [
            Container(
              padding: const EdgeInsets.all(18),
              decoration: BoxDecoration(
                color: theme.colorScheme.surface,
                borderRadius: BorderRadius.circular(14),
              ),
              child: Column(
                children: [
                  SessionAvatar(
                    isGroup: true,
                    avatarTitle: controller.displayGroupName,
                    avatarColor: AppTheme.getAvatarColor(
                      controller.displayGroupName,
                    ),
                    members: const [],
                    size: 80,
                    borderRadius: 12,
                  ),
                  const SizedBox(height: 14),
                  Text(
                    controller.displayGroupName,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    textAlign: TextAlign.center,
                    style: theme.textTheme.titleMedium?.copyWith(
                      fontWeight: FontWeight.w700,
                      fontSize: 18,
                    ),
                  ),
                  const SizedBox(height: 10),
                  _MetaRow(
                    label: 'group_invite_owner'.tr,
                    value: controller.displayOwner,
                  ),
                  const SizedBox(height: 6),
                  _MetaRow(
                    label: 'group_invite_member_count'.tr,
                    value: '${controller.memberCount.value}',
                  ),
                  const SizedBox(height: 18),
                  if (controller.actionError.value.trim().isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: Text(
                        controller.actionError.value.trim(),
                        textAlign: TextAlign.center,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: AppTheme.errorColor,
                        ),
                      ),
                    ),
                  SizedBox(
                    width: double.infinity,
                    child: ElevatedButton(
                      onPressed:
                          (controller.isLoading.value ||
                              controller.isJoining.value)
                          ? null
                          : controller.joinOrEnterGroup,
                      child: controller.isJoining.value
                          ? const SizedBox(
                              width: 18,
                              height: 18,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            )
                          : Text(controller.actionButtonLabel),
                    ),
                  ),
                ],
              ),
            ),
          ],
        );
      }),
    );
  }
}

class _MetaRow extends StatelessWidget {
  const _MetaRow({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      children: [
        Text(
          label,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.secondary.withValues(alpha: 0.8),
          ),
        ),
        const Spacer(),
        Text(
          value,
          style: theme.textTheme.bodyMedium?.copyWith(
            fontWeight: FontWeight.w600,
          ),
        ),
      ],
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
