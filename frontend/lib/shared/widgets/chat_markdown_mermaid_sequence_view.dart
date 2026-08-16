import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../mermaid/chat_mermaid_model.dart';
import '../mermaid/chat_mermaid_sequence_layout.dart';
import 'chat_markdown_mermaid_zoomable_viewport.dart';

class ChatMarkdownMermaidSequenceView extends StatelessWidget {
  const ChatMarkdownMermaidSequenceView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
    this.zoomController,
    this.exportBoundaryKey,
  });

  final ChatMermaidSequenceDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;
  final ChatMarkdownMermaidZoomController? zoomController;
  final GlobalKey? exportBoundaryKey;

  static const ChatMermaidSequenceLayoutEngine _layoutEngine =
      ChatMermaidSequenceLayoutEngine();

  @override
  Widget build(BuildContext context) {
    final textDirection = Directionality.of(context);
    final participantStyle = textStyle.copyWith(
      fontWeight: FontWeight.w700,
      fontSize: (textStyle.fontSize ?? 13) - 2,
    );
    final messageStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 2,
    );
    final noteStyle = textStyle.copyWith(
      fontSize: (textStyle.fontSize ?? 13) - 1,
      height: 1.25,
    );
    final groupStyle = textStyle.copyWith(
      fontWeight: FontWeight.w700,
      fontSize: (textStyle.fontSize ?? 13) - 1,
    );

    final layout = _layoutEngine.layout(
      diagram: diagram,
      participantStyle: participantStyle,
      messageStyle: messageStyle,
      noteStyle: noteStyle,
      groupStyle: groupStyle,
      textDirection: textDirection,
    );
    final viewportHeight =
        math.max(1, math.min(layout.canvasSize.height, 420)).toDouble();

    return Semantics(
      container: true,
      label: _semanticLabel(),
      child: ChatMarkdownMermaidZoomableViewport(
        viewportHeight: viewportHeight,
        canvasSize: layout.canvasSize,
        zoomController: zoomController,
        exportBoundaryKey: exportBoundaryKey,
        minScale: 0.75,
        maxScale: 2.4,
        showControls: false,
        controlsFillColor:
            ThemeData.estimateBrightnessForColor(backgroundColor) ==
                    Brightness.dark
                ? const Color(0xFF111827).withValues(alpha: 0.92)
                : Colors.white.withValues(alpha: 0.96),
        controlsBorderColor: (textStyle.color ?? const Color(0xFF2A2214))
            .withValues(alpha: 0.22),
        controlsIconColor: (textStyle.color ?? const Color(0xFF2A2214))
            .withValues(alpha: 0.88),
        child: CustomPaint(
          painter: _ChatMermaidSequencePainter(
            layout: layout,
            textStyle: textStyle,
            participantStyle: participantStyle,
            messageStyle: messageStyle,
            noteStyle: noteStyle,
            groupStyle: groupStyle,
            backgroundColor: backgroundColor,
          ),
        ),
      ),
    );
  }

  String _semanticLabel() {
    final buffer = StringBuffer('chat_mermaid_sequence_label'.tr);
    for (final participant in diagram.participants) {
      buffer.write(' participant ${participant.label}');
    }
    for (final event in diagram.events) {
      switch (event) {
        case ChatMermaidSequenceMessage():
          buffer.write(' message ${event.label}');
          break;
        case ChatMermaidSequenceNote():
          buffer.write(' note ${event.text}');
          break;
        case ChatMermaidSequenceGroupStart():
          buffer.write(' group ${event.label}');
          break;
        case ChatMermaidSequenceGroupDivider():
          buffer.write(' divider ${event.label}');
          break;
        case ChatMermaidSequenceGroupEnd():
          break;
      }
    }
    return buffer.toString();
  }
}

class _ChatMermaidSequencePainter extends CustomPainter {
  const _ChatMermaidSequencePainter({
    required this.layout,
    required this.textStyle,
    required this.participantStyle,
    required this.messageStyle,
    required this.noteStyle,
    required this.groupStyle,
    required this.backgroundColor,
  });

  final ChatMermaidSequenceLayout layout;
  final TextStyle textStyle;
  final TextStyle participantStyle;
  final TextStyle messageStyle;
  final TextStyle noteStyle;
  final TextStyle groupStyle;
  final Color backgroundColor;

