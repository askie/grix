import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/themes/app_theme.dart';
import '../../data/providers/saved_account_store.dart';
import '../../shared/widgets/app_dialog_style.dart';
import '../../shared/widgets/avatar_network_image.dart';
import 'controllers/account_switch_controller.dart';

/// 已登录账号列表页：点击切换、移除、添加账号。
class AccountSwitchView extends GetView<AccountSwitchController> {
  const AccountSwitchView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return PopScope<Object?>(
      canPop: false,
      onPopInvokedWithResult: (didPop, result) {
        if (didPop) return;
        if (controller.isBusy.value) return;
        if (!controller.isLoggedIn) {
          controller.handleBackWhenLoggedOut();
          return;
        }
        Get.back();
      },
      child: Scaffold(
        appBar: AppBar(
          leading: IconButton(
            icon: const Icon(Icons.arrow_back_ios_rounded, size: 20),
            onPressed: () {
              if (controller.isBusy.value) return;
              if (!controller.isLoggedIn) {
                controller.handleBackWhenLoggedOut();
                return;
              }
              Get.back();
            },
          ),
          title: Text(
            'account_switch_title'.tr,
            style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
          ),
        ),
        body: Obx(() {
          if (!controller.isLoaded.value) {
            return const Center(child: CircularProgressIndicator());
          }
          return Stack(
            children: [
              ListView(
                children: [
                  const SizedBox(height: 12),
                  _buildSection(context, [
                    for (final account in controller.accounts) ...[
                      _buildAccountTile(context, account),
                      if (account != controller.accounts.last)
                        Divider(
                          height: 1,
                          indent: 72,
                          color: theme.dividerColor.withValues(alpha: 0.4),
                        ),
                    ],
                    if (controller.accounts.isNotEmpty)
                      Divider(
                        height: 1,
                        color: theme.dividerColor.withValues(alpha: 0.4),
                      ),
                    _buildAddTile(context),
                  ]),
                  const SizedBox(height: 24),
                ],
              ),
              if (controller.isBusy.value)
                Container(
                  color: Colors.black.withValues(alpha: 0.2),
                  alignment: Alignment.center,
                  child: const CircularProgressIndicator(),
                ),
            ],
          );
        }),
      ),
    );
  }

  Widget _buildAccountTile(BuildContext context, SavedAccount account) {
    final theme = Theme.of(context);
    final isCurrent = controller.isCurrent(account);
    final subtitle = account.needsRelogin
        ? 'account_switch_expired_hint'.tr
        : _accountIdentityLabel(account);
    return ListTile(
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
      leading: _buildAvatar(account),
      title: Text(
        account.displayName.isNotEmpty ? account.displayName : account.userId,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w500),
      ),
      subtitle: subtitle.isEmpty
          ? null
          : Text(
              subtitle,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: 13,
                color: account.needsRelogin
                    ? theme.colorScheme.error
                    : theme.textTheme.bodySmall?.color,
              ),
            ),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (isCurrent)
            const Padding(
              padding: EdgeInsets.only(right: 4),
              child: Icon(
                Icons.check_circle_rounded,
                size: 20,
                color: AppTheme.primaryColor,
              ),
            ),
          IconButton(
            icon: Icon(
              Icons.delete_outline_rounded,
              size: 20,
              color: theme.textTheme.bodySmall?.color,
            ),
            onPressed: () => _confirmRemove(context, account),
          ),
        ],
      ),
      onTap: () => controller.switchTo(account),
    );
  }

  Widget _buildAddTile(BuildContext context) {
    final theme = Theme.of(context);
    return ListTile(
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
      leading: Container(
        width: 44,
        height: 44,
        decoration: BoxDecoration(
          color: theme.colorScheme.surfaceContainerHighest,
          borderRadius: BorderRadius.circular(8),
        ),
        child: Icon(
          Icons.add_rounded,
          size: 24,
          color: theme.textTheme.bodyMedium?.color,
        ),
      ),
      title: Text(
        'account_switch_add_account'.tr,
        style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w500),
      ),
      onTap: controller.addAccount,
    );
  }

  Widget _buildAvatar(SavedAccount account) {
    final fallbackText = account.displayName.isNotEmpty
        ? account.displayName.characters.first.toUpperCase()
        : '?';
    final fallback = Container(
      alignment: Alignment.center,
      decoration: const BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [AppTheme.primaryColor, AppTheme.primaryDark],
        ),
      ),
      child: Text(
        fallbackText,
        style: const TextStyle(
          color: Colors.white,
          fontSize: 18,
          fontWeight: FontWeight.w600,
        ),
      ),
    );
    return SizedBox(
      width: 44,
      height: 44,
      child: ClipRRect(
        borderRadius: BorderRadius.circular(8),
        child: account.avatarUrl.trim().isNotEmpty
            ? AvatarNetworkImage(
                avatarUrl: account.avatarUrl,
                fallback: fallback,
              )
            : fallback,
      ),
    );
  }

  String _accountIdentityLabel(SavedAccount account) {
    if (account.email.trim().isNotEmpty) return account.email.trim();
    if (account.phoneE164.trim().isNotEmpty) return account.phoneE164.trim();
    return account.username.trim();
  }

  Widget _buildSection(BuildContext context, List<Widget> children) {
    final theme = Theme.of(context);
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(12),
      ),
      clipBehavior: Clip.antiAlias,
      child: Column(children: children),
    );
  }

  Future<void> _confirmRemove(
    BuildContext context,
    SavedAccount account,
  ) async {
    if (controller.isBusy.value) return;
    final isCurrent = controller.isCurrent(account);
    await showAppGetDialog<void>(
      AlertDialog(
        title: Text('account_switch_remove_title'.tr),
        content: Text(
          isCurrent
              ? 'account_switch_remove_current_message'.tr
              : 'account_switch_remove_message'.tr,
        ),
        actions: [
          TextButton(
            onPressed: () => Get.back(),
            child: Text('common_cancel'.tr),
          ),
          ElevatedButton(
            onPressed: () async {
              Get.back();
              await controller.removeAccount(account);
            },
            child: Text('account_switch_remove_confirm'.tr),
          ),
        ],
      ),
    );
  }
}
