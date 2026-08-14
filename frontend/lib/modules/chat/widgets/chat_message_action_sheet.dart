import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../app/themes/app_theme.dart';
import '../../../shared/utils/sheet_guard.dart';

enum ChatMessageAction {
  forward,
  selectMultiple,
  copy,
  reply,
  revoke,
}

class ChatMessageActionSheet extends StatelessWidget {
  const ChatMessageActionSheet({
    super.key,
    required this.canCopy,
    required this.canReply,
    required this.canRevoke,
    required this.canForward,
    required this.canSelectMultiple,
    this.onForwardLongPress,
  });

  final bool canCopy;
  final bool canReply;
  final bool canRevoke;
  final bool canForward;
  final bool canSelectMultiple;
  // 转发按钮长按回调，用于复制消息 ID 等附加操作
  final VoidCallback? onForwardLongPress;

  static Future<ChatMessageAction?> show(
    BuildContext context, {
    required bool canCopy,
    required bool canReply,
    required bool canRevoke,
    required bool canForward,
    required bool canSelectMultiple,
    VoidCallback? onForwardLongPress,
  }) {
    // 防重复触发：菜单未关闭前再次长按直接忽略。
    return SheetGuard.run<ChatMessageAction>(
      'chat_message_menu',
      () => showModalBottomSheet<ChatMessageAction>(
        context: context,
        builder: (_) => ChatMessageActionSheet(
          canCopy: canCopy,
          canReply: canReply,
          canRevoke: canRevoke,
          canForward: canForward,
          canSelectMultiple: canSelectMultiple,
          onForwardLongPress: onForwardLongPress,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (canForward)
            ListTile(
              leading: Icon(
                Icons.forward_rounded,
                color: theme.colorScheme.onSurface,
              ),
              title: Text(
                'chat_forward'.tr,
                style: TextStyle(color: theme.colorScheme.onSurface),
              ),
              onTap: () => popSheetOnce(context, ChatMessageAction.forward),
              onLongPress: onForwardLongPress,
            ),
          if (canSelectMultiple)
            ListTile(
              leading: Icon(
                Icons.checklist_rounded,
                color: theme.colorScheme.onSurface,
              ),
              title: Text(
                'chat_forward_select_multiple'.tr,
                style: TextStyle(color: theme.colorScheme.onSurface),
              ),
              onTap: () =>
                  popSheetOnce(context, ChatMessageAction.selectMultiple),
            ),
          if (canCopy)
            ListTile(
              leading: Icon(
                Icons.copy_rounded,
                color: theme.colorScheme.onSurface,
              ),
              title: Text(
                'chat_copy'.tr,
                style: TextStyle(color: theme.colorScheme.onSurface),
              ),
              onTap: () => popSheetOnce(context, ChatMessageAction.copy),
            ),
          if (canReply)
            ListTile(
              leading: Icon(
                Icons.reply_rounded,
                color: theme.colorScheme.onSurface,
              ),
              title: Text(
                'chat_reply'.tr,
                style: TextStyle(color: theme.colorScheme.onSurface),
              ),
              onTap: () => popSheetOnce(context, ChatMessageAction.reply),
            ),
          if (canRevoke)
            ListTile(
              leading:
                  Icon(Icons.restore_rounded, color: theme.colorScheme.error),
              title: Text(
                'chat_revoke'.tr,
                style: TextStyle(color: theme.colorScheme.error),
              ),
              onTap: () => popSheetOnce(context, ChatMessageAction.revoke),
            ),
          const Divider(height: 1),
          ListTile(
            leading:
                const Icon(Icons.close_rounded, color: AppTheme.errorColor),
            title: Text(
              'common_cancel'.tr,
              style: const TextStyle(color: AppTheme.errorColor),
            ),
            onTap: () => popSheetOnce<ChatMessageAction>(context),
          ),
        ],
      ),
    );
  }
}
