import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart' hide Response;
import 'package:grix/shared/utils/app_runtime_endpoints.dart';
import 'package:grix/shared/widgets/app_dialog_style.dart';
import 'package:device_info_plus/device_info_plus.dart';
import 'package:open_filex/open_filex.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path_provider/path_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../platform/platform_capability.dart';
import '../../shared/utils/toast_util.dart';
import 'auth_service.dart';
import 'desktop_auto_updater.dart';

/// Update info returned by the server check-update API.
class AppUpdateInfo {
  const AppUpdateInfo({
    required this.hasUpdate,
    required this.force,
    this.version = '',
    this.buildNumber = 0,
    this.changelog = '',
    this.updateMethod = '',
    this.downloadUrl = '',
    this.appStoreUrl = '',
    this.fileSize = 0,
    this.sha256 = '',
  });

  final bool hasUpdate;
  final bool force;
  final String version;
  final int buildNumber;
  final String changelog;
  final String updateMethod; // download | app_store | google_play
  final String downloadUrl;
  final String appStoreUrl;
  final int fileSize;
  final String sha256;
}

/// Service that checks for app updates and presents update dialogs.
class AppUpdateService extends GetxService {
  AppUpdateService({Dio? dio})
    : _dio =
          dio ??
          Dio(
            BaseOptions(
              baseUrl: AppRuntimeEndpoints.apiBaseUrl,
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 10),
            ),
          );

  static const _lastCheckKey = 'app_update_last_check_ts';
  static const _checkInterval = Duration(hours: 24);

  final Dio _dio;
  static bool _prefsUnavailableLogged = false;

  /// Initializes the service: attaches auth interceptor and listens for login.
  Future<AppUpdateService> init() async {
    final auth = Get.find<AuthService>();
    auth.attachAuthInterceptor(_dio);

    // On login, check for update after a short delay (let home load first)
    ever(auth.isLoggedInRx, (loggedIn) {
      if (loggedIn) {
        Future.delayed(
          const Duration(seconds: 3),
          () => maybeCheckAndShowUpdate(),
        );
      }
    });

    // If already logged in, check on startup
    if (auth.isLoggedIn) {
      Future.delayed(const Duration(seconds: 3), () => checkOnStartup());
    }

    return this;
  }

  /// Checks for updates. Returns null if no update or on error.
  Future<AppUpdateInfo?> checkForUpdate() async {
    if (kIsWeb) return null; // Web doesn't support in-app updates
    // Desktop platforms (macOS/Windows) use Sparkle/WinSparkle for auto-update.
    // Skip API-based check to avoid dual update prompts.
    if (!kIsWeb && (Platform.isMacOS || Platform.isWindows)) return null;

    try {
      final packageInfo = await PackageInfo.fromPlatform();
      final platform = _currentPlatform();
      if (platform == null) return null;

      final osVersion = await _currentOsVersion();
      final response = await _dio.get(
        '/app/check-update',
        queryParameters: {
          'platform': platform,
          'version': packageInfo.version,
          'build_number': packageInfo.buildNumber,
          if (osVersion != null) 'os_version': osVersion,
        },
      );

      if (response.statusCode == 200 && response.data['code'] == 0) {
        final data = response.data['data'];
        if (data == null || data['has_update'] != true) return null;

        final latest = data['latest'] as Map<String, dynamic>? ?? {};
        return AppUpdateInfo(
          hasUpdate: true,
          force: data['force'] == true,
          version: latest['version'] as String? ?? '',
          buildNumber: latest['build_number'] as int? ?? 0,
          changelog: latest['changelog'] as String? ?? '',
          updateMethod: latest['update_method'] as String? ?? 'download',
          downloadUrl: latest['download_url'] as String? ?? '',
          appStoreUrl: latest['app_store_url'] as String? ?? '',
          fileSize: latest['file_size'] as int? ?? 0,
          sha256: latest['sha256'] as String? ?? '',
        );
      }
    } catch (e) {
      debugPrint('AppUpdateService.checkForUpdate error: $e');
    }
    return null;
  }

  /// Performs a periodic check if 24h have passed since last check.
  /// If an update is found, shows the update dialog.
  Future<void> maybeCheckAndShowUpdate() async {
    if (kIsWeb || Platform.isMacOS || Platform.isWindows) return;

    final prefs = await _safeGetPrefs();
    if (prefs == null) return;

    final lastCheck = prefs.getInt(_lastCheckKey) ?? 0;
    final now = DateTime.now().millisecondsSinceEpoch;
    if (now - lastCheck < _checkInterval.inMilliseconds) return;

    final update = await checkForUpdate();
    await prefs.setInt(_lastCheckKey, now);

    if (update != null && Get.context != null) {
      _showUpdateDialog(Get.context!, update);
    }
  }

  /// 用户主动触发的检查（关于页点版本号 / 桌面托盘菜单）。
  ///
  /// 与自动检查的两点关键区别：
  ///   1. 不受 24h 节流限制——用户明确要求了，就得真去查一次。
  ///   2. **无论结果如何都必须给反馈**。自动检查在"已是最新"时静默是对的，
  ///      但手动点击后什么都不发生，用户只会认为按钮坏了。
  ///
  /// 桌面走 Sparkle/WinSparkle（它自带有无更新的弹窗），移动端走服务端接口，
  /// Web 没有安装包的概念，直接说明即可。
  Future<void> checkForUpdateInteractive() async {
    if (kIsWeb) {
      CustomToast.show('update_check_web_unsupported'.tr, isError: false);
      return;
    }

    if (Platform.isMacOS || Platform.isWindows) {
      try {
        await Get.find<DesktopAutoUpdaterService>()
            .checkForUpdatesInteractive();
      } catch (_) {
        CustomToast.show('update_check_failed'.tr);
      }
      return;
    }

    final AppUpdateInfo? update;
    try {
      update = await checkForUpdate();
    } catch (_) {
      CustomToast.show('update_check_failed'.tr);
      return;
    }

    // 手动检查也要刷新节流时间戳，避免刚查完又被自动检查重复打扰。
    final prefs = await _safeGetPrefs();
    await prefs?.setInt(_lastCheckKey, DateTime.now().millisecondsSinceEpoch);

    if (update != null && Get.context != null) {
      _showUpdateDialog(Get.context!, update);
      return;
    }
    CustomToast.show('update_already_latest'.tr, isError: false);
  }

  /// Forces an update check on app startup (after login).
  /// Shows dialog if update found. Return value is unused by callers.
  Future<bool> checkOnStartup() async {
    if (kIsWeb || Platform.isMacOS || Platform.isWindows) return false;

    final update = await checkForUpdate();

    // Record check time
    final prefs = await _safeGetPrefs();
    if (prefs != null) {
      await prefs.setInt(_lastCheckKey, DateTime.now().millisecondsSinceEpoch);
    }

    if (update != null && Get.context != null) {
      _showUpdateDialog(Get.context!, update);
      return update.force;
    }
    return false;
  }

  void _showUpdateDialog(BuildContext context, AppUpdateInfo update) {
    showAppGetDialog(_UpdateDialog(update: update), barrierDismissible: true);
  }

  /// Resolves the current platform string for the API.
  static String? _currentPlatform() {
    if (PlatformCapability.isMacOS) return 'macos';
    if (PlatformCapability.isWindows) return 'windows';
    if (!kIsWeb && Platform.isLinux) return 'linux';
    if (defaultTargetPlatform == TargetPlatform.iOS) return 'ios';
    if (defaultTargetPlatform == TargetPlatform.android) return 'android';
    return null;
  }

  /// Returns the OS version string for the current platform, or null on error.
  static Future<String?> _currentOsVersion() async {
    try {
      final info = DeviceInfoPlugin();
      if (!kIsWeb && Platform.isIOS) {
        final ios = await info.iosInfo;
        return ios.systemVersion;
      }
      if (!kIsWeb && Platform.isAndroid) {
        final android = await info.androidInfo;
        return android.version.release;
      }
    } catch (_) {}
    return null;
  }

  Future<SharedPreferences?> _safeGetPrefs() async {
    try {
      return await SharedPreferences.getInstance();
    } on MissingPluginException catch (e) {
      _logPrefsUnavailable(e);
      return null;
    } on PlatformException catch (e) {
      _logPrefsUnavailable(e);
      return null;
    }
  }

  void _logPrefsUnavailable(Object error) {
    if (_prefsUnavailableLogged) return;
    _prefsUnavailableLogged = true;
    debugPrint('SharedPreferences unavailable for AppUpdateService: $error');
  }

  /// Reports a completed download to the server for statistics.
  static Future<void> reportDownload({
    required int buildNumber,
    required String platform,
    String? errorMsg,
    int? durationMs,
  }) async {
    try {
      final dio = Dio(
        BaseOptions(
          baseUrl: AppRuntimeEndpoints.apiBaseUrl,
          connectTimeout: const Duration(seconds: 5),
        ),
      );
      final auth = Get.find<AuthService>();
      auth.attachAuthInterceptor(dio);

      final packageInfo = await PackageInfo.fromPlatform();
      final fromBuild = int.tryParse(packageInfo.buildNumber);
      await dio.post(
        '/app/report-download',
        data: {
          'build_number': buildNumber,
          'from_build': fromBuild,
          'platform': platform,
          'error_msg': errorMsg ?? '',
          'duration_ms': durationMs ?? 0,
        },
      );
    } catch (e) {
      debugPrint('AppUpdateService.reportDownload error: $e');
    }
  }
}

