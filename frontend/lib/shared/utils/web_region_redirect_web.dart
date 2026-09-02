import 'package:web/web.dart' as web;

import 'app_region_config.dart';

/// Web implementation: redirects the browser to the target region's root URL
/// when the current page is on a different production domain.
///
/// Returns true if a redirect was initiated (page is navigating away).
/// Returns false if already on the correct domain or on a non-production host.
bool redirectToRegionIfNeeded(AppRegion region) {
  final currentHost = web.window.location.hostname.toLowerCase().trim();
  final cnHost = _hostOf(kCnApiUrl);
  final globalHost = _hostOf(kGlobalApiUrl);

  // Only redirect from known production domains; leave local dev alone.
  final isProduction = currentHost == cnHost || currentHost == globalHost;
  if (!isProduction) return false;

  final targetHost = region == AppRegion.cn ? cnHost : globalHost;
  if (currentHost == targetHost) return false;

  // 注入的区域地址是带 /v1 的接口地址（如 https://grix.dhf.pub/v1）；整页跳转
  // 必须只取域名根，否则会跳到接口路径 /v1 导致 404 白屏。
  web.window.location.href = regionWebRootUrl(region);
  return true;
}

String _hostOf(String url) =>
    (Uri.tryParse(url.trim())?.host ?? '').toLowerCase();
