import 'dart:async';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_cache_manager/flutter_cache_manager.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';
import 'package:video_player/video_player.dart';

/// 聊天媒体专用磁盘缓存（原生平台实现）。
///
/// 复用 flutter_cache_manager 做落盘与淘汰（按条数 + 最近访问时间自动清理），
/// 但下载走 dio：与视频下载共用同一条超时/取消语义，且 tailnet 自签 HTTPS
/// 已由全局 HttpOverrides 信任，直连原始地址即可。
final CacheManager _cacheManager = CacheManager(
  Config(
    'grix_chat_media',
    stalePeriod: const Duration(days: 14),
    maxNrOfCacheObjects: 24,
  ),
);

/// 媒体缓存下载不设接收超时（大文件可能持续数十秒），仅保留连接超时。
final Dio _dio = Dio(
  BaseOptions(connectTimeout: const Duration(seconds: 20)),
);

/// 同一 URL 的进行中下载去重：并发调用共享同一个 Future。
final Map<String, Future<String?>> _inflight = <String, Future<String?>>{};

Future<String?> cachedMediaPath(Uri mediaUri) async {
  final info = await _cacheManager.getFileFromCache(mediaUri.toString());
  final file = info?.file;
  if (file == null || !await file.exists()) {
    return null;
  }
  return file.path;
}

Future<String?> ensureCachedMedia(Uri mediaUri, {CancelToken? cancelToken}) {
  final key = mediaUri.toString();
  final existing = _inflight[key];
  if (existing != null) {
    return existing;
  }
  final future = _ensureCachedMedia(mediaUri, cancelToken: cancelToken)
      .whenComplete(() => _inflight.remove(key));
  _inflight[key] = future;
  return future;
}

Future<String?> _ensureCachedMedia(
  Uri mediaUri, {
  CancelToken? cancelToken,
}) async {
  final hit = await cachedMediaPath(mediaUri);
  if (hit != null) {
    return hit;
  }
  final tempDir = await getTemporaryDirectory();
  final tempPath = path.join(
    tempDir.path,
    'grix_media_cache_${DateTime.now().microsecondsSinceEpoch}',
  );
  try {
    await _dio.downloadUri(mediaUri, tempPath, cancelToken: cancelToken);
    // 流式搬进缓存，不把整个文件读进内存。
    final cached = await _cacheManager.putFileStream(
      mediaUri.toString(),
      File(tempPath).openRead(),
      fileExtension: fileExtensionForMediaUri(mediaUri),
    );
    return cached.path;
  } finally {
    await _deleteQuietly(tempPath);
  }
}

void prefetchMediaToCache(Uri mediaUri) {
  unawaited(
    ensureCachedMedia(mediaUri).catchError((Object error) {
      debugPrint('prefetch media cache failed: $error');
      return null;
    }),
  );
}

VideoPlayerController createMediaPlayerController(
  Uri networkUri, {
  String? cachedPath,
}) {
  if (cachedPath != null && cachedPath.isNotEmpty) {
    return VideoPlayerController.file(File(cachedPath));
  }
  return VideoPlayerController.networkUrl(networkUri);
}

/// 缓存文件的扩展名沿用 URL 里的原始扩展名，原生播放器靠它识别容器格式。
/// 非法/缺失/过长的扩展名回退到 mp4。
@visibleForTesting
String fileExtensionForMediaUri(Uri mediaUri) {
  final ext = path.extension(mediaUri.path).replaceFirst('.', '');
  final sanitized = ext.replaceAll(RegExp(r'[^A-Za-z0-9]'), '');
  if (sanitized.isEmpty || sanitized.length > 8) {
    return 'mp4';
  }
  return sanitized.toLowerCase();
}

Future<void> _deleteQuietly(String filePath) async {
  try {
    final file = File(filePath);
    if (await file.exists()) {
      await file.delete();
    }
  } catch (_) {
    // 临时文件清理失败不影响缓存结果。
  }
}
