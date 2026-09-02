import 'package:dio/dio.dart';
import 'package:video_player/video_player.dart';

import 'chat_media_cache_noop.dart'
    if (dart.library.io) 'chat_media_cache_io.dart'
    as impl;

/// 聊天媒体卡片（视频/音频）的统一本地缓存门面。
///
/// 同一个 URL 只完整下载一次：播放命中缓存直接播本地文件，
/// 保存到相册/Downloads 也直接复制缓存文件，不再重复走网络。
/// Web 端没有本地文件系统，全部退化为直连网络（浏览器自带 HTTP 缓存）。

/// 返回已缓存的本地文件路径；未缓存（或 Web 端）返回 null，不触发下载。
Future<String?> cachedMediaPath(Uri mediaUri) => impl.cachedMediaPath(mediaUri);

/// 确保媒体已进缓存并返回本地路径：命中直接返回，未命中先下载再入缓存。
/// 同一 URL 的并发调用共享同一次下载。Web 端恒返回 null。
Future<String?> ensureCachedMedia(Uri mediaUri, {CancelToken? cancelToken}) =>
    impl.ensureCachedMedia(mediaUri, cancelToken: cancelToken);

/// 后台静默把媒体拉进缓存（fire-and-forget，失败只记日志）。
void prefetchMediaToCache(Uri mediaUri) => impl.prefetchMediaToCache(mediaUri);

/// 取消指定 URL 正在进行的后台拉取（若有），供用户显式下载前调用。
void cancelInflightMediaDownload(Uri mediaUri) =>
    impl.cancelInflightMediaDownload(mediaUri);

/// 按缓存命中情况创建播放控制器：有缓存播本地文件，否则播网络流。
VideoPlayerController createMediaPlayerController(
  Uri networkUri, {
  String? cachedPath,
}) => impl.createMediaPlayerController(networkUri, cachedPath: cachedPath);