/// The update dialog shown to users.
class _UpdateDialog extends StatefulWidget {
  const _UpdateDialog({required this.update});

  final AppUpdateInfo update;

  @override
  State<_UpdateDialog> createState() => _UpdateDialogState();
}

class _UpdateDialogState extends State<_UpdateDialog> {
  bool _isUpdating = false;

  AppUpdateInfo get update => widget.update;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return PopScope(
      canPop: true,
      child: AlertDialog(
        title: Text('update_available_title'.tr),
        content: SizedBox(
          width: 320,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                '${update.version} (${update.buildNumber})',
                style: theme.textTheme.titleMedium?.copyWith(
                  color: colorScheme.primary,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 12),
              if (update.changelog.isNotEmpty)
                ConstrainedBox(
                  constraints: const BoxConstraints(maxHeight: 200),
                  child: SingleChildScrollView(
                    child: Text(
                      update.changelog,
                      style: theme.textTheme.bodyMedium,
                    ),
                  ),
                ),
              if (update.fileSize > 0) ...[
                const SizedBox(height: 8),
                Text(
                  '${'update_file_size'.tr}: ${_formatFileSize(update.fileSize)}',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: Text('update_later'.tr),
          ),
          FilledButton(
            onPressed: _isUpdating ? null : () => _performUpdate(context),
            child: Text('update_now'.tr),
          ),
        ],
      ),
    );
  }

