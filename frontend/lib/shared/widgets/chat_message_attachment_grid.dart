import 'dart:math' as math;

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:video_player/video_player.dart';

import '../../modules/text_document/services/text_document_open_service.dart';
import '../markdown/chat_markdown_uri_policy.dart';
import '../models/chat_message_attachment.dart';
import '../utils/app_external_links.dart';
import '../utils/local_tailnet_proxy.dart';
import '../utils/toast_util.dart';
import '../utils/user_image_cache_manager.dart';
import 'app_dialog_style.dart';
import 'chat_message_media_swipe_viewer.dart';
import 'chat_video_card_body.dart';

class ChatMessageAttachmentGrid extends StatelessWidget {
  static const int _maxVisibleAttachments = 9;

  const ChatMessageAttachmentGrid({
    super.key,
    required this.attachments,
    this.limitToNine = true,
  });

  final List<ChatMessageAttachment> attachments;
  final bool limitToNine;

  @override
  Widget build(BuildContext context) {
    if (attachments.isEmpty) {
      return const SizedBox.shrink();
    }
    final viewportWidth = MediaQuery.sizeOf(context).width;

    final hiddenCount = limitToNine
        ? attachments.length - _maxVisibleAttachments
        : 0;
    final visibleAttachments = hiddenCount > 0
        ? attachments.take(_maxVisibleAttachments).toList(growable: false)
        : attachments;

    // 图片/视频才能进滑动查看器；普通文件保持原样点击外部打开。
    // 用完整 attachments（而非截断后的 visibleAttachments）算，
    // 这样九宫格里能点到的图片/视频，滑动查看器里也能继续滑到被折叠的其余项。
    final mediaAttachments = attachments
        .where((a) => a.isImage || a.isVideo)
        .toList(growable: false);
    final mediaIndexByOriginalIndex = <int, int>{};
    var mediaCursor = 0;
    for (var i = 0; i < attachments.length; i++) {
      if (attachments[i].isImage || attachments[i].isVideo) {
        mediaIndexByOriginalIndex[i] = mediaCursor;
        mediaCursor++;
      }
    }

    return LayoutBuilder(
      builder: (context, constraints) {
        final crossAxisCount = switch (visibleAttachments.length) {
          1 => 1,
          2 => 2,
          _ => 3,
        };
        const spacing = 6.0;
        final availableWidth = constraints.maxWidth.isFinite
            ? constraints.maxWidth
            : viewportWidth * 0.8;
        final tileWidth =
            (availableWidth - spacing * (crossAxisCount - 1)) / crossAxisCount;
        final isSinglePlainFile =
            visibleAttachments.length == 1 &&
            !visibleAttachments.first.isImage &&
            !visibleAttachments.first.isVideo;
        final mainAxisExtent = isSinglePlainFile
            ? 80.0
            : visibleAttachments.length == 1
            ? math.min(tileWidth / 1.35, 220.0)
            : math.min(tileWidth, 120.0);

        return GridView.builder(
          key: const Key('chat_message_attachment_grid'),
          shrinkWrap: true,
          padding: EdgeInsets.zero,
          physics: const NeverScrollableScrollPhysics(),
          itemCount: visibleAttachments.length,
          gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: crossAxisCount,
            mainAxisSpacing: spacing,
            crossAxisSpacing: spacing,
            mainAxisExtent: mainAxisExtent,
          ),
          itemBuilder: (context, index) {
            final attachment = visibleAttachments[index];
            final isOverflowTile =
                hiddenCount > 0 && index == visibleAttachments.length - 1;
            return _AttachmentTile(
              key: Key('chat_message_attachment_tile_$index'),
              attachment: attachment,
              mediaAttachments: mediaAttachments,
              mediaIndex: mediaIndexByOriginalIndex[index],
              overflowCount: isOverflowTile ? hiddenCount : 0,
              onTapOverflow: isOverflowTile
                  ? () => _openAllAttachmentsDialog(context)
                  : null,
            );
          },
        );
      },
    );
  }

  Future<void> _openAllAttachmentsDialog(BuildContext context) {
    return showAppDialog<void>(
      context: context,
      builder: (dialogContext) {
        return Dialog(
          insetPadding: const EdgeInsets.symmetric(
            horizontal: 20,
            vertical: 24,
          ),
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 560, maxHeight: 720),
            child: Column(
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 12, 8, 8),
                  child: Row(
                    children: [
                      Expanded(
                        child: Text(
                          'chat_attachment_all'.tr,
                          style: Theme.of(dialogContext).textTheme.titleMedium,
                        ),
                      ),
                      IconButton(
                        tooltip: 'chat_attachment_close_panel'.tr,
                        onPressed: () => Navigator.of(dialogContext).pop(),
                        icon: const Icon(Icons.close_rounded),
                      ),
                    ],
                  ),
                ),
                const Divider(height: 1),
                Expanded(
                  child: SingleChildScrollView(
                    padding: const EdgeInsets.all(16),
                    child: ChatMessageAttachmentGrid(
                      attachments: attachments,
                      limitToNine: false,
                    ),
                  ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }
}

class _AttachmentTile extends StatelessWidget {
  const _AttachmentTile({
    super.key,
    required this.attachment,
    required this.mediaAttachments,
    required this.mediaIndex,
    this.overflowCount = 0,
    this.onTapOverflow,
  });

  final ChatMessageAttachment attachment;

  /// 同一消息内所有图片/视频（不含普通文件），用于滑动查看器的完整数据源。
  final List<ChatMessageAttachment> mediaAttachments;

  /// 当前 attachment 在 [mediaAttachments] 中的下标；普通文件为 null。
  final int? mediaIndex;
  final int overflowCount;
  final VoidCallback? onTapOverflow;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: BorderRadius.circular(10),
        onTap: overflowCount > 0
            ? onTapOverflow
            : () => _openAttachment(context),
        child: Ink(
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(10),
            color: attachment.isImage
                ? Colors.transparent
                : const Color(0xFFF2F3F5),
            border: Border.all(color: const Color(0xFFE2E5E9)),
          ),
          child: ClipRRect(
            borderRadius: BorderRadius.circular(10),
            child: attachment.isImage
                ? _buildImageTile()
                : attachment.isVideo
                ? _buildVideoTile()
                : _buildFileTile(),
          ),
        ),
      ),
    );
  }

  Widget _buildImageTile() {
    final uri = ChatMarkdownUriPolicy.resolveSafeImageUri(attachment.url);
    if (uri == null) {
      return _buildOverflowWrapper(
        child: _buildFallbackTile(
          icon: Icons.broken_image_outlined,
          label: attachment.fileName,
        ),
      );
    }

    final safeUrl = uri.toString();
    final cacheManager = UserImageCacheManager.current();
    if (cacheManager == null) {
      return _buildOverflowWrapper(
        child: Image.network(
          safeUrl,
          fit: BoxFit.contain,
          errorBuilder: (_, __, ___) => _buildFallbackTile(
            icon: Icons.broken_image_outlined,
            label: attachment.fileName,
          ),
          loadingBuilder: (context, child, progress) {
            if (progress == null) {
              return child;
            }
            return _buildFallbackTile(
              icon: Icons.image_outlined,
              label: attachment.fileName,
            );
          },
        ),
      );
    }

    return _buildOverflowWrapper(
      child: CachedNetworkImage(
        imageUrl: safeUrl,
        cacheManager: cacheManager,
        fit: BoxFit.contain,
        placeholder: (_, __) => _buildFallbackTile(
          icon: Icons.image_outlined,
          label: attachment.fileName,
        ),
        errorWidget: (_, __, ___) => _buildFallbackTile(
          icon: Icons.broken_image_outlined,
          label: attachment.fileName,
        ),
      ),
    );
  }

  Widget _buildVideoTile() {
    final uri = ChatMarkdownUriPolicy.resolveSafeLinkUri(attachment.url);
    return _buildOverflowWrapper(
      child: _VideoThumbnail(
        videoUri: uri,
        fallback: _buildFallbackTile(
          icon: Icons.videocam_outlined,
          label: attachment.fileName.trim().isEmpty
              ? attachment.type
              : attachment.fileName.trim(),
        ),
      ),
    );
  }

  Widget _buildFileTile() {
    final label = attachment.fileName.trim().isEmpty
        ? attachment.type
        : attachment.fileName.trim();

    return _buildOverflowWrapper(
      child: _buildFallbackTile(
        icon: Icons.insert_drive_file_outlined,
        label: label,
      ),
    );
  }

  Widget _buildOverflowWrapper({required Widget child}) {
    if (overflowCount <= 0) {
      return child;
    }
    return Stack(
      fit: StackFit.expand,
      children: [
        child,
        Container(color: Colors.black.withValues(alpha: 0.5)),
        Center(
          child: Text(
            '+$overflowCount',
            style: const TextStyle(
              color: Colors.white,
              fontSize: 24,
              fontWeight: FontWeight.w700,
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildFallbackTile({required IconData icon, required String label}) {
    return Container(
      color: attachment.isVideo
          ? const Color(0xFF20242C)
          : const Color(0xFFF2F3F5),
      padding: const EdgeInsets.all(10),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            icon,
            size: 30,
            color: attachment.isVideo
                ? Colors.white70
                : const Color(0xFF5A6472),
          ),
          const SizedBox(height: 8),
          Text(
            label,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            textAlign: TextAlign.center,
            style: TextStyle(
              fontSize: 12,
              height: 1.3,
              color: attachment.isVideo
                  ? Colors.white
                  : const Color(0xFF2C3440),
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _openAttachment(BuildContext context) async {
    final index = mediaIndex;
    if (index != null) {
      await showAppDialog<void>(
        context: context,
        useSafeArea: false,
        barrierColor: Colors.black.withValues(alpha: 0.92),
        builder: (dialogContext) => ChatMessageMediaSwipeViewer(
          attachments: mediaAttachments,
          initialIndex: index,
        ),
      );
      return;
    }

    final uri = ChatMarkdownUriPolicy.resolveSafeLinkUri(attachment.url);
    if (uri == null) {
      return;
    }
    if (TextDocumentOpenService.supportsAttachment(attachment)) {
      try {
        await TextDocumentOpenService.openRemoteAttachment(attachment);
      } catch (_) {
        if (!context.mounted) return;
        CustomToast.show('Unable to open this text file', isError: true);
      }
      return;
    }
    await AppExternalLinks.open(uri.toString());
  }
}

/// Renders the first frame of a video attachment as a static poster with a
/// play overlay. Falls back to [fallback] while loading or on failure, so the
/// tile always fills its slot instead of collapsing to a flat bar.
class _VideoThumbnail extends StatefulWidget {
  const _VideoThumbnail({required this.videoUri, required this.fallback});

  final Uri? videoUri;
  final Widget fallback;

  @override
  State<_VideoThumbnail> createState() => _VideoThumbnailState();
}

class _VideoThumbnailState extends State<_VideoThumbnail> {
  VideoPlayerController? _controller;
  bool _ready = false;
  bool _failed = false;

  @override
  void initState() {
    super.initState();
    _initialize();
  }

  Future<void> _initialize() async {
    final uri = widget.videoUri;
    if (uri == null) {
      setState(() => _failed = true);
      return;
    }
    // tailnet 自签 HTTPS 媒体 → 改写成本机 loopback 反代地址，
    // 让原生播放器栈走明文 http，证书校验交回 dart:io HttpClient 完成。
    final playbackUri = await rewriteTailnetMediaUrl(uri);
    if (!mounted) return;
    final controller = VideoPlayerController.networkUrl(playbackUri);
    _controller = controller;
    try {
      await controller.initialize();
      await controller.setVolume(0);
      await controller.seekTo(Duration.zero);
      if (!mounted) {
        await controller.dispose();
        return;
      }
      setState(() => _ready = true);
    } catch (_) {
      if (!mounted) {
        await controller.dispose();
        return;
      }
      setState(() => _failed = true);
    }
  }

  @override
  void dispose() {
    _controller?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final controller = _controller;
    final showFrame = _ready && !_failed && controller != null;

    final coverWidget = showFrame
        ? ClipRect(
            child: FittedBox(
              fit: BoxFit.cover,
              clipBehavior: Clip.hardEdge,
              child: SizedBox(
                width: controller.value.size.width,
                height: controller.value.size.height,
                child: VideoPlayer(controller),
              ),
            ),
          )
        : widget.fallback;

    return ChatVideoCardBody(cover: coverWidget);
  }
}
