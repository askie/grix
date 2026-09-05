import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../app/themes/app_theme.dart';
import '../../data/providers/user_session_favorite_service.dart';
import '../../shared/widgets/session_avatar.dart';
import 'controllers/favorites_controller.dart';
import 'home_view.dart';
import 'widgets/session_avatar_view.dart';

class FavoritesView extends StatelessWidget {
  const FavoritesView({super.key});

  @override
  Widget build(BuildContext context) {
    final controller = Get.find<FavoritesController>();
    final theme = Theme.of(context);
    // The swipe shortcut mirrors the mobile home page: narrow layouts only.
    final isNarrow = MediaQuery.sizeOf(context).width < kHomeWideBreakpoint;

    return Scaffold(
      backgroundColor: theme.scaffoldBackgroundColor,
      appBar: AppBar(
        title: Text(
          'favorites_title'.tr,
          style: theme.textTheme.titleLarge?.copyWith(fontSize: 18),
        ),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh_rounded),
            onPressed: controller.refresh,
            tooltip: 'common_refresh'.tr,
          ),
        ],
      ),
      body: GestureDetector(
        behavior: HitTestBehavior.translucent,
        onHorizontalDragEnd: (details) =>
            _handleHorizontalDragEnd(isNarrow, details),
        child: Column(
          children: [
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              color: theme.appBarTheme.backgroundColor,
              child: Obx(() {
                final hasQuery = controller.searchQuery.value.isNotEmpty;
                return TextField(
                  controller: controller.searchController,
                  decoration: InputDecoration(
                    hintText: 'favorites_search_hint'.tr,
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
                              color: theme.colorScheme.secondary.withValues(
                                alpha: 0.5,
                              ),
                              size: 18,
                            ),
                            onPressed: () {
                              controller.searchController.clear();
                              controller.onSearchChanged('');
                            },
                          )
                        : null,
                    isDense: true,
                  ),
                  onChanged: controller.onSearchChanged,
                );
              }),
            ),
            Expanded(
              child: Obx(() {
                if (controller.isLoading) {
                  return const Center(
                    child: Padding(
                      padding: EdgeInsets.all(24),
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  );
                }
                final items = controller.displayItems;
                if (items.isEmpty) {
                  return Center(
                    child: Padding(
                      padding: const EdgeInsets.all(24),
                      child: Text(
                        'favorites_empty'.tr,
                        style: TextStyle(
                          color: theme.colorScheme.secondary.withValues(
                            alpha: 0.5,
                          ),
                          fontSize: 14,
                        ),
                      ),
                    ),
                  );
                }
                return RefreshIndicator(
                  onRefresh: controller.refresh,
                  child: ListView.separated(
                    padding: EdgeInsets.zero,
                    itemCount: items.length,
                    separatorBuilder: (_, __) => Divider(
                      height: 1,
                      indent: 72,
                      color: theme.colorScheme.outline.withValues(alpha: 0.1),
                    ),
                    itemBuilder: (context, index) {
                      final item = items[index];
                      return _FavoriteSessionTile(
                        item: item,
                        onTap: () => controller.openSession(item),
                        onLongPress: () =>
                            _showRemoveSheet(context, item, controller),
                      );
                    },
                  ),
                );
              }),
            ),
          ],
        ),
      ),
    );
  }

  /// A left swipe closes the favorites page, mirroring the right swipe that
  /// opens it from the mobile home page. Wide layouts keep their existing
  /// behavior; vertical scrolling and the search field are untouched because
  /// the horizontal recognizer only wins on horizontally dominant drags.
  void _handleHorizontalDragEnd(bool isNarrow, DragEndDetails details) {
    if (!isNarrow) {
      return;
    }
    final velocity = details.primaryVelocity ?? 0;
    if (velocity >= -kHomeSwipeVelocityThreshold) {
      return;
    }
    // Repeated flings must not pop past the favorites page.
    if (Get.currentRoute != AppRoutes.favorites) {
      return;
    }
    Get.back();
  }
}

