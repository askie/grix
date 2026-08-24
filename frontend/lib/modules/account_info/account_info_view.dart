import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../app/themes/app_theme.dart';
import '../../data/models/session_model.dart';
import '../../data/providers/user_session_favorite_service.dart';
import '../chat/widgets/chat_forward_target_picker_sheet.dart';
import '../chat/widgets/send_message_to_agent_dialog.dart';
import '../home/controllers/conversations_controller.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_dialog_style.dart';
import '../../shared/widgets/avatar_network_image.dart';
import '../../shared/widgets/session_avatar.dart';
import '../../shared/widgets/session_status_icon.dart';
import 'controllers/account_info_controller.dart';

enum _AccountInfoMenuAction { forward, editRemark, report, deleteFriend }

class AccountInfoView extends GetView<AccountInfoController> {
  const AccountInfoView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      backgroundColor: theme.scaffoldBackgroundColor,
      appBar: AppBar(
        title: Obx(() {
          final showNickname = controller.showTitleNickname.value;
          final titleText = showNickname
              ? controller.displayNickname
              : 'account_info_title'.tr;
          return AnimatedSwitcher(
            duration: const Duration(milliseconds: 200),
            transitionBuilder: (child, animation) =>
                FadeTransition(opacity: animation, child: child),
            child: Text(
              titleText,
              key: ValueKey(showNickname),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.titleLarge?.copyWith(fontSize: 18),
            ),
          );
        }),
        actions: [_AccountInfoMoreActions(controller: controller)],
      ),
      body: Obx(() {
        final sessions = controller.canStartChat
            ? controller.conversationSessions
            : const <SessionModel>[];
        const hPadding = EdgeInsets.symmetric(horizontal: 16);

        return CustomScrollView(
          controller: controller.scrollController,
          slivers: [
            SliverToBoxAdapter(
              child: _SizeReportingWidget(
                onSizeChanged: (size) =>
                    controller.updateProfileCardExtent(size.height),
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(16, 16, 16, 0),
                  child: _ProfileCard(controller: controller),
                ),
              ),
            ),
            const SliverToBoxAdapter(
              child: Padding(padding: hPadding, child: SizedBox(height: 18)),
            ),
            if (controller.canStartChat) ...[
              SliverToBoxAdapter(
                child: Padding(
                  padding: hPadding,
                  child: Row(
                    children: [
                      Expanded(
                        child: Align(
                          alignment: Alignment.centerLeft,
                          child: Obx(() {
                            final totalCount = controller.allConversationSessions.length;
                            return Text(
                              'account_info_history_count'.trParams({
                                'count': '$totalCount',
                              }),
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: theme.colorScheme.secondary.withValues(
                                  alpha: 0.75,
                                ),
                              ),
                            );
                          }),
                        ),
                      ),
                      const SizedBox(width: 10),
                      Expanded(child: _SessionSearchField(controller: controller)),
                    ],
                  ),
                ),
              ),
              const SliverToBoxAdapter(child: SizedBox(height: 10)),
              if (sessions.isEmpty)
                SliverToBoxAdapter(
                  child: Padding(
                    padding: hPadding,
                    child: _HistoryEmptyCard(
                      hasQuery: controller.searchQuery.value.trim().isNotEmpty,
                    ),
                  ),
                )
              else
                _HistorySliverList(
                  sessions: sessions,
                  controller: controller,
                ),
              if (controller.isThreadHistoryLoading.value)
                const SliverToBoxAdapter(
                  child: Padding(
                    padding: EdgeInsets.symmetric(vertical: 16),
                    child: Center(
                      child: SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                    ),
                  ),
                ),
            ] else ...[
              if (controller.peerTypeHint == 1)
                SliverToBoxAdapter(
                  child: Padding(
                    padding: hPadding,
                    child: _NotFriendHintCard(),
                  ),
                ),
            ],
            const SliverToBoxAdapter(
              child: SizedBox(height: 24),
            ),
          ],
        );
      }),
    );
  }
}

class _ProfileCard extends StatelessWidget {
  const _ProfileCard({required this.controller});

