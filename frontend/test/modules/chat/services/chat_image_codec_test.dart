import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_image_codec.dart';
import 'package:image/image.dart' as img;

/// 造一张有内容的位图，模拟 Windows 剪贴板给出的未压缩 BMP。
Uint8List _buildBmp({required int width, required int height}) {
  final image = img.Image(width: width, height: height);
  for (var y = 0; y < height; y++) {
    for (var x = 0; x < width; x++) {
      image.setPixelRgb(x, y, x % 256, y % 256, (x + y) % 256);
    }
  }
  return img.encodeBmp(image);
}

Uint8List _buildPng({required int width, required int height}) {
  final image = img.Image(width: width, height: height);
  for (var y = 0; y < height; y++) {
    for (var x = 0; x < width; x++) {
      image.setPixelRgb(x, y, x % 256, y % 256, (x + y) % 256);
    }
  }
  return img.encodePng(image);
}

Future<ui.Image> _decode(Uint8List bytes) async {
  final codec = await ui.instantiateImageCodec(bytes);
  final frame = await codec.getNextFrame();
  codec.dispose();
  return frame.image;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('isPng 只认 PNG 魔数', () {
    expect(ChatImageCodec.isPng(_buildPng(width: 4, height: 4)), isTrue);
    expect(ChatImageCodec.isPng(_buildBmp(width: 4, height: 4)), isFalse);
    expect(ChatImageCodec.isPng(Uint8List(0)), isFalse);
  });

  test('toPng 把 Windows 剪贴板的 BMP 转成 PNG 且保留尺寸', () async {
    final bmp = _buildBmp(width: 120, height: 80);
    expect(ChatImageCodec.isPng(bmp), isFalse);

    final png = await ChatImageCodec.toPng(bmp);

    expect(ChatImageCodec.isPng(png), isTrue);
    final decoded = await _decode(png);
    expect(decoded.width, 120);
    expect(decoded.height, 80);
    decoded.dispose();
  });

  test('toPng 对已经是 PNG 的字节原样返回', () async {
    final png = _buildPng(width: 10, height: 10);
    final result = await ChatImageCodec.toPng(png);
    expect(identical(result, png), isTrue);
  });

  test('BMP 转 PNG 后体积大幅下降（未压缩位图是粘贴失败的根因）', () async {
    // 1080p 位图约 8MB，超过 5MB 上传门槛；转 PNG 后应远低于门槛。
    final bmp = _buildBmp(width: 1920, height: 1080);
    expect(bmp.length, greaterThan(5 * 1024 * 1024));

    final png = await ChatImageCodec.toPng(bmp);
    expect(png.length, lessThan(bmp.length));
  });

  test('encodeScaledJpeg 按目标尺寸输出 JPEG（Windows/Linux 的压缩兜底）',
      () async {
    final source = _buildPng(width: 3000, height: 2000);

    final jpeg = await ChatImageCodec.encodeScaledJpeg(
      bytes: source,
      targetWidth: 2560,
      targetHeight: 1707,
      quality: 88,
    );

    expect(jpeg, isNotEmpty);
    // JPEG 魔数 FF D8 FF
    expect(jpeg[0], 0xFF);
    expect(jpeg[1], 0xD8);
    expect(jpeg[2], 0xFF);

    final decoded = await _decode(jpeg);
    expect(decoded.width, 2560);
    expect(decoded.height, 1707);
    decoded.dispose();
  });

  test('encodeScaledJpeg 能把超限的大图压到 5MB 门槛以内', () async {
    final source = _buildPng(width: 4000, height: 3000);

    final jpeg = await ChatImageCodec.encodeScaledJpeg(
      bytes: source,
      targetWidth: 2560,
      targetHeight: 1920,
      quality: 88,
    );

    expect(jpeg.length, lessThan(5 * 1024 * 1024));
  });

  test('encodeScaledJpeg 把透明区域按白底合成，不留黑块', () async {
    final transparent = img.Image(width: 20, height: 20, numChannels: 4);
    for (var y = 0; y < 20; y++) {
      for (var x = 0; x < 20; x++) {
        transparent.setPixelRgba(x, y, 0, 0, 0, 0);
      }
    }

    final jpeg = await ChatImageCodec.encodeScaledJpeg(
      bytes: img.encodePng(transparent),
      targetWidth: 20,
      targetHeight: 20,
      quality: 90,
    );

    final decoded = img.decodeJpg(jpeg)!;
    final pixel = decoded.getPixel(10, 10);
    expect(pixel.r, greaterThan(240));
    expect(pixel.g, greaterThan(240));
    expect(pixel.b, greaterThan(240));
  });
}
