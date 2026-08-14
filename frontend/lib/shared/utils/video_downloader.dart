import 'package:dio/dio.dart';

import 'video_downloader_stub.dart'
    if (dart.library.js_interop) 'video_downloader_web.dart'
    if (dart.library.io) 'video_downloader_io.dart' as impl;
import 'video_downloader_types.dart';

/// 下载远程视频。移动端存系统相册，桌面端写 Downloads 目录，Web 触发浏览器下载。
///
/// 全程流式（Web 除外，无文件系统只能走内存），不把整段视频读进内存，适配大文件。
/// 传入 [cancelToken] 可在调用方销毁时取消进行中的下载（仅移动端/桌面生效）。
/// 传入 [localSourcePath]（已缓存的本地文件）时直接复制该文件，不再走网络。
Future<VideoDownloadResult> downloadVideo(
  Uri videoUri, {
  required String fileName,
  CancelToken? cancelToken,
  String? localSourcePath,
}) {
  return impl.downloadVideo(
    videoUri,
    fileName: fileName,
    cancelToken: cancelToken,
    localSourcePath: localSourcePath,
  );
}
