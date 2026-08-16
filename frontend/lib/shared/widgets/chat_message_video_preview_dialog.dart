import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:video_player/video_player.dart';

import '../utils/audio_session_util.dart';
import '../utils/chat_media_cache.dart';
import '../utils/local_tailnet_proxy.dart';
import '../utils/toast_util.dart';
import '../utils/video_downloader.dart';

class ChatMessageVideoPreviewDialog extends StatefulWidget {
  const ChatMessageVideoPreviewDialog({
    super.key,
    required this.videoUri,
    this.title,
    this.autoPlay = true,
    this.onScrubbingChanged,
  });

  /// 原始视频地址（可能是 tailnet 自签 HTTPS）。播放前会先经过反代改写。
  final Uri videoUri;
  final String? title;

  /// 是否在初始化完成后自动播放；嵌入滑动查看器时非当前页应传 false。
  final bool autoPlay;

  /// 拖动进度条开始/结束时回调，供外层滑动查看器据此临时锁住横滑切页。
  final ValueChanged<bool>? onScrubbingChanged;

  @override
  State<ChatMessageVideoPreviewDialog> createState() =>
      _ChatMessageVideoPreviewDialogState();
}

class _ChatMessageVideoPreviewDialogState
    extends State<ChatMessageVideoPreviewDialog> {
  Uri? _playbackUri;
  String? _cachedPath;

  @override
  void initState() {
    super.initState();
    _resolvePlaybackUri();
  }

  Future<void> _resolvePlaybackUri() async {
    // 命中本地缓存直接播缓存文件，不走网络（也无需 tailnet 反代改写）。
    try {
      final cachedPath = await cachedMediaPath(widget.videoUri);
      if (cachedPath != null) {
        if (!mounted) return;
        setState(() {
          _cachedPath = cachedPath;
          _playbackUri = widget.videoUri;
        });
        return;
      }
    } catch (e) {
      debugPrint('cachedMediaPath lookup failed: $e');
    }
    try {
      final uri = await rewriteTailnetMediaUrl(widget.videoUri);
      if (!mounted) return;
      setState(() => _playbackUri = uri);
    } catch (e) {
      debugPrint('rewriteTailnetMediaUrl failed: $e');
      if (!mounted) return;
      // 兜底：反代起不来时退回原 URL，让播放器自己试一下（外部正常 HTTPS 仍能播）。
      setState(() => _playbackUri = widget.videoUri);
    }
    // 首次播放走网络流的同时，后台把完整文件拉进缓存，
    // 下次播放/保存直接用缓存，不再重复下载。
    prefetchMediaToCache(widget.videoUri);
  }

  @override
  Widget build(BuildContext context) {
    final uri = _playbackUri;
    if (uri == null) {
      return const Dialog.fullscreen(
        backgroundColor: Colors.black,
        child: SafeArea(
          child: Center(
            child: CircularProgressIndicator(color: Colors.white),
          ),
        ),
      );
    }
    return _VideoPreviewPlayer(
      playbackUri: uri,
      // 下载按钮走 dio 直连原 https，由 dart:io HttpClient + HttpOverrides
      // 信任自签 CA，不经过反代，因此用原始 URL。
      originalUri: widget.videoUri,
      cachedPath: _cachedPath,
      title: widget.title,
      autoPlay: widget.autoPlay,
      onScrubbingChanged: widget.onScrubbingChanged,
    );
  }
}

/// 真正持有 `VideoPlayerController` 的内层 widget。
///
/// 把 dialog 拆成两层是为了用同步 `late final` 创建 controller —
/// 外层负责异步把 tailnet HTTPS 改写成 loopback 反代地址，
/// 解析完成后再把 [playbackUri] 喂给内层创建播放器。
class _VideoPreviewPlayer extends StatefulWidget {
  const _VideoPreviewPlayer({
    required this.playbackUri,
    required this.originalUri,
    required this.cachedPath,
    required this.title,
    required this.autoPlay,
    required this.onScrubbingChanged,
  });

