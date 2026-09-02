import 'package:grix/shared/utils/chat_media_cache_io.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('fileExtensionForMediaUri', () {
    test('沿用 URL path 里的原始扩展名并统一小写', () {
      expect(
        fileExtensionForMediaUri(Uri.parse('https://a.com/demo/video.mp4')),
        'mp4',
      );
      expect(
        fileExtensionForMediaUri(Uri.parse('https://a.com/REC.MOV')),
        'mov',
      );
      expect(
        fileExtensionForMediaUri(Uri.parse('https://a.com/song.m4a')),
        'm4a',
      );
    });

    test('查询参数不影响扩展名解析', () {
      expect(
        fileExtensionForMediaUri(
          Uri.parse('https://a.com/v.webm?sign=abc.def&t=1'),
        ),
        'webm',
      );
    });

    test('缺失或非法扩展名回退到 mp4', () {
      expect(
        fileExtensionForMediaUri(Uri.parse('https://a.com/stream')),
        'mp4',
      );
      expect(fileExtensionForMediaUri(Uri.parse('https://a.com/file.')), 'mp4');
      expect(
        fileExtensionForMediaUri(
          Uri.parse('https://a.com/x.superlongextension'),
        ),
        'mp4',
      );
    });
  });
}
