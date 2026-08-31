import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_cache_manager/flutter_cache_manager.dart';

import '../markdown/chat_markdown_uri_policy.dart';
import '../utils/chat_image_dimension_cache.dart';
import '../utils/user_image_cache_manager.dart';
import 'app_dialog_style.dart';
import 'chat_markdown_image_preview_dialog.dart';
import 'chat_markdown_image_preview_scope.dart';

class ChatMarkdownImageView extends StatefulWidget {
  const ChatMarkdownImageView({
    super.key,
    required this.src,
    this.alt,
    this.inline = false,
    this.previewIndex,
  });

  final String src;
  final String? alt;
  final bool inline;
  final int? previewIndex;

  @override
  State<ChatMarkdownImageView> createState() => _ChatMarkdownImageViewState();
}

class _ChatMarkdownImageViewState extends State<ChatMarkdownImageView> {
  ImageStream? _dimensionStream;
  ImageStreamListener? _dimensionListener;
  String _resolvedDimensionSrc = '';

  @override
  void didUpdateWidget(ChatMarkdownImageView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.src != widget.src) {
      _stopDimensionResolve();
    }
  }

  @override
  void dispose() {
    _stopDimensionResolve();
    super.dispose();
  }

  /// Rides the same image stream the visible widget resolves (identical
  /// provider key), so this costs no extra fetch or decode.
  void _resolveDimensionsIfNeeded(
    String safeSrc,
    BaseCacheManager? cacheManager,
  ) {
    if (_resolvedDimensionSrc == safeSrc) {
      return;
    }
    if (ChatImageDimensionCache.lookup(safeSrc) != null) {
      _resolvedDimensionSrc = safeSrc;
      return;
    }
    _stopDimensionResolve();
    _resolvedDimensionSrc = safeSrc;
    final ImageProvider provider = cacheManager == null
        ? NetworkImage(safeSrc)
        : CachedNetworkImageProvider(safeSrc, cacheManager: cacheManager);
    final listener = ImageStreamListener((imageInfo, synchronousCall) {
      ChatImageDimensionCache.store(
        safeSrc,
        Size(
          imageInfo.image.width.toDouble(),
          imageInfo.image.height.toDouble(),
        ),
      );
      imageInfo.dispose();
      if (mounted && !synchronousCall) {
        setState(() {});
      }
    }, onError: (error, stackTrace) {});
    final stream = provider.resolve(ImageConfiguration.empty);
    _dimensionStream = stream;
    _dimensionListener = listener;
    stream.addListener(listener);
  }

  void _stopDimensionResolve() {
    final stream = _dimensionStream;
    final listener = _dimensionListener;
    if (stream != null && listener != null) {
      stream.removeListener(listener);
    }
    _dimensionStream = null;
    _dimensionListener = null;
    _resolvedDimensionSrc = '';
  }

  @override
  Widget build(BuildContext context) {
    final src = widget.src;
    final alt = widget.alt;
    final inline = widget.inline;
    if (src.isEmpty) {
      return Text(alt ?? '');
    }
    final safeUri = ChatMarkdownUriPolicy.resolveSafeImageUri(src);
    if (safeUri == null) {
      final fallbackText = (alt != null && alt.isNotEmpty) ? alt : src;
      return Text(fallbackText);
    }

    final placeholderHeight = inline ? 96.0 : 150.0;
    final maxHeight = inline ? 120.0 : 280.0;
    final safeSrc = safeUri.toString();
    final cacheManager = UserImageCacheManager.current();
    _resolveDimensionsIfNeeded(safeSrc, cacheManager);
    final previewItems =
        ChatMarkdownImagePreviewScope.maybeOf(context)?.items ??
        const <ChatMarkdownImagePreviewItem>[];
    final previewIndex = widget.previewIndex;
    final hasPreviewItem =
        previewIndex != null &&
        previewIndex >= 0 &&
        previewIndex < previewItems.length;

    final image = ClipRRect(
      borderRadius: BorderRadius.circular(8),
      child: cacheManager == null
          ? Image.network(
              safeSrc,
              fit: BoxFit.contain,
              frameBuilder: (context, child, frame, wasSynchronouslyLoaded) {
                if (wasSynchronouslyLoaded || frame != null) {
                  return child;
                }
                return _buildPlaceholder(
                  height: placeholderHeight,
                  icon: Icons.image_outlined,
                );
              },
              errorBuilder: (context, error, stackTrace) => _buildPlaceholder(
                height: placeholderHeight,
                icon: Icons.broken_image_outlined,
              ),
            )
          : CachedNetworkImage(
              imageUrl: safeSrc,
              cacheManager: cacheManager,
              placeholder: (context, url) => _buildPlaceholder(
                height: placeholderHeight,
                icon: Icons.image_outlined,
              ),
              errorWidget: (context, url, error) => _buildPlaceholder(
                height: placeholderHeight,
                icon: Icons.broken_image_outlined,
              ),
              fit: BoxFit.contain,
            ),
    );

    // With a known intrinsic size, reserve the exact final layout box up
    // front so placeholder -> image never changes the bubble height.
    // The reservation must be expressed with constraint widgets, not a
    // LayoutBuilder: markdown tables size their columns with
    // IntrinsicColumnWidth, and a LayoutBuilder inside a table cell makes
    // Table's intrinsic-width pass throw ("LayoutBuilder does not support
    // returning intrinsic dimensions").
    final intrinsic = ChatImageDimensionCache.lookup(safeSrc);
    final content = (intrinsic == null || intrinsic.isEmpty)
        ? image
        : ConstrainedBox(
            // Never upscale past the intrinsic width, and keep a finite
            // width when the parent hands down unbounded constraints.
            constraints: BoxConstraints(maxWidth: intrinsic.width),
            child: AspectRatio(
              aspectRatio: intrinsic.width / intrinsic.height,
              // Carries the intrinsic width up to Table's IntrinsicColumnWidth
              // pass; layout itself is driven by the tight constraints
              // AspectRatio hands down, so the box never upscales.
              child: SizedBox(
                width: intrinsic.width,
                height: intrinsic.height,
                child: image,
              ),
            ),
          );

    return Semantics(
      image: true,
      button: true,
      label: (alt != null && alt.isNotEmpty) ? alt : null,
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        child: GestureDetector(
          onTap: () => _openPreview(
            context,
            safeUri: safeUri,
            cacheManager: cacheManager,
            previewItems: hasPreviewItem
                ? previewItems
                : const <ChatMarkdownImagePreviewItem>[],
            previewIndex: hasPreviewItem ? previewIndex : 0,
          ),
          child: ConstrainedBox(
            constraints: BoxConstraints(maxHeight: maxHeight),
            child: content,
          ),
        ),
      ),
    );
  }

  Future<void> _openPreview(
    BuildContext context, {
    required Uri safeUri,
    required BaseCacheManager? cacheManager,
    required List<ChatMarkdownImagePreviewItem> previewItems,
    required int previewIndex,
  }) {
    return showAppDialog<void>(
      context: context,
      useSafeArea: false,
      barrierColor: Colors.black.withValues(alpha: 0.92),
      builder: (dialogContext) => ChatMarkdownImagePreviewDialog(
        imageUri: safeUri,
        alt: widget.alt,
        cacheManager: cacheManager,
        galleryItems: previewItems,
        initialIndex: previewIndex,
      ),
    );
  }

  Widget _buildPlaceholder({required double height, required IconData icon}) {
    return Container(
      height: height,
      width: widget.inline ? 120 : double.infinity,
      color: const Color(0x11000000),
      alignment: Alignment.center,
      child: Icon(icon, color: Colors.grey, size: 28),
    );
  }
}
