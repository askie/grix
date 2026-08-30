import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/data/services/link_safety_service.dart';

/// LinkInterstitial 安全中间页：可疑级别可显式放行。
/// 恶意（黑名单）链接由调用方直接静默不响应，不经中间页。
/// 始终展示真实落地域名，防显示文本与 href 不一致的伪装。
class LinkInterstitial {
  /// 可疑：黄屏，主操作"返回"，次操作"仍要访问"。
  /// 返回 true 表示用户选择继续访问。
  static Future<bool> showWarning(String url, LinkVerdict v) async {
    final r = await Get.dialog<bool>(
      _Sheet(
        url: url,
        verdict: v,
        accent: Colors.amber.shade800,
        icon: Icons.warning_amber_outlined,
        title: 'link_safety_warning_title'.tr,
        body: 'link_safety_warning_body'.tr,
        primary: _Action('link_safety_back'.tr, false),
        secondary: _Action('link_safety_proceed'.tr, true),
      ),
      barrierDismissible: false,
    );
    return r ?? false;
  }
}

class _Action {
  const _Action(this.label, this.result);
  final String label;
  final bool result;
}

class _Sheet extends StatelessWidget {
  const _Sheet({
    required this.url,
    required this.verdict,
    required this.accent,
    required this.icon,
    required this.title,
    required this.body,
    required this.primary,
    required this.secondary,
  });

  final String url;
  final LinkVerdict verdict;
  final Color accent;
  final IconData icon;
  final String title;
  final String body;
  final _Action primary;
  final _Action? secondary;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isDark = theme.brightness == Brightness.dark;
    final realHost = verdict.canonicalHost.isNotEmpty
        ? verdict.canonicalHost
        : _hostOf(url);
    return Dialog(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(icon, color: accent, size: 28),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    title,
                    style: TextStyle(
                      fontSize: 18,
                      fontWeight: FontWeight.w600,
                      color: accent,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            Text(body),
            const SizedBox(height: 16),
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
              decoration: BoxDecoration(
                color: isDark ? AppTheme.darkInput : Colors.grey.shade100,
                borderRadius: BorderRadius.circular(8),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'link_safety_real_host'.tr,
                    style: TextStyle(
                      fontSize: 12,
                      color: isDark
                          ? AppTheme.darkTextSecondary
                          : Colors.grey.shade600,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    realHost,
                    style: const TextStyle(
                      fontSize: 14,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 24),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                if (secondary != null) ...[
                  TextButton(
                    onPressed: () => Get.back(result: secondary!.result),
                    style: TextButton.styleFrom(
                      foregroundColor: isDark
                          ? AppTheme.darkTextSecondary
                          : Colors.grey.shade600,
                    ),
                    child: Text(secondary!.label),
                  ),
                  const SizedBox(width: 8),
                ],
                ElevatedButton(
                  onPressed: () => Get.back(result: primary.result),
                  style: ElevatedButton.styleFrom(
                    backgroundColor: accent,
                    foregroundColor: Colors.white,
                  ),
                  child: Text(primary.label),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  static String _hostOf(String raw) {
    return Uri.tryParse(raw)?.host ?? raw;
  }
}
