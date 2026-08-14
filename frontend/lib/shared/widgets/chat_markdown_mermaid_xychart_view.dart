import 'dart:math' as math;

import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';

import '../mermaid/chat_mermaid_model.dart';

class ChatMarkdownMermaidXyChartView extends StatelessWidget {
  const ChatMarkdownMermaidXyChartView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidXyChartDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  static const _barPalette = <Color>[
    Color(0xFF0F766E),
    Color(0xFF1D4ED8),
    Color(0xFFB45309),
    Color(0xFF166534),
    Color(0xFF9A3412),
    Color(0xFF7C3AED),
    Color(0xFFDB2777),
    Color(0xFF0891B2),
    Color(0xFF65A30D),
    Color(0xFFDC2626),
  ];

  @override
  Widget build(BuildContext context) {
    // Flatten all series for rendering
    final allBarValues = [
      for (final s in diagram.barSeries) ...s,
    ];
    final allLineValues = [
      for (final s in diagram.lineSeries) ...s,
    ];
    final barCount = allBarValues.length;
    if (barCount == 0 && allLineValues.isEmpty) {
      return const SizedBox.shrink();
    }

    final brightness = ThemeData.estimateBrightnessForColor(backgroundColor);
    final isDark = brightness == Brightness.dark;
    final surfaceColor = isDark
        ? Colors.white.withValues(alpha: 0.04)
        : Colors.white.withValues(alpha: 0.9);
    final borderColor =
        (textStyle.color ?? const Color(0xFF2A2214)).withValues(alpha: 0.18);
    final labelColor =
        textStyle.color?.withValues(alpha: 0.72) ?? const Color(0xFF666666);
    final gridColor = isDark
        ? Colors.white.withValues(alpha: 0.08)
        : Colors.black.withValues(alpha: 0.06);
    final chartWidth = (barCount * 48.0).clamp(200.0, 500.0);

    final barGroups = <BarChartGroupData>[];
    for (var i = 0; i < barCount; i++) {
      barGroups.add(
        BarChartGroupData(
          x: i,
          barRods: [
            BarChartRodData(
              toY: allBarValues[i],
              fromY: diagram.yAxisMin,
              color: _barPalette[i % _barPalette.length],
              width: 20,
              borderRadius: const BorderRadius.only(
                topLeft: Radius.circular(4),
                topRight: Radius.circular(4),
              ),
            ),
          ],
        ),
      );
    }

    final yMax = diagram.yAxisMax > 0 ? diagram.yAxisMax : null;
    final yMin = diagram.yAxisMin;

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: surfaceColor,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: borderColor),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          if (diagram.title.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Text(
                diagram.title,
                style: textStyle.copyWith(
                  fontWeight: FontWeight.w700,
                  fontSize: (textStyle.fontSize ?? 13) + 1,
                ),
              ),
            ),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: SizedBox(
              width: diagram.horizontal ? 200.0 : chartWidth,
              height: diagram.horizontal ? chartWidth : 200,
              child: BarChart(
                BarChartData(
                  maxY: yMax,
                  minY: yMin,
                  alignment: BarChartAlignment.spaceAround,
                  barGroups: barGroups,
                  titlesData: FlTitlesData(
                    leftTitles: AxisTitles(
                      sideTitles: SideTitles(
                        showTitles: true,
                        reservedSize: 48,
                        getTitlesWidget: (value, meta) =>
                            diagram.horizontal
                                ? _buildXLabel(value, meta, labelColor)
                                : _buildYLabel(value, meta, labelColor, yMax),
                      ),
                    ),
                    rightTitles: const AxisTitles(
                      sideTitles: SideTitles(showTitles: false),
                    ),
                    topTitles: const AxisTitles(
                      sideTitles: SideTitles(showTitles: false),
                    ),
                    bottomTitles: AxisTitles(
                      sideTitles: SideTitles(
                        showTitles: true,
                        reservedSize: 36,
                        getTitlesWidget: (value, meta) =>
                            diagram.horizontal
                                ? _buildYLabel(value, meta, labelColor, yMax)
                                : _buildXLabel(value, meta, labelColor),
                      ),
                    ),
                  ),
                  borderData: FlBorderData(show: false),
                  gridData: FlGridData(
                    show: true,
                    drawVerticalLine: false,
                    horizontalInterval: _gridInterval(yMax ?? 0),
                    getDrawingHorizontalLine: (value) => FlLine(
                      color: gridColor,
                      strokeWidth: 1,
                    ),
                  ),
                  barTouchData: BarTouchData(
                    touchTooltipData: BarTouchTooltipData(
                      getTooltipItem: (group, groupIndex, rod, rodIndex) {
                        final label = groupIndex < diagram.xAxisLabels.length
                            ? diagram.xAxisLabels[groupIndex]
                            : '';
                        return BarTooltipItem(
                          '$label\n${_formatNumber(rod.toY)}',
                          TextStyle(
                            color: Colors.white,
                            fontSize: (textStyle.fontSize ?? 13) - 1,
                            fontWeight: FontWeight.w600,
                          ),
                        );
                      },
                    ),
                  ),
                ),
              ),
            ),
          ),
          if (diagram.yAxisTitle.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Text(
                diagram.yAxisTitle,
                style: textStyle.copyWith(
                  fontSize: (textStyle.fontSize ?? 13) - 2,
                  color: labelColor,
                ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildXLabel(double value, TitleMeta meta, Color color) {
    final index = value.toInt();
    if (index < 0 || index >= diagram.xAxisLabels.length) {
      return const SizedBox.shrink();
    }
    return Padding(
      padding: const EdgeInsets.only(top: 6),
      child: Text(
        diagram.xAxisLabels[index],
        style: textStyle.copyWith(
          fontSize: (textStyle.fontSize ?? 13) - 2,
          color: color,
        ),
        overflow: TextOverflow.ellipsis,
        maxLines: 1,
      ),
    );
  }

  Widget _buildYLabel(
    double value,
    TitleMeta meta,
    Color color,
    double? yMax,
  ) {
    final interval = _gridInterval(yMax ?? 0);
    if (interval > 0 && (value % interval).abs() > 0.01 && value > 0) {
      return const SizedBox.shrink();
    }
    return Text(
      _formatNumber(value),
      style: textStyle.copyWith(
        fontSize: (textStyle.fontSize ?? 13) - 2,
        color: color,
      ),
    );
  }

  double _gridInterval(double yMax) {
    if (yMax <= 0) return 0;
    const targetLines = 5;
    var raw = yMax / targetLines;
    final magnitude = _pow10(raw < 1 ? 0 : (math.log(raw) / math.ln10).floor());
    raw = (raw / magnitude).ceilToDouble() * magnitude;
    return raw;
  }

  double _pow10(int n) {
    var result = 1.0;
    for (var i = 0; i < n.abs(); i++) {
      result *= 10;
    }
    return n < 0 ? 1.0 / result : result;
  }

  String _formatNumber(double value) {
    if (value == 0) return '0';
    if (value >= 10000) {
      final wan = value / 10000;
      return wan == wan.roundToDouble()
          ? '${wan.toInt()}万'
          : '${wan.toStringAsFixed(1)}万';
    }
    if (value == value.roundToDouble()) {
      return value.toInt().toString();
    }
    return value.toStringAsFixed(1);
  }
}