  final AccountInfoController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Obx(() {
      final isProfileLoading = controller.isProfileLoading.value;
      final friendRequestSent = controller.friendRequestSent.value;

      return Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: theme.colorScheme.surface,
          borderRadius: BorderRadius.circular(14),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                _AccountAvatar(controller: controller),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        controller.displayNickname,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.titleMedium?.copyWith(
                          fontWeight: FontWeight.w700,
                          fontSize: 18,
                        ),
                      ),
                      const SizedBox(height: 8),
                      _AccountMetaBlock(controller: controller),
                      if (controller.introductionPreview.isNotEmpty) ...[
                        const SizedBox(height: 10),
                        Text(
                          controller.introductionPreview,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.bodyMedium?.copyWith(
                            color: theme.colorScheme.onSurface.withValues(
                              alpha: 0.82,
                            ),
                          ),
                        ),
                      ],
                      if (isProfileLoading) ...[
                        const SizedBox(height: 8),
                        Row(
                          children: [
                            SizedBox(
                              width: 14,
                              height: 14,
                              child: CircularProgressIndicator(
                                strokeWidth: 2,
                                valueColor: AlwaysStoppedAnimation<Color>(
                                  theme.primaryColor,
                                ),
                              ),
                            ),
                            const SizedBox(width: 8),
                            Text(
                              'common_loading'.tr,
                              style: theme.textTheme.bodySmall,
                            ),
                          ],
                        ),
                      ],
                    ],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            if (controller.canStartChat)
              // 建会话等待期间不要把 ElevatedButton 置为 disabled（onPressed:null）：
              // Material 会立刻掐断正在扩散的红色 overlay/splash，看起来像「红影卡住再顿一下」。
              // 防重入交给 controller.isActionProcessing；这里只换 loading 子节点。
              SizedBox(
                width: double.infinity,
                child: Obx(() {
                  final isActionProcessing =
                      controller.isActionProcessing.value;
                  return ElevatedButton(
                    onPressed: controller.startChatFromProfile,
                    child: isActionProcessing
                        ? SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              valueColor: AlwaysStoppedAnimation<Color>(
                                theme.colorScheme.onPrimary,
                              ),
                            ),
                          )
                        : Text('account_info_start_chat'.tr),
                  );
                }),
              )
            else if (controller.canAddFriend || friendRequestSent)
              SizedBox(
                width: double.infinity,
                child: Obx(() {
                  final isActionProcessing =
                      controller.isActionProcessing.value;
                  return OutlinedButton(
                    onPressed: (isActionProcessing || friendRequestSent)
                        ? null
                        : controller.sendFriendRequest,
                    child: Text(
                      friendRequestSent
                          ? 'account_info_friend_request_sent'.tr
                          : 'account_info_add_friend'.tr,
                    ),
                  );
                }),
              ),
          ],
        ),
      );
    });
  }
}

class _AccountInfoMoreActions extends StatelessWidget {
  const _AccountInfoMoreActions({required this.controller});

  final AccountInfoController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final canForward = controller.canForwardProfileCard;
      final canEditRemark = controller.canEditRemark;
      final canReportUser = controller.canReportUser;
      final canDeleteFriend = controller.canDeleteFriend;
      if (!canForward && !canEditRemark && !canReportUser && !canDeleteFriend) {
        return const SizedBox.shrink();
      }

      final isActionProcessing = controller.isActionProcessing.value;

      return PopupMenuButton<_AccountInfoMenuAction>(
        icon: const Icon(Icons.more_vert_rounded),
        onSelected: (_AccountInfoMenuAction action) async {
          switch (action) {
            case _AccountInfoMenuAction.forward:
              await _forwardProfileCard(context, controller);
              break;
            case _AccountInfoMenuAction.editRemark:
              _showRemarkEditDialog(context, controller);
              break;
            case _AccountInfoMenuAction.report:
              controller.openReportPage();
              break;
            case _AccountInfoMenuAction.deleteFriend:
              _showDeleteFriendDialog(context, controller);
              break;
          }
        },
        itemBuilder: (_) => [
          if (canForward)
            PopupMenuItem<_AccountInfoMenuAction>(
              value: _AccountInfoMenuAction.forward,
              enabled: !isActionProcessing,
              child: Text('chat_forward'.tr),
            ),
          if (canEditRemark)
            PopupMenuItem<_AccountInfoMenuAction>(
              value: _AccountInfoMenuAction.editRemark,
              enabled: !isActionProcessing,
              child: Text('account_info_edit_remark'.tr),
            ),
          if (canReportUser)
            PopupMenuItem<_AccountInfoMenuAction>(
              value: _AccountInfoMenuAction.report,
              enabled: !isActionProcessing,
              child: Text(
                'account_info_report_user'.tr,
                style: const TextStyle(color: AppTheme.errorColor),
              ),
            ),
          if (canDeleteFriend)
            PopupMenuItem<_AccountInfoMenuAction>(
              value: _AccountInfoMenuAction.deleteFriend,
              enabled: !isActionProcessing,
              child: Text(
                'account_info_delete_friend'.tr,
                style: const TextStyle(color: AppTheme.errorColor),
              ),
            ),
        ],
      );
    });
  }
}