void _showRemoveSheet(
  BuildContext context,
  FavoriteSessionItem item,
  FavoritesController controller,
) {
  final theme = Theme.of(context);
  showModalBottomSheet(
    context: context,
    backgroundColor: theme.colorScheme.surface,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
    ),
    builder: (_) => SafeArea(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          ListTile(
            leading: const Icon(Icons.bookmark_remove_rounded),
            title: Text('conversations_unfavorite'.tr),
            onTap: () {
              Navigator.pop(context);
              controller.removeFavorite(item.sessionId);
            },
          ),
        ],
      ),
    ),
  );
}

class _FavoriteSessionTile extends StatelessWidget {
  const _FavoriteSessionTile({
    required this.item,
    required this.onTap,
    required this.onLongPress,
  });

  final FavoriteSessionItem item;
  final VoidCallback onTap;
  final VoidCallback onLongPress;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isGroup = item.sessionType == 2;
    final avatarColor = AppTheme.getAvatarColor(item.sessionId);
    final title = item.title.isNotEmpty ? item.title : item.sessionId;

    return InkWell(
      onTap: onTap,
      onLongPress: onLongPress,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        child: Row(
          children: [
            _FavoriteAvatar(
              item: item,
              isGroup: isGroup,
              avatarTitle: title,
              avatarColor: avatarColor,
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.bodyMedium?.copyWith(
                            fontWeight: FontWeight.w600,
                            fontSize: 15,
                          ),
                        ),
                      ),
                      if (isGroup)
                        Padding(
                          padding: const EdgeInsets.only(left: 4),
                          child: Icon(
                            Icons.group_rounded,
                            size: 13,
                            color: theme.colorScheme.secondary.withValues(
                              alpha: 0.5,
                            ),
                          ),
                        ),
                    ],
                  ),
                  if (item.lastMsg.isNotEmpty) ...[
                    const SizedBox(height: 2),
                    Text(
                      item.lastMsg,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.secondary.withValues(
                          alpha: 0.6,
                        ),
                        fontSize: 12,
                      ),
                    ),
                  ],
                ],
              ),
            ),
            Icon(
              Icons.chevron_right_rounded,
              size: 18,
              color: theme.colorScheme.secondary.withValues(alpha: 0.3),
            ),
          ],
        ),
      ),
    );
  }
}

/// Renders a favorite's avatar via the shared [SessionAvatarView] when the
/// session is available locally (covering agent / friend / group nine-grid),
/// falling back to the first-letter placeholder otherwise.
class _FavoriteAvatar extends StatelessWidget {
  const _FavoriteAvatar({
    required this.item,
    required this.isGroup,
    required this.avatarTitle,
    required this.avatarColor,
  });

  final FavoriteSessionItem item;
  final bool isGroup;
  final String avatarTitle;
  final Color avatarColor;

  @override
  Widget build(BuildContext context) {
    final conv = Get.find<FavoritesController>().conversations;
    if (conv == null) {
      return SessionAvatar(
        isGroup: isGroup,
        avatarTitle: avatarTitle,
        avatarColor: avatarColor,
      );
    }

    return Obx(() {
      // Subscribe to local session availability so a favorite whose session is
      // not yet cached (e.g. cold start / never-opened session) upgrades from
      // the placeholder to the real avatar in place, without leaving the page.
      conv.imService.sessionsLoadTick.value;
      conv.imService.sessions.length;
      final session = conv.imService.findSessionById(item.sessionId);
      if (session == null) {
        return SessionAvatar(
          isGroup: isGroup,
          avatarTitle: avatarTitle,
          avatarColor: avatarColor,
        );
      }
      return SessionAvatarView(
        session: session,
        avatarTitle: avatarTitle,
        avatarColor: avatarColor,
      );
    });
  }
}
