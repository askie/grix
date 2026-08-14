import 'dart:typed_data';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_cache_manager/flutter_cache_manager.dart';

import '../utils/remote_binary_loader.dart';
import '../utils/mermaid_image_exporter.dart';
import '../utils/toast_util.dart';
import 'chat_markdown_image_viewer_scaffold.dart';
import 'transparency_checkerboard.dart';

class ChatMarkdownImagePreviewDialog extends StatelessWidget {
  const ChatMarkdownImagePreviewDialog({
    super.key,
    required this.imageUri,
    this.alt,
    this.cacheManager,
    this.onZoomStateChanged,
  });

  final Uri imageUri;
  final String? alt;
  final BaseCacheManager? cacheManager;
  final ValueChanged<bool>? onZoomStateChanged;

  @override
  Widget build(BuildContext context) {
    return ChatMarkdownImageViewerScaffold(
      saveTooltip: '下载图片',
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
      final message = result.isDownload
          ? '已开始下载: ${result.location}'
          : result.isGallery
              ? '图片已保存到系统相册'
              : '图片已保存: ${result.location}';
      CustomToast.show(message, isError: false);
    } catch (_) {
      CustomToast.show('下载图片失败，请稍后重试');
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