Future<void> _forwardProfileCard(
  BuildContext context,
  AccountInfoController controller,
) async {
  final options = controller.buildForwardTargetOptions();
  if (options.isEmpty) {
    CustomToast.show('chat_forward_no_target'.tr);
    return;
  }

  final target = await ChatForwardTargetPickerSheet.show(
    context,
    options: options,
    onSendToAgent: () {
      unawaited(
        showSendMessageToAgentDialog(
          context,
          initialMessage: controller.buildProfileCardAgentDraft(),
        ),
      );
    },
  );
  if (target == null || !context.mounted) {
    return;
  }

  final sentCount = await controller.forwardProfileCard(
    targetSessionId: target.sessionId,
  );
  if (!context.mounted) {
    return;
  }
  if (sentCount <= 0) {
    CustomToast.show('chat_forward_failed'.tr);
    return;
  }

  CustomToast.show(
    'chat_forward_queued'.trParams({'count': '$sentCount'}),
    isError: false,
  );
}

Future<void> _showRemarkEditDialog(
  BuildContext context,
  AccountInfoController controller,
) async {
  final textController = TextEditingController(
    text: controller.currentRemarkName,
  );
  try {
    await showAppDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('account_info_edit_remark'.tr),
        content: TextField(
          controller: textController,
          maxLength: 50,
          decoration: InputDecoration(hintText: 'account_info_remark_hint'.tr),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: Text('common_cancel'.tr),
          ),
          TextButton(
            onPressed: () async {
              final success = await controller.updateFriendRemark(
                textController.text,
              );
              if (!dialogContext.mounted) return;
              if (!success) return;
              Navigator.of(dialogContext).pop();
              CustomToast.show(
                'account_info_remark_updated'.tr,
                isError: false,
              );
            },
            child: Text('common_confirm'.tr),
          ),
        ],
      ),
    );
  } finally {
    textController.dispose();
  }
}

Future<void> _showDeleteFriendDialog(
  BuildContext context,
  AccountInfoController controller,
) async {
  final targetName = controller.displayNickname;
  final shouldDelete = await showAppConfirmDialog(
    context: context,
    title: 'account_info_delete_friend'.tr,
    message: 'account_info_delete_friend_confirm'.trParams({'name': targetName}),
    confirmText: 'common_delete'.tr,
    isDestructive: true,
  );
  if (!shouldDelete) {
    return;
  }

  final success = await controller.deleteFriend();
  if (!context.mounted || !success) {
    return;
  }
  CustomToast.show('account_info_delete_friend_success'.tr, isError: false);
}

class _AccountAvatar extends StatelessWidget {
  const _AccountAvatar({required this.controller});

  final AccountInfoController controller;

  @override
  Widget build(BuildContext context) {
    const size = 82.0;
    const borderRadius = 0.0;

    return Obx(() {
      final avatarUrl = controller.avatarUrl.value.trim();

      if (avatarUrl.isEmpty) {
        return SessionAvatar(
          isGroup: false,
          avatarTitle: controller.avatarTitle,
          avatarColor: AppTheme.getAvatarColor(controller.avatarSeed),
          size: size,
          borderRadius: borderRadius,
        );
      }

      final fallbackAvatar = SessionAvatar(
        isGroup: false,
        avatarTitle: controller.avatarTitle,
        avatarColor: AppTheme.getAvatarColor(controller.avatarSeed),
        size: size,
        borderRadius: borderRadius,
      );

      return ClipRRect(
        borderRadius: BorderRadius.zero,
        child: SizedBox(
          width: size,
          height: size,
          child: AvatarNetworkImage(
            avatarUrl: avatarUrl,
            fallback: fallbackAvatar,
          ),
        ),
      );
    });
  }
}

