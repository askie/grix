import 'package:flutter/material.dart';
import 'package:get/get.dart';

class ChatAttachmentMenu extends StatelessWidget {
  const ChatAttachmentMenu({
    super.key,
    required this.enabled,
    required this.onImageTap,
    required this.onVideoTap,
    required this.onFileTap,
    this.showHideSendAction = false,
    this.isHideSendActive = false,
    this.onHideSendTap,
    this.onVoiceCallTap,  // 仅与 type=4 语音模型私聊时传入
    this.onVoiceBrainTap, // 仅与文字 agent 私聊时传入（与 onVoiceCallTap 互斥）
    this.onBrowseFilesTap, // 仅 Agent 会话时传入
  });

  final bool enabled;
  final VoidCallback onImageTap;
  final VoidCallback onVideoTap;
  final VoidCallback onFileTap;
  final bool showHideSendAction;
  final bool isHideSendActive;
  final VoidCallback? onHideSendTap;
  final VoidCallback? onVoiceCallTap;
  final VoidCallback? onVoiceBrainTap;
  final VoidCallback? onBrowseFilesTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final actions = <_ChatAttachmentMenuItemData?>[
      if (showHideSendAction)
        _ChatAttachmentMenuItemData(
          key: const Key('chat_attachment_menu_hide_send_button'),
          icon: isHideSendActive ? Icons.lock_rounded : Icons.lock_outline,
          label: 'chat_attachment_hide_send'.tr,
          onTap: enabled ? onHideSendTap : null,
        ),
      _ChatAttachmentMenuItemData(
        key: const Key('chat_attachment_menu_image_button'),
        icon: Icons.image_outlined,
        label: 'chat_attachment_upload_image'.tr,
        onTap: enabled ? onImageTap : null,
      ),
      _ChatAttachmentMenuItemData(
        key: const Key('chat_attachment_menu_video_button'),
        icon: Icons.videocam_outlined,
        label: 'chat_attachment_upload_video'.tr,
        onTap: enabled ? onVideoTap : null,
      ),
      _ChatAttachmentMenuItemData(
        key: const Key('chat_attachment_menu_file_button'),
        icon: Icons.attach_file_rounded,
        label: 'chat_attachment_upload_file'.tr,
        onTap: enabled ? onFileTap : null,
      ),
      if (onBrowseFilesTap != null)
        _ChatAttachmentMenuItemData(
          key: const Key('chat_attachment_menu_browse_files_button'),
          icon: Icons.folder_open_outlined,
          label: 'chat_attachment_browse_remote'.tr,
          onTap: enabled ? onBrowseFilesTap : null,
        ),
      if (onVoiceCallTap != null)
        _ChatAttachmentMenuItemData(
          key: const Key('chat_attachment_menu_voice_call_button'),
          icon: Icons.call_outlined,
          label: 'chat_attachment_voice_call'.tr,
          onTap: enabled ? onVoiceCallTap : null,
        ),
      if (onVoiceBrainTap != null)
        _ChatAttachmentMenuItemData(
          key: const Key('chat_attachment_menu_voice_brain_button'),
          icon: Icons.record_voice_over_outlined,
          label: 'chat_attachment_voice_brain'.tr,
          onTap: enabled ? onVoiceBrainTap : null,
        ),
    ];
    while (actions.length < 8) {
      actions.add(null);
    }

    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 300),
        child: Container(
          key: const Key('chat_attachment_menu_panel'),
          margin: const EdgeInsets.only(top: 8),
          padding: const EdgeInsets.fromLTRB(8, 8, 8, 4),
          decoration: BoxDecoration(
            color: theme.colorScheme.surface,
            borderRadius: BorderRadius.circular(18),
            border: Border.all(
              color: theme.colorScheme.outline.withValues(alpha: 0.12),
            ),
          ),
          child: GridView.builder(
            shrinkWrap: true,
            physics: const NeverScrollableScrollPhysics(),
            itemCount: actions.length,
            gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: 4,
              mainAxisSpacing: 4,
              crossAxisSpacing: 4,
              childAspectRatio: 0.85,
            ),
            itemBuilder: (context, index) {
              final item = actions[index];
              if (item == null) {
                return const SizedBox.expand();
              }
              return _ChatAttachmentMenuItem(data: item);
            },
          ),
        ),
      ),
    );
  }
}

class _ChatAttachmentMenuItemData {
  const _ChatAttachmentMenuItemData({
    required this.key,
    required this.icon,
    required this.label,
    required this.onTap,
  });

  final Key key;
  final IconData icon;
  final String label;
  final VoidCallback? onTap;
}

class _ChatAttachmentMenuItem extends StatelessWidget {
  const _ChatAttachmentMenuItem({required this.data});

  final _ChatAttachmentMenuItemData data;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final enabled = data.onTap != null;
    final accentColor = theme.primaryColor;

    return Material(
      color: Colors.transparent,
      child: InkWell(
        key: data.key,
        onTap: data.onTap,
        borderRadius: BorderRadius.circular(14),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 4),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            mainAxisSize: MainAxisSize.min,
            children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(
                  color: enabled
                      ? accentColor.withValues(alpha: 0.12)
                      : theme.disabledColor.withValues(alpha: 0.08),
                  borderRadius: BorderRadius.circular(14),
                ),
                alignment: Alignment.center,
                child: Icon(
                  data.icon,
                  size: 24,
                  color: enabled ? accentColor : theme.disabledColor,
                ),
              ),
              const SizedBox(height: 4),
              Text(
                data.label,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                textAlign: TextAlign.center,
                // 格子高度由 childAspectRatio 锁死不随字体缩放，标签关闭系统
                // 字体缩放，避免用户调大字号后内容超出格子高度溢出。
                textScaler: TextScaler.noScaling,
                style: TextStyle(
                  fontSize: 11,
                  height: 1.2,
                  color: enabled
                      ? theme.colorScheme.onSurface
                      : theme.disabledColor,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
