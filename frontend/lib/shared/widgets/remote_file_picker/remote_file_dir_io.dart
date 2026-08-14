import 'dart:io';

import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

/// 递归创建目录（已存在则无操作）。原生平台实现。
Future<void> ensureDirectory(String path) async {
  await Directory(path).create(recursive: true);
}

/// iOS 下载落地目录：App 自己的 Documents 下的子目录（沙盒内，可直接写）。
/// iOS 无法写入系统目录选择器返回的受限路径，统一落到这里；
/// 配合 Info.plist 的 UIFileSharingEnabled / LSSupportsOpeningDocumentsInPlace，
/// 该目录在「文件」App 的「我的 iPhone → Grix」下可见、可移动。
Future<String> appVisibleDownloadDirectory(String subfolder) async {
  final docs = await getApplicationDocumentsDirectory();
  final dir = p.join(docs.path, subfolder);
  await Directory(dir).create(recursive: true);
  return dir;
}

/// 删除文件（不存在则忽略）。用于下载失败/取消后清理半截文件。
/// 删除本身失败不抛出——这是收尾清理，主操作的失败已单独上报。
Future<void> deleteFileQuietly(String path) async {
  try {
    final f = File(path);
    if (await f.exists()) await f.delete();
  } catch (_) {
    // 清理失败不影响主流程，忽略。
  }
}