class _AccountMetaBlock extends StatelessWidget {
  const _AccountMetaBlock({required this.controller});

  final AccountInfoController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final showAccountLine = controller.peerTypeHint == 1;
      final userIdValue = controller.displayUserId;
      final userIdCopy = userIdValue == '-' ? null : userIdValue;

      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (showAccountLine) ...[
            _AccountMetaLine(
              label: 'account_info_account'.tr,
              value: controller.displayAccount,
            ),
            const SizedBox(height: 6),
          ],
          _AccountMetaLine(
            label: 'account_info_user_id'.tr,
            value: userIdValue,
            copyValue: userIdCopy,
          ),
        ],
      );
    });
  }
}

class _AccountMetaLine extends StatelessWidget {
  const _AccountMetaLine({
    required this.label,
    required this.value,
    this.copyValue,
  });

  final String label;
  final String value;

  /// 点击复制使用的实际文本。
  /// 为 null 时不启用复制；为空字符串等无效值由调用方负责过滤。
  final String? copyValue;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final textWidget = Text.rich(
      TextSpan(
        children: [
          TextSpan(
            text: '$label ',
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.secondary.withValues(alpha: 0.75),
              fontWeight: FontWeight.w500,
            ),
          ),
          TextSpan(
            text: value,
            style: theme.textTheme.bodyMedium?.copyWith(
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );

    final copyText = copyValue;
    if (copyText == null || copyText.isEmpty) {
      return textWidget;
    }

    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTap: () => _copyToClipboard(copyText),
      child: textWidget,
    );
  }

  Future<void> _copyToClipboard(String text) async {
    await Clipboard.setData(ClipboardData(text: text));
    CustomToast.show('chat_copy_success'.tr, isError: false);
  }
}

class _HistoryEmptyCard extends StatelessWidget {
  const _HistoryEmptyCard({this.hasQuery = false});

  final bool hasQuery;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 24),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(14),
      ),
      child: Center(
        child: Text(
          hasQuery
              ? 'account_info_history_search_empty'.tr
              : 'account_info_history_empty'.tr,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.secondary.withValues(alpha: 0.7),
          ),
        ),
      ),
    );
  }
}

class _SessionSearchField extends StatefulWidget {
  const _SessionSearchField({required this.controller});

  final AccountInfoController controller;

  @override
  State<_SessionSearchField> createState() => _SessionSearchFieldState();
}

class _SessionSearchFieldState extends State<_SessionSearchField> {
  late final TextEditingController _editController;

  @override
  void initState() {
    super.initState();
    _editController = TextEditingController(text: widget.controller.searchQuery.value);
    _editController.addListener(_onTextChanged);
  }

  @override
  void dispose() {
    _editController.removeListener(_onTextChanged);
    _editController.dispose();
    super.dispose();
  }

  void _onTextChanged() {
    widget.controller.searchQuery.value = _editController.text;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Obx(() {
      final query = widget.controller.searchQuery.value;
      return TextField(
        controller: _editController,
        style: theme.textTheme.bodyMedium,
        decoration: InputDecoration(
          hintText: 'account_info_history_search_hint'.tr,
          hintStyle: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.secondary.withValues(alpha: 0.5),
          ),
          prefixIcon: Icon(
            Icons.search_rounded,
            size: 20,
            color: theme.colorScheme.secondary.withValues(alpha: 0.6),
          ),
          suffixIcon: query.isNotEmpty
              ? GestureDetector(
                  onTap: () {
                    _editController.clear();
                  },
                  child: Icon(
                    Icons.close_rounded,
                    size: 18,
                    color: theme.colorScheme.secondary.withValues(alpha: 0.6),
                  ),
                )
              : null,
          filled: true,
          fillColor: theme.colorScheme.surface,
          contentPadding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
          border: OutlineInputBorder(
            borderRadius: BorderRadius.circular(10),
            borderSide: BorderSide.none,
          ),
          enabledBorder: OutlineInputBorder(
            borderRadius: BorderRadius.circular(10),
            borderSide: BorderSide.none,
          ),
          focusedBorder: OutlineInputBorder(
            borderRadius: BorderRadius.circular(10),
            borderSide: BorderSide.none,
          ),
        ),
      );
    });
  }
}