  @override
  void paint(Canvas canvas, Size size) {
    final edgeColor = (textStyle.color ?? Colors.white).withValues(alpha: 0.88);
    final participantFill = _surfaceFill();
    final noteFill = const Color(0xFFFDE68A).withValues(alpha: 0.92);
    const noteBorder = Color(0xFFCA8A04);

    _paintGroups(canvas, edgeColor);
    _paintParticipants(canvas, participantFill, edgeColor);
    _paintLifelines(canvas, edgeColor.withValues(alpha: 0.38));
    _paintEvents(canvas, edgeColor, noteFill, noteBorder);
  }

  void _paintParticipants(Canvas canvas, Color fill, Color borderColor) {
    for (final participant in layout.participants) {
      final rect = participant.rect;
      final rrect = RRect.fromRectAndRadius(rect, const Radius.circular(12));
      canvas.drawRRect(
        rrect,
        Paint()
          ..color = fill
          ..style = PaintingStyle.fill,
      );
      canvas.drawRRect(
        rrect,
        Paint()
          ..color = borderColor
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.2,
      );
      _paintCenteredTextInRect(
        canvas: canvas,
        text: participant.participant.label,
        style: participantStyle,
        rect: Rect.fromLTWH(
          rect.left + 10,
          rect.top + 6,
          rect.width - 20,
          rect.height - 12,
        ),
        maxLines: 2,
      );
    }
  }

  void _paintCenteredTextInRect({
    required Canvas canvas,
    required String text,
    required TextStyle style,
    required Rect rect,
    required int maxLines,
  }) {
    final painter = TextPainter(
      text: TextSpan(text: text, style: style),
      textDirection: TextDirection.ltr,
      textAlign: TextAlign.center,
      maxLines: maxLines,
      ellipsis: '...',
    )..layout(minWidth: rect.width, maxWidth: rect.width);
    final top = rect.top + math.max(0, (rect.height - painter.height) / 2);
    painter.paint(canvas, Offset(rect.left, top));
  }

  void _paintLifelines(Canvas canvas, Color color) {
    final paint = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.0;
    for (final participant in layout.participants) {
      final x = participant.centerX;
      _drawDashedLine(
        canvas,
        Offset(x, layout.headerBottom),
        Offset(x, layout.lifelineBottom),
        paint,
      );
    }
  }

  void _paintGroups(Canvas canvas, Color borderColor) {
    final fillBase = _surfaceFill().withValues(alpha: 0.18);
    for (final group in layout.groups) {
      final fill = fillBase.withValues(
          alpha: math.max(0.06, 0.18 - (group.depth * 0.03)));
      final border = borderColor.withValues(
          alpha: math.max(0.16, 0.32 - (group.depth * 0.04)));
      final rect = group.rect;
      final rrect = RRect.fromRectAndRadius(rect, const Radius.circular(12));
      canvas.drawRRect(
        rrect,
        Paint()
          ..color = fill
          ..style = PaintingStyle.fill,
      );
      canvas.drawRRect(
        rrect,
        Paint()
          ..color = border
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.0,
      );
      final labelRect = Rect.fromLTWH(rect.left + 10, rect.top + 6, 220, 20);
      _paintChip(
          canvas, labelRect, _groupLabel(group.kind, group.label), groupStyle);

      for (final divider in group.dividers) {
        final paint = Paint()
          ..color = borderColor.withValues(alpha: 0.28)
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1;
        _drawDashedLine(
          canvas,
          Offset(rect.left + 8, divider.y),
          Offset(rect.right - 8, divider.y),
          paint,
        );
        _paintChip(
          canvas,
          Rect.fromLTWH(rect.left + 18, divider.y - 10, 120, 20),
          divider.label,
          groupStyle,
        );
      }
    }
  }

  void _paintEvents(
    Canvas canvas,
    Color edgeColor,
    Color noteFill,
    Color noteBorder,
  ) {
    for (final eventLayout in layout.events) {
      switch (eventLayout) {
        case ChatMermaidSequenceMessageLayout():
          _paintMessage(canvas, eventLayout, edgeColor);
          break;
        case ChatMermaidSequenceNoteLayout():
          _paintNote(canvas, eventLayout, noteFill, noteBorder);
          break;
        case ChatMermaidSequenceDividerLayout():
        case ChatMermaidSequenceSpacerLayout():
          break;
      }
    }
  }

