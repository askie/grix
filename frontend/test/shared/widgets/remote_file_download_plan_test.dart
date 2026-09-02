import 'package:flutter_test/flutter_test.dart';
import 'package:path/path.dart' as p;
import 'package:grix/shared/widgets/remote_file_picker/remote_file_download_plan.dart';

void main() {
  group('planDirectoryDownload', () {
    test('文件项映射为 hostPath(abs) + 本地 destDir/root/rel', () {
      final manifest = {
        'truncated': false,
        'entries': [
          {
            'rel': 'a.txt',
            'is_dir': false,
            'size': 5,
            'abs': '/host/proj/a.txt',
          },
          {
            'rel': 'sub/b.bin',
            'is_dir': false,
            'size': 10,
            'abs': '/host/proj/sub/b.bin',
          },
        ],
      };
      final plan = planDirectoryDownload(
        destDir: '/phone/dl',
        rootName: 'proj',
        manifest: manifest,
      );

      expect(plan.truncated, isFalse);
      expect(plan.dirs, isEmpty);
      expect(plan.files, [
        RemoteDownloadItem(
          hostPath: '/host/proj/a.txt',
          savePath: p.join('/phone/dl', 'proj', 'a.txt'),
        ),
        RemoteDownloadItem(
          hostPath: '/host/proj/sub/b.bin',
          savePath: p.joinAll(['/phone/dl', 'proj', 'sub', 'b.bin']),
        ),
      ]);
    });

    test('目录项收集为待建空目录，rel 用 / 拆分重建本地子目录', () {
      final manifest = {
        'entries': [
          {'rel': 'sub', 'is_dir': true},
          {'rel': 'sub/empty', 'is_dir': true},
        ],
      };
      final plan = planDirectoryDownload(
        destDir: '/phone/dl',
        rootName: 'proj',
        manifest: manifest,
      );

      expect(plan.files, isEmpty);
      expect(plan.dirs, [
        p.joinAll(['/phone/dl', 'proj', 'sub']),
        p.joinAll(['/phone/dl', 'proj', 'sub', 'empty']),
      ]);
    });

    test('文件缺 abs 则跳过；空 rel 跳过；truncated 透传', () {
      final manifest = {
        'truncated': true,
        'entries': [
          {'rel': '', 'is_dir': false, 'abs': '/host/x'},
          {'rel': 'no_abs.txt', 'is_dir': false},
          {'rel': 'ok.txt', 'is_dir': false, 'abs': '/host/ok.txt'},
        ],
      };
      final plan = planDirectoryDownload(
        destDir: '/phone/dl',
        rootName: 'proj',
        manifest: manifest,
      );

      expect(plan.truncated, isTrue);
      expect(plan.files, [
        RemoteDownloadItem(
          hostPath: '/host/ok.txt',
          savePath: p.join('/phone/dl', 'proj', 'ok.txt'),
        ),
      ]);
    });

    test('空清单产出空计划', () {
      final plan = planDirectoryDownload(
        destDir: '/phone/dl',
        rootName: 'proj',
        manifest: {'entries': []},
      );
      expect(plan.files, isEmpty);
      expect(plan.dirs, isEmpty);
      expect(plan.truncated, isFalse);
    });
  });
}
