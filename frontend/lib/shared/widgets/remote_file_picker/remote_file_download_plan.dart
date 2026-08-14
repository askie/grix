import 'package:path/path.dart' as p;

/// 单个文件的下载任务：从宿主机绝对路径 [hostPath] 拉取，存到本机 [savePath]。
class RemoteDownloadItem {
  const RemoteDownloadItem({required this.hostPath, required this.savePath});

  /// 宿主机上文件的绝对路径，作为 /download?path= 的参数。
  final String hostPath;

  /// 手机本地保存的完整路径。
  final String savePath;

  @override
  bool operator ==(Object other) =>
      other is RemoteDownloadItem &&
      other.hostPath == hostPath &&
      other.savePath == savePath;

  @override
  int get hashCode => Object.hash(hostPath, savePath);

  @override
  String toString() => 'RemoteDownloadItem($hostPath -> $savePath)';
}

/// 一个目录展开后的下载计划：待拉取的文件、需预建的（空）子目录、是否被截断。
class RemoteDownloadPlan {
  const RemoteDownloadPlan({
    required this.files,
    required this.dirs,
    required this.truncated,
  });

  final List<RemoteDownloadItem> files;
  final List<String> dirs;
  final bool truncated;
}

/// 把 /manifest 返回的目录递归清单，转成相对 [destDir]/[rootName] 的本地下载计划。
///
/// 清单项约定：`rel` 为相对根目录、统一 '/' 分隔的路径；`is_dir` 标识目录；
/// 文件项带 `abs`（宿主机绝对路径，用于发起下载）。目录项用于重建空子目录结构。
/// 跨平台安全：本地路径用 [p.joinAll] 按当前平台分隔符拼接，不直接拼宿主机分隔符。
RemoteDownloadPlan planDirectoryDownload({
  required String destDir,
  required String rootName,
  required Map<dynamic, dynamic> manifest,
}) {
  final files = <RemoteDownloadItem>[];
  final dirs = <String>[];
  final truncated = manifest['truncated'] == true;
  final entries = (manifest['entries'] as List?) ?? const [];
  for (final e in entries) {
    if (e is! Map) continue;
    final rel = (e['rel'] ?? '').toString();
    if (rel.isEmpty) continue;
    final localPath = p.joinAll([destDir, rootName, ...rel.split('/')]);
    if (e['is_dir'] == true) {
      dirs.add(localPath);
    } else {
      final abs = (e['abs'] ?? '').toString();
      if (abs.isEmpty) continue;
      files.add(RemoteDownloadItem(hostPath: abs, savePath: localPath));
    }
  }
  return RemoteDownloadPlan(files: files, dirs: dirs, truncated: truncated);
}
