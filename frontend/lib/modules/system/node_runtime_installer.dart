import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

/// 桌面端私有 Node 运行时：本机没有 Node / Node 太老时，把官方 Node LTS 发行包
/// 解到 `~/.grix/node-runtime/current`，Manager 的所有 shell 调用把它的 bin 目录
/// 前置到 PATH。
///
/// 为什么不走 brew / winget / 官方安装器：
/// - 它们要么需要 sudo/UAC（GUI 进程里没有 tty 可交互），要么脚本和包都托管在
///   GitHub / ghcr.io 上，国内基本拉不下来；
/// - 私有运行时只依赖 nodejs.org 的发行包，且 npmmirror 有完整镜像，官方源不通
///   时自动切镜像；
/// - 不改用户系统环境，不和用户自己装的 Node 打架，只在 Manager 自己的进程树里生效。
class NodeRuntimeInstaller {
  NodeRuntimeInstaller({
    required this.homeDir,
    Dio? dio,
    Future<ProcessResult> Function(String executable, List<String> arguments)?
        processRun,
  })  : _dio = dio ?? Dio(),
        _processRun = processRun ?? _defaultProcessRun;

  /// 只装 22 这条 LTS 线；具体补丁版由发行索引决定，索引都拉不到时用兜底版本。
  static const ltsMajor = 22;
  static const fallbackVersion = 'v22.12.0';

  /// 官方源优先，被墙时退到 npmmirror（目录结构和官方完全一致）。
  static const distSources = [
    'https://nodejs.org/dist',
    'https://npmmirror.com/mirrors/node',
  ];

  static const downloadTimeout = Duration(minutes: 5);

  final String homeDir;
  final Dio _dio;
  final Future<ProcessResult> Function(String executable, List<String> arguments)
      _processRun;

  static Future<ProcessResult> _defaultProcessRun(
    String executable,
    List<String> arguments,
  ) =>
      Process.run(executable, arguments).timeout(const Duration(minutes: 2));

  String get rootDir => '$homeDir${Platform.pathSeparator}.grix'
      '${Platform.pathSeparator}node-runtime';
  String get currentDir => '$rootDir${Platform.pathSeparator}current';

  /// 需要前置到 PATH 的目录。Windows 发行包 node.exe / npm.cmd 与全局包都在根目录，
  /// macOS/Linux 在 bin/ 下。
  String get binDir => Platform.isWindows
      ? currentDir
      : '$currentDir${Platform.pathSeparator}bin';

  String get nodeBinary => Platform.isWindows
      ? '$binDir${Platform.pathSeparator}node.exe'
      : '$binDir${Platform.pathSeparator}node';

  bool get isInstalled => File(nodeBinary).existsSync();

  static String get _arch {
    final v = Platform.version.toLowerCase();
    if (v.contains('arm64') || v.contains('aarch64')) return 'arm64';
    return 'x64';
  }

  /// 发行包文件名（不含版本目录）。
  @visibleForTesting
  static String archiveName(String version, {bool? windows, String? arch}) {
    final a = arch ?? _arch;
    if (windows ?? Platform.isWindows) return 'node-$version-win-$a.zip';
    final os = Platform.isMacOS ? 'darwin' : 'linux';
    return 'node-$version-$os-$a.tar.gz';
  }

  /// 从发行索引里挑最新的 LTS 主版本；索引拉不到就用兜底版本。
  Future<String> resolveVersion(String source) async {
    try {
      final resp = await _dio.get<dynamic>(
        '$source/index.json',
        options: Options(
          responseType: ResponseType.json,
          receiveTimeout: const Duration(seconds: 15),
        ),
      );
      final data = resp.data;
      if (data is List) {
        for (final entry in data) {
          if (entry is! Map) continue;
          final version = '${entry['version'] ?? ''}';
          if (version.startsWith('v$ltsMajor.') && entry['lts'] != false) {
            return version;
          }
        }
      }
    } catch (e) {
      debugPrint('[NodeRuntime] index.json unavailable at $source: $e');
    }
    return fallbackVersion;
  }

  /// 下载 + 解压 + 原子切换到 current。任一源全流程成功即返回 true。
  Future<bool> install({void Function(String line)? onLog}) async {
    final log = onLog ?? (_) {};
    for (final source in distSources) {
      log('[node-runtime] source: $source');
      try {
        if (await _installFrom(source, log)) return true;
      } catch (e) {
        log('[node-runtime] failed: $e');
      }
    }
    return false;
  }

  Future<bool> _installFrom(
    String source,
    void Function(String line) log,
  ) async {
    final version = await resolveVersion(source);
    final archive = archiveName(version);
    final url = '$source/$version/$archive';
    final tmpDir = Directory('$rootDir${Platform.pathSeparator}tmp');
    if (tmpDir.existsSync()) tmpDir.deleteSync(recursive: true);
    tmpDir.createSync(recursive: true);
    final archivePath = '${tmpDir.path}${Platform.pathSeparator}$archive';

    log('[node-runtime] downloading $url');
    await _dio.download(
      url,
      archivePath,
      options: Options(receiveTimeout: downloadTimeout),
    );

    log('[node-runtime] extracting $archive');
    final ProcessResult result;
    if (Platform.isWindows) {
      result = await _processRun('powershell', [
        '-NoProfile',
        '-NonInteractive',
        '-Command',
        "Expand-Archive -Force -LiteralPath '$archivePath' "
            "-DestinationPath '${tmpDir.path}'",
      ]);
    } else {
      result = await _processRun('tar', ['-xzf', archivePath, '-C', tmpDir.path]);
    }
    if (result.exitCode != 0) {
      throw StateError('extract exit ${result.exitCode}: ${result.stderr}');
    }

    // 解出来的目录名 = 包名去掉扩展名
    final extractedName = archive.replaceAll(RegExp(r'\.(zip|tar\.gz)$'), '');
    final extracted =
        Directory('${tmpDir.path}${Platform.pathSeparator}$extractedName');
    if (!extracted.existsSync()) {
      throw StateError('extracted dir missing: ${extracted.path}');
    }

    final current = Directory(currentDir);
    if (current.existsSync()) current.deleteSync(recursive: true);
    extracted.renameSync(currentDir);
    tmpDir.deleteSync(recursive: true);

    if (!isInstalled) throw StateError('node binary missing after install');
    log('[node-runtime] installed $version at $currentDir');
    return true;
  }
}
