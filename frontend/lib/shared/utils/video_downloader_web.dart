import 'dart:async';
import 'dart:js_interop';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:web/web.dart' as web;

import 'video_downloader_types.dart';

/// Web 视频下载不设接收超时（大文件可能持续数十秒）。
final Dio _dio = Dio(
  BaseOptions(
    responseType: ResponseType.bytes,
    connectTimeout: const Duration(seconds: 20),
  ),
);

Future<VideoDownloadResult> downloadVideo(
  Uri videoUri, {
  required String fileName,
  CancelToken? cancelToken,
  String? localSourcePath,
}) async {
  // Web 无文件系统，只能整段读进内存再造 Blob 触发下载。大文件内存占用偏高，
  // 更优解是对象存储签名 URL 带 Content-Disposition=attachment，由服务端控制。
  final response = await _dio.getUri<List<int>>(
    videoUri,
    cancelToken: cancelToken,
  );
  final statusCode = response.statusCode ?? 0;
  final data = response.data;
  if (statusCode < 200 || statusCode >= 300 || data == null || data.isEmpty) {
    throw StateError('Failed to download video: $videoUri');
  }

  final bytes = Uint8List.fromList(data);
  final blob = web.Blob(
    [bytes.toJS].toJS,
    web.BlobPropertyBag(type: _resolveMimeType(fileName)),
  );
  final url = web.URL.createObjectURL(blob);
  final anchor = web.HTMLAnchorElement()
    ..href = url
    ..download = fileName
    ..style.display = 'none';
  web.document.body?.append(anchor);
  anchor.click();
  anchor.remove();
  // Chrome/Safari may not synchronously take ownership of the Blob URL when a
  // download is triggered from an async fetch path. Revoking immediately can
  // make the first click appear to do nothing, especially while video playback
  // is still settling. Keep it alive briefly and clean it up later.
  unawaited(
    Future<void>.delayed(const Duration(minutes: 1)).then((_) {
      web.URL.revokeObjectURL(url);
    }),
  );

  return VideoDownloadResult(location: fileName, isDownload: true);
}

String _resolveMimeType(String fileName) {
  final dot = fileName.lastIndexOf('.');
  final ext = dot >= 0 ? fileName.substring(dot).toLowerCase() : '';
  switch (ext) {
    case '.mov':
      return 'video/quicktime';
    case '.webm':
      return 'video/webm';
    case '.mkv':
      return 'video/x-matroska';
    case '.avi':
      return 'video/x-msvideo';
    case '.m4v':
      return 'video/x-m4v';
    case '.mp4':
    default:
      return 'video/mp4';
  }
}