  void _paintMessage(
    Canvas canvas,
    ChatMermaidSequenceMessageLayout layoutEvent,
    Color color,
  ) {
    final event = layoutEvent.event as ChatMermaidSequenceMessage;
    final paint = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = _strokeWidth(event.style)
      ..strokeCap = StrokeCap.round;

    final labelRect = Rect.fromCenter(
      center: layoutEvent.labelCenter,
      width: math.min(
        220,
        math.max(72, _measureWidth(event.label, messageStyle) + 16),
      ),
      height: 22,
    );
    _paintChip(canvas, labelRect, event.label, messageStyle);

    if (event.isSelfMessage) {
      final start = layoutEvent.start;
      final loopWidth = 56 + (layoutEvent.selfTurn * 8);
      final bottomY = layoutEvent.centerY + 16;
      final path = Path()
        ..moveTo(start.dx, layoutEvent.centerY)
        ..lineTo(start.dx + loopWidth, layoutEvent.centerY)
        ..lineTo(start.dx + loopWidth, bottomY)
        ..lineTo(start.dx + 10, bottomY);
      _drawLineStyle(canvas, path, paint, event.style);
      _drawArrow(
        canvas: canvas,
        tip: Offset(start.dx + 10, bottomY),
        tail: Offset(start.dx + loopWidth, bottomY),
        paint: paint,
      );
      return;
    }

    final path = Path()
      ..moveTo(layoutEvent.start.dx, layoutEvent.centerY)
      ..lineTo(layoutEvent.end.dx, layoutEvent.centerY);
    _drawLineStyle(canvas, path, paint, event.style);
    _drawArrow(
      canvas: canvas,
      tip: layoutEvent.end,
      tail: layoutEvent.start,
      paint: paint,
    );
  }

  void _paintNote(
    Canvas canvas,
    ChatMermaidSequenceNoteLayout noteLayout,
    Color fill,
    Color border,
  ) {
    final path = Path()
      ..moveTo(noteLayout.rect.left + 12, noteLayout.rect.top)
      ..lineTo(noteLayout.rect.right - 10, noteLayout.rect.top)
      ..lineTo(noteLayout.rect.right, noteLayout.rect.top + 10)
      ..lineTo(noteLayout.rect.right, noteLayout.rect.bottom)
      ..lineTo(noteLayout.rect.left, noteLayout.rect.bottom)
      ..lineTo(noteLayout.rect.left, noteLayout.rect.top + 12)
      ..close();
    canvas.drawPath(
      path,
      Paint()
        ..color = fill
        ..style = PaintingStyle.fill,
    );
    canvas.drawPath(
      path,
      Paint()
        ..color = border
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1,
    );
    canvas.drawLine(
      Offset(noteLayout.rect.right - 10, noteLayout.rect.top),
      Offset(noteLayout.rect.right - 10, noteLayout.rect.top + 10),
      Paint()
        ..color = border
        ..strokeWidth = 1,
    );
    canvas.drawLine(
      Offset(noteLayout.rect.right - 10, noteLayout.rect.top + 10),
      Offset(noteLayout.rect.right, noteLayout.rect.top + 10),
      Paint()
        ..color = border
        ..strokeWidth = 1,
    );
    final event = noteLayout.event as ChatMermaidSequenceNote;
    _paintText(
      canvas,
      event.text,
      noteStyle.copyWith(color: const Color(0xFF3F2A00)),
      Rect.fromLTWH(
        noteLayout.rect.left + 10,
        noteLayout.rect.top + 8,
        noteLayout.rect.width - 20,
        noteLayout.rect.height - 16,
      ),
      TextAlign.center,
    );
  }

  void _drawLineStyle(
    Canvas canvas,
    Path path,
    Paint paint,
    ChatMermaidSequenceMessageStyle style,
  ) {
    if (style == ChatMermaidSequenceMessageStyle.dashedArrow ||
        style == ChatMermaidSequenceMessageStyle.dashedLine) {
      for (final metric in path.computeMetrics()) {
        var distance = 0.0;
        while (distance < metric.length) {
          final segment = metric.extractPath(
            distance,
            math.min(distance + 7, metric.length),
          );
          canvas.drawPath(segment, paint);
          distance += 12;
        }
      }
      return;
    }
    canvas.drawPath(path, paint);
  }

