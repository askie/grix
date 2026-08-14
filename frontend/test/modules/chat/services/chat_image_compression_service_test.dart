import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_attachment_limit_policy.dart';
import 'package:grix/modules/chat/services/chat_image_compression_service.dart';
import 'package:image/image.dart' as img;

void main() {
  test('prepareForUpload keeps image unchanged when already within 5MB',
      () async {
    final sourceBytes = Uint8List(1024);
    final service = ChatImageCompressionService(
      sizeResolver: (_) async => const Size(1200, 800),
      bytesCompressor: (_) async {
        throw StateError('compressor should not be called');
      },
    );

    final result = await service.prepareForUpload(
      bytes: sourceBytes,
      fileName: 'sample.png',
      contentType: 'image/png',
    );

    expect(result, isNotNull);
    expect(result!.compressed, isFalse);
    expect(result.fileName, 'sample.png');
    expect(result.contentType, 'image/png');
    expect(identical(result.bytes, sourceBytes), isTrue);
  });

  test('prepareForUpload compresses oversized image into uploadable jpeg',
      () async {
    final requests = <ChatImageCompressionRequest>[];
    final service = ChatImageCompressionService(
      sizeResolver: (_) async => const Size(3000, 2000),
      bytesCompressor: (request) async {
        requests.add(request);
        if (requests.length == 1) {
          return Uint8List(ChatAttachmentLimitPolicy.maxImageBytes + 64);
        }
        return Uint8List(ChatAttachmentLimitPolicy.maxImageBytes - 128);
      },
    );

    final result = await service.prepareForUpload(
      bytes: Uint8List(ChatAttachmentLimitPolicy.maxImageBytes + 1024),
      fileName: 'edited.png',
      contentType: 'image/png',
    );

    expect(result, isNotNull);
    expect(result!.compressed, isTrue);
    expect(result.fileName, 'edited.jpg');
    expect(result.contentType, 'image/jpeg');
    expect(result.bytes.length,
        lessThanOrEqualTo(ChatAttachmentLimitPolicy.maxImageBytes));
    expect(requests, hasLength(2));
    expect(requests.first.targetWidth, 2560);
    expect(requests.first.targetHeight, 1707);
    expect(requests.first.quality, 88);
    expect(requests[1].targetWidth, 2160);
    expect(requests[1].targetHeight, 1440);
    expect(requests[1].quality, 80);
  });

  test('prepareForUpload returns null when compressed image still exceeds 5MB',
      () async {
    final service = ChatImageCompressionService(
      sizeResolver: (_) async => const Size(4000, 3000),
      bytesCompressor: (_) async =>
          Uint8List(ChatAttachmentLimitPolicy.maxImageBytes + 512),
    );

    final result = await service.prepareForUpload(
      bytes: Uint8List(ChatAttachmentLimitPolicy.maxImageBytes + 2048),
      fileName: 'oversized.png',
      contentType: 'image/png',
    );

    expect(result, isNull);
  });

  // Windows / Linux 没有 flutter_image_compress 实现，之前默认压缩器会直接抛
  // UnimplementedError，导致粘贴的截图（BMP，一张 1080p 就超过 5MB）静默丢失。
  for (final platform in <TargetPlatform>[
    TargetPlatform.windows,
    TargetPlatform.linux,
  ]) {
    test('$platform 上默认压缩器把超限的剪贴板截图压成可上传的 jpeg', () async {
      debugDefaultTargetPlatformOverride = platform;
      addTearDown(() => debugDefaultTargetPlatformOverride = null);

      final oversizedBitmap = _buildBmp(width: 1920, height: 1080);
      expect(
        oversizedBitmap.length,
        greaterThan(ChatAttachmentLimitPolicy.maxImageBytes),
      );

      const service = ChatImageCompressionService();
      final result = await service.prepareForUpload(
        bytes: oversizedBitmap,
        fileName: 'clipboard_1.png',
        contentType: 'image/png',
      );

      expect(result, isNotNull);
      expect(result!.compressed, isTrue);
      expect(result.contentType, 'image/jpeg');
      expect(result.fileName, 'clipboard_1.jpg');
      expect(
        result.bytes.length,
        lessThanOrEqualTo(ChatAttachmentLimitPolicy.maxImageBytes),
      );
    });
  }
}

/// 模拟 Windows 剪贴板给出的未压缩位图。
Uint8List _buildBmp({required int width, required int height}) {
  final image = img.Image(width: width, height: height);
  for (var y = 0; y < height; y++) {
    for (var x = 0; x < width; x++) {
      image.setPixelRgb(x, y, x % 256, y % 256, (x + y) % 256);
    }
  }
  return img.encodeBmp(image);
}
