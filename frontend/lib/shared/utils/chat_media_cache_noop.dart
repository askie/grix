import 'package:dio/dio.dart';
import 'package:video_player/video_player.dart';

/// Web / 无文件系统平台实现：不做本地缓存，播放与下载全部直连网络。

Future<String?> cachedMediaPath(Uri mediaUri) async => null;

Future<String?> ensureCachedMedia(
  Uri mediaUri, {
  CancelToken? cancelToken,
}) async =>
    null;

void prefetchMediaToCache(Uri mediaUri) {}

VideoPlayerController createMediaPlayerController(
  Uri networkUri, {
  String? cachedPath,
}) =>
    VideoPlayerController.networkUrl(networkUri);