  void _drawArrow({
    required Canvas canvas,
    required Offset tip,
    required Offset tail,
    required Paint paint,
  }) {
    final direction = tip - tail;
    if (direction.distance == 0) {
      return;
    }
    final unit = direction / direction.distance;
    const length = 8.0;
    const width = 4.5;
    final base = tip - (unit * length);
    final normal = Offset(-unit.dy, unit.dx);
    final path = Path()
      ..moveTo(tip.dx, tip.dy)
      ..lineTo(base.dx + (normal.dx * width), base.dy + (normal.dy * width))
      ..lineTo(base.dx - (normal.dx * width), base.dy - (normal.dy * width))
      ..close();
    canvas.drawPath(
      path,
      Paint()
        ..color = paint.color
        ..style = PaintingStyle.fill,
    );
  }

  void _drawDashedLine(Canvas canvas, Offset start, Offset end, Paint paint) {
    final delta = end - start;
    final length = delta.distance;
    if (length == 0) {
      return;
    }
    final unit = delta / length;
    var distance = 0.0;
    while (distance < length) {
      final dashStart = start + (unit * distance);
      final dashEnd = start + (unit * math.min(distance + 6, length));
      canvas.drawLine(dashStart, dashEnd, paint);
      distance += 12;
    }
  }

  void _paintChip(Canvas canvas, Rect rect, String text, TextStyle style) {
    final rrect = RRect.fromRectAndRadius(rect, const Radius.circular(999));
    canvas.drawRRect(
      rrect,
      Paint()
        ..color = _surfaceFill().withValues(alpha: 0.94)
        ..style = PaintingStyle.fill,
    );
    canvas.drawRRect(
      rrect,
      Paint()
        ..color = (textStyle.color ?? Colors.white).withValues(alpha: 0.22)
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1,
    );
    _paintText(
      canvas,
      text,
      style,
      Rect.fromLTWH(
          rect.left + 8, rect.top + 3, rect.width - 16, rect.height - 6),
      TextAlign.center,
    );
  }

  void _paintText(
    Canvas canvas,
    String text,
    TextStyle style,
    Rect rect,
    TextAlign align,
  ) {
    final painter = TextPainter(
      text: TextSpan(text: text, style: style),
      textDirection: TextDirection.ltr,
      textAlign: align,
      maxLines: 4,
      ellipsis: '...',
    )..layout(maxWidth: rect.width);
    painter.paint(canvas, Offset(rect.left, rect.top));
  }

  double _measureWidth(String text, TextStyle style) {
    final painter = TextPainter(
      text: TextSpan(text: text, style: style),
      textDirection: TextDirection.ltr,
      maxLines: 1,
    )..layout();
    return painter.width;
  }

  double _strokeWidth(ChatMermaidSequenceMessageStyle style) {
    switch (style) {
      case ChatMermaidSequenceMessageStyle.solidArrow:
      case ChatMermaidSequenceMessageStyle.solidLine:
        return 1.8;
      case ChatMermaidSequenceMessageStyle.dashedArrow:
      case ChatMermaidSequenceMessageStyle.dashedLine:
        return 1.6;
    }
  }

  String _groupLabel(ChatMermaidSequenceGroupKind kind, String label) {
    switch (kind) {
      case ChatMermaidSequenceGroupKind.loop:
        return 'loop $label';
      case ChatMermaidSequenceGroupKind.alt:
        return 'alt $label';
      case ChatMermaidSequenceGroupKind.opt:
        return 'opt $label';
      case ChatMermaidSequenceGroupKind.par:
        return 'par $label';
      case ChatMermaidSequenceGroupKind.critical:
        return 'critical $label';
      case ChatMermaidSequenceGroupKind.breakBlock:
        return 'break $label';
    }
  }

  Color _surfaceFill() {
    final brightness = ThemeData.estimateBrightnessForColor(backgroundColor);
    return brightness == Brightness.dark
        ? Colors.white.withValues(alpha: 0.08)
        : Colors.white.withValues(alpha: 0.92);
  }

  @override
  bool shouldRepaint(covariant _ChatMermaidSequencePainter oldDelegate) {
    return oldDelegate.layout != layout ||
        oldDelegate.textStyle != textStyle ||
        oldDelegate.participantStyle != participantStyle ||
        oldDelegate.messageStyle != messageStyle ||
        oldDelegate.noteStyle != noteStyle ||
        oldDelegate.groupStyle != groupStyle ||
        oldDelegate.backgroundColor != backgroundColor;
  }
}
