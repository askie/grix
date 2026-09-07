import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/android_update_support.dart';
import 'package:grix/data/providers/apk_downloader.dart';

void main() {
  group('ApkDownloader.planResume', () {
    test('no local bytes means a plain full request', () {
      final plan = ApkDownloader.planResume(existingBytes: 0);

      expect(plan.startOffset, 0);
      expect(plan.headers, isEmpty);
    });

    test('partial file asks the server to continue from that offset', () {
      final plan = ApkDownloader.planResume(existingBytes: 1024);

      expect(plan.startOffset, 1024);
      expect(plan.headers['range'], 'bytes=1024-');
    });
  });

  group('ApkDownloader.resolveResumeMode', () {
    test('206 on a ranged request appends to the local file', () {
      expect(
        ApkDownloader.resolveResumeMode(statusCode: 206, requestedOffset: 1024),
        ResumeMode.append,
      );
    });

    test('server ignoring Range (200) forces a rewrite, never an append', () {
      // Appending a full body onto a half-downloaded file silently corrupts the
      // APK — the SHA256 check would be the only thing catching it.
      expect(
        ApkDownloader.resolveResumeMode(statusCode: 200, requestedOffset: 1024),
        ResumeMode.restart,
      );
    });

    test('a first request always writes from the start', () {
      expect(
        ApkDownloader.resolveResumeMode(statusCode: 200, requestedOffset: 0),
        ResumeMode.restart,
      );
      expect(
        ApkDownloader.resolveResumeMode(statusCode: 206, requestedOffset: 0),
        ResumeMode.restart,
      );
    });
  });

  group('ApkDownloader total size', () {
    test('Content-Range wins over Content-Length', () {
      expect(
        ApkDownloader.resolveTotalBytes(
          mode: ResumeMode.append,
          startOffset: 100,
          contentLength: 900,
          contentRange: 'bytes 100-999/1000',
        ),
        1000,
      );
    });

    test('appending adds the offset back to the remaining length', () {
      expect(
        ApkDownloader.resolveTotalBytes(
          mode: ResumeMode.append,
          startOffset: 100,
          contentLength: 900,
        ),
        1000,
      );
    });

    test('restarting takes Content-Length as-is', () {
      expect(
        ApkDownloader.resolveTotalBytes(
          mode: ResumeMode.restart,
          startOffset: 100,
          contentLength: 1000,
        ),
        1000,
      );
    });

    test('unknown length reports 0 rather than a wrong total', () {
      expect(
        ApkDownloader.resolveTotalBytes(
          mode: ResumeMode.restart,
          startOffset: 0,
          contentLength: null,
        ),
        0,
      );
      expect(ApkDownloader.parseTotalFromContentRange('bytes */1000'), 1000);
      expect(ApkDownloader.parseTotalFromContentRange('bytes 0-9/*'), isNull);
      expect(ApkDownloader.parseTotalFromContentRange(null), isNull);
    });
  });

  group('AndroidUpdateSupport.installHintKey', () {
    test('maps each vendor family to its own guidance', () {
      expect(
        AndroidUpdateSupport.installHintKey('Xiaomi'),
        'update_install_hint_xiaomi',
      );
      expect(
        AndroidUpdateSupport.installHintKey('Redmi'),
        'update_install_hint_xiaomi',
      );
      expect(
        AndroidUpdateSupport.installHintKey('samsung'),
        'update_install_hint_samsung',
      );
      expect(
        AndroidUpdateSupport.installHintKey('HUAWEI'),
        'update_install_hint_huawei',
      );
      expect(
        AndroidUpdateSupport.installHintKey('HONOR'),
        'update_install_hint_huawei',
      );
      for (final m in ['vivo', 'OPPO', 'realme', 'OnePlus']) {
        expect(
          AndroidUpdateSupport.installHintKey(m),
          'update_install_hint_bbk',
          reason: m,
        );
      }
    });

    test('unknown or missing manufacturer falls back to generic copy', () {
      expect(
        AndroidUpdateSupport.installHintKey('Google'),
        'update_install_hint_generic',
      );
      expect(
        AndroidUpdateSupport.installHintKey(null),
        'update_install_hint_generic',
      );
      expect(
        AndroidUpdateSupport.installHintKey('  '),
        'update_install_hint_generic',
      );
    });
  });

  group('AndroidUpdateSupport storage', () {
    test('parses the Available column of df -k', () {
      const output = '''
Filesystem     1K-blocks     Used Available Use% Mounted on
/dev/block/dm-5 55000000 30000000  24000000  56% /data
''';
      expect(
        AndroidUpdateSupport.parseDfAvailableBytes(output),
        24000000 * 1024,
      );
    });

    test('returns null for unparsable output instead of guessing', () {
      expect(AndroidUpdateSupport.parseDfAvailableBytes(''), isNull);
      expect(
        AndroidUpdateSupport.parseDfAvailableBytes('Filesystem Size Used'),
        isNull,
      );
    });

    test('needs twice the package size, and never blocks on unknown space', () {
      expect(
        AndroidUpdateSupport.hasEnoughSpace(
          freeBytes: 200 * 1024 * 1024,
          fileSize: 60 * 1024 * 1024,
        ),
        isTrue,
      );
      expect(
        AndroidUpdateSupport.hasEnoughSpace(
          freeBytes: 100 * 1024 * 1024,
          fileSize: 60 * 1024 * 1024,
        ),
        isFalse,
      );
      expect(
        AndroidUpdateSupport.hasEnoughSpace(freeBytes: null, fileSize: 60),
        isTrue,
      );
    });
  });

  group('resumed download writes', () {
    test('append keeps the earlier bytes, restart drops them', () async {
      final dir = await Directory.systemTemp.createTemp('apk-resume-test');
      addTearDown(() => dir.deleteSync(recursive: true));
      final file = File('${dir.path}/grix-test.apk');
      file.writeAsBytesSync([1, 2, 3]);

      final plan = ApkDownloader.planResume(existingBytes: file.lengthSync());
      expect(plan.headers['range'], 'bytes=3-');

      final appendSink = file.openWrite(mode: FileMode.append);
      appendSink.add([4, 5]);
      await appendSink.close();
      expect(file.readAsBytesSync(), [1, 2, 3, 4, 5]);

      final restartSink = file.openWrite(mode: FileMode.write);
      restartSink.add([9]);
      await restartSink.close();
      expect(file.readAsBytesSync(), [9]);
    });
  });
}
