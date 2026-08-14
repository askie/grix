import 'package:flutter/material.dart';
import 'package:video_player/video_player.dart';

import '../markdown/chat_markdown_uri_policy.dart';
import '../utils/audio_session_util.dart';
import '../utils/chat_media_cache.dart';
import '../utils/local_tailnet_proxy.dart';

/// Renders an `<audio>` Markdown node as a self-contained player card with an
/// inline play/pause button, a scrubbable progress bar, and a time readout.
///
/// Playback reuses the project's existing [VideoPlayerController], which plays
/// audio-only sources, so no extra dependency is introduced. The controller is
/// created lazily on the first play tap to avoid loading every audio card in a
/// long message up front.
class ChatMarkdownAudioView extends StatefulWidget {
  const ChatMarkdownAudioView({
    super.key,
    required this.src,
    this.title,
    this.inline = false,
  });

  final String src;
  final String? title;
  final bool inline;

  @override
  State<ChatMarkdownAudioView> createState() => _ChatMarkdownAudioViewState();
}

class _ChatMarkdownAudioViewState extends State<ChatMarkdownAudioView> {
  static const double _width = 260;

  VideoPlayerController? _controller;
  bool _initializing = false;
  bool _failed = false;
  bool _wasPlaying = false;

  @override
  void dispose() {
    _controller?.removeListener(_onControllerUpdate);
    _controller?.dispose();
    // 卡片销毁时（如播放途中关闭会话）归还系统声道，恢复被打断的音乐。
    AudioSessionReleaser.release();
    super.dispose();
  }

  void _onControllerUpdate() {
    final controller = _controller;
    final isPlaying = controller?.value.isPlaying ?? false;
    // 播放从进行中转为停止（暂停或播放结束）时，把声道还给系统，
    // 让之前被打断的背景音乐恢复播放。
    if (_wasPlaying && !isPlaying) {
      AudioSessionReleaser.release();
    }
    _wasPlaying = isPlaying;

    if (!mounted) {
      return;
    }
    if (controller != null &&
        controller.value.hasError &&
        !_failed) {
      setState(() => _failed = true);
      return;
    }
    setState(() {});
  }

  Future<void> _togglePlay(Uri safeUri) async {
    final existing = _controller;
    if (existing != null && existing.value.isInitialized) {
      if (existing.value.isPlaying) {
        await existing.pause();
      } else {
        // Replay from start once playback has reached the end.
        if (existing.value.position >= existing.value.duration &&
            existing.value.duration > Duration.zero) {
          await existing.seekTo(Duration.zero);
        }
        await existing.play();
      }
      return;
    }

    if (_initializing) {
      return;
    }
    setState(() {
      _initializing = true;
      _failed = false;
    });

    // 命中本地缓存直接播缓存文件；未命中才走网络（tailnet 自签 HTTPS →
    // 本机 loopback 反代，绕开原生播放器栈的证书校验）。
    String? cachedPath;
    try {
      cachedPath = await cachedMediaPath(safeUri);
    } catch (_) {
      cachedPath = null;
    }
    Uri playbackUri = safeUri;
    if (cachedPath == null) {
      playbackUri = await rewriteTailnetMediaUrl(safeUri);
    }
    if (!mounted) {
      setState(() => _initializing = false);
      return;
    }
    final controller = createMediaPlayerController(
      playbackUri,
      cachedPath: cachedPath,
    );
    try {
      await controller.initialize();
      controller.addListener(_onControllerUpdate);
      if (cachedPath == null) {
        // 首次播放走网络流的同时后台入缓存，下次播放不再下载。
        prefetchMediaToCache(safeUri);
      }
      if (!mounted) {
        await controller.dispose();
        return;
      }
      setState(() {
        _controller = controller;
        _initializing = false;
      });
      await controller.play();
    } catch (_) {
      await controller.dispose();
      if (!mounted) {
        return;
      }
      setState(() {
        _initializing = false;
        _failed = true;
      });
    }
  }

  String _formatDuration(Duration d) {
    final totalSeconds = d.inSeconds;
    final minutes = (totalSeconds ~/ 60).toString();
    final seconds = (totalSeconds % 60).toString().padLeft(2, '0');
    return '$minutes:$seconds';
  }

  @override
  Widget build(BuildContext context) {
    if (widget.src.isEmpty) {
      return const SizedBox.shrink();
    }
    final safeUri = ChatMarkdownUriPolicy.resolveSafeAudioUri(widget.src);
    if (safeUri == null) {
      return Text(widget.src);
    }

    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final controller = _controller;
    final value = controller?.value;
    final isInitialized = value?.isInitialized ?? false;
    final isPlaying = value?.isPlaying ?? false;
    final duration = value?.duration ?? Duration.zero;
    final position = value?.position ?? Duration.zero;

    final hasDuration = duration > Duration.zero;
    final positionMs = position.inMilliseconds
        .clamp(0, hasDuration ? duration.inMilliseconds : 0)
        .toDouble();
    final maxMs = hasDuration ? duration.inMilliseconds.toDouble() : 1.0;

    final accent = scheme.primary;
    final onAccent = scheme.onPrimary;

    final Widget leading;
    if (_initializing) {
      leading = SizedBox(
        width: 36,
        height: 36,
        child: Padding(
          padding: const EdgeInsets.all(8),
          child: CircularProgressIndicator(strokeWidth: 2, color: accent),
        ),
      );
    } else {
      leading = Material(
        color: _failed ? scheme.error : accent,
        shape: const CircleBorder(),
        child: InkWell(
          customBorder: const CircleBorder(),
          onTap: () => _togglePlay(safeUri),
          child: SizedBox(
            width: 36,
            height: 36,
            child: Icon(
              _failed
                  ? Icons.refresh_rounded
                  : (isPlaying
                      ? Icons.pause_rounded
                      : Icons.play_arrow_rounded),
              color: onAccent,
              size: 22,
            ),
          ),
        ),
      );
    }

    final timeLabel = _failed
        ? '加载失败'
        : (isInitialized
            ? '${_formatDuration(position)} / ${_formatDuration(duration)}'
            : (widget.title?.trim().isNotEmpty == true
                ? widget.title!.trim()
                : '音频'));

    final progress = SliderTheme(
      data: SliderTheme.of(context).copyWith(
        trackHeight: 3,
        activeTrackColor: accent,
        inactiveTrackColor: scheme.outlineVariant,
        thumbColor: accent,
        thumbShape: const RoundSliderThumbShape(enabledThumbRadius: 6),
        overlayShape: const RoundSliderOverlayShape(overlayRadius: 12),
        trackShape: const RoundedRectSliderTrackShape(),
      ),
      child: Slider(
        value: isInitialized ? positionMs.clamp(0, maxMs) : 0,
        max: maxMs,
        onChanged: (isInitialized && hasDuration)
            ? (v) => controller!.seekTo(Duration(milliseconds: v.round()))
            : null,
      ),
    );

    return Container(
      width: _width,
      margin: widget.inline
          ? const EdgeInsets.symmetric(vertical: 2)
          : EdgeInsets.zero,
      padding: const EdgeInsets.fromLTRB(8, 6, 12, 6),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest.withValues(alpha: 0.6),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: scheme.outlineVariant.withValues(alpha: 0.5)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          leading,
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SizedBox(height: 24, child: progress),
                Padding(
                  padding: const EdgeInsets.only(left: 4),
                  child: Text(
                    timeLabel,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: _failed
                          ? scheme.error
                          : scheme.onSurfaceVariant,
                      fontFeatures: const [FontFeature.tabularFigures()],
                    ),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