  final Uri playbackUri;
  final Uri originalUri;

  /// 命中的本地缓存文件路径；非空时直接播本地文件。
  final String? cachedPath;
  final String? title;
  final bool autoPlay;
  final ValueChanged<bool>? onScrubbingChanged;

  @override
  State<_VideoPreviewPlayer> createState() => _VideoPreviewPlayerState();
}

class _VideoPreviewPlayerState extends State<_VideoPreviewPlayer> {
  static const List<double> _speedOptions = <double>[0.5, 1.0, 1.5, 2.0];

  /// 顶栏按钮加半透明圆形底（与中央播放按钮同风格）：
  /// 白色画面下白色图标/加载圈也能看清。禁用态（下载中）保持同底色。
  static final ButtonStyle _topBarButtonStyle = IconButton.styleFrom(
    backgroundColor: Colors.black.withValues(alpha: 0.45),
    disabledBackgroundColor: Colors.black.withValues(alpha: 0.45),
  );

  late final VideoPlayerController _controller = createMediaPlayerController(
    widget.playbackUri,
    cachedPath: widget.cachedPath,
  );
  late final Future<void> _initializeFuture = _controller.initialize().then((
    _,
  ) {
    if (widget.autoPlay) {
      _controller.play();
      _scheduleHideControls();
    }
  });

  bool _controlsVisible = true;
  double _playbackSpeed = 1.0;
  bool _isDownloading = false;
  Timer? _hideTimer;
  CancelToken? _downloadCancelToken;

