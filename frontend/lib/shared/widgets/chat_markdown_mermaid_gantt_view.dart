import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../mermaid/chat_mermaid_model.dart';
import 'chat_markdown_mermaid_zoomable_viewport.dart';

class ChatMarkdownMermaidGanttView extends StatefulWidget {
  const ChatMarkdownMermaidGanttView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
    this.zoomController,
    this.exportBoundaryKey,
  });

  final ChatMermaidGanttDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;
  final ChatMarkdownMermaidZoomController? zoomController;
  final GlobalKey? exportBoundaryKey;

  static const double _labelColumnWidth = 208;
  static const double _dayCellWidth = 72;
  static const double _headerHeight = 64;
  static const double _sectionHeight = 38;
  static const double _taskRowHeight = 56;
  static const double _cardBorderWidth = 1;

  @override
  State<ChatMarkdownMermaidGanttView> createState() =>
      _ChatMarkdownMermaidGanttViewState();
}

class _ChatMarkdownMermaidGanttViewState
    extends State<ChatMarkdownMermaidGanttView> {
  static const double _minScale = 0.8;
  static const double _maxScale = 2.2;
  static const double _zoomStep = 0.1;

  late final ScrollController _horizontalScrollController;
  double _zoomScale = _minScale;

  ChatMermaidGanttDiagram get diagram => widget.diagram;
  TextStyle get textStyle => widget.textStyle;
  Color get backgroundColor => widget.backgroundColor;

  @override
  void initState() {
    super.initState();
    _horizontalScrollController = ScrollController();
    _syncZoomController();
  }

  @override
  void didUpdateWidget(covariant ChatMarkdownMermaidGanttView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.zoomController != widget.zoomController) {
      oldWidget.zoomController?.unbind();
    }
    _syncZoomController();
  }

  @override
  void dispose() {
    widget.zoomController?.unbind();
    _horizontalScrollController.dispose();
    super.dispose();
  }

  void _zoomBy(double delta) {
    final targetScale = (_zoomScale + delta).clamp(_minScale, _maxScale);
    if ((targetScale - _zoomScale).abs() < 0.001) {
      return;
    }
    setState(() {
      _zoomScale = targetScale.toDouble();
    });
    _syncZoomController();
  }

  void _syncZoomController() {
    widget.zoomController?.bind(
      currentScale: _zoomScale,
      minScale: _minScale,
      maxScale: _maxScale,
      onZoomIn: () => _zoomBy(_zoomStep),
      onZoomOut: () => _zoomBy(-_zoomStep),
    );
  }

  @override
  Widget build(BuildContext context) {
    final dateFormatter = DateFormat(_datePattern(diagram.axisFormat));
    final scale = _zoomScale;
    final labelColumnWidth =
        ChatMarkdownMermaidGanttView._labelColumnWidth * scale;
    final baseDayCellWidth = ChatMarkdownMermaidGanttView._dayCellWidth * scale;
    final headerHeight = ChatMarkdownMermaidGanttView._headerHeight * scale;
    final sectionHeight = ChatMarkdownMermaidGanttView._sectionHeight * scale;
    final taskRowHeight = ChatMarkdownMermaidGanttView._taskRowHeight * scale;
    final totalDays = diagram.rangeEndExclusive
        .difference(diagram.rangeStart)
        .inDays;
    final taskCount = diagram.sections.fold<int>(
      0,
      (count, section) => count + section.tasks.length,
    );
    final contentHeight =
        headerHeight +
        (diagram.sections.length * sectionHeight) +
        (taskCount * taskRowHeight);
    final viewportHeight = math.min(contentHeight, 420).toDouble();
    final background = backgroundColor;
    final surface = _resolveSurfaceColor(background);
    final sectionFill = _resolveSectionFill(background);
    final borderColor = _resolveBorderColor(textStyle.color);

    return LayoutBuilder(
      builder: (context, constraints) {
        final desiredTimelineWidth = totalDays * baseDayCellWidth;
        final desiredContentWidth =
            labelColumnWidth +
            desiredTimelineWidth +
            (ChatMarkdownMermaidGanttView._cardBorderWidth * 2);
        final availableWidth = constraints.maxWidth;
        final needsMinorTightening =
            availableWidth.isFinite &&
            desiredContentWidth > availableWidth &&
            (desiredContentWidth - availableWidth) <= 8 &&
            totalDays > 0;
        final dayCellWidth = needsMinorTightening
            ? ((availableWidth -
                          (ChatMarkdownMermaidGanttView._cardBorderWidth * 2)) -
                      labelColumnWidth) /
                  totalDays
            : baseDayCellWidth;
        final timelineWidth = totalDays * dayCellWidth;

        return SizedBox(
          width: double.infinity,
          height: viewportHeight,
          child: SingleChildScrollView(
            child: Scrollbar(
              controller: _horizontalScrollController,
              scrollbarOrientation: ScrollbarOrientation.bottom,
              thumbVisibility: true,
              trackVisibility: true,
              interactive: true,
              thickness: 8,
              radius: const Radius.circular(999),
              child: SingleChildScrollView(
                controller: _horizontalScrollController,
                scrollDirection: Axis.horizontal,
                child: UnconstrainedBox(
                  constrainedAxis: Axis.vertical,
                  alignment: Alignment.topLeft,
                  child: RepaintBoundary(
                    key: widget.exportBoundaryKey,
                    child: Container(
                      width:
                          labelColumnWidth +
                          timelineWidth +
                          (ChatMarkdownMermaidGanttView._cardBorderWidth * 2),
                      decoration: BoxDecoration(
                        color: surface,
                        borderRadius: BorderRadius.circular(16),
                        border: Border.all(
                          color: borderColor.withValues(alpha: 0.18),
                        ),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          _buildHeader(
                            dateFormatter: dateFormatter,
                            totalDays: totalDays,
                            scale: scale,
                            labelColumnWidth: labelColumnWidth,
                            headerHeight: headerHeight,
                            dayCellWidth: dayCellWidth,
                            borderColor: borderColor,
                          ),
                          for (final section in diagram.sections) ...[
                            Container(
                              width: double.infinity,
                              height: sectionHeight,
                              padding: EdgeInsets.symmetric(
                                horizontal: 14 * scale,
                                vertical: 8 * scale,
                              ),
                              decoration: BoxDecoration(
                                color: sectionFill,
                                border: Border(
                                  top: BorderSide(
                                    color: borderColor.withValues(alpha: 0.12),
                                  ),
                                  bottom: BorderSide(
                                    color: borderColor.withValues(alpha: 0.12),
                                  ),
                                ),
                              ),
                              alignment: Alignment.centerLeft,
                              child: Text(
                                section.title,
                                style: textStyle.copyWith(
                                  fontWeight: FontWeight.w700,
                                  fontSize:
                                      ((textStyle.fontSize ?? 13) + 0.5) *
                                      scale,
                                ),
                              ),
                            ),
                            for (final task in section.tasks)
                              _buildTaskRow(
                                task: task,
                                totalDays: totalDays,
                                scale: scale,
                                labelColumnWidth: labelColumnWidth,
                                taskRowHeight: taskRowHeight,
                                dayCellWidth: dayCellWidth,
                                borderColor: borderColor,
                                barColor: _taskBarColor(section.order),
                                barTextColor: _taskBarTextColor(section.order),
                              ),
                          ],
                        ],
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        );
      },
    );
  }

  Widget _buildHeader({
    required DateFormat dateFormatter,
    required int totalDays,
    required double scale,
    required double labelColumnWidth,
    required double headerHeight,
    required double dayCellWidth,
    required Color borderColor,
  }) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Container(
          width: labelColumnWidth,
          height: headerHeight,
          padding: EdgeInsets.fromLTRB(
            14 * scale,
            12 * scale,
            14 * scale,
            10 * scale,
          ),
          decoration: BoxDecoration(
            border: Border(
              right: BorderSide(color: borderColor.withValues(alpha: 0.12)),
              bottom: BorderSide(color: borderColor.withValues(alpha: 0.12)),
            ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (diagram.title.isNotEmpty)
                Text(
                  diagram.title,
                  style: textStyle.copyWith(
                    fontWeight: FontWeight.w700,
                    fontSize: ((textStyle.fontSize ?? 13) + 1) * scale,
                  ),
                ),
              Text(
                '${dateFormatter.format(diagram.rangeStart)} - '
                '${dateFormatter.format(diagram.rangeEndExclusive.subtract(const Duration(days: 1)))}',
                style: textStyle.copyWith(
                  fontSize: ((textStyle.fontSize ?? 13) - 1) * scale,
                  color: textStyle.color?.withValues(alpha: 0.72),
                ),
              ),
            ],
          ),
        ),
        for (var dayIndex = 0; dayIndex < totalDays; dayIndex += 1)
          Container(
            width: dayCellWidth,
            height: headerHeight,
            alignment: Alignment.center,
            padding: EdgeInsets.symmetric(vertical: 10 * scale),
            decoration: BoxDecoration(
              border: Border(
                left: BorderSide(color: borderColor.withValues(alpha: 0.08)),
                bottom: BorderSide(color: borderColor.withValues(alpha: 0.12)),
              ),
            ),
            child: Text(
              dateFormatter.format(
                diagram.rangeStart.add(Duration(days: dayIndex)),
              ),
              style: textStyle.copyWith(
                fontWeight: FontWeight.w600,
                fontSize: ((textStyle.fontSize ?? 13) - 1) * scale,
              ),
            ),
          ),
      ],
    );
  }

  Widget _buildTaskRow({
    required ChatMermaidGanttTask task,
    required int totalDays,
    required double scale,
    required double labelColumnWidth,
    required double taskRowHeight,
    required double dayCellWidth,
    required Color borderColor,
    required Color barColor,
    required Color barTextColor,
  }) {
    final startOffsetDays = task.startDate
        .difference(diagram.rangeStart)
        .inDays;
    final left = (startOffsetDays * dayCellWidth) + (6 * scale);
    final width = (task.durationDays * dayCellWidth) - (12 * scale);
    final subtitle =
        '${_formatTaskDate(task.startDate)} · ${task.durationDays}d';

    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Container(
          width: labelColumnWidth,
          padding: EdgeInsets.fromLTRB(
            14 * scale,
            10 * scale,
            14 * scale,
            10 * scale,
          ),
          decoration: BoxDecoration(
            border: Border(
              right: BorderSide(color: borderColor.withValues(alpha: 0.12)),
              bottom: BorderSide(color: borderColor.withValues(alpha: 0.08)),
            ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                task.label,
                style: textStyle.copyWith(
                  fontWeight: FontWeight.w600,
                  fontSize: ((textStyle.fontSize ?? 13) - 1) * scale,
                ),
              ),
              SizedBox(height: 2 * scale),
              Text(
                subtitle,
                style: textStyle.copyWith(
                  fontSize: ((textStyle.fontSize ?? 13) - 1) * scale,
                  color: textStyle.color?.withValues(alpha: 0.7),
                ),
              ),
            ],
          ),
        ),
        SizedBox(
          width: totalDays * dayCellWidth,
          height: taskRowHeight,
          child: Stack(
            children: [
              Row(
                children: [
                  for (var index = 0; index < totalDays; index += 1)
                    Container(
                      width: dayCellWidth,
                      decoration: BoxDecoration(
                        border: Border(
                          left: BorderSide(
                            color: borderColor.withValues(alpha: 0.08),
                          ),
                          bottom: BorderSide(
                            color: borderColor.withValues(alpha: 0.08),
                          ),
                        ),
                      ),
                    ),
                ],
              ),
              Positioned(
                left: left,
                top: 10 * scale,
                child: Container(
                  width: width.clamp(24, double.infinity).toDouble(),
                  height: 36 * scale,
                  padding: EdgeInsets.symmetric(horizontal: 10 * scale),
                  decoration: BoxDecoration(
                    color: barColor,
                    borderRadius: BorderRadius.circular(12 * scale),
                    border: Border.all(color: barColor.withValues(alpha: 0.82)),
                    boxShadow: [
                      BoxShadow(
                        color: barColor.withValues(alpha: 0.16),
                        blurRadius: 12 * scale,
                        offset: Offset(0, 4 * scale),
                      ),
                    ],
                  ),
                  alignment: Alignment.centerLeft,
                  child: Text(
                    task.id ?? '${task.durationDays}d',
                    style: textStyle.copyWith(
                      color: barTextColor,
                      fontWeight: FontWeight.w700,
                      fontSize: ((textStyle.fontSize ?? 13) - 1) * scale,
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }

  String _datePattern(String axisFormat) {
    switch (axisFormat) {
      case '%Y-%m-%d':
        return 'yyyy-MM-dd';
      case '%m-%d':
        return 'MM-dd';
    }
    return 'MM-dd';
  }

  String _formatTaskDate(DateTime date) =>
      DateFormat('yyyy-MM-dd').format(date);

  Color _resolveSurfaceColor(Color background) {
    final brightness = ThemeData.estimateBrightnessForColor(background);
    return brightness == Brightness.dark
        ? Colors.white.withValues(alpha: 0.04)
        : Colors.white.withValues(alpha: 0.9);
  }

  Color _resolveSectionFill(Color background) {
    final brightness = ThemeData.estimateBrightnessForColor(background);
    return brightness == Brightness.dark
        ? Colors.white.withValues(alpha: 0.03)
        : const Color(0xFFF0F5FF);
  }

  Color _resolveBorderColor(Color? textColor) =>
      (textColor ?? const Color(0xFF2A2214)).withValues(alpha: 0.86);

  Color _taskBarColor(int sectionOrder) {
    const palette = <Color>[
      Color(0xFF0F766E),
      Color(0xFF1D4ED8),
      Color(0xFFB45309),
      Color(0xFF166534),
      Color(0xFF9A3412),
    ];
    return palette[sectionOrder % palette.length];
  }

  Color _taskBarTextColor(int sectionOrder) {
    final barColor = _taskBarColor(sectionOrder);
    final brightness = ThemeData.estimateBrightnessForColor(barColor);
    return brightness == Brightness.dark
        ? Colors.white
        : const Color(0xFF111827);
  }
}
