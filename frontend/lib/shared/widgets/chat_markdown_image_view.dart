import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_cache_manager/flutter_cache_manager.dart';

import '../markdown/chat_markdown_uri_policy.dart';
import '../utils/user_image_cache_manager.dart';
import 'app_dialog_style.dart';
import 'chat_markdown_image_preview_dialog.dart';

class ChatMarkdownImageView extends StatelessWidget {
  const ChatMarkdownImageView({
    super.key,
    required this.src,
    this.alt,
    this.inline = false,
  });

  final String src;
  final String? alt;
  final bool inline;

  @override
  Widget build(BuildContext context) {
    if (src.isEmpty) {
      return Text(alt ?? '');
    }
    final safeUri = ChatMarkdownUriPolicy.resolveSafeImageUri(src);
    if (safeUri == null) {
      final fallbackText = (alt != null && alt!.isNotEmpty) ? alt! : src;
      return Text(fallbackText);
    }

    final height = inline ? 96.0 : 150.0;
    final safeSrc = safeUri.toString();
    final cacheManager = UserImageCacheManager.current();
    final content = Stack(
      clipBehavior: Clip.none,
      children: [
        ClipRRect(
          borderRadius: BorderRadius.circular(8),
          child: cacheManager == null
              ? Image.network(
                  safeSrc,
                  fit: BoxFit.contain,
                  frameBuilder:
                      (context, child, frame, wasSynchronouslyLoaded) {
                        if (wasSynchronouslyLoaded || frame != null) {
                          return child;
                        }
                        return _buildPlaceholder(
                          height: height,
                          icon: Icons.image_outlined,
                        );
                      },
                  errorBuilder: (context, error, stackTrace) =>
                      _buildPlaceholder(
                        height: height,
                        icon: Icons.broken_image_outlined,
                      ),
                )
              : CachedNetworkImage(
                  imageUrl: safeSrc,
                  cacheManager: cacheManager,
                  placeholder: (context, url) => _buildPlaceholder(
                    height: height,
                    icon: Icons.image_outlined,
                  ),
                  errorWidget: (context, url, error) => _buildPlaceholder(
                    height: height,
                    icon: Icons.broken_image_outlined,
                  ),
                  fit: BoxFit.contain,
                ),
        ),
      ],
    );

    return Semantics(
      image: true,
      button: true,
      label: (alt != null && alt!.isNotEmpty) ? alt : null,
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        child: GestureDetector(
          onTap: () => _openPreview(
            context,
            safeUri: safeUri,
            cacheManager: cacheManager,
          ),
          child: ConstrainedBox(
            constraints: BoxConstraints(maxHeight: inline ? 120 : 280),
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
  }) {
    return showAppDialog<void>(
      context: context,
      useSafeArea: false,
      barrierColor: Colors.black.withValues(alpha: 0.92),
      builder: (dialogContext) => ChatMarkdownImagePreviewDialog(
        imageUri: safeUri,
        alt: alt,
        cacheManager: cacheManager,
      ),
    );
  }

  Widget _buildPlaceholder({required double height, required IconData icon}) {
    return Container(
      height: height,
      width: inline ? 120 : double.infinity,
      color: const Color(0x11000000),
      alignment: Alignment.center,
      child: Icon(icon, color: Colors.grey, size: 28),
    );
  }
}
