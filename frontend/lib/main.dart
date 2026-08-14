import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:sentry_flutter/sentry_flutter.dart';

import 'app/bootstrap/app_bootstrap.dart';
import 'app/profile/instance_profile_bootstrap.dart';
import 'shared/utils/tailnet_https_trust.dart';

const _sentryDsn = String.fromEnvironment('SENTRY_DSN');

void main(List<String> args) async {
  WidgetsFlutterBinding.ensureInitialized();
  // 桌面端多实例 profile：解析实例身份、抢占运行锁、迁移旧全局凭证。
  // 同 profile 重复启动时会前台化已有实例并在内部结束本进程。
  await bootstrapInstanceProfile(args);
  // 信任宿主机 tailnet HTTPS 文件服务的自签证书（仅限 tailnet 段，详见实现）。
  // 让上传/下载/图片预览都能直接走 https，用户无需手动安装证书。
  installTailnetHttpsTrust();
  // 正式版关闭 debugPrint：避免热路径（消息下行/流式 chunk 等）中的字符串
  // 拼接与 IO 在 release 包里持续消耗 CPU。
  if (kReleaseMode) {
    debugPrint = (String? message, {int? wrapWidth}) {};
  }
  await _preloadChineseUiFont();

  if (_sentryDsn.isNotEmpty) {
    await SentryFlutter.init((options) {
      options.dsn = _sentryDsn;
      options.tracesSampleRate = kDebugMode ? 1.0 : 0.2;
    }, appRunner: () => runApp(const AppBootstrap()));
  } else {
    runApp(const AppBootstrap());
  }
}

/// Preloads the GrixUiZh font so it is available to the text engine before
/// the first frame. Without this, CJK glyphs briefly render as tofu boxes
/// (□□□) because the font data loads asynchronously after it has been
/// registered from the pubspec.yaml `fonts:` declaration.
Future<void> _preloadChineseUiFont() async {
  try {
    const fontFamily = 'GrixUiZh';
    const fontAssetPath = 'assets/fonts/grix_ui_zh_subset.ttf';
    final loader = FontLoader(fontFamily);
    loader.addFont(rootBundle.load(fontAssetPath));
    await loader.load();
  } catch (e) {
    debugPrint('Chinese font preload skipped (non-fatal): $e');
  }
}
