import 'package:flutter/material.dart';

import '../markdown/chat_markdown_uri_policy.dart';
import '../models/chat_message_attachment.dart';
import '../utils/user_image_cache_manager.dart';
import 'chat_markdown_image_preview_dialog.dart';
import 'chat_message_video_preview_dialog.dart';

/// 同一气泡内多张图片/视频的全屏滑动查看器：左右滑动在图片、视频间混排切换。
/// 当前页图片放大、或视频正在拖动进度条时临时锁住切页手势，避免和缩放/拖拽打架；
/// 非当前页的视频不自动播放，切走立即暂停，避免多个视频同时占用播放资源。
class ChatMessageMediaSwipeViewer extends StatefulWidget {
  const ChatMessageMediaSwipeViewer({
    super.key,
    required this.attachments,
    required this.initialIndex,
  });

  /// 仅包含图片/视频的附件列表，调用方需预先过滤掉普通文件。
  final List<ChatMessageAttachment> attachments;
  final int initialIndex;

  @override
  State<ChatMessageMediaSwipeViewer> createState() =>
      _ChatMessageMediaSwipeViewerState();
}

class _ChatMessageMediaSwipeViewerState
    extends State<ChatMessageMediaSwipeViewer> {
  late final PageController _pageController;
  late int _currentIndex;
  bool _swipeLocked = false;

  @override
  void initState() {
    super.initState();
    _currentIndex = widget.initialIndex;
    _pageController = PageController(initialPage: widget.initialIndex);
  }

  @override
  void dispose() {
    _pageController.dispose();
    super.dispose();
  }

  void _handlePageChanged(int index) {
    setState(() {
      _currentIndex = index;
      _swipeLocked = false;
    });
  }

  void _setSwipeLocked(int forIndex, bool locked) {
    if (forIndex != _currentIndex || _swipeLocked == locked) {
      return;
    }
    setState(() => _swipeLocked = locked);
  }

  @override
  Widget build(BuildContext context) {
    final total = widget.attachments.length;
    return Stack(
      children: [
        PageView.builder(
          key: const ValueKey('chat_message_media_swipe_viewer_pages'),
          controller: _pageController,
          physics: _swipeLocked
              ? const NeverScrollableScrollPhysics()
              : const PageScrollPhysics(),
          itemCount: total,
          onPageChanged: _handlePageChanged,
          itemBuilder: (context, index) =>
              _buildPage(widget.attachments[index], index),
        ),
        if (total > 1)
          Positioned(
            top: 8,
            left: 0,
            right: 0,
            child: SafeArea(
              bottom: false,
              child: Center(
                child: Container(
                  key: const ValueKey('chat_message_media_swipe_viewer_index'),
                  padding: const EdgeInsets.symmetric(
                    horizontal: 12,
                    vertical: 4,
                  ),
                  decoration: BoxDecoration(
                    color: Colors.black.withValues(alpha: 0.45),
                    borderRadius: BorderRadius.circular(999),
                  ),
                  child: Text(
                    '${_currentIndex + 1}/$total',
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 12,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
              ),
            ),
          ),
      ],
    );
  }

  Widget _buildPage(ChatMessageAttachment attachment, int index) {
    if (attachment.isImage) {
      final uri = ChatMarkdownUriPolicy.resolveSafeImageUri(attachment.url);
      if (uri == null) {
        return const ColoredBox(color: Colors.black);
      }
      return ChatMarkdownImagePreviewDialog(
        imageUri: uri,
        alt: attachment.fileName,
        cacheManager: UserImageCacheManager.current(),
        onZoomStateChanged: (allowSwipe) => _setSwipeLocked(index, !allowSwipe),
      );
    }

    final uri = ChatMarkdownUriPolicy.resolveSafeLinkUri(attachment.url);
    if (uri == null) {
      return const ColoredBox(color: Colors.black);
    }
    return ChatMessageVideoPreviewDialog(
      videoUri: uri,
      title: attachment.fileName,
      autoPlay: index == _currentIndex,
      onScrubbingChanged: (scrubbing) => _setSwipeLocked(index, scrubbing),
    );
  }
}
