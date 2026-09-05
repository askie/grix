import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/agent_service.dart';
import '../../data/providers/im_service.dart';
import '../../data/providers/session_service.dart';
import '../../shared/utils/toast_util.dart';
import '../chat/services/chat_route_navigator.dart';

/// 把一件手机端做不了的事交给机器上的 agent：打开与它的最新会话，
/// 再以主人身份把说明发进去。
///
/// 远程安装失败后的求助（remote_agent_install_sheet）和只有 hermes 时的
/// 装连接器引导（agents_view）走的是同一条路，差别只在消息正文。
///
/// 调用方一般已经关掉了自己的弹窗，这里再抛异常就没有 UI 能承接，
/// 所以失败只 toast 并返回 false。
Future<bool> openAgentSessionAndSend({
  required AgentModel agent,
  required String message,
}) async {
  try {
    final sessionId = await Get.find<SessionService>().openLatestSession(
      agent.id,
      2,
    );
    if (sessionId == null || sessionId.isEmpty) {
      CustomToast.show(
        'remote_install_help_open_failed'.trParams({'agent': agent.agentName}),
      );
      return false;
    }
    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: sessionId,
        title: agent.agentName,
        type: 'private',
      ),
    );
    await Get.find<ImService>().sendMessage(message, sessionId);
    return true;
  } catch (e) {
    CustomToast.show(
      'remote_install_help_open_failed'.trParams({'agent': agent.agentName}),
    );
    debugPrint('agent session handoff failed: $e');
    return false;
  }
}
