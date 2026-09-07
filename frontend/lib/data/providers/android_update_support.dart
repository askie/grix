import 'dart:io';

/// Android 应用内更新的机型相关辅助逻辑。
///
/// 单独成文件是为了让厂商文案映射和剩余空间解析这两块纯逻辑能被单测覆盖——
/// 它们都不依赖 Flutter binding，也不依赖真机。
class AndroidUpdateSupport {
  AndroidUpdateSupport._();

  /// 按厂商返回「去哪儿开安装权限」的提示文案 key。
  ///
  /// 国产 ROM 在系统的「安装未知应用」开关之外还各自加了一层拦截，只说
  /// 「请允许安装」用户照样卡住，所以要按 `Build.MANUFACTURER` 给到具体位置。
  static String installHintKey(String? manufacturer) {
    final m = (manufacturer ?? '').trim().toLowerCase();
    if (m.isEmpty) return 'update_install_hint_generic';
    if (m.contains('xiaomi') ||
        m.contains('redmi') ||
        m.contains('poco') ||
        m.contains('blackshark')) {
      return 'update_install_hint_xiaomi';
    }
    if (m.contains('samsung')) return 'update_install_hint_samsung';
    if (m.contains('huawei') || m.contains('honor')) {
      return 'update_install_hint_huawei';
    }
    if (m.contains('vivo') ||
        m.contains('oppo') ||
        m.contains('realme') ||
        m.contains('oneplus')) {
      return 'update_install_hint_bbk';
    }
    return 'update_install_hint_generic';
  }

  /// 解析 `df -k <path>` 的输出，返回可用字节数；解析不出返回 null。
  ///
  /// toybox/busybox/coreutils 的列宽和表头都不一样，唯一稳定的是数据行倒数
  /// 第三列是 Available（单位 KB，因为传了 -k）。挂载点含空格的行会被跳过。
  static int? parseDfAvailableBytes(String output) {
    final lines = output
        .split('\n')
        .map((l) => l.trim())
        .where((l) => l.isNotEmpty)
        .toList();
    if (lines.length < 2) return null;
    for (final line in lines.skip(1)) {
      final cols = line.split(RegExp(r'\s+'));
      if (cols.length < 4) continue;
      final availableKb = int.tryParse(cols[cols.length - 3]);
      if (availableKb == null) continue;
      return availableKb * 1024;
    }
    return null;
  }

  /// 查询 [path] 所在分区的剩余字节数；查不到返回 null（调用方按「未知」处理，
  /// 不能因为查不到就拦住更新）。
  static Future<int?> freeSpaceBytes(String path) async {
    if (!Platform.isAndroid && !Platform.isLinux && !Platform.isMacOS) {
      return null;
    }
    try {
      final result = await Process.run('df', ['-k', path]);
      if (result.exitCode != 0) return null;
      return parseDfAvailableBytes(result.stdout.toString());
    } catch (_) {
      return null;
    }
  }

  /// 判断剩余空间是否够装这个包。
  ///
  /// 要求 2 倍包体：一份是下载下来的 APK，一份是系统安装器解包安装时的开销。
  /// [freeBytes] 为 null（查不到）时放行。
  static bool hasEnoughSpace({required int? freeBytes, required int fileSize}) {
    if (freeBytes == null || fileSize <= 0) return true;
    return freeBytes >= fileSize * 2;
  }
}
