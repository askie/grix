import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter/services.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

import 'video_downloader_types.dart';

const MethodChannel _gallerySaveChannel = MethodChannel(
  'pub.dhf.grix/mermaid_image_saver',
);

/// 视频下载不设接收超时（大文件可能持续数十秒），仅保留连接超时。
final Dio _dio = Dio(BaseOptions(connectTimeout: const Duration(seconds: 20)));

Future<VideoDownloadResult> downloadVideo(
  Uri videoUri, {
  required String fileName,
  CancelToken? cancelToken,
  String? localSourcePath,
}) async {
  if (Platform.isAndroid || Platform.isIOS) {
    return _downloadToGallery(
      videoUri,
      fileName: fileName,
      cancelToken: cancelToken,
      localSourcePath: localSourcePath,
    );
  }
  return _downloadToDownloads(
    videoUri,
    fileName: fileName,
    cancelToken: cancelToken,
    localSourcePath: localSourcePath,
  );
}

/// 移动端：先流式下载到临时目录，再交给原生存进系统相册。
/// 有本地缓存文件时改为复制缓存副本，不走网络——原生保存会移走该临时文件，
/// 所以必须复制一份，不能把缓存文件路径直接交出去。
Future<VideoDownloadResult> _downloadToGallery(
  Uri videoUri, {
  required String fileName,
  CancelToken? cancelToken,
  String? localSourcePath,
}) async {
  final tempDir = await getTemporaryDirectory();
  final tempPath = path.join(
    tempDir.path,
    'grix_video_dl_${DateTime.now().millisecondsSinceEpoch}_$fileName',
  );

  try {
    if (localSourcePath != null && await File(localSourcePath).exists()) {
      await File(localSourcePath).copy(tempPath);
    } else {
      await _dio.downloadUri(videoUri, tempPath, cancelToken: cancelToken);
    }

    final savedLocation = await _gallerySaveChannel
        .invokeMethod<String>('saveVideoToGallery', <String, Object>{
          'filePath': tempPath,
          'fileName': fileName,
          'mimeType': _resolveMimeType(fileName),
        });
    if (savedLocation == null || savedLocation.isEmpty) {
      throw StateError('Native gallery saver returned empty location');
    }
    return VideoDownloadResult(location: savedLocation, isGallery: true);
  } finally {
    // 覆盖所有路径（下载失败/原生抛错/成功后）的临时文件清理。
    // iOS 端 shouldMoveFile=true 成功时会移走文件，这里 exists 判空兜底。
    await _deleteQuietly(tempPath);
  }
}

/// 桌面端：流式下载到系统 Downloads 目录（重名自动加序号）。
/// 有本地缓存文件时直接复制缓存，不走网络。
Future<VideoDownloadResult> _downloadToDownloads(
  Uri videoUri, {
  required String fileName,
  CancelToken? cancelToken,
  String? localSourcePath,
}) async {
  Directory? outputDir;
  try {
    outputDir = await getDownloadsDirectory();
  } catch (_) {
    outputDir = null;
  }
  outputDir ??= await getApplicationDocumentsDirectory();
  if (!await outputDir.exists()) {
    await outputDir.create(recursive: true);
  }

  final targetPath = _resolveUniquePath(outputDir.path, fileName);
  try {
    if (localSourcePath != null && await File(localSourcePath).exists()) {
      await File(localSourcePath).copy(targetPath);
    } else {
      await _dio.downloadUri(videoUri, targetPath, cancelToken: cancelToken);
    }
    return VideoDownloadResult(location: targetPath);
  } catch (_) {
    // 下载失败时清理可能残留的半截文件，避免脏文件留在 Downloads。
    await _deleteQuietly(targetPath);
    rethrow;
  }
}

Future<void> _deleteQuietly(String filePath) async {
  try {
    final file = File(filePath);
    if (await file.exists()) {
      await file.delete();
    }
  } catch (_) {
    // 临时/残留文件清理失败不影响下载结果。
  }
}

String _resolveUniquePath(String dir, String fileName) {
  final ext = path.extension(fileName);
  final base = path.basenameWithoutExtension(fileName);
  var candidate = path.join(dir, fileName);
  var index = 1;
  while (File(candidate).existsSync()) {
    candidate = path.join(dir, '$base($index)$ext');
    index++;
  }
  return candidate;
}

String _resolveMimeType(String fileName) {
  final ext = path.extension(fileName).toLowerCase();
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
