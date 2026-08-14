/// Web 桩：下载到本机目录在 Web 不支持，调用即报错（实际不会被调到，
/// 下载入口已按 kIsWeb 屏蔽）。
Future<void> ensureDirectory(String path) async {
  throw UnsupportedError('download to local directory is not supported on web');
}

/// Web 桩：无本地文件可清理，空实现。
Future<void> deleteFileQuietly(String path) async {}

/// Web 桩：Web 不支持下载到本机目录（入口已按 kIsWeb 屏蔽）。
Future<String> appVisibleDownloadDirectory(String subfolder) async {
  throw UnsupportedError('download to local directory is not supported on web');
}
