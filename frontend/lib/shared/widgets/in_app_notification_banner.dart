import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../modules/chat/services/chat_route_navigator.dart';
import '../services/in_app_notification_service.dart';

/// 全局卡片消息通知横幅
///
/// 在 grix_app builder 的 Stack 中通过 Positioned 定位使用。
class InAppNotificationBanner extends StatelessWidget {
  const InAppNotificationBanner({super.key});

  @override
  Widget build(BuildContext context) {
    final service = Get.find<InAppNotificationService>();
    final notification = service.currentNotification.value!;

    return GestureDetector(
      onTap: () {
        service.dismiss();
        ChatRouteNavigator.toChat(
          sessionId: notification.sessionId,
          title: notification.title,
          type: notification.sessionType,
        );
      },
      onHorizontalDragEnd: (_) => service.dismiss(),
      child: Material(
        color: Colors.transparent,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
          decoration: BoxDecoration(
            color: const Color(0xFF1A73E8),
            borderRadius: BorderRadius.circular(10),
            boxShadow: [
              BoxShadow(
                color: Colors.black.withValues(alpha: 0.2),
                blurRadius: 12,
                offset: const Offset(0, 4),
              ),
            ],
          ),
          child: Row(
            children: [
              const Icon(Icons.approval_rounded, size: 18, color: Colors.white),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      notification.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        color: Colors.white,
                        fontSize: 13,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    if (notification.summary.isNotEmpty) ...[
                      const SizedBox(height: 2),
                      Text(
                        notification.summary,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          color: Colors.white70,
                          fontSize: 11,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
              const SizedBox(width: 8),
              const Icon(Icons.chevron_right, size: 18, color: Colors.white70),
            ],
          ),
        ),
      ),
    );
  }
}
