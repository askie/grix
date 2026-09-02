import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../app/themes/app_theme.dart';

enum ChatAttachmentSourceAction { camera, gallery }

class ChatAttachmentSourceSheet extends StatelessWidget {
  const ChatAttachmentSourceSheet({super.key});

  static Future<ChatAttachmentSourceAction?> show(BuildContext context) {
    return showModalBottomSheet<ChatAttachmentSourceAction>(
      context: context,
      builder: (_) => const ChatAttachmentSourceSheet(),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          ListTile(
            key: const Key('chat_attachment_source_camera'),
            leading: Icon(
              Icons.photo_camera_outlined,
              color: theme.colorScheme.onSurface,
            ),
            title: Text(
              'chat_attachment_source_capture'.tr,
              style: TextStyle(color: theme.colorScheme.onSurface),
            ),
            onTap: () =>
                Navigator.of(context).pop(ChatAttachmentSourceAction.camera),
          ),
          ListTile(
            key: const Key('chat_attachment_source_gallery'),
            leading: Icon(
              Icons.photo_library_outlined,
              color: theme.colorScheme.onSurface,
            ),
            title: Text(
              'chat_attachment_source_gallery'.tr,
              style: TextStyle(color: theme.colorScheme.onSurface),
            ),
            onTap: () =>
                Navigator.of(context).pop(ChatAttachmentSourceAction.gallery),
          ),
          const Divider(height: 1),
          ListTile(
            leading: const Icon(
              Icons.close_rounded,
              color: AppTheme.errorColor,
            ),
            title: Text(
              'common_cancel'.tr,
              style: const TextStyle(color: AppTheme.errorColor),
            ),
            onTap: () => Navigator.of(context).pop(),
          ),
        ],
      ),
    );
  }
}
