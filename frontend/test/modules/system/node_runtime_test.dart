import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/system/grix_connector_service.dart';
import 'package:grix/modules/system/node_runtime_installer.dart';

/// 私有 Node 运行时：发行包命名必须和 nodejs.org / npmmirror 目录一致，
/// PATH 前置必须在两种 shell 语法下都正确，否则自举装上的 Node 谁也找不到。
void main() {
  test('发行包文件名与 nodejs.org 目录一致', () {
    expect(
      NodeRuntimeInstaller.archiveName('v22.12.0', windows: true, arch: 'x64'),
      'node-v22.12.0-win-x64.zip',
    );
    expect(
      NodeRuntimeInstaller.archiveName(
        'v22.12.0',
        windows: false,
        arch: 'arm64',
      ),
      Platform.isMacOS
          ? 'node-v22.12.0-darwin-arm64.tar.gz'
          : 'node-v22.12.0-linux-arm64.tar.gz',
    );
  });

  test('镜像源排在官方源之后', () {
    expect(
      NodeRuntimeInstaller.distSources.first,
      startsWith('https://nodejs.org'),
    );
    expect(NodeRuntimeInstaller.distSources.last, contains('npmmirror.com'));
  });

  test('未安装运行时的机器上命令原样执行', () {
    final service = GrixConnectorService();
    if (service.nodeRuntime.isInstalled) {
      // 开发机上真的装过私有运行时：前置目录必须出现在命令里
      expect(
        service.withRuntimePath('node --version'),
        contains(service.nodeRuntime.binDir),
      );
      return;
    }
    expect(service.extraPathDirs(), isEmpty);
    expect(service.withRuntimePath('node --version'), 'node --version');
  });

  test('安装运行时后 PATH 前置到命令里', () async {
    final home = await Directory.systemTemp.createTemp('grix-node-rt');
    addTearDown(() => home.delete(recursive: true));
    final installer = NodeRuntimeInstaller(homeDir: home.path);
    expect(installer.isInstalled, isFalse);
    File(installer.nodeBinary).createSync(recursive: true);
    expect(installer.isInstalled, isTrue);
    expect(installer.binDir, endsWith(Platform.isWindows ? 'current' : 'bin'));
  });
}
