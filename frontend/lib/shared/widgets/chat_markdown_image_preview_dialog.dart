import 'dart:typed_data';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_cache_manager/flutter_cache_manager.dart';
import 'package:get/get.dart';

import '../utils/remote_binary_loader.dart';
import '../utils/mermaid_image_exporter.dart';
import '../utils/toast_util.dart';
import 'chat_markdown_image_preview_scope.dart';
import 'chat_markdown_image_viewer_scaffold.dart';
import 'transparency_checkerboard.dart';

class ChatMarkdownImagePreviewDialog extends StatelessWidget {
  const ChatMarkdownImagePreviewDialog({
    super.key,
    required this.imageUri,
    this.alt,
    this.cacheManager,
    this.onZoomStateChanged,
    this.galleryItems = const <ChatMarkdownImagePreviewItem>[],
    this.initialIndex = 0,
  });

  final Uri imageUri;
  final String? alt;
  final BaseCacheManager? cacheManager;
  final ValueChanged<bool>? onZoomStateChanged;
  final List<ChatMarkdownImagePreviewItem> galleryItems;
  final int initialIndex;

  @override
  Widget build(BuildContext context) {
    if (galleryItems.length > 1) {
      return _ChatMarkdownImageGallery(
        items: galleryItems,
        initialIndex: initialIndex,
        cacheManager: cacheManager,
      );
    }
    return ChatMarkdownImageViewerScaffold(
      saveTooltip: 'chat_export_download_image'.tr,
      onSave: _saveImage,
      onZoomStateChanged: onZoomStateChanged,
      child: _buildImage(),
    );
  }

  Widget _buildImage() {
    final url = imageUri.toString();
    final ImageProvider provider = cacheManager == null
        ? NetworkImage(url)
        : CachedNetworkImageProvider(url, cacheManager: cacheManager);

    return CheckerboardBackedImage(
      image: provider,
      loadingBuilder: (_) => _buildLoadingState(),
      errorBuilder: (_) => _buildErrorState(),
    );
  }

  Widget _buildLoadingState() {
    return const SizedBox(
      width: 36,
      height: 36,
      child: CircularProgressIndicator(
        strokeWidth: 2.4,
        valueColor: AlwaysStoppedAnimation<Color>(Colors.white70),
      ),
    );
  }

  Widget _buildErrorState() {
    final altText = alt?.trim() ?? '';
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        const Icon(
          Icons.broken_image_outlined,
          color: Colors.white70,
          size: 38,
        ),
        if (altText.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: 10),
            child: Text(
              altText,
              style: const TextStyle(color: Colors.white70),
              textAlign: TextAlign.center,
            ),
          ),
      ],
    );
  }

  Future<void> _saveImage() async {
    try {
      final bytes = await _loadImageBytes();
      final fileName = _resolveFileName(imageUri);
      final result = await exportMermaidPng(bytes, fileName: fileName);
      CustomToast.show(
        localizedExportResultMessage(
          isDownload: result.isDownload,
          isGallery: result.isGallery,
          location: result.location,
          kindKey: 'chat_export_kind_image',
        ),
        isError: false,
      );
    } catch (_) {
      CustomToast.show(
        'chat_export_download_failed'.trParams({
          'kind': 'chat_export_kind_image'.tr,
        }),
      );
    }
  }

  Future<Uint8List> _loadImageBytes() async {
    final url = imageUri.toString();

    if (cacheManager != null) {
      try {
        final file = await cacheManager!.getSingleFile(url);
        return await file.readAsBytes();
      } catch (_) {
        // Fallback to direct network fetch when cache retrieval fails.
      }
    }

    return RemoteBinaryLoader.load(imageUri);
  }

  String _resolveFileName(Uri uri) {
    final rawName = uri.pathSegments.isNotEmpty ? uri.pathSegments.last : '';
    final decoded = _decodeFileName(rawName);
    final sanitized = decoded.replaceAll(RegExp(r'[^A-Za-z0-9._-]'), '_');
    final fallback = 'chat_image_${DateTime.now().millisecondsSinceEpoch}.png';
    if (sanitized.isEmpty) {
      return fallback;
    }
    if (sanitized.contains('.')) {
      return sanitized;
    }
    return '$sanitized.png';
  }

  String _decodeFileName(String rawName) {
    if (rawName.isEmpty) {
      return rawName;
    }
    try {
      return Uri.decodeComponent(rawName);
    } catch (_) {
      return rawName;
    }
  }
}

class _ChatMarkdownImageGallery extends StatefulWidget {
  const _ChatMarkdownImageGallery({
    required this.items,
    required this.initialIndex,
    required this.cacheManager,
  });

  final List<ChatMarkdownImagePreviewItem> items;
  final int initialIndex;
  final BaseCacheManager? cacheManager;

  @override
  State<_ChatMarkdownImageGallery> createState() =>
      _ChatMarkdownImageGalleryState();
}

class _ChatMarkdownImageGalleryState extends State<_ChatMarkdownImageGallery> {
  late final PageController _pageController;
  late int _currentIndex;
  bool _swipeLocked = false;

  @override
  void initState() {
    super.initState();
    _currentIndex = widget.initialIndex
        .clamp(0, widget.items.length - 1)
        .toInt();
    _pageController = PageController(initialPage: _currentIndex);
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

  void _setSwipeLocked(int index, bool locked) {
    if (index != _currentIndex || locked == _swipeLocked) {
      return;
    }
    setState(() => _swipeLocked = locked);
  }

  @override
  Widget build(BuildContext context) {
    return Stack(
      children: [
        PageView.builder(
          key: const ValueKey('markdown_image_preview_gallery_pages'),
          controller: _pageController,
          physics: _swipeLocked
              ? const NeverScrollableScrollPhysics()
              : const PageScrollPhysics(),
          itemCount: widget.items.length,
          onPageChanged: _handlePageChanged,
          itemBuilder: (context, index) {
            final item = widget.items[index];
            return ChatMarkdownImagePreviewDialog(
              imageUri: item.imageUri,
              alt: item.alt,
              cacheManager: widget.cacheManager,
              onZoomStateChanged: (allowSwipe) =>
                  _setSwipeLocked(index, !allowSwipe),
            );
          },
        ),
        Positioned(
          left: 0,
          right: 0,
          bottom: 24,
          child: SafeArea(
            top: false,
            child: Center(
              child: Container(
                key: const ValueKey('markdown_image_preview_gallery_index'),
                padding: const EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 4,
                ),
                decoration: BoxDecoration(
                  color: Colors.black.withValues(alpha: 0.45),
                  borderRadius: BorderRadius.circular(999),
                ),
                child: Text(
                  '${_currentIndex + 1}/${widget.items.length}',
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
}
