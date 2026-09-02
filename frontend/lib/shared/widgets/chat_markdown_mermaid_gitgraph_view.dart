import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';
import 'chat_markdown_mermaid_zoomable_viewport.dart';

class ChatMarkdownMermaidGitGraphView extends StatelessWidget {
  const ChatMarkdownMermaidGitGraphView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
    this.zoomController,
    this.exportBoundaryKey,
  });

  final ChatMermaidGitGraphDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;
  final ChatMarkdownMermaidZoomController? zoomController;
  final GlobalKey? exportBoundaryKey;

  static const double _commitSpacing = 56;
  static const double _laneWidth = 36;
  static const double _commitRadius = 8;
  static const double _leftPadding = 16;
  static const double _topPadding = 40;

  @override
  Widget build(BuildContext context) {
    final branchLanes = <String, int>{};
    for (var i = 0; i < diagram.branches.length; i++) {
      branchLanes[diagram.branches[i]] = i;
    }

    final canvasWidth =
        _leftPadding +
        (diagram.branches.length * _laneWidth) +
        160; // extra for labels
    final canvasHeight =
        _topPadding + (diagram.commits.length * _commitSpacing) + 32;
    final viewportHeight = math.min(canvasHeight, 400.0);

    final edgeColor = _resolveEdgeColor(textStyle.color);
    final isDark =
        ThemeData.estimateBrightnessForColor(backgroundColor) ==
        Brightness.dark;

    return ChatMarkdownMermaidZoomableViewport(
      viewportHeight: viewportHeight,
      canvasSize: Size(canvasWidth, canvasHeight),
      zoomController: zoomController,
      exportBoundaryKey: exportBoundaryKey,
      minScale: 0.6,
      maxScale: 2.5,
      showControls: false,
      controlsFillColor: isDark
          ? const Color(0xFF111827).withValues(alpha: 0.92)
          : Colors.white.withValues(alpha: 0.96),
      controlsBorderColor: edgeColor.withValues(alpha: 0.2),
      controlsIconColor: edgeColor,
      child: CustomPaint(
        painter: _GitGraphPainter(
          diagram: diagram,
          branchLanes: branchLanes,
          textStyle: textStyle,
          edgeColor: edgeColor,
          isDark: isDark,
        ),
        size: Size(canvasWidth, canvasHeight),
      ),
    );
  }

  Color _resolveEdgeColor(Color? textColor) =>
      (textColor ?? const Color(0xFF2A2214)).withValues(alpha: 0.88);
}

class _GitGraphPainter extends CustomPainter {
  const _GitGraphPainter({
    required this.diagram,
    required this.branchLanes,
    required this.textStyle,
    required this.edgeColor,
    required this.isDark,
  });

  final ChatMermaidGitGraphDiagram diagram;
  final Map<String, int> branchLanes;
  final TextStyle textStyle;
  final Color edgeColor;
  final bool isDark;

  static const _branchColors = <Color>[
    Color(0xFF16A34A), // main - green
    Color(0xFF2563EB), // develop - blue
    Color(0xFFF97316), // feature - orange
    Color(0xFF7C3AED), // purple
    Color(0xFFDB2777), // pink
    Color(0xFF0891B2), // cyan
    Color(0xFFDC2626), // red
    Color(0xFF65A30D), // lime
  ];

  Offset _commitPosition(int index, String branch) {
    final lane = branchLanes[branch] ?? 0;
    return Offset(
      ChatMarkdownMermaidGitGraphView._leftPadding +
          (lane * ChatMarkdownMermaidGitGraphView._laneWidth) +
          ChatMarkdownMermaidGitGraphView._laneWidth / 2,
      ChatMarkdownMermaidGitGraphView._topPadding +
          (index * ChatMarkdownMermaidGitGraphView._commitSpacing),
    );
  }

  Color _branchColor(String branch) {
    final lane = branchLanes[branch] ?? 0;
    return _branchColors[lane % _branchColors.length];
  }

