import 'package:flutter/material.dart';

/// 通用筛选 BottomSheet：在窄屏下由筛选按钮触发弹出。
///
/// [title] — sheet 标题
/// [content] — 筛选控件区域（由调用方提供）
/// [activeCount] — 当前已激活的筛选条件数量，用于角标
/// [onReset] — 重置按钮回调
class FilterBottomSheet extends StatelessWidget {
  const FilterBottomSheet({
    super.key,
    required this.title,
    required this.content,
    this.activeCount = 0,
    this.onReset,
  });

  final String title;
  final Widget content;
  final int activeCount;
  final VoidCallback? onReset;

  /// 弹出筛选 BottomSheet。
  static Future<void> show(
    BuildContext context, {
    required String title,
    required Widget content,
    int activeCount = 0,
    VoidCallback? onReset,
  }) {
    return showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => FilterBottomSheet(
        title: title,
        content: content,
        activeCount: activeCount,
        onReset: onReset,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        // 顶栏：标题 + 重置按钮
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 16, 8, 0),
          child: Row(
            children: [
              Text(title,
                  style: Theme.of(context).textTheme.titleMedium),
              const Spacer(),
              if (onReset != null)
                TextButton(
                  onPressed: () {
                    onReset!();
                    Navigator.of(context).pop();
                  },
                  child: const Text('重置'),
                ),
              const SizedBox(width: 4),
              IconButton(
                onPressed: () => Navigator.of(context).pop(),
                icon: const Icon(Icons.close),
              ),
            ],
          ),
        ),
        const Divider(height: 1),
        // 筛选内容
        Padding(
          padding: const EdgeInsets.all(16),
          child: content,
        ),
        const SizedBox(height: 16),
      ],
    );
  }
}

/// 筛选图标按钮，带角标显示已激活筛选数量。
class FilterBadgeIcon extends StatelessWidget {
  const FilterBadgeIcon({
    super.key,
    required this.onTap,
    this.activeCount = 0,
  });

  final VoidCallback onTap;
  final int activeCount;

  @override
  Widget build(BuildContext context) {
    return Stack(
      clipBehavior: Clip.none,
      children: [
        IconButton(
          onPressed: onTap,
          icon: const Icon(Icons.filter_list),
          tooltip: '筛选',
        ),
        if (activeCount > 0)
          Positioned(
            right: 4,
            top: 4,
            child: Container(
              padding: const EdgeInsets.all(4),
              decoration: BoxDecoration(
                color: Theme.of(context).colorScheme.primary,
                shape: BoxShape.circle,
              ),
              constraints: const BoxConstraints(minWidth: 16, minHeight: 16),
              child: Text(
                '$activeCount',
                style: TextStyle(
                  color: Theme.of(context).colorScheme.onPrimary,
                  fontSize: 10,
                  fontWeight: FontWeight.bold,
                ),
                textAlign: TextAlign.center,
              ),
            ),
          ),
      ],
    );
  }
}