  void _performUpdate(BuildContext context) {
    if (_isUpdating) return;

    final url = _resolveUpdateUrl();
    if (url.isEmpty) {
      // No URL available — show feedback.
      // Even for force updates, if there's nothing the user can do,
      // don't trap them in the dialog. Log the issue and allow dismissal.
      CustomToast.show('update_no_url'.tr, isError: true);
      Navigator.of(context).pop();
      return;
    }

    // On Android with direct download, download APK and trigger install
    if (!kIsWeb && Platform.isAndroid && update.updateMethod == 'download') {
      _downloadAndInstallApk(context, url);
      return;
    }

    _launchUrl(url);
    // Report download for non-Android platforms
    final platform = AppUpdateService._currentPlatform() ?? '';
    if (platform.isNotEmpty) {
      AppUpdateService.reportDownload(
        buildNumber: update.buildNumber,
        platform: platform,
      );
    }
    Navigator.of(context).pop();
  }

  Future<void> _downloadAndInstallApk(BuildContext context, String url) async {
    if (_isUpdating) return;
    setState(() => _isUpdating = true);

    final stopwatch = Stopwatch()..start();
    CustomToast.show('Downloading...', isError: false);

    try {
      final dir = await getTemporaryDirectory();
      final fileName = 'grix-${update.version}-${update.buildNumber}.apk';
      final savePath = '${dir.path}/$fileName';

      // 下载重试：CDN 连接中断多为瞬时问题，失败时自动重试 1 次
      String? downloadErr;
      for (int attempt = 1; attempt <= 2; attempt++) {
        if (attempt > 1) {
          CustomToast.show('update_download_retrying'.tr, isError: false);
          try {
            File(savePath).deleteSync();
          } catch (_) {}
          await Future.delayed(const Duration(milliseconds: 500));
        }
        try {
          await Dio(
            BaseOptions(
              connectTimeout: const Duration(seconds: 15),
              receiveTimeout: const Duration(minutes: 3),
            ),
          ).download(
            url,
            savePath,
            onReceiveProgress: (received, total) {
              if (total > 0) {
                debugPrint(
                  'APK download: ${(received / total * 100).toStringAsFixed(0)}%',
                );
              }
            },
          );
          downloadErr = null;
          break;
        } catch (e) {
          downloadErr = e.toString();
          debugPrint('APK download attempt $attempt/2 failed: $e');
        }
      }

      // 重试耗尽 -> 报错 + 浏览器兜底
      if (downloadErr != null) {
        stopwatch.stop();
        AppUpdateService.reportDownload(
          buildNumber: update.buildNumber,
          platform: 'android',
          errorMsg: downloadErr,
          durationMs: stopwatch.elapsedMilliseconds,
        );
        CustomToast.show('update_download_failed'.tr, isError: true);
        // 清理重试耗尽后的残缺文件
        try {
          File(savePath).deleteSync();
        } catch (_) {}
        _launchUrl(url);
        if (context.mounted) Navigator.of(context).pop();
        return;
      }

      // 校验 SHA256 完整性
      if (update.sha256.isNotEmpty) {
        final file = File(savePath);
        final fileHash = await _computeFileSha256(file);
        if (fileHash != update.sha256.toLowerCase()) {
          debugPrint(
            'APK SHA256 mismatch: expected=${update.sha256}, got=$fileHash',
          );
          stopwatch.stop();
          AppUpdateService.reportDownload(
            buildNumber: update.buildNumber,
            platform: 'android',
            errorMsg: 'sha256_mismatch',
            durationMs: stopwatch.elapsedMilliseconds,
          );
          CustomToast.show('update_integrity_failed'.tr, isError: true);
          try {
            file.deleteSync();
          } catch (_) {}
          if (context.mounted) Navigator.of(context).pop();
          return;
        }
      }

      final result = await OpenFilex.open(savePath);
      if (result.type != ResultType.done) {
        debugPrint('OpenFilex failed: ${result.message}');
        // Fallback to url_launcher
        _launchUrl(url);
      } else {
        // Clean up temp APK after a delay (give the installer time to read it)
        Future.delayed(const Duration(seconds: 30), () {
          try {
            final file = File(savePath);
            if (file.existsSync()) file.deleteSync();
          } catch (_) {}
        });
      }
      // Report successful download
      stopwatch.stop();
      AppUpdateService.reportDownload(
        buildNumber: update.buildNumber,
        platform: 'android',
        durationMs: stopwatch.elapsedMilliseconds,
      );
    } catch (e) {
      debugPrint('APK download failed: $e');
      // Report failed download
      stopwatch.stop();
      AppUpdateService.reportDownload(
        buildNumber: update.buildNumber,
        platform: 'android',
        errorMsg: e.toString(),
        durationMs: stopwatch.elapsedMilliseconds,
      );
      // Fallback to url_launcher
      _launchUrl(url);
    }

    if (context.mounted) {
      Navigator.of(context).pop();
    }
  }

  String _resolveUpdateUrl() {
    switch (update.updateMethod) {
      case 'app_store':
        return update.appStoreUrl;
      case 'google_play':
        return update.appStoreUrl;
      case 'download':
      default:
        return update.downloadUrl;
    }
  }

  Future<void> _launchUrl(String url) async {
    try {
      final uri = Uri.parse(url);
      if (await canLaunchUrl(uri)) {
        await launchUrl(uri, mode: LaunchMode.externalApplication);
      } else {
        debugPrint('Cannot launch URL: $url');
      }
    } catch (e) {
      debugPrint('Failed to launch update URL: $e');
    }
  }

  String _formatFileSize(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  }

  /// 计算文件 SHA256 哈希值
  Future<String> _computeFileSha256(File file) async {
    final stream = file.openRead();
    final digest = await sha256.bind(stream).first;
    return digest.toString();
  }
}
