import 'dart:async';

import 'package:flutter/material.dart';

/// 三点轮转等待指示器。
///
/// 每 300ms 高亮下一个圆点（约 3.3fps），由 [Timer] 阶梯式驱动；不再使用每帧
/// 60fps 的 [AnimationController] 与 [AnimatedOpacity]。原因：动画的渲染成本
/// 主要来自帧产出（软渲染下 raster 线程每帧做全窗口合成），而非动画内容本身，
/// 因此降低帧率是决定性优化（60fps → 3.3fps，约 18 倍）。
///
/// 外层包 [RepaintBoundary] 将动画限定在独立合成层，避免动画期间触发祖先
/// （消息气泡/列表）的连锁重绘——这是 AI 长时间处理时手机持续发热的主要来源。
class StreamPendingIndicator extends StatefulWidget {
  final Color color;
  final double dotSize;
  final double spacing;

  const StreamPendingIndicator({
    super.key,
    required this.color,
    this.dotSize = 6,
    this.spacing = 4,
  });

  @override
  State<StreamPendingIndicator> createState() => _StreamPendingIndicatorState();
}

class _StreamPendingIndicatorState extends State<StreamPendingIndicator> {
  static const _stepDuration = Duration(milliseconds: 300);

  Timer? _timer;
  int _activeIndex = 0;

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(_stepDuration, (_) {
      if (!mounted) return;
      setState(() {
        _activeIndex = (_activeIndex + 1) % 3;
      });
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return RepaintBoundary(
      child: SizedBox(
        key: const ValueKey('stream_pending_indicator'),
        width: widget.dotSize * 3 + widget.spacing * 2,
        height: widget.dotSize,
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: List.generate(3, (index) {
            return Padding(
              padding: EdgeInsets.only(right: index == 2 ? 0 : widget.spacing),
              child: Container(
                width: widget.dotSize,
                height: widget.dotSize,
                decoration: BoxDecoration(
                  color: widget.color.withValues(
                    alpha: index == _activeIndex ? 1 : 0.28,
                  ),
                  shape: BoxShape.circle,
                ),
              ),
            );
          }),
        ),
      ),
    );
  }
}
