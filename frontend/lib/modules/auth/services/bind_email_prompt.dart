// 手机号注册的账号没有邮箱，登录后引导补绑的弹窗入口。
//
// 与绑定手机号的引导不同：邮箱是找回账号的唯一凭据，所以「暂不」只在本次运行内生效，
// 不写本地永久静默；只要还没绑上，下次冷启动仍会再弹一次。
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../../data/providers/auth_service.dart';
import '../widgets/bind_email_dialog.dart';

/// 本次进程内是否已经弹过，避免同一次运行里反复打扰。
bool _promptedThisRun = false;

@visibleForTesting
void resetBindEmailPromptForTest() => _promptedThisRun = false;

/// 启动到 home 后调用一次：若当前账号还没绑邮箱就弹一次引导。
/// 不阻塞 home 主流程；任何异常都吞掉（属于增强引导）。
Future<void> maybePromptBindEmail() async {
  try {
    if (_promptedThisRun) return;
    if (!Get.isRegistered<AuthService>()) return;
    final auth = Get.find<AuthService>();
    final user = auth.user;
    if (user == null) return;
    if (user.hasEmail) return;
    if (user.id.trim().isEmpty) return;

    final ctx = Get.context;
    if (ctx == null || !ctx.mounted) return;

    _promptedThisRun = true;
    await showBindEmailDialog(ctx);
  } catch (e, st) {
    debugPrint('bind email prompt error: $e\n$st');
  }
}
