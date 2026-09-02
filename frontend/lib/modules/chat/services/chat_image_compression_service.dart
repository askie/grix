import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_image_compress/flutter_image_compress.dart';
import 'package:path/path.dart' as path;

import 'chat_attachment_limit_policy.dart';
import 'chat_image_codec.dart';

class ChatPreparedImageUpload {
  const ChatPreparedImageUpload({
    required this.bytes,
    required this.fileName,
    required this.contentType,
    required this.compressed,
  });

  final Uint8List bytes;
  final String fileName;
  final String contentType;
  final bool compressed;
}

class ChatImageCompressionRequest {
  const ChatImageCompressionRequest({
    required this.bytes,
    required this.quality,
    required this.targetWidth,
    required this.targetHeight,
  });

  final Uint8List bytes;
  final int quality;
  final int targetWidth;
  final int targetHeight;
}

typedef ChatImageSizeResolver = Future<Size> Function(Uint8List bytes);
typedef ChatImageBytesCompressor =
    Future<Uint8List> Function(ChatImageCompressionRequest request);

class ChatImageCompressionService {
  const ChatImageCompressionService({
    this.maxUploadBytes = ChatAttachmentLimitPolicy.maxImageBytes,
    ChatImageSizeResolver? sizeResolver,
    ChatImageBytesCompressor? bytesCompressor,
  }) : _sizeResolver = sizeResolver ?? _defaultSizeResolver,
       _bytesCompressor = bytesCompressor ?? _defaultBytesCompressor;

  final int maxUploadBytes;
  final ChatImageSizeResolver _sizeResolver;
  final ChatImageBytesCompressor _bytesCompressor;

  static const List<_CompressionProfile> _profiles = <_CompressionProfile>[
    _CompressionProfile(maxLongSide: 2560, quality: 88),
    _CompressionProfile(maxLongSide: 2160, quality: 80),
    _CompressionProfile(maxLongSide: 1920, quality: 72),
    _CompressionProfile(maxLongSide: 1600, quality: 64),
    _CompressionProfile(maxLongSide: 1280, quality: 56),
  ];

  Future<ChatPreparedImageUpload?> prepareForUpload({
    required Uint8List bytes,
    required String fileName,
    required String contentType,
  }) async {
    if (!ChatAttachmentLimitPolicy.shouldCompressImage(bytes.length)) {
      return ChatPreparedImageUpload(
        bytes: bytes,
        fileName: fileName,
        contentType: contentType,
        compressed: false,
      );
    }

    final sourceSize = await _sizeResolver(bytes);
    Uint8List? bestAttempt;

    for (final profile in _profiles) {
      final targetDimensions = _resolveTargetDimensions(
        sourceSize: sourceSize,
        maxLongSide: profile.maxLongSide,
      );
      final compressed = await _bytesCompressor(
        ChatImageCompressionRequest(
          bytes: bytes,
          quality: profile.quality,
          targetWidth: targetDimensions.width,
          targetHeight: targetDimensions.height,
        ),
      );
      if (compressed.isEmpty) {
        continue;
      }
      if (bestAttempt == null || compressed.length < bestAttempt.length) {
        bestAttempt = compressed;
      }
      if (compressed.length <= maxUploadBytes) {
        return ChatPreparedImageUpload(
          bytes: compressed,
          fileName: _resolveCompressedFileName(fileName),
          contentType: 'image/jpeg',
          compressed: true,
        );
      }
    }

    if (bestAttempt != null && bestAttempt.length <= maxUploadBytes) {
      return ChatPreparedImageUpload(
        bytes: bestAttempt,
        fileName: _resolveCompressedFileName(fileName),
        contentType: 'image/jpeg',
        compressed: true,
      );
    }
    return null;
  }

  _TargetDimensions _resolveTargetDimensions({
    required Size sourceSize,
    required int maxLongSide,
  }) {
    final sourceWidth = math.max(1, sourceSize.width.round());
    final sourceHeight = math.max(1, sourceSize.height.round());
    final widthIsLongSide = sourceWidth >= sourceHeight;
    final longSide = widthIsLongSide ? sourceWidth : sourceHeight;

    if (longSide <= maxLongSide) {
      return _TargetDimensions(width: sourceWidth, height: sourceHeight);
    }

    if (widthIsLongSide) {
      final targetWidth = maxLongSide;
      final targetHeight = math.max(
        1,
        (targetWidth * sourceHeight / sourceWidth).round(),
      );
      return _TargetDimensions(width: targetWidth, height: targetHeight);
    }

    final targetHeight = maxLongSide;
    final targetWidth = math.max(
      1,
      (targetHeight * sourceWidth / sourceHeight).round(),
    );
    return _TargetDimensions(width: targetWidth, height: targetHeight);
  }

  static String _resolveCompressedFileName(String fileName) {
    final normalized = fileName.trim();
    if (normalized.isEmpty) {
      return 'chat_image_compressed.jpg';
    }
    return path.setExtension(normalized, '.jpg');
  }

  static Future<Size> _defaultSizeResolver(Uint8List bytes) async {
    final codec = await ui.instantiateImageCodec(bytes);
    try {
      final frame = await codec.getNextFrame();
      try {
        return Size(
          frame.image.width.toDouble(),
          frame.image.height.toDouble(),
        );
      } finally {
        frame.image.dispose();
      }
    } finally {
      codec.dispose();
    }
  }

  static Future<Uint8List> _defaultBytesCompressor(
    ChatImageCompressionRequest request,
  ) {
    if (!_hasPlatformCompressor) {
      return ChatImageCodec.encodeScaledJpeg(
        bytes: request.bytes,
        targetWidth: request.targetWidth,
        targetHeight: request.targetHeight,
        quality: request.quality,
      );
    }
    return FlutterImageCompress.compressWithList(
      request.bytes,
      minWidth: request.targetWidth,
      minHeight: request.targetHeight,
      quality: request.quality,
      format: CompressFormat.jpeg,
      keepExif: false,
    );
  }

  /// flutter_image_compress 只覆盖 Android / iOS / macOS / Web / 鸿蒙，
  /// Windows 与 Linux 上调用会直接抛 UnimplementedError。
  static bool get _hasPlatformCompressor {
    if (kIsWeb) return true;
    return defaultTargetPlatform != TargetPlatform.windows &&
        defaultTargetPlatform != TargetPlatform.linux;
  }
}

class _CompressionProfile {
  const _CompressionProfile({required this.maxLongSide, required this.quality});

  final int maxLongSide;
  final int quality;
}

class _TargetDimensions {
  const _TargetDimensions({required this.width, required this.height});

  final int width;
  final int height;
}
