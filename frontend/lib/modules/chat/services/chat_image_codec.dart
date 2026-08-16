import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:image/image.dart' as img;

/// 不依赖平台插件的图像编解码能力。
///
/// flutter_image_compress 没有 Windows / Linux 实现（调用即抛 UnimplementedError），
/// 这里用引擎解码 + 纯 Dart 编码补上这两个平台，同时把剪贴板拿到的非 PNG 位图
/// （Windows 剪贴板给的是 BMP）统一成 PNG。
class ChatImageCodec {
  const ChatImageCodec._();

  static const List<int> _pngSignature = <int>[0x89, 0x50, 0x4E, 0x47];

  static bool isPng(Uint8List bytes) {
    if (bytes.length < _pngSignature.length) return false;
    for (var i = 0; i < _pngSignature.length; i++) {
      if (bytes[i] != _pngSignature[i]) return false;
    }
    return true;
  }

  /// 把任意受支持的图像字节统一转成 PNG；已经是 PNG 时原样返回。
  static Future<Uint8List> toPng(Uint8List bytes) async {
    if (isPng(bytes)) return bytes;

    final image = await _decode(bytes);
    try {
      final data = await image.toByteData(format: ui.ImageByteFormat.png);
      if (data == null) {
        throw StateError('Failed to encode clipboard image as PNG');
      }
      return data.buffer.asUint8List();
    } finally {
      image.dispose();
    }
  }

  /// 缩放到目标尺寸并编码为 JPEG，输出与其他平台的压缩结果保持同一格式。
  static Future<Uint8List> encodeScaledJpeg({
    required Uint8List bytes,
    required int targetWidth,
    required int targetHeight,
    required int quality,
  }) async {
    final image = await _decode(
      bytes,
      targetWidth: targetWidth,
      targetHeight: targetHeight,
    );
    final Uint8List rgba;
    final int width;
    final int height;
    try {
      final data = await image.toByteData(format: ui.ImageByteFormat.rawRgba);
      if (data == null) return Uint8List(0);
      rgba = data.buffer.asUint8List();
      width = image.width;
      height = image.height;
    } finally {
      image.dispose();
    }

    return compute(
      _encodeJpeg,
      _JpegEncodeJob(
        rgba: rgba,
        width: width,
        height: height,
        quality: quality,
      ),
    );
  }

  static Future<ui.Image> _decode(
    Uint8List bytes, {
    int? targetWidth,
    int? targetHeight,
  }) async {
    final codec = await ui.instantiateImageCodec(
      bytes,
      targetWidth: targetWidth,
      targetHeight: targetHeight,
    );
    try {
      final frame = await codec.getNextFrame();
      return frame.image;
    } finally {
      codec.dispose();
    }
  }
}

class _JpegEncodeJob {
  const _JpegEncodeJob({
    required this.rgba,
    required this.width,
    required this.height,
    required this.quality,
  });

  final Uint8List rgba;
  final int width;
  final int height;
  final int quality;
}

Uint8List _encodeJpeg(_JpegEncodeJob job) {
  final rgba = job.rgba;
  // 引擎给出的是预乘 alpha 的 RGBA，而 JPEG 没有透明通道；先按白底合成，
  // 否则透明区域会被编码成黑块。
  // 钳制到 255：预乘下各通道恒 ≤ alpha、加上补值不会越界，但 Uint8List 溢出是回绕不是饱和，
  // 一旦上游改成 straight RGBA 就会静默出花斑，这里兜住。
  for (var i = 0; i + 3 < rgba.length; i += 4) {
    final alpha = rgba[i + 3];
    if (alpha == 255) continue;
    final complement = 255 - alpha;
    rgba[i] = math.min(255, rgba[i] + complement);
    rgba[i + 1] = math.min(255, rgba[i + 1] + complement);
    rgba[i + 2] = math.min(255, rgba[i + 2] + complement);
    rgba[i + 3] = 255;
  }

  final image = img.Image.fromBytes(
    width: job.width,
    height: job.height,
    bytes: rgba.buffer,
    numChannels: 4,
    order: img.ChannelOrder.rgba,
  );
  return img.encodeJpg(image, quality: job.quality);
}
