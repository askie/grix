import 'package:flutter/material.dart';
import 'package:get/get.dart';
import '../../app/routes/app_routes.dart';
import '../../app/themes/app_theme.dart';
import '../../data/providers/friend_service.dart';
import '../../shared/widgets/app_dialog_style.dart';
import 'controllers/contacts_controller.dart';
import 'widgets/contact_quick_actions.dart';

class ContactsView extends GetView<ContactsController> {
  const ContactsView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      backgroundColor: theme.scaffoldBackgroundColor,
      appBar: AppBar(
        title: Text(
          'contacts_title'.tr,
          style: theme.textTheme.titleLarge?.copyWith(fontSize: 18),
        ),
        actions: [
          // Friend requests badge
          Obx(() {
            final pendingCount = controller.friendService.friendRequests
                .where((r) => r.status == 0)
                .length;
            return Stack(
              children: [
                IconButton(
                  icon: Container(
                    padding: const EdgeInsets.all(6),
                    decoration: BoxDecoration(
                      color: theme.primaryColor.withValues(alpha: 0.1),
                      borderRadius: BorderRadius.circular(10),
                    ),
                    child: Icon(
                      Icons.person_add_rounded,
                      color: theme.primaryColor,
                      size: 20,
                    ),
                  ),
                  onPressed: () => ContactQuickActions.showAddFriendDialog(
                    context,
                    controller,
                  ),
                ),
                if (pendingCount > 0)
                  Positioned(
                    right: 4,
                    top: 4,
                    child: Container(
                      padding: const EdgeInsets.all(4),
                      decoration: BoxDecoration(
                        color: AppTheme.errorColor,
                        shape: BoxShape.circle,
                        border: Border.all(
                          color:
                              theme.appBarTheme.backgroundColor ??
                              theme.scaffoldBackgroundColor,
                          width: 1.5,
                        ),
                      ),
                      constraints: const BoxConstraints(
                        minWidth: 18,
                        minHeight: 18,
                      ),
                      child: Text(
                        '$pendingCount',
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 10,
                          fontWeight: FontWeight.w600,
                        ),
                        textAlign: TextAlign.center,
                      ),
                    ),
                  ),
              ],
            );
          }),
          const SizedBox(width: 4),
        ],
      ),
      body: Column(
        children: [
          // Search bar
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
            color: theme.appBarTheme.backgroundColor,
            child: Obx(() {
              final hasQuery = controller.hasSearchQuery.value;
              return TextField(
                controller: controller.searchController,
                decoration: InputDecoration(
                  hintText: 'friend_search_hint'.tr,
                  hintStyle: TextStyle(
                    color: theme.colorScheme.secondary.withValues(alpha: 0.5),
                    fontSize: 14,
                  ),
                  prefixIcon: Icon(
                    Icons.search_rounded,
                    color: theme.colorScheme.secondary.withValues(alpha: 0.5),
                  ),
                  suffixIcon: hasQuery
                      ? IconButton(
                          icon: Icon(
                            Icons.close_rounded,
                            color: theme.colorScheme.secondary
                                .withValues(alpha: 0.5),
                            size: 18,
                          ),
                          onPressed: controller.resetSearch,
                        )
                      : null,
                  isDense: true,
                ),
                onChanged: (v) {
                  if (v.trim().isNotEmpty) {
                    controller.searchUsers(v);
                  } else {
                    controller.resetSearch();
                  }
                },
                onSubmitted: controller.searchUsers,
              );
            }),
          ),
          Expanded(
            child: Obx(() {
              final isSearching = controller.isSearching.value;
              final results = controller.searchResults;
              final hasQuery = controller.hasSearchQuery.value;

              if (hasQuery) {
                if (isSearching) {
                  return const Center(
                    child: Padding(
                      padding: EdgeInsets.all(24),
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  );
                }
                if (results.isEmpty) {
                  return Center(
                    child: Padding(
                      padding: const EdgeInsets.all(24),
                      child: Text(
                        'friend_no_result'.tr,
                        style: TextStyle(
                          color:
                              theme.colorScheme.secondary.withValues(alpha: 0.5),
                          fontSize: 14,
                        ),
                      ),
                    ),
                  );
                }
                return ListView.separated(
                  padding: const EdgeInsets.symmetric(
                      horizontal: 12, vertical: 8),
                  itemCount: results.length,
                  separatorBuilder: (_, __) => Divider(
                    height: 1,
                    indent: 68,
                    color: theme.colorScheme.outline.withValues(alpha: 0.15),
                  ),
                  itemBuilder: (context, index) {
                    final user = results[index];
                    final isSent =
                        controller.sentUsernames.contains(user.username);
                    final isAlreadyFriend = controller
                        .friendService.friendList
                        .any((f) => f.username == user.username);
                    const colors = AppTheme.avatarColors;
                    final color =
                        colors[user.id.hashCode.abs() % colors.length];
                    return ListTile(
                      leading: Container(
                        width: 44,
                        height: 44,
                        decoration: BoxDecoration(
                          gradient: LinearGradient(
                            colors: [color, color.withValues(alpha: 0.7)],
                          ),
                          borderRadius: BorderRadius.zero,
                        ),
                        child: Center(
                          child: Text(
                            (user.nickname.isNotEmpty
                                    ? user.nickname
                                    : user.username)
                                .isNotEmpty
                                ? (user.nickname.isNotEmpty
                                        ? user.nickname
                                        : user.username)[0]
                                    .toUpperCase()
                                : '?',
                            style: const TextStyle(
                              color: Colors.white,
                              fontSize: 15,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                      ),
                      title: Text(
                        user.nickname.isNotEmpty
                            ? user.nickname
                            : user.username,
                        style: const TextStyle(
                            fontWeight: FontWeight.w500, fontSize: 14),
                      ),
                      subtitle: Text(
                        '@${user.username}',
                        style: TextStyle(
                          fontSize: 12,
                          color: theme.colorScheme.secondary
                              .withValues(alpha: 0.6),
                        ),
                      ),
                      trailing: isAlreadyFriend
                          ? Container(
                              padding: const EdgeInsets.symmetric(
                                  horizontal: 12, vertical: 6),
                              decoration: BoxDecoration(
                                color: theme.colorScheme.secondary
                                    .withValues(alpha: 0.1),
                                borderRadius: BorderRadius.circular(8),
                              ),
                              child: Text(
                                'friend_already_friend'.tr,
                                style: TextStyle(
                                  fontSize: 12,
                                  color: theme.colorScheme.secondary
                                      .withValues(alpha: 0.6),
                                ),
                              ),
                            )
                          : isSent
                              ? Container(
                                  padding: const EdgeInsets.symmetric(
                                      horizontal: 12, vertical: 6),
                                  decoration: BoxDecoration(
                                    color: theme.colorScheme.secondary
                                        .withValues(alpha: 0.1),
                                    borderRadius: BorderRadius.circular(8),
                                  ),
                                  child: Text(
                                    'friend_request_sent'.tr,
                                    style: TextStyle(
                                      fontSize: 12,
                                      color: theme.colorScheme.secondary
                                          .withValues(alpha: 0.6),
                                    ),
                                  ),
                                )
                              : TextButton(
                                  onPressed: () =>
                                      controller.sendFriendRequest(user),
                                  style: TextButton.styleFrom(
                                    backgroundColor:
                                        theme.primaryColor.withValues(alpha: 0.1),
                                    foregroundColor: theme.primaryColor,
                                    padding: const EdgeInsets.symmetric(
                                        horizontal: 12, vertical: 6),
                                    minimumSize: Size.zero,
                                    tapTargetSize:
                                        MaterialTapTargetSize.shrinkWrap,
                                    shape: RoundedRectangleBorder(
                                      borderRadius: BorderRadius.circular(8),
                                    ),
                                  ),
                                  child: Text(
                                    'contacts_add_friend'.tr,
                                    style: const TextStyle(fontSize: 12),
                                  ),
                                ),
                    );
                  },
                );
              }

              // Default: friend list + function tiles
              return RefreshIndicator(
                onRefresh: controller.refreshContacts,
                child: ListView(
                  key: const PageStorageKey<String>('home_contacts_scroll'),
                  children: [
                    const SizedBox(height: 8),
                    // Function entries
                    _buildSection(context, [
                      _ContactFunctionTile(
                        icon: Icons.person_add_alt_1_rounded,
                        iconColor: AppTheme.successColor,
                        title: 'contacts_add_friend'.tr,
                        onTap: () => ContactQuickActions.showAddFriendDialog(
                          context,
                          controller,
                        ),
                      ),
                      _ContactFunctionTile(
                        icon: Icons.qr_code_scanner_rounded,
                        iconColor: AppTheme.primaryColor,
                        title: 'conversations_scan_user_qr'.tr,
                        onTap: () => controller.openUserQrScanner(),
                      ),
                      Obx(() {
                        final pendingCount = controller
                            .friendService
                            .friendRequests
                            .where((r) => r.status == 0)
                            .length;
                        return _ContactFunctionTile(
                          icon: Icons.mark_email_unread_rounded,
                          iconColor: AppTheme.warningColor,
                          title: 'friend_requests'.tr,
                          badge: pendingCount > 0 ? pendingCount : null,
                          onTap: () => Get.toNamed(AppRoutes.friendRequests),
                        );
                      }),
                    ]),

                    const SizedBox(height: 12),

                    // Friends (dynamic)
                    _buildGroupHeader(
                      context,
                      'contacts_friends'.tr,
                      Icons.people_rounded,
                    ),
                    Obx(() {
                      final friends = controller.friendService.friendList;
                      if (friends.isEmpty) {
                        return _buildSection(context, [
                          Padding(
                            padding:
                                const EdgeInsets.symmetric(vertical: 24),
                            child: Center(
                              child: Column(
                                children: [
                                  Icon(
                                    Icons.people_outline_rounded,
                                    size: 40,
                                    color: theme.colorScheme.secondary
                                        .withValues(alpha: 0.3),
                                  ),
                                  const SizedBox(height: 8),
                                  Text(
                                    'contacts_empty'.tr,
                                    style: TextStyle(
                                      color: theme.colorScheme.secondary
                                          .withValues(alpha: 0.5),
                                      fontSize: 14,
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          ),
                        ]);
                      }
                      return _buildSection(
                        context,
                        friends.map((f) {
                          const colors = AppTheme.avatarColors;
                          final color =
                              colors[f.userId.hashCode.abs() % colors.length];
                          return _ContactTile(
                            name: f.nickname.isNotEmpty
                                ? f.nickname
                                : f.username,
                            subtitle: '@${f.username}',
                            avatarColor: color,
                            isOnline: false,
                            onTap: () => controller.navigateToAccountInfo(f),
                            onCreateSession: () =>
                                controller.createNewChat(f),
                            onDeleteFriend: () => _showDeleteFriendDialog(
                                context, controller, f),
                            onBlockUser: () =>
                                _showBlockUserDialog(context, controller, f),
                          );
                        }).toList(),
                      );
                    }),

                    const SizedBox(height: 16),
                  ],
                ),
              );
            }),
          ),
        ],
      ),
    );
  }

  Widget _buildGroupHeader(BuildContext context, String title, IconData icon) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      child: Row(
        children: [
          Icon(icon, size: 16, color: theme.colorScheme.secondary),
          const SizedBox(width: 6),
          Text(
            title,
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w600,
              color: theme.colorScheme.secondary,
              letterSpacing: 0.5,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildSection(BuildContext context, List<Widget> children) {
    final theme = Theme.of(context);
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        children: children
            .asMap()
            .entries
            .map(
              (entry) => Column(
                children: [
                  entry.value,
                  if (entry.key < children.length - 1)
                    Divider(
                      indent: 68,
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

class _ContactFunctionTile extends StatelessWidget {
  final IconData icon;
  final Color iconColor;
  final String title;
  final VoidCallback onTap;
  final int? badge;

  const _ContactFunctionTile({
    required this.icon,
    required this.iconColor,
    required this.title,
    required this.onTap,
    this.badge,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ListTile(
      leading: Container(
        width: 44,
        height: 44,
        decoration: BoxDecoration(
          color: iconColor.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Icon(icon, color: iconColor, size: 22),
      ),
      title: Text(
        title,
        style: TextStyle(
          fontSize: 14,
          fontWeight: FontWeight.w500,
          color: theme.colorScheme.onSurface,
        ),
      ),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (badge != null)
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
              decoration: BoxDecoration(
                color: AppTheme.errorColor,
                borderRadius: BorderRadius.circular(10),
              ),
              child: Text(
                '$badge',
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 12,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
          if (badge != null) const SizedBox(width: 6),
          Icon(
            Icons.chevron_right_rounded,
            color: theme.colorScheme.secondary.withValues(alpha: 0.4),
          ),
        ],
      ),
      onTap: onTap,
    );
  }
}

class _ContactTile extends StatelessWidget {
  static const double _avatarSize = 44;

  final String name;
  final String subtitle;
  final Color avatarColor;
  final bool isOnline;
  final VoidCallback onTap;
  final VoidCallback? onCreateSession;
  final VoidCallback? onDeleteFriend;
  final VoidCallback? onBlockUser;

  const _ContactTile({
    required this.name,
    required this.subtitle,
    required this.avatarColor,
    this.isOnline = false,
    required this.onTap,
    this.onCreateSession,
    this.onDeleteFriend,
    this.onBlockUser,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ListTile(
      leading: Stack(
        children: [
          Container(
            width: _avatarSize,
            height: _avatarSize,
            decoration: BoxDecoration(
              gradient: LinearGradient(
                begin: Alignment.topLeft,
                end: Alignment.bottomRight,
                colors: [avatarColor, avatarColor.withValues(alpha: 0.7)],
              ),
              borderRadius: BorderRadius.zero,
            ),
            child: Center(
              child: Text(
                name.isNotEmpty ? name[0].toUpperCase() : '?',
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 16,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ),
          ),
          if (isOnline)
            Positioned(
              right: -2,
              bottom: -2,
              child: Container(
                width: 14,
                height: 14,
                decoration: BoxDecoration(
                  color: AppTheme.successColor,
                  shape: BoxShape.circle,
                  border: Border.all(
                    color: theme.colorScheme.surface,
                    width: 2,
                  ),
                ),
              ),
            ),
        ],
      ),
      title: Text(
        name,
        style: TextStyle(
          fontSize: 14,
          fontWeight: FontWeight.w600,
          color: theme.colorScheme.onSurface,
        ),
      ),
      subtitle: Text(
        subtitle,
        style: TextStyle(fontSize: 12, color: theme.colorScheme.secondary),
      ),
      trailing: _ContactTileActions(
        onCreateSession: onCreateSession,
        onDeleteFriend: onDeleteFriend,
        onBlockUser: onBlockUser,
      ),
      onTap: onTap,
    );
  }
}

enum _ContactMenuAction { deleteFriend, blockUser }

class _ContactTileActions extends StatelessWidget {
  const _ContactTileActions({
    this.onCreateSession,
    this.onDeleteFriend,
    this.onBlockUser,
  });

  final VoidCallback? onCreateSession;
  final VoidCallback? onDeleteFriend;
  final VoidCallback? onBlockUser;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final hasMenu = onDeleteFriend != null || onBlockUser != null;
    if (onCreateSession == null && !hasMenu) {
      return const SizedBox.shrink();
    }

    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (onCreateSession != null)
          IconButton(
            tooltip: 'contacts_new_session'.tr,
            icon: Icon(
              Icons.add_comment_outlined,
              color: theme.colorScheme.secondary.withValues(alpha: 0.6),
              size: 20,
            ),
            onPressed: onCreateSession,
          ),
        if (hasMenu)
          PopupMenuButton<_ContactMenuAction>(
            tooltip: 'contacts_friend_actions'.tr,
            onSelected: (_ContactMenuAction action) {
              switch (action) {
                case _ContactMenuAction.deleteFriend:
                  onDeleteFriend?.call();
                  break;
                case _ContactMenuAction.blockUser:
                  onBlockUser?.call();
                  break;
              }
            },
            itemBuilder: (_) => [
              if (onDeleteFriend != null)
                PopupMenuItem<_ContactMenuAction>(
                  value: _ContactMenuAction.deleteFriend,
                  child: Text('account_info_delete_friend'.tr),
                ),
              if (onBlockUser != null)
                PopupMenuItem<_ContactMenuAction>(
                  value: _ContactMenuAction.blockUser,
                  child: Text(
                    'contacts_block_friend'.tr,
                    style: const TextStyle(color: AppTheme.errorColor),
                  ),
                ),
            ],
          ),
      ],
    );
  }
}

Future<void> _showDeleteFriendDialog(
  BuildContext context,
  ContactsController controller,
  FriendItem friend,
) async {
  final confirmed = await showAppConfirmDialog(
    context: context,
    title: 'account_info_delete_friend'.tr,
    message: 'account_info_delete_friend_confirm'.trParams({
      'name': friend.username,
    }),
    isDestructive: true,
  );

  if (confirmed) {
    await controller.deleteFriend(friend);
  }
}

Future<void> _showBlockUserDialog(
  BuildContext context,
  ContactsController controller,
  FriendItem friend,
) async {
  final confirmed = await showAppConfirmDialog(
    context: context,
    title: 'contacts_block_friend'.tr,
    message: 'contacts_block_friend_confirm'.trParams({'name': friend.username}),
    isDestructive: true,
  );

  if (confirmed) {
    await controller.blockUser(friend);
  }
}
