import 'dart:async';

import 'package:get/get.dart';

/// 应用内全局通知事件数据
class InAppNotification {
  const InAppNotification({
    required this.sessionId,
    required this.sessionType,
    required this.title,
    required this.summary,
  });

  final String sessionId;
  final String sessionType;
  final String title;
  final String summary;
}

/// 全局卡片消息拦截通知服务
///
/// 当非当前会话收到需要紧急提醒的卡片消息时，通过此服务触发顶部横幅通知。
class InAppNotificationService extends GetxService {
  final currentNotification = Rxn<InAppNotification>();

  Timer? _autoDismissTimer;

  static const _autoDismissDuration = Duration(seconds: 6);

  Future<InAppNotificationService> init() async => this;

  /// 显示一条通知横幅
  void show({
    required String sessionId,
    required String sessionType,
    required String title,
    required String summary,
  }) {
    _autoDismissTimer?.cancel();
    currentNotification.value = InAppNotification(
      sessionId: sessionId,
      sessionType: sessionType,
      title: title,
      summary: summary,
    );
    _autoDismissTimer = Timer(_autoDismissDuration, dismiss);
  }

  /// 关闭当前通知
  void dismiss() {
    _autoDismissTimer?.cancel();
    currentNotification.value = null;
  }

  @override
  void onClose() {
    _autoDismissTimer?.cancel();
    super.onClose();
  }
}