  @override
  void paint(Canvas canvas, Size size) {
    // Draw branch labels at top
    for (final entry in branchLanes.entries) {
      final x =
          ChatMarkdownMermaidGitGraphView._leftPadding +
          (entry.value * ChatMarkdownMermaidGitGraphView._laneWidth) +
          ChatMarkdownMermaidGitGraphView._laneWidth / 2;
      final labelPainter = TextPainter(
        text: TextSpan(
          text: entry.key,
          style: textStyle.copyWith(
            fontSize: (textStyle.fontSize ?? 13) - 2,
            fontWeight: FontWeight.w700,
            color: _branchColor(entry.key),
          ),
        ),
        textDirection: TextDirection.ltr,
        maxLines: 1,
      )..layout();
      labelPainter.paint(canvas, Offset(x - labelPainter.width / 2, 8));
    }

    // Draw lane lines (vertical)
    final lastCommitByBranch = <String, int>{};
    for (var i = 0; i < diagram.commits.length; i++) {
      lastCommitByBranch[diagram.commits[i].branch] = i;
    }

    // Draw connections between consecutive commits on same branch
    final prevCommitOnBranch = <String, int>{};
    for (var i = 0; i < diagram.commits.length; i++) {
      final commit = diagram.commits[i];
      final pos = _commitPosition(i, commit.branch);

      // Connect to previous commit on same branch
      final prevIdx = prevCommitOnBranch[commit.branch];
      if (prevIdx != null) {
        final prevPos = _commitPosition(prevIdx, commit.branch);
        canvas.drawLine(
          prevPos,
          pos,
          Paint()
            ..color = _branchColor(commit.branch).withValues(alpha: 0.5)
            ..strokeWidth = 2.4
            ..strokeCap = StrokeCap.round,
        );
      }
      prevCommitOnBranch[commit.branch] = i;

      // Draw merge line
      if (commit.mergeFrom != null) {
        // Find the last commit on the source branch before this commit
        int? sourceIdx;
        for (var j = i - 1; j >= 0; j--) {
          if (diagram.commits[j].branch == commit.mergeFrom) {
            sourceIdx = j;
            break;
          }
        }
        if (sourceIdx != null) {
          final sourcePos = _commitPosition(sourceIdx, commit.mergeFrom!);
          final mergeColor = _branchColor(
            commit.mergeFrom!,
          ).withValues(alpha: 0.4);
          final path = Path()
            ..moveTo(sourcePos.dx, sourcePos.dy)
            ..cubicTo(
              sourcePos.dx,
              sourcePos.dy +
                  ChatMarkdownMermaidGitGraphView._commitSpacing * 0.5,
              pos.dx,
              pos.dy - ChatMarkdownMermaidGitGraphView._commitSpacing * 0.5,
              pos.dx,
              pos.dy,
            );
          canvas.drawPath(
            path,
            Paint()
              ..color = mergeColor
              ..style = PaintingStyle.stroke
              ..strokeWidth = 2
              ..strokeCap = StrokeCap.round,
          );
        }
      }
    }

    // Draw commit nodes and labels
    for (var i = 0; i < diagram.commits.length; i++) {
      final commit = diagram.commits[i];
      final pos = _commitPosition(i, commit.branch);
      final commitColor = _branchColor(commit.branch);

      // Commit circle
      canvas.drawCircle(
        pos,
        ChatMarkdownMermaidGitGraphView._commitRadius,
        Paint()
          ..color = commitColor
          ..style = PaintingStyle.fill,
      );
      canvas.drawCircle(
        pos,
        ChatMarkdownMermaidGitGraphView._commitRadius,
        Paint()
          ..color = isDark ? Colors.white.withValues(alpha: 0.3) : Colors.white
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2,
      );

      // Tag label
      if (commit.tag != null) {
        final tagPainter = TextPainter(
          text: TextSpan(
            text: commit.tag,
            style: textStyle.copyWith(
              fontSize: (textStyle.fontSize ?? 13) - 3,
              fontWeight: FontWeight.w700,
              color: commitColor,
            ),
          ),
          textDirection: TextDirection.ltr,
          maxLines: 1,
        )..layout();

        final tagX =
            pos.dx +
            (diagram.branches.length *
                ChatMarkdownMermaidGitGraphView._laneWidth) -
            pos.dx +
            ChatMarkdownMermaidGitGraphView._leftPadding +
            16;
        final tagRect = RRect.fromRectAndRadius(
          Rect.fromLTWH(
            tagX - 2,
            pos.dy - tagPainter.height / 2 - 3,
            tagPainter.width + 10,
            tagPainter.height + 6,
          ),
          const Radius.circular(4),
        );
        canvas.drawRRect(
          tagRect,
          Paint()
            ..color = commitColor.withValues(alpha: 0.12)
            ..style = PaintingStyle.fill,
        );
        canvas.drawRRect(
          tagRect,
          Paint()
            ..color = commitColor.withValues(alpha: 0.4)
            ..style = PaintingStyle.stroke
            ..strokeWidth = 1,
        );
        tagPainter.paint(
          canvas,
          Offset(tagX + 3, pos.dy - tagPainter.height / 2),
        );
      }

      // Commit ID (small, next to commit)
      final idPainter = TextPainter(
        text: TextSpan(
          text: commit.mergeFrom != null
              ? 'merge ${commit.mergeFrom}'
              : commit.id,
          style: textStyle.copyWith(
            fontSize: (textStyle.fontSize ?? 13) - 3,
            color: edgeColor.withValues(alpha: 0.5),
          ),
        ),
        textDirection: TextDirection.ltr,
        maxLines: 1,
      )..layout();
      idPainter.paint(
        canvas,
        Offset(
          pos.dx + ChatMarkdownMermaidGitGraphView._commitRadius + 6,
          pos.dy - idPainter.height / 2,
        ),
      );
    }
  }

  @override
  bool shouldRepaint(covariant _GitGraphPainter oldDelegate) {
    return oldDelegate.diagram != diagram || oldDelegate.edgeColor != edgeColor;
  }
}
