import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../markdown/chat_markdown_uri_policy.dart';
import 'app_dialog_style.dart';
import 'chat_message_video_preview_dialog.dart';
import 'chat_video_card_body.dart';

/// Renders a `<video>` Markdown node as a tappable card. Tapping opens the
/// shared fullscreen [ChatMessageVideoPreviewDialog] for playback, mirroring
/// the image preview interaction.
class ChatMarkdownVideoView extends StatelessWidget {
  const ChatMarkdownVideoView({
    super.key,
    required this.src,
    this.width,
    this.poster,
    this.inline = false,
  });

  final String src;
  final double? width;
  final String? poster;
  final bool inline;

  static const double _defaultWidth = 280;
  static const double _aspectRatio = 16 / 9;

  @override
  Widget build(BuildContext context) {
    if (src.isEmpty) {
      return const SizedBox.shrink();
    }
    final safeUri = ChatMarkdownUriPolicy.resolveSafeVideoUri(src);
    if (safeUri == null) {
      return Text(src);
    }

    final cardWidth = (width ?? _defaultWidth).clamp(120.0, 640.0);
    final cardHeight = cardWidth / _aspectRatio;

    final coverWidget = (poster != null && poster!.isNotEmpty)
        ? Image.network(
            poster!,
            fit: BoxFit.cover,
            errorBuilder: (_, __, ___) =>
                const ColoredBox(color: Color(0xFF20242C)),
          )
        : const ColoredBox(color: Color(0xFF20242C));

    return Semantics(
      button: true,
      label: 'chat_export_play_video'.tr,
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        child: GestureDetector(
          onTap: () => _openPlayer(context, safeUri),
          child: ClipRRect(
            borderRadius: BorderRadius.circular(8),
            child: SizedBox(
              width: cardWidth,
              height: cardHeight,
              child: ChatVideoCardBody(cover: coverWidget),
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _openPlayer(BuildContext context, Uri safeUri) {
    return showAppDialog<void>(
      context: context,
      useSafeArea: false,
      barrierColor: Colors.black.withValues(alpha: 0.92),
      builder: (dialogContext) =>
          ChatMessageVideoPreviewDialog(videoUri: safeUri),
    );
  }
}