class _NotFriendHintCard extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 24),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(14),
      ),
      child: Center(
        child: Text(
          'account_info_not_friend_hint'.tr,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.secondary.withValues(alpha: 0.72),
          ),
          textAlign: TextAlign.center,
        ),
      ),
    );
  }
}

/// Lazy-loaded session history list using SliverList.
///
/// Replaces the previous shrinkWrap ListView which forced all items to build
/// in the first frame, dropping the page transition animation when the list
/// was long.
class _HistorySliverList extends StatelessWidget {
  const _HistorySliverList({required this.sessions, required this.controller});

  final List<SessionModel> sessions;
  final AccountInfoController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final surfaceColor = theme.colorScheme.surface;
    final dividerColor = theme.colorScheme.outline.withValues(alpha: 0.15);
    final count = sessions.length;

    return SliverPadding(
      padding: const EdgeInsets.symmetric(horizontal: 16),
      sliver: SliverList(
        delegate: SliverChildBuilderDelegate(
          (context, index) {
            final isLast = index == count - 1;
            final session = sessions[index];

            BorderRadius? borderRadius;
            if (index == 0 && isLast) {
              borderRadius = BorderRadius.circular(14);
            } else if (index == 0) {
              borderRadius =
                  const BorderRadius.vertical(top: Radius.circular(14));
            } else if (isLast) {
              borderRadius =
                  const BorderRadius.vertical(bottom: Radius.circular(14));
            }

            return Container(
              decoration: BoxDecoration(
                color: surfaceColor,
                borderRadius: borderRadius,
              ),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  _SessionHistoryTile(
                    session: session,
                    controller: controller,
                  ),
                if (!isLast)
                  Divider(
                    height: 1,
                    indent: 16,
                    endIndent: 16,
                    color: dividerColor,
                  ),
              ],
            ),
          );
        },
        childCount: count,
      ),
      ),
    );
  }
}

class _SessionHistoryTile extends StatelessWidget {
  const _SessionHistoryTile({required this.session, required this.controller});

  final SessionModel session;
  final AccountInfoController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final title = controller.sessionThreadTitle(session);
    final preview = controller.sessionThreadPreview(session);
    final timeLabel = controller.formatSessionTime(session);

