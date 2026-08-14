import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../app/theme/app_palette.dart';
import '../../shared/widgets/admin_scaffold.dart';
import 'dashboard_controller.dart';
import 'dashboard_models.dart';

class DashboardView extends GetView<DashboardController> {
  const DashboardView({super.key});

  @override
  Widget build(BuildContext context) {
    return AdminScaffold(
      title: '首页',
      actions: [
        Obx(
          () => IconButton(
            tooltip: '刷新',
            onPressed: controller.loading.value ? null : controller.loadStats,
            icon: controller.loading.value
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.refresh),
          ),
        ),
      ],
      body: Obx(() {
        final stats = controller.stats.value;
        if (stats == null && controller.loading.value) {
          return const Center(child: CircularProgressIndicator());
        }
        if (stats == null) {
          return Center(
            child: FilledButton.icon(
              onPressed: controller.loadStats,
              icon: const Icon(Icons.refresh),
              label: const Text('重新加载'),
            ),
          );
        }
        return RefreshIndicator(
          onRefresh: controller.loadStats,
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              _MetricGrid(stats: stats),
              const SizedBox(height: 16),
              _RegistrationChart(items: stats.dailyRegistrants),
            ],
          ),
        );
      }),
    );
  }
}

class _MetricGrid extends StatelessWidget {
  const _MetricGrid({required this.stats});

  final DashboardStats stats;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final columns = constraints.maxWidth >= 900
            ? 3
            : constraints.maxWidth >= 620
            ? 2
            : 1;
        return GridView.count(
          crossAxisCount: columns,
          crossAxisSpacing: 12,
          mainAxisSpacing: 12,
          childAspectRatio: columns == 1 ? 3.8 : 2.4,
          shrinkWrap: true,
          physics: const NeverScrollableScrollPhysics(),
          children: [
            _MetricCard(
              label: '用户注册量',
              value: stats.totalUsers,
              icon: Icons.person_add_alt_1_outlined,
              color: const Color(0xFF2563EB),
            ),
            _MetricCard(
              label: '当前在线用户',
              value: stats.onlineUsers,
              icon: Icons.sensors_outlined,
              color: const Color(0xFF059669),
              onTap: () => Get.toNamed(AppRoutes.onlineUsers),
            ),
            _MetricCard(
              label: '在线 Agent',
              value: stats.onlineAgents,
              icon: Icons.smart_toy_outlined,
              color: const Color(0xFF7C3AED),
            ),
          ],
        );
      },
    );
  }
}

class _MetricCard extends StatelessWidget {
  const _MetricCard({
    required this.label,
    required this.value,
    required this.icon,
    required this.color,
    this.onTap,
  });

  final String label;
  final int value;
  final IconData icon;
  final Color color;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(12),
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: Row(
            children: [
              Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: color.withValues(alpha: 0.12),
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Icon(icon, color: color),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  mainAxisAlignment: MainAxisAlignment.center,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(label, style: theme.textTheme.bodyMedium),
                    const SizedBox(height: 4),
                    Text(
                      _formatInt(value),
                      style: theme.textTheme.headlineSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                        color: AppPalette.textPrimary,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _RegistrationChart extends StatelessWidget {
  const _RegistrationChart({required this.items});

  final List<DailyRegistrationStat> items;

  @override
  Widget build(BuildContext context) {
    final maxCount = items.fold<int>(0, (m, e) => e.count > m ? e.count : m);
    final maxY = maxCount <= 0 ? 1.0 : (maxCount * 1.25).ceilToDouble();
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(18, 18, 18, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Text('按天注册量', style: Theme.of(context).textTheme.titleMedium),
                const Spacer(),
                Text(
                  '近 ${items.length} 天',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
            const SizedBox(height: 18),
            SizedBox(
              height: 280,
              child: BarChart(
                BarChartData(
                  maxY: maxY,
                  minY: 0,
                  gridData: FlGridData(
                    show: true,
                    drawVerticalLine: false,
                    getDrawingHorizontalLine: (value) => FlLine(
                      color: Theme.of(
                        context,
                      ).dividerColor.withValues(alpha: 0.5),
                      strokeWidth: 1,
                    ),
                  ),
                  borderData: FlBorderData(show: false),
                  barTouchData: BarTouchData(
                    enabled: true,
                    touchTooltipData: BarTouchTooltipData(
                      getTooltipItem: (group, groupIndex, rod, rodIndex) {
                        final item = items[group.x.toInt()];
                        return BarTooltipItem(
                          '${item.date}\n${item.count}',
                          const TextStyle(
                            color: Colors.white,
                            fontWeight: FontWeight.w600,
                          ),
                        );
                      },
                    ),
                  ),
                  titlesData: FlTitlesData(
                    topTitles: const AxisTitles(
                      sideTitles: SideTitles(showTitles: false),
                    ),
                    rightTitles: const AxisTitles(
                      sideTitles: SideTitles(showTitles: false),
                    ),
                    leftTitles: AxisTitles(
                      sideTitles: SideTitles(
                        showTitles: true,
                        reservedSize: 42,
                        getTitlesWidget: (value, meta) {
                          if (value == 0 || value == meta.max) {
                            return const SizedBox.shrink();
                          }
                          return Text(
                            value.toInt().toString(),
                            style: Theme.of(context).textTheme.labelSmall,
                          );
                        },
                      ),
                    ),
                    bottomTitles: AxisTitles(
                      sideTitles: SideTitles(
                        showTitles: true,
                        reservedSize: 32,
                        getTitlesWidget: (value, meta) {
                          final i = value.toInt();
                          if (i < 0 || i >= items.length || i.isOdd) {
                            return const SizedBox.shrink();
                          }
                          final date = items[i].date;
                          final label = date.length >= 10
                              ? date.substring(5)
                              : date;
                          return Padding(
                            padding: const EdgeInsets.only(top: 8),
                            child: Text(
                              label,
                              style: Theme.of(context).textTheme.labelSmall,
                            ),
                          );
                        },
                      ),
                    ),
                  ),
                  barGroups: [
                    for (var i = 0; i < items.length; i++)
                      BarChartGroupData(
                        x: i,
                        barRods: [
                          BarChartRodData(
                            toY: items[i].count.toDouble(),
                            width: 14,
                            borderRadius: const BorderRadius.vertical(
                              top: Radius.circular(4),
                            ),
                            color: Theme.of(context).colorScheme.primary,
                          ),
                        ],
                      ),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

String _formatInt(int value) {
  final s = value.toString();
  final buffer = StringBuffer();
  for (var i = 0; i < s.length; i++) {
    if (i > 0 && (s.length - i) % 3 == 0) {
      buffer.write(',');
    }
    buffer.write(s[i]);
  }
  return buffer.toString();
}
