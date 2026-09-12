import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../utils/chat_draft_index.dart';

/// 会话草稿徽标：任一会话存在未发送文字草稿时展示"草稿"标记。
///
/// 供主会话列表、用户资料页会话列表等多处复用；内部订阅
/// [ChatDraftIndex.version]，草稿变化时自动刷新。无草稿时占位为零尺寸。
class SessionDraftBadge extends StatelessWidget {
  const SessionDraftBadge({super.key, required this.sessionIds});

  /// 列表项关联的会话 id 集合（主列表的分组项可能聚合多个会话）。
  final List<String> sessionIds;

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
            padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
            decoration: BoxDecoration(
              color: secondary.withValues(alpha: 0.12),
              borderRadius: BorderRadius.circular(999),
            ),
            child: Text(
              'conversations_draft_badge'.tr,
              style: TextStyle(
                color: secondary.withValues(alpha: 0.85),
                fontSize: 10,
                fontWeight: FontWeight.w600,
                height: 1,
              ),
            ),
          ),
        ],
      );
    });
  }
}
