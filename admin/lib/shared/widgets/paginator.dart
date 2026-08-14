import 'package:flutter/material.dart';

/// 分页控制条：显示总数与页码，提供上一页/下一页。
class Paginator extends StatelessWidget {
  const Paginator({
    super.key,
    required this.page,
    required this.pageSize,
    required this.total,
    required this.onPageChanged,
  });

  final int page;
  final int pageSize;
  final int total;
  final ValueChanged<int> onPageChanged;

  int get totalPages {
    if (total <= 0) return 1;
    return ((total + pageSize - 1) ~/ pageSize).clamp(1, 1 << 30);
  }

  @override
  Widget build(BuildContext context) {
    final hasPrev = page > 1;
    final hasNext = page < totalPages;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.end,
        children: [
          Text('共 $total 条', style: Theme.of(context).textTheme.bodySmall),
          const SizedBox(width: 16),
          IconButton(
            tooltip: '上一页',
            onPressed: hasPrev ? () => onPageChanged(page - 1) : null,
            icon: const Icon(Icons.chevron_left),
          ),
          Text('$page / $totalPages'),
          IconButton(
            tooltip: '下一页',
            onPressed: hasNext ? () => onPageChanged(page + 1) : null,
            icon: const Icon(Icons.chevron_right),
          ),
        ],
      ),
    );
  }
}
