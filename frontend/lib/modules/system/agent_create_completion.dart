import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/gateway_service.dart';
import '../../data/providers/session_service.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/agent_session_list/agent_session_list.dart';
import '../../shared/widgets/app_dialog_style.dart';
import '../chat/services/chat_route_navigator.dart';

/// 建完 agent 之后的收尾，桌面端（本机 127 admin API）与手机端（ws 转发到
/// 目标主机的连接器）唯一共用的一段：
///
/// 1. 支持中转的类型自动接入 Grix 中转（开专属虚拟 Key + 下发配置给 connector），
///    用户不用手填任何供应商网址和 Key；不支持的类型（Gemini/Cursor 等）跳过——
///    这些工具绑自己的账号/BYOK，不支持自定义端点。
/// 2. 让会话列表失效并打开该 agent 的最新会话。
///
/// 两端建 agent 的前半段不同，这一段必须只有一份实现。
Future<void> configureAndOpenNewAgent({
  required String agentId,
  required String agentName,
  required String clientType,
}) async {
  if (GatewayService.supportedClientTypes.contains(clientType)) {
    final gatewayService = Get.isRegistered<GatewayService>()
        ? Get.find<GatewayService>()
        : Get.put(GatewayService());
    final configured = await gatewayService.configureAgentProvider(agentId);
    if (!configured) {
      CustomToast.show('system_gateway_configure_failed'.tr);
    }
  }

  AgentSessionList.invalidateCache();
  final sessionId = await Get.find<SessionService>().openLatestSession(
    agentId,
    2,
  );
  if (sessionId != null && sessionId.isNotEmpty) {
    ChatRouteNavigator.toChat(
      sessionId: sessionId,
      title: agentName,
      type: 'private',
    );
  }
}

/// 给新 agent 起一个不重名的默认名字：`<clientType>-<n>`。
/// [usedNames] 是当前可见的同主机/同连接器 agent 名字，用来避开重名。
String defaultAgentNameFor({
  required String clientType,
  required Iterable<String> usedNames,
  required int sameTypeCount,
}) {
  final used = usedNames.map((name) => name.trim()).toSet();
  for (var i = sameTypeCount + 1; i < 1000; i++) {
    final candidate = '$clientType-$i';
    if (!used.contains(candidate)) return candidate;
  }
  return '$clientType-${DateTime.now().millisecondsSinceEpoch}';
}

/// 弹出「给新 agent 起名」对话框，返回 trim 过的名字；取消返回 null。
Future<String?> promptNewAgentName({
  required BuildContext context,
  required String typeLabel,
  required String initialName,
}) async {
  final nameCtrl = TextEditingController(text: initialName);
  final nameFocus = FocusNode();
  var disposed = false;
  final nameNotEmpty = nameCtrl.text.trim().isNotEmpty.obs;
  nameCtrl.addListener(
    () => nameNotEmpty.value = nameCtrl.text.trim().isNotEmpty,
  );

  final future = showAppDialog<String>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Text('system_add_type_agent'.trParams({'label': typeLabel})),
      content: SizedBox(
        width: resolveDialogConstraints(
          ctx,
          size: AppDialogSize.standard,
        ).maxWidth,
        child: TextField(
          controller: nameCtrl,
          focusNode: nameFocus,
          decoration: InputDecoration(labelText: 'system_name'.tr),
          textInputAction: TextInputAction.done,
          onSubmitted: (value) {
            final name = value.trim();
            if (name.isEmpty) return;
            Navigator.pop(ctx, name);
          },
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(ctx),
          child: Text('common_cancel'.tr),
        ),
        Obx(
          () => FilledButton(
            onPressed: nameNotEmpty.value
                ? () => Navigator.pop(ctx, nameCtrl.text.trim())
                : null,
            child: Text('system_create'.tr),
          ),
        ),
      ],
    ),
  ).whenComplete(() {
    disposed = true;
    nameCtrl.dispose();
    nameFocus.dispose();
  });

  WidgetsBinding.instance.addPostFrameCallback((_) {
    if (disposed || !nameFocus.canRequestFocus) return;
    nameFocus.requestFocus();
    nameCtrl.selection = TextSelection(
      baseOffset: 0,
      extentOffset: nameCtrl.text.length,
    );
  });

  return future;
}