  @override
  void didUpdateWidget(covariant _VideoPreviewPlayer oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.autoPlay == widget.autoPlay ||
        !_controller.value.isInitialized) {
      return;
    }
    if (widget.autoPlay) {
      _controller.play();
      _showControls();
    } else {
      _controller.pause();
      _hideTimer?.cancel();
      setState(() => _controlsVisible = true);
    }
  }

  @override
  void dispose() {
    _hideTimer?.cancel();
    // 关闭弹窗时取消进行中的下载，避免后台继续拉取并保存被遗弃的文件。
    _downloadCancelToken?.cancel('dialog disposed');
    _controller.dispose();
    // 关闭预览时归还系统声道，恢复被打断的背景音乐。
    AudioSessionReleaser.release();
    super.dispose();
  }

  void _scheduleHideControls() {
    _hideTimer?.cancel();
    _hideTimer = Timer(const Duration(seconds: 3), () {
      if (!mounted) {
        return;
      }
      if (_controller.value.isPlaying) {
        setState(() => _controlsVisible = false);
      }
    });
  }

  void _showControls() {
    setState(() => _controlsVisible = true);
    _scheduleHideControls();
  }

  /// 点击视频画面：播放条隐藏时先把它唤出来，不打断播放；
  /// 播放条已经显示时再点才真正暂停/播放。
  void _onVideoTap() {
    if (!_controlsVisible) {
      _showControls();
      return;
    }
    _togglePlay();
  }

  void _togglePlay() {
    if (_controller.value.isPlaying) {
      _controller.pause();
      _hideTimer?.cancel();
      setState(() => _controlsVisible = true);
    } else {
      _controller.play();
      _showControls();
    }
  }

  void _onScrubStart() {
    widget.onScrubbingChanged?.call(true);
    _hideTimer?.cancel();
    setState(() => _controlsVisible = true);
    if (_controller.value.isPlaying) {
      _controller.pause();
    }
  }

  void _onScrubEnd() {
    widget.onScrubbingChanged?.call(false);
    if (!_controller.value.isInitialized) {
      return;
    }
    final value = _controller.value;
    // 拖到结尾就停在结尾,避免松手立刻 loop 回 0。
    if (value.duration > Duration.zero && value.position >= value.duration) {
      _showControls();
      return;
    }
    _controller.play();
    _showControls();
  }

  Future<void> _downloadVideo() async {
    if (_isDownloading) {
      return;
    }
    final cancelToken = CancelToken();
    setState(() {
      _isDownloading = true;
      _downloadCancelToken = cancelToken;
    });
    _showControls();
    // 保存未缓存的视频需要完整拉取整段文件，会与正在播放/缓冲的流抢同一条网络连接。
    // 首次打开时下载按钮可能先于播放器初始化完成被点击，此时 isPlaying 还是 false，
    // 但底层 <video>/原生播放器已经在拉 Range。下载前等初始化落定并暂停一次，
    // 让播放器把网络流让给下载；只有原本真的在播放时，下载结束后才恢复。
    final bool resumeAfterDownload = await _pausePlaybackForDownload();
    try {
      // 保存也走统一缓存：已缓存直接复制零流量；未缓存则这次下载顺带入缓存，
      // 之后播放/再次保存都不用重新下载。
      String? localSourcePath = widget.cachedPath;
      if (localSourcePath == null) {
        try {
          localSourcePath = await ensureCachedMedia(
            widget.originalUri,
            cancelToken: cancelToken,
          );
        } on DioException {
          rethrow;
        } catch (e) {
          // 缓存落盘失败不阻塞保存，退回直连下载。
          debugPrint('ensureCachedMedia failed, fallback to direct: $e');
          localSourcePath = null;
        }
      }
      final result = await downloadVideo(
        widget.originalUri,
        fileName: _resolveFileName(widget.originalUri),
        cancelToken: cancelToken,
        localSourcePath: localSourcePath,
      );
      CustomToast.show(
        localizedExportResultMessage(
          isDownload: result.isDownload,
          isGallery: result.isGallery,
          location: result.location,
          kindKey: 'chat_export_kind_video',
        ),
        isError: false,
      );
    } on DioException catch (error) {
      // 用户关闭弹窗触发的取消：静默忽略，不弹失败提示。
      if (CancelToken.isCancel(error)) {
        return;
      }
      debugPrint('video download failed (network): $error');
      CustomToast.show(
        'chat_export_download_failed_network'.trParams({
          'kind': 'chat_export_kind_video'.tr,
        }),
      );
    } on PlatformException catch (error) {
      debugPrint('video download failed (native): ${error.code} ${error.message}');
      final bool isPermission = error.code == 'permission_denied';
      CustomToast.show(
        isPermission
            ? 'chat_export_no_album_permission'.tr
            : 'chat_export_save_failed'.trParams({
                'kind': 'chat_export_kind_video'.tr,
              }),
      );
    } catch (error) {
      debugPrint('video download failed: $error');
      CustomToast.show(
        'chat_export_download_failed'.trParams({
          'kind': 'chat_export_kind_video'.tr,
        }),
      );
    } finally {
      if (mounted) {
        setState(() {
          _isDownloading = false;
          _downloadCancelToken = null;
        });
        // 为下载腾网络而暂停过播放、且还没播到结尾的，存完自动恢复播放。
        // 加 widget.autoPlay 守卫：嵌入滑动查看器时若下载中途翻到别的页，
        // 本页 autoPlay 已翻 false（约定切走即暂停、不播），此时不得强行恢复，
        // 否则会把已滑走、看不见的视频重新播出来（还带声音）。
        if (resumeAfterDownload &&
            widget.autoPlay &&
            _controller.value.isInitialized) {
          final value = _controller.value;
          final bool atEnd = value.duration > Duration.zero &&
              value.position >= value.duration;
          if (!atEnd) {
            _controller.play();
            _scheduleHideControls();
          }
        }
      }
    }
  }

  String _resolveFileName(Uri uri) {
    final rawName = uri.pathSegments.isNotEmpty ? uri.pathSegments.last : '';
    final decoded = _decodeFileName(rawName);
    final sanitized = decoded.replaceAll(RegExp(r'[^A-Za-z0-9._-]'), '_');
    final fallback = 'grix_video_${DateTime.now().millisecondsSinceEpoch}.mp4';
    if (sanitized.isEmpty) {
      return fallback;
    }
    if (sanitized.contains('.')) {
      return sanitized;
    }
    return '$sanitized.mp4';
  }

  Future<bool> _pausePlaybackForDownload() async {
    if (widget.cachedPath != null) {
      return false;
    }

    if (!_controller.value.isInitialized) {
      try {
        await _initializeFuture.timeout(const Duration(seconds: 8));
      } on TimeoutException {
        debugPrint('video initialize before download timed out');
      } catch (e) {
        debugPrint('video initialize before download failed: $e');
      }
      if (!mounted) {
        return false;
      }
    }

    if (!_controller.value.isInitialized) {
      return false;
    }

    final bool resumeAfterDownload = _controller.value.isPlaying;
    try {
      await _controller.pause();
    } catch (e) {
      debugPrint('video pause before download failed: $e');
    }
    if (!mounted) {
      return resumeAfterDownload;
    }

    _hideTimer?.cancel();
    setState(() => _controlsVisible = true);
    await Future<void>.delayed(const Duration(milliseconds: 120));
    return resumeAfterDownload;
  }

  String _decodeFileName(String rawName) {
    if (rawName.isEmpty) {
      return rawName;
    }
    try {
      return Uri.decodeComponent(rawName);
    } catch (_) {
      return rawName;
    }
  }

  void _cycleSpeed() {
    final int idx = _speedOptions.indexOf(_playbackSpeed);
    final double next = _speedOptions[(idx + 1) % _speedOptions.length];
    _playbackSpeed = next;
    _controller.setPlaybackSpeed(next);
    if (_controller.value.isPlaying) {
      _controller.play();
    }
    _showControls();
  }

  String _formatSpeed(double speed) {
    if (speed == speed.roundToDouble()) {
      return '${speed.toInt()}x';
    }
    return '${speed}x';
  }

  String _formatDuration(Duration d) {
    String two(int n) => n.toString().padLeft(2, '0');
    final String minutes = two(d.inMinutes.remainder(60));
    final String seconds = two(d.inSeconds.remainder(60));
    if (d.inHours > 0) {
      return '${two(d.inHours)}:$minutes:$seconds';
    }
    return '$minutes:$seconds';
  }

  @override
  Widget build(BuildContext context) {
    final String title = widget.title?.trim() ?? '';
    return Dialog.fullscreen(
      backgroundColor: Colors.black,
      child: SafeArea(
        child: Stack(
          children: [
            Center(
              child: FutureBuilder<void>(
                future: _initializeFuture,
                builder: (context, snapshot) {
                  if (snapshot.connectionState != ConnectionState.done) {
                    return const CircularProgressIndicator(color: Colors.white);
                  }
                  if (snapshot.hasError) {
                    return const Icon(
                      Icons.broken_image_outlined,
                      color: Colors.white70,
                      size: 48,
                    );
                  }
                  return GestureDetector(
                    onTap: _onVideoTap,
                    child: AspectRatio(
                      aspectRatio: _controller.value.aspectRatio <= 0
                          ? 16 / 9
                          : _controller.value.aspectRatio,
                      child: Stack(
                        alignment: Alignment.center,
                        children: [
                          VideoPlayer(_controller),
                          _buildCenterPlayButton(),
                        ],
                      ),
                    ),
                  );
                },
              ),
            ),
            _buildTopBar(title),
            _buildBottomControls(),
          ],
        ),
      ),
    );
  }

  Widget _buildCenterPlayButton() {
    final bool show = !_controller.value.isPlaying;
    return AnimatedOpacity(
      opacity: show ? 1 : 0,
      duration: const Duration(milliseconds: 200),
      child: IgnorePointer(
        ignoring: !show,
        child: GestureDetector(
          onTap: _togglePlay,
          child: Container(
            width: 64,
            height: 64,
            decoration: BoxDecoration(
              color: Colors.black.withValues(alpha: 0.55),
              shape: BoxShape.circle,
            ),
            child: const Icon(
              Icons.play_arrow_rounded,
              color: Colors.white,
              size: 40,
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildTopBar(String title) {
    return Positioned(
      top: 8,
      left: 8,
      right: 8,
      child: Row(
        children: [
          IconButton(
            tooltip: 'chat_export_download_video'.tr,
            style: _topBarButtonStyle,
            onPressed: _isDownloading ? null : _downloadVideo,
            icon: _isDownloading
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      valueColor: AlwaysStoppedAnimation<Color>(Colors.white),
                    ),
                  )
                : const Icon(Icons.download_rounded, color: Colors.white),
          ),
          Expanded(
            child: title.isNotEmpty
                ? Text(
                    title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 14,
                      fontWeight: FontWeight.w600,
                    ),
                  )
                : const SizedBox.shrink(),
          ),
          IconButton(
            tooltip: 'chat_export_close_video'.tr,
            style: _topBarButtonStyle,
            onPressed: () => Navigator.of(context).pop(),
            icon: const Icon(Icons.close_rounded, color: Colors.white),
          ),
        ],
      ),
    );
  }

  Widget _buildBottomControls() {
    if (!_controller.value.isInitialized) {
      return const SizedBox.shrink();
    }
    return Positioned(
      left: 0,
      right: 0,
      bottom: 0,
      child: AnimatedOpacity(
        opacity: _controlsVisible ? 1 : 0,
        duration: const Duration(milliseconds: 200),
        child: IgnorePointer(
          ignoring: !_controlsVisible,
          child: GestureDetector(
            behavior: HitTestBehavior.opaque,
            onTap: () {},
            child: Container(
              padding: const EdgeInsets.fromLTRB(16, 24, 16, 12),
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.bottomCenter,
                  colors: [
                    Colors.transparent,
                    Colors.black.withValues(alpha: 0.6),
                  ],
                ),
              ),
              child: ValueListenableBuilder<VideoPlayerValue>(
                valueListenable: _controller,
                builder: (context, value, _) {
                  return Row(
                    children: [
                      Text(
                        _formatDuration(value.position),
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 12,
                        ),
                      ),
                      Expanded(
                        child: Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 12),
                          child: _VideoScrubBar(
                            controller: _controller,
                            playedColor:
                                Theme.of(context).colorScheme.primary,
                            onScrubStart: _onScrubStart,
                            onScrubEnd: _onScrubEnd,
                          ),
                        ),
                      ),
                      Text(
                        _formatDuration(value.duration),
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 12,
                        ),
                      ),
                      const SizedBox(width: 8),
                      TextButton(
                        onPressed: _cycleSpeed,
                        style: TextButton.styleFrom(
                          foregroundColor: Colors.white,
                          padding: const EdgeInsets.symmetric(horizontal: 8),
                          minimumSize: const Size(0, 32),
                          tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                        ),
                        child: Text(
                          _formatSpeed(_playbackSpeed),
                          style: const TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                    ],
                  );
                },
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// 自绘进度条:命中区域加高到 28px 方便按住拖动,
/// 拖动开始/结束通过回调通知外层暂停/恢复播放。
class _VideoScrubBar extends StatefulWidget {
  const _VideoScrubBar({
    required this.controller,
    required this.playedColor,
    required this.onScrubStart,
    required this.onScrubEnd,
  });

  final VideoPlayerController controller;
  final Color playedColor;
  final VoidCallback onScrubStart;
  final VoidCallback onScrubEnd;

  @override
  State<_VideoScrubBar> createState() => _VideoScrubBarState();
}

class _VideoScrubBarState extends State<_VideoScrubBar> {
  static const double _hitHeight = 28;
  static const double _trackHeight = 4;
  static const double _trackHeightActive = 6;
  static const double _thumbSize = 12;
  static const double _thumbSizeActive = 16;

  bool _scrubbing = false;
  double? _scrubFraction;

  void _seekToFraction(double fraction) {
    final value = widget.controller.value;
    if (!value.isInitialized || value.duration <= Duration.zero) {
      return;
    }
    final clamped = fraction.clamp(0.0, 1.0);
    final target = Duration(
      milliseconds: (value.duration.inMilliseconds * clamped).round(),
    );
    widget.controller.seekTo(target);
  }

  double _fractionFromDx(double dx, double width) {
    if (width <= 0) return 0;
    return (dx / width).clamp(0.0, 1.0);
  }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final width = constraints.maxWidth;
        return GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTapDown: (details) {
            final f = _fractionFromDx(details.localPosition.dx, width);
            setState(() => _scrubFraction = f);
            widget.onScrubStart();
            _seekToFraction(f);
          },
          onTapUp: (_) {
            setState(() {
              _scrubbing = false;
              _scrubFraction = null;
            });
            widget.onScrubEnd();
          },
          onTapCancel: () {
            setState(() {
              _scrubbing = false;
              _scrubFraction = null;
            });
            widget.onScrubEnd();
          },
          onHorizontalDragStart: (details) {
            final f = _fractionFromDx(details.localPosition.dx, width);
            setState(() {
              _scrubbing = true;
              _scrubFraction = f;
            });
            widget.onScrubStart();
            _seekToFraction(f);
          },
          onHorizontalDragUpdate: (details) {
            final f = _fractionFromDx(details.localPosition.dx, width);
            setState(() => _scrubFraction = f);
            _seekToFraction(f);
          },
          onHorizontalDragEnd: (_) {
            setState(() {
              _scrubbing = false;
              _scrubFraction = null;
            });
            widget.onScrubEnd();
          },
          onHorizontalDragCancel: () {
            setState(() {
              _scrubbing = false;
              _scrubFraction = null;
            });
            widget.onScrubEnd();
          },
          child: SizedBox(
            height: _hitHeight,
            child: ValueListenableBuilder<VideoPlayerValue>(
              valueListenable: widget.controller,
              builder: (context, value, _) {
                final durMs = value.duration.inMilliseconds;
                final posMs = value.position.inMilliseconds;
                final controllerFraction = durMs > 0
                    ? (posMs / durMs).clamp(0.0, 1.0)
                    : 0.0;
                // 拖动期间用本地分数,避免 controller 回报延迟带来的视觉抖动。
                final played = _scrubFraction ?? controllerFraction;
                double buffered = 0;
                if (durMs > 0 && value.buffered.isNotEmpty) {
                  final end = value.buffered.last.end.inMilliseconds;
                  buffered = (end / durMs).clamp(0.0, 1.0);
                }
                final h = _scrubbing ? _trackHeightActive : _trackHeight;
                final thumbSize = _scrubbing ? _thumbSizeActive : _thumbSize;
                return Stack(
                  alignment: Alignment.centerLeft,
                  children: [
                    Container(
                      height: h,
                      decoration: BoxDecoration(
                        color: Colors.white24,
                        borderRadius:
                            BorderRadius.circular(h / 2),
                      ),
                    ),
                    FractionallySizedBox(
                      widthFactor: buffered,
                      child: Container(
                        height: h,
                        decoration: BoxDecoration(
                          color: Colors.white38,
                          borderRadius:
                              BorderRadius.circular(h / 2),
                        ),
                      ),
                    ),
                    FractionallySizedBox(
                      widthFactor: played,
                      child: Container(
                        height: h,
                        decoration: BoxDecoration(
                          color: widget.playedColor,
                          borderRadius:
                              BorderRadius.circular(h / 2),
                        ),
                      ),
                    ),
                    Positioned(
                      left:
                          (played * width - thumbSize / 2)
                              .clamp(0.0, width - thumbSize),
                      child: Container(
                        width: thumbSize,
                        height: thumbSize,
                        decoration: const BoxDecoration(
                          color: Colors.white,
                          shape: BoxShape.circle,
                          boxShadow: [
                            BoxShadow(
                              color: Colors.black54,
                              blurRadius: 2,
                            ),
                          ],
                        ),
                      ),
                    ),
                  ],
                );
              },
            ),
          ),
        );
      },
    );
  }
}
