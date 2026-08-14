import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import 'controllers/profile_controller.dart';
import '../../app/themes/app_theme.dart';
import '../../shared/widgets/avatar_network_image.dart';
import '../../shared/widgets/app_version_text.dart';

class ProfileView extends GetView<ProfileController> {
  const ProfileView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      backgroundColor: theme.scaffoldBackgroundColor,
      body: CustomScrollView(
        key: const PageStorageKey<String>('home_profile_scroll'),
        slivers: [
          // 个人信息头部
          SliverToBoxAdapter(child: _buildProfileHeader(context)),

          // 功能菜单
          SliverToBoxAdapter(
            child: Column(
              children: [
                const SizedBox(height: 16),

                // 第一组: 账户相关
                _buildMenuSection(context, [
                  _MenuTile(
                    icon: Icons.settings_outlined,
                    iconColor: AppTheme.infoColor,
                    title: 'me_account_settings'.tr,
                    onTap: () => Get.toNamed(AppRoutes.settings),
                  ),
                  _MenuTile(
                    icon: Icons.qr_code_2_rounded,
                    iconColor: AppTheme.primaryColor,
                    title: 'me_my_qr'.tr,
                    onTap: controller.openMyFriendQr,
                  ),
                  _MenuTile(
                    icon: Icons.password_rounded,
                    iconColor: AppTheme.primaryColor,
                    title: 'me_change_password'.tr,
                    onTap: () => Get.toNamed(AppRoutes.changePassword),
                  ),
                  _MenuTile(
                    icon: Icons.notifications_none_rounded,
                    iconColor: AppTheme.warningColor,
                    title: 'me_notification'.tr,
                    onTap: () => Get.toNamed(AppRoutes.notifications),
                  ),
                  _MenuTile(
                    icon: Icons.lock_outline_rounded,
                    iconColor: AppTheme.successColor,
                    title: 'me_privacy'.tr,
                    onTap: () => Get.toNamed(AppRoutes.privacy),
                  ),
                  _MenuTile(
                    icon: Icons.storage_rounded,
                    iconColor: AppTheme.primaryDark,
                    title: 'me_storage'.tr,
                    onTap: () => Get.toNamed(AppRoutes.storage),
                  ),
                  _MenuTile(
                    icon: Icons.help_outline_rounded,
                    iconColor: AppTheme.primaryDark,
                    title: 'me_help'.tr,
                    onTap: () => Get.toNamed(AppRoutes.help),
                  ),
                  _MenuTile(
                    icon: Icons.info_outline_rounded,
                    iconColor: theme.colorScheme.secondary,
                    title: 'me_about'.tr,
                    trailing: AppVersionText(
                      style: TextStyle(
                        fontSize: 13,
                        color: theme.colorScheme.secondary,
                      ),
                    ),
                    onTap: () => Get.toNamed(AppRoutes.about),
                  ),
                ]),

                const SizedBox(height: 12),

                // 退出登录按钮
                Container(
                  margin: const EdgeInsets.symmetric(horizontal: 12),
                  decoration: BoxDecoration(
                    color: theme.colorScheme.surface,
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: ListTile(
                    shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(12),
                    ),
                    title: Center(
                      child: Text(
                        'me_logout'.tr,
                        style: const TextStyle(
                          color: AppTheme.errorColor,
                          fontWeight: FontWeight.w600,
                          fontSize: 15,
                        ),
                      ),
                    ),
                    onTap: controller.showLogoutConfirm,
                  ),
                ),

                const SizedBox(height: 24),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildProfileHeader(BuildContext context) {
    final theme = Theme.of(context);
    return Obx(() {
      final user = controller.authService.user;
      final nickname = user?.nickname ?? 'profile_default_nickname'.tr;
      final username = user?.username ?? '';
      final introduction = user?.introduction.trim() ?? '';
      final email = user?.email ?? '';
      final emailDisplay = email.isNotEmpty ? email : '--';
      final avatarUrl = controller.buildAvatarDisplayUrl(user?.avatarUrl);
      final isUploadingAvatar = controller.isUploadingAvatar.value;

      return Container(
        padding: EdgeInsets.only(
          top: MediaQuery.of(context).padding.top + 16,
          left: 20,
          right: 20,
          bottom: 24,
        ),
        decoration: BoxDecoration(
          color: theme.colorScheme.surface,
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.03),
              blurRadius: 10,
              offset: const Offset(0, 2),
            ),
          ],
        ),
        child: Row(
          children: [
            // 头像
            GestureDetector(
              onTap: isUploadingAvatar
                  ? null
                  : controller.showAvatarPreviewDialog,
              child: Stack(
                children: [
                  Container(
                    width: 68,
                    height: 68,
                    decoration: BoxDecoration(
                      gradient: const LinearGradient(
                        begin: Alignment.topLeft,
                        end: Alignment.bottomRight,
                        colors: [AppTheme.primaryColor, AppTheme.primaryDark],
                      ),
                      borderRadius: BorderRadius.zero,
                      boxShadow: [
                        BoxShadow(
                          color: AppTheme.primaryColor.withValues(alpha: 0.3),
                          blurRadius: 12,
                          offset: const Offset(0, 4),
                        ),
                      ],
                    ),
                    clipBehavior: Clip.antiAlias,
                    child: avatarUrl.isNotEmpty
                        ? _buildCachedAvatarImage(
                            avatarUrl: avatarUrl,
                            fallback: _buildAvatarFallback(nickname),
                          )
                        : _buildAvatarFallback(nickname),
                  ),
                  Positioned(
                    right: -2,
                    bottom: -2,
                    child: Container(
                      width: 22,
                      height: 22,
                      decoration: BoxDecoration(
                        color: theme.colorScheme.surface,
                        shape: BoxShape.circle,
                        border: Border.all(
                          color: theme.scaffoldBackgroundColor,
                          width: 1.5,
                        ),
                      ),
                      child: Icon(
                        Icons.camera_alt_rounded,
                        size: 14,
                        color: theme.colorScheme.secondary,
                      ),
                    ),
                  ),
                  if (isUploadingAvatar)
                    Positioned.fill(
                      child: Container(
                        decoration: BoxDecoration(
                          color: Colors.black.withValues(alpha: 0.28),
                          borderRadius: BorderRadius.zero,
                        ),
                        child: const Center(
                          child: SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          ),
                        ),
                      ),
                    ),
                ],
              ),
            ),
            const SizedBox(width: 16),
            // 用户信息
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    nickname,
                    style: TextStyle(
                      fontSize: 19,
                      fontWeight: FontWeight.w700,
                      color: theme.colorScheme.onSurface,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    '@$username',
                    style: TextStyle(
                      fontSize: 13,
                      color: theme.colorScheme.secondary,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    emailDisplay,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 13,
                      color: theme.colorScheme.secondary,
                    ),
                  ),
                  if (introduction.isNotEmpty) ...[
                    const SizedBox(height: 6),
                    Text(
                      introduction,
                      maxLines: 3,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 13,
                        color: theme.colorScheme.onSurface.withValues(
                          alpha: 0.78,
                        ),
                      ),
                    ),
                  ],
                ],
              ),
            ),
            // 编辑图标
            IconButton(
              icon: Container(
                padding: const EdgeInsets.all(8),
                decoration: BoxDecoration(
                  color: theme.scaffoldBackgroundColor,
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Icon(
                  Icons.edit_outlined,
                  color: theme.colorScheme.secondary,
                  size: 18,
                ),
              ),
              onPressed: controller.showEditProfileDialog,
            ),
          ],
        ),
      );
    });
  }

  Widget _buildAvatarFallback(String nickname) {
    return Center(
      child: Text(
        nickname.isNotEmpty ? nickname[0].toUpperCase() : 'U',
        style: const TextStyle(
          color: Colors.white,
          fontSize: 24,
          fontWeight: FontWeight.w700,
        ),
      ),
    );
  }

  Widget _buildCachedAvatarImage({
    required String avatarUrl,
    required Widget fallback,
  }) {
    return AvatarNetworkImage(avatarUrl: avatarUrl, fallback: fallback);
  }

  Widget _buildMenuSection(BuildContext context, List<_MenuTile> tiles) {
    final theme = Theme.of(context);
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        children: tiles
            .asMap()
            .entries
            .map(
              (entry) => Column(
                children: [
                  entry.value,
                  if (entry.key < tiles.length - 1)
                    Divider(
                      indent: 56,
                      color: theme.colorScheme.outline.withValues(alpha: 0.15),
                    ),
                ],
              ),
            )
            .toList(),
      ),
    );
  }
}

class _MenuTile extends StatelessWidget {
  final IconData icon;
  final Color iconColor;
  final String title;
  final Widget? trailing;
  final VoidCallback onTap;

  const _MenuTile({
    required this.icon,
    required this.iconColor,
    required this.title,
    this.trailing,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ListTile(
      leading: Container(
        width: 36,
        height: 36,
        decoration: BoxDecoration(
          color: iconColor.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(10),
        ),
        child: Icon(icon, color: iconColor, size: 20),
      ),
      title: Text(
        title,
        style: TextStyle(
          fontSize: 14,
          fontWeight: FontWeight.w400,
          color: theme.colorScheme.onSurface,
        ),
      ),
      trailing:
          trailing ??
          Icon(
            Icons.chevron_right_rounded,
            color: theme.colorScheme.secondary.withValues(alpha: 0.4),
          ),
      onTap: onTap,
    );
  }
}