    return Obx(() {
      final isLastTapped =
          controller.lastTappedSessionId.value == session.sessionId;
      return Container(
        decoration: BoxDecoration(
          color: isLastTapped
              ? AppTheme.primaryColor.withValues(alpha: 0.06)
              : Colors.transparent,
          borderRadius: BorderRadius.circular(14),
        ),
        child: InkWell(
          onTap: () => controller.openSession(session),
          onLongPress: () => _showSessionContextMenu(context, session),
          borderRadius: BorderRadius.circular(14),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
            child: Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          SessionStatusIcon(
                            isPinned: session.isPinned,
                            isActive: controller.imService
                                .hasSessionLiveActivity(session.sessionId),
                            spacing: 6,
                          ),
                          Expanded(
                            child: Row(
                              children: [
                                Expanded(
                                  child: Text(
                                    title,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: theme.textTheme.bodyLarge?.copyWith(
                                      fontSize: 15,
                                      fontWeight: FontWeight.w600,
                                    ),
                                  ),
                                ),
                              ],
                            ),
                          ),
                          if (session.isMuted) ...[
                            const SizedBox(width: 6),
                            Container(
                              width: 6,
                              height: 6,
                              color: AppTheme.unreadBadgeColor,
                            ),
                          ],
                          if (timeLabel.isNotEmpty) ...[
                            const SizedBox(width: 10),
                            Text(
                              timeLabel,
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: theme.colorScheme.secondary.withValues(
                                  alpha: 0.7,
                                ),
                              ),
                            ),
                          ],
                        ],
                      ),
                      if (preview.isNotEmpty) ...[
                        const SizedBox(height: 4),
                        Text(
                          preview,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.bodyMedium?.copyWith(
                            color: theme.colorScheme.secondary.withValues(
                              alpha: 0.9,
                            ),
                            fontSize: 13,
                          ),
                        ),
                      ],
                    ],
                  ),
                ),
                if (session.unreadCount > 0 && !session.isMuted) ...[
                  const SizedBox(width: 12),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 7,
                      vertical: 2,
                    ),
                    decoration: BoxDecoration(
                      color: AppTheme.unreadBadgeColor,
                      borderRadius: BorderRadius.circular(10),
                    ),
                    child: Text(
                      session.unreadCount > 99
                          ? '99+'
                          : session.unreadCount.toString(),
                      style: const TextStyle(
                        color: Colors.white,
                        fontSize: 10,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      );
    });
  }

  Future<void> _showSessionContextMenu(
      BuildContext context, SessionModel session) async {
    final favoriteService = UserSessionFavoriteService();
    final favIds = await favoriteService.listIds();
    final isFavorited = favIds.contains(session.sessionId);

    if (!context.mounted) return;

    final theme = Theme.of(context);
    showModalBottomSheet(
      context: context,
      backgroundColor: theme.colorScheme.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) {
        return SafeArea(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ListTile(
                leading: Icon(
                  session.isPinned
                      ? Icons.push_pin_outlined
                      : Icons.push_pin_rounded,
                ),
                title: Text(
                  session.isPinned
                      ? 'conversations_unpin'.tr
                      : 'conversations_pin'.tr,
                ),
                onTap: () {
                  Navigator.pop(context);
                  controller.setSessionPinned(
                    session,
                    isPinned: !session.isPinned,
                  );
                },
              ),
              ListTile(
                leading: Icon(
                  session.isMuted
                      ? Icons.notifications_active_outlined
                      : Icons.notifications_off_outlined,
                ),
                title: Text(
                  session.isMuted
                      ? 'conversations_unmute_notifications'.tr
                      : 'conversations_mute_notifications'.tr,
                ),
                onTap: () {
                  Navigator.pop(context);
                  controller.setSessionMuted(
                    session,
                    isMuted: !session.isMuted,
                  );
                },
              ),
              ListTile(
                leading: Icon(
                  isFavorited
                      ? Icons.bookmark_rounded
                      : Icons.bookmark_border_rounded,
                  color: isFavorited ? AppTheme.primaryColor : null,
                ),
                title: Text(
                  isFavorited
                      ? 'conversations_unfavorite'.tr
                      : 'conversations_favorite'.tr,
                ),
                onTap: () async {
                  Navigator.pop(context);
                  if (isFavorited) {
                    await favoriteService.remove(session.sessionId);
                  } else {
                    await favoriteService.add(session.sessionId);
                  }
                  if (Get.isRegistered<ConversationsController>()) {
                    Get.find<ConversationsController>().reloadFavoriteIds();
                  }
                },
              ),
            ],
          ),
        );
      },
    );
  }
}

/// 上报子组件实测尺寸，用于资料卡高度驱动顶栏标题切换。
class _SizeReportingWidget extends SingleChildRenderObjectWidget {
  const _SizeReportingWidget({
    required this.onSizeChanged,
    required super.child,
  });

  final ValueChanged<Size> onSizeChanged;

  @override
  RenderObject createRenderObject(BuildContext context) {
    return _RenderSizeReporting(onSizeChanged);
  }

  @override
  void updateRenderObject(
    BuildContext context,
    covariant _RenderSizeReporting renderObject,
  ) {
    renderObject.onSizeChanged = onSizeChanged;
  }
}

class _RenderSizeReporting extends RenderProxyBox {
  _RenderSizeReporting(this.onSizeChanged);

  ValueChanged<Size> onSizeChanged;
  Size? _previousSize;

  @override
  void performLayout() {
    super.performLayout();
    final size = child?.size;
    if (size == null || size == _previousSize) {
      return;
    }
    _previousSize = size;
    WidgetsBinding.instance.addPostFrameCallback((_) => onSizeChanged(size));
  }
}
