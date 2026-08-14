// 老 email 用户登录后引导绑定手机号的弹窗。
// 弹一次后用户选择"暂不"则本地永久记忆该用户 ID（不跨账号），不再骚扰；
// 选择"绑定"则跳 /phone-login?mode=bind。
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../../app/routes/app_routes.dart';
import '../../../data/providers/auth_service.dart';
import '../../../shared/utils/app_region_config.dart';
import '../../../shared/widgets/app_dialog_style.dart';

const _bindPromptDismissedKeyPrefix = 'auth.bind_phone_prompt_dismissed.';

/// 启动到 home 后调用一次：若用户未绑手机号且未明确忽略过，弹一次引导。
/// 不阻塞 home 主流程；任何异常都吞掉（属于增强引导）。
Future<void> maybePromptBindPhone() async {
  try {
    if (!Get.isRegistered<AuthService>()) return;
    final auth = Get.find<AuthService>();
    final user = auth.user;
    if (user == null) return;
    if (user.hasPhone) return;
    final userId = user.id.trim();
    if (userId.isEmpty) return;

    final prefs = await SharedPreferences.getInstance();
    final dismissed = prefs.getBool('$_bindPromptDismissedKeyPrefix$userId');
    if (dismissed == true) return;

    final region = await resolveInitialRegion();
    final methodsResult = await auth.fetchAuthMethods(region: region.name);
    if (!methodsResult.ok || !methodsResult.data!.phoneLoginEnabled) {
      return;
    }

    final ctx = Get.context;
    if (ctx == null || !ctx.mounted) return;

    final confirmed = await showAppConfirmDialog(
      context: ctx,
      title: 'phone_bind_prompt_title'.tr,
      message: 'phone_bind_prompt_body'.tr,
      confirmText: 'phone_bind_prompt_now'.tr,
      cancelText: 'phone_bind_prompt_later'.tr,
    );
    if (confirmed) {
      await Get.toNamed(AppRoutes.phoneLogin, arguments: {'mode': 'bind'});
    } else {
      // 用户未确认绑定（暂不/关闭）：本地永久记忆，不再骚扰
      await prefs.setBool('$_bindPromptDismissedKeyPrefix$userId', true);
    }
  } catch (e, st) {
    debugPrint('bind phone prompt error: $e\n$st');
  }
}
