import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/themes/app_theme.dart';
import '../utils/chat_draft_index.dart';

/// 会话草稿徽标：任一会话存在未发送文字草稿时展示"草稿"标记。
///
/// 供主会话列表、用户资料页会话列表等多处复用；内部订阅
/// [ChatDraftIndex.version]，草稿变化时自动刷新。无草稿时占位为零尺寸。
///
/// 资料页标题行旁边还有灰色时间，默认的浅色胶囊容易被淹没，
/// 那里传 [emphasized] 用实心警示色。
class SessionDraftBadge extends StatelessWidget {
  const SessionDraftBadge({
    super.key,
    required this.sessionIds,
    this.emphasized = false,
  });

  /// 列表项关联的会话 id 集合（主列表的分组项可能聚合多个会话）。
  final List<String> sessionIds;

  /// 实心警示胶囊，用于资料页等容易和次要文字混在一起的位置。
  final bool emphasized;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      ChatDraftIndex.version.value;
      final hasDraft = sessionIds.any(ChatDraftIndex.hasDraft);
      if (!hasDraft) {
        return const SizedBox.shrink();
      }
      final secondary = Theme.of(context).colorScheme.secondary;
      return Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          const SizedBox(width: 6),
          Container(
            padding: EdgeInsets.symmetric(
              horizontal: emphasized ? 8 : 7,
              vertical: emphasized ? 4 : 3,
            ),
            decoration: BoxDecoration(
              color: emphasized
                  ? AppTheme.warningColor
                  : secondary.withValues(alpha: 0.12),
              borderRadius: BorderRadius.circular(999),
            ),
            child: Text(
              'conversations_draft_badge'.tr,
              style: TextStyle(
                color: emphasized
                    ? AppTheme.lightTextPrimary
                    : secondary.withValues(alpha: 0.85),
                fontSize: emphasized ? 12 : 10,
                fontWeight: emphasized ? FontWeight.w700 : FontWeight.w600,
                height: 1,
              ),
            ),
          ),
        ],
      );
    });
  }
}
