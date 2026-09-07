import 'dart:async';
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
import 'package:permission_handler/permission_handler.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../platform/platform_capability.dart';
import '../../shared/utils/toast_util.dart';
import 'android_update_support.dart';
import 'apk_downloader.dart';
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

  /// 下载成功、已拉起安装器但还不知道装没装上的目标构建号。
  static const pendingInstallBuildKey = 'pending_install_build';
  static const _pendingInstallFromBuildKey = 'pending_install_from_build';
  static const _pendingInstallTsKey = 'pending_install_ts';

  /// 超过这个时长仍未装上就判定安装没完成——用户多半在系统安装页放弃了。
  static const _pendingInstallGiveUp = Duration(hours: 24);

  /// 下载缓存的 APK 文件名前缀，启动时按此清理临时目录。
  static const _apkFilePrefix = 'grix-';

  final Dio _dio;
  static bool _prefsUnavailableLogged = false;

  /// Initializes the service: attaches auth interceptor and listens for login.
  Future<AppUpdateService> init() async {
    final auth = Get.find<AuthService>();
    auth.attachAuthInterceptor(_dio);

    // 上次下载留下的 APK 与「装了没」的判定都放到启动时处理：
    // 下载完立刻定时删包会把还没点安装的用户坑死（国产 ROM 的风险提示、
    // 纯净模式、密码验证走完往往超过半分钟，删了就是「解析包出错」）。
    unawaited(reconcilePendingInstall());
    unawaited(cleanupStaleApks());

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

  static Future<SharedPreferences?> _safeGetPrefsStatic() async {
    try {
      return await SharedPreferences.getInstance();
    } on MissingPluginException catch (_) {
      return null;
    } on PlatformException catch (_) {
      return null;
    }
  }

  void _logPrefsUnavailable(Object error) {
    if (_prefsUnavailableLogged) return;
    _prefsUnavailableLogged = true;
    debugPrint('SharedPreferences unavailable for AppUpdateService: $error');
  }

  /// 记下「包已下完、安装器已拉起」，等下次启动回来对账。
  static Future<void> markPendingInstall({
    required int buildNumber,
    required int? fromBuild,
  }) async {
    final prefs = await _safeGetPrefsStatic();
    if (prefs == null) return;
    await prefs.setInt(pendingInstallBuildKey, buildNumber);
    await prefs.setInt(
      _pendingInstallTsKey,
      DateTime.now().millisecondsSinceEpoch,
    );
    if (fromBuild != null) {
      await prefs.setInt(_pendingInstallFromBuildKey, fromBuild);
    } else {
      await prefs.remove(_pendingInstallFromBuildKey);
    }
  }

  /// 丢掉待装标记。安装器根本没拉起来时用，避免同一次失败被重复上报。
  static Future<void> clearPendingInstall() async {
    final prefs = await _safeGetPrefsStatic();
    if (prefs == null) return;
    await prefs.remove(pendingInstallBuildKey);
    await prefs.remove(_pendingInstallFromBuildKey);
    await prefs.remove(_pendingInstallTsKey);
  }

  /// 启动时对账上一次的安装结果。
  ///
  /// 「下载成功」从来不等于「装上了」——线上安卓 from_build 一直不涨就是这么
  /// 漏掉的。这里用当前 versionCode 和待装构建号比对，把真实结果补报上去。
  static Future<void> reconcilePendingInstall() async {
    if (kIsWeb || !Platform.isAndroid) return;
    final prefs = await _safeGetPrefsStatic();
    if (prefs == null) return;
    final pending = prefs.getInt(pendingInstallBuildKey);
    if (pending == null) return;

    final fromBuild = prefs.getInt(_pendingInstallFromBuildKey);
    final markedAt = prefs.getInt(_pendingInstallTsKey) ?? 0;

    int currentBuild = 0;
    try {
      final info = await PackageInfo.fromPlatform();
      currentBuild = int.tryParse(info.buildNumber) ?? 0;
    } catch (_) {
      return; // 读不到自己的版本号就不下结论，留到下次启动再说
    }

    Future<void> clear() => clearPendingInstall();

    if (currentBuild >= pending) {
      await reportDownload(
        buildNumber: pending,
        platform: 'android',
        stage: 'install',
        fromBuild: fromBuild,
      );
      await clear();
      return;
    }

    final age = DateTime.now().millisecondsSinceEpoch - markedAt;
    if (markedAt > 0 && age >= _pendingInstallGiveUp.inMilliseconds) {
      await reportDownload(
        buildNumber: pending,
        platform: 'android',
        stage: 'install',
        errorMsg: UpdateErrorCode.installNotCompleted,
        fromBuild: fromBuild,
      );
      await clear();
    }
  }

  /// 清理临时目录里遗留的更新包。
  ///
  /// 下载完不再定时删包，改由下一次冷启动清理：那时安装器早已走完，删了不会
  /// 把正在安装的包抽走。
  static Future<void> cleanupStaleApks() async {
    if (kIsWeb || !Platform.isAndroid) return;
    try {
      final dir = await getTemporaryDirectory();
      if (!dir.existsSync()) return;
      for (final entity in dir.listSync()) {
        if (entity is! File) continue;
        final name = entity.uri.pathSegments.last;
        if (!name.startsWith(_apkFilePrefix) || !name.endsWith('.apk')) {
          continue;
        }
        try {
          // 只清上一次运行留下的包。刚写的文件不碰，免得清理和本次会话里
          // 已经开跑的下载抢同一个文件。
          final age = DateTime.now().difference(entity.statSync().modified);
          if (age < const Duration(minutes: 5)) continue;
          entity.deleteSync();
        } catch (_) {}
      }
    } catch (e) {
      debugPrint('AppUpdateService.cleanupStaleApks error: $e');
    }
  }

  /// Reports a download or install outcome to the server for statistics.
  ///
  /// [stage] 只接受 `download` / `install`；[errorMsg] 只接受
  /// [UpdateErrorCode] 里的固定枚举值，空串表示成功。
  static Future<void> reportDownload({
    required int buildNumber,
    required String platform,
    String? errorMsg,
    int? durationMs,
    String stage = 'download',
    int? fromBuild,
  }) async {
    try {
      final dio = Dio(
        BaseOptions(
          baseUrl: AppRuntimeEndpoints.apiBaseUrl,
          connectTimeout: const Duration(seconds: 5),
        ),
      );
      if (Get.isRegistered<AuthService>()) {
        Get.find<AuthService>().attachAuthInterceptor(dio);
      }

      final packageInfo = await PackageInfo.fromPlatform();
      final resolvedFromBuild =
          fromBuild ?? int.tryParse(packageInfo.buildNumber);
      final device = await _deviceFacts();
      await dio.post(
        '/app/report-download',
        data: {
          'build_number': buildNumber,
          'from_build': resolvedFromBuild,
          'platform': platform,
          'error_msg': errorMsg ?? '',
          'duration_ms': durationMs ?? 0,
          'stage': stage,
          if (device.model != null) 'device_model': device.model,
          if (device.osVersion != null) 'os_version': device.osVersion,
          if (device.abi != null) 'abi': device.abi,
        },
      );
    } catch (e) {
      debugPrint('AppUpdateService.reportDownload error: $e');
    }
  }

  static Future<_DeviceFacts> _deviceFacts() async {
    try {
      if (!kIsWeb && Platform.isAndroid) {
        final info = await DeviceInfoPlugin().androidInfo;
        return _DeviceFacts(
          model: '${info.manufacturer} ${info.model}'.trim(),
          osVersion: 'Android ${info.version.release} (${info.version.sdkInt})',
          abi: info.supportedAbis.isNotEmpty ? info.supportedAbis.first : null,
        );
      }
      if (!kIsWeb && Platform.isIOS) {
        final info = await DeviceInfoPlugin().iosInfo;
        return _DeviceFacts(
          model: info.utsname.machine,
          osVersion: 'iOS ${info.systemVersion}',
        );
      }
    } catch (_) {}
    return const _DeviceFacts();
  }
}

class _DeviceFacts {
  const _DeviceFacts({this.model, this.osVersion, this.abi});

  final String? model;
  final String? osVersion;
  final String? abi;
}

/// 更新对话框的阶段。
enum _UpdateStage {
  /// 展示更新说明，等待用户点「立即更新」。
  idle,

  /// 安装权限没开，等用户去系统设置里打开后回到前台。
  awaitingPermission,

  /// 正在下载安装包。
  downloading,

  /// 下载完成，正在校验完整性。
  verifying,

  /// 已拉起系统安装器；对话框保留，兜底入口不能收。
  launched,

  /// 失败，展示原因与兜底入口。
  failed,
}

/// The update dialog shown to users.
class _UpdateDialog extends StatefulWidget {
  const _UpdateDialog({required this.update});

  final AppUpdateInfo update;

  @override
  State<_UpdateDialog> createState() => _UpdateDialogState();
}

class _UpdateDialogState extends State<_UpdateDialog>
    with WidgetsBindingObserver {
  _UpdateStage _stage = _UpdateStage.idle;
  int _received = 0;
  int _total = 0;
  String _failureKey = '';
  CancelToken? _cancelToken;
  bool _resumeCheckInFlight = false;

  AppUpdateInfo get update => widget.update;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _cancelToken?.cancel('dialog disposed');
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // 用户去系统设置开完「安装未知应用」后回到 App，这里再查一次；
    // 部分 ROM 的 activity result 回来时权限状态还没刷新，只靠 request()
    // 的返回值会误判成「没开」。
    if (state != AppLifecycleState.resumed) return;
    if (_stage != _UpdateStage.awaitingPermission) return;
    if (_resumeCheckInFlight) return;
    _resumeCheckInFlight = true;
    unawaited(
      _isInstallPermissionGranted().then((granted) {
        _resumeCheckInFlight = false;
        if (!mounted || _stage != _UpdateStage.awaitingPermission) return;
        if (granted) unawaited(_beginDownload());
      }),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return PopScope(
      canPop: _stage != _UpdateStage.downloading,
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
              if (_stage == _UpdateStage.idle && update.changelog.isNotEmpty)
                ConstrainedBox(
                  constraints: const BoxConstraints(maxHeight: 200),
                  child: SingleChildScrollView(
                    child: Text(
                      update.changelog,
                      style: theme.textTheme.bodyMedium,
                    ),
                  ),
                ),
              if (_stage == _UpdateStage.idle && update.fileSize > 0) ...[
                const SizedBox(height: 8),
                Text(
                  '${'update_file_size'.tr}: ${_formatFileSize(update.fileSize)}',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
              ..._buildStageContent(theme, colorScheme),
            ],
          ),
        ),
        actions: _buildActions(context),
      ),
    );
  }

  List<Widget> _buildStageContent(ThemeData theme, ColorScheme colorScheme) {
    switch (_stage) {
      case _UpdateStage.idle:
        return const [];
      case _UpdateStage.awaitingPermission:
        return [
          Text(
            'update_install_permission_body'.tr,
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 8),
          Text(
            _installHintText,
            style: theme.textTheme.bodySmall?.copyWith(
              color: colorScheme.onSurfaceVariant,
            ),
          ),
        ];
      case _UpdateStage.downloading:
        final progress = _total > 0 ? _received / _total : null;
        return [
          LinearProgressIndicator(value: progress),
          const SizedBox(height: 8),
          Text(
            progress == null
                ? '${'update_downloading'.tr} ${_formatFileSize(_received)}'
                : '${(progress * 100).toStringAsFixed(0)}%  '
                      '${_formatFileSize(_received)} / ${_formatFileSize(_total)}',
            style: theme.textTheme.bodySmall?.copyWith(
              color: colorScheme.onSurfaceVariant,
            ),
          ),
        ];
      case _UpdateStage.verifying:
        return [
          const LinearProgressIndicator(),
          const SizedBox(height: 8),
          Text('update_verifying'.tr, style: theme.textTheme.bodySmall),
        ];
      case _UpdateStage.launched:
        return [
          Text('update_installer_opened'.tr, style: theme.textTheme.bodyMedium),
        ];
      case _UpdateStage.failed:
        return [
          Text(
            _failureKey.isEmpty ? 'update_download_failed'.tr : _failureKey.tr,
            style: theme.textTheme.bodyMedium?.copyWith(
              color: colorScheme.error,
            ),
          ),
        ];
    }
  }

  List<Widget> _buildActions(BuildContext context) {
    switch (_stage) {
      case _UpdateStage.idle:
        return [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: Text('update_later'.tr),
          ),
          FilledButton(
            onPressed: () => _performUpdate(context),
            child: Text('update_now'.tr),
          ),
        ];
      case _UpdateStage.awaitingPermission:
        return [
          TextButton(
            onPressed: _openInBrowser,
            child: Text('update_open_in_browser'.tr),
          ),
          FilledButton(
            onPressed: _requestInstallPermission,
            child: Text('update_go_settings'.tr),
          ),
        ];
      case _UpdateStage.downloading:
        return [
          TextButton(
            onPressed: _cancelDownload,
            child: Text('common_cancel'.tr),
          ),
          TextButton(
            onPressed: _openInBrowser,
            child: Text('update_open_in_browser'.tr),
          ),
        ];
      case _UpdateStage.verifying:
        return [
          TextButton(
            onPressed: _openInBrowser,
            child: Text('update_open_in_browser'.tr),
          ),
        ];
      case _UpdateStage.launched:
        // 安装器拉起后不自动关闭：厂商拦截、密码验证、纯净模式都可能让用户
        // 回到 App，这时兜底的浏览器下载入口必须还在。
        return [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: Text('update_later'.tr),
          ),
          TextButton(
            onPressed: _openInBrowser,
            child: Text('update_open_in_browser'.tr),
          ),
        ];
      case _UpdateStage.failed:
        return [
          TextButton(
            onPressed: _openInBrowser,
            child: Text('update_open_in_browser'.tr),
          ),
          FilledButton(
            onPressed: () => _performUpdate(context),
            child: Text('common_retry'.tr),
          ),
        ];
    }
  }

  String get _installHintText {
    final key = AndroidUpdateSupport.installHintKey(_manufacturer);
    return key.tr;
  }

  String? _manufacturer;

  void _performUpdate(BuildContext context) {
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
      unawaited(_startAndroidUpdate());
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

  /// 安卓侧的完整更新流程：先确认安装权限，再下载，最后拉起安装器。
  ///
  /// 权限必须在下载前确认。此前的实现是下完 60MB 才撞上系统的
  /// 「不允许安装来自此来源的未知应用」，流量白费且用户无从下手。
  Future<void> _startAndroidUpdate() async {
    if (await _isInstallPermissionGranted()) {
      await _beginDownload();
      return;
    }
    await _promptInstallPermission();
  }

  Future<bool> _isInstallPermissionGranted() async {
    if (kIsWeb || !Platform.isAndroid) return true;
    try {
      final info = await DeviceInfoPlugin().androidInfo;
      _manufacturer = info.manufacturer;
      // REQUEST_INSTALL_PACKAGES 的按应用授权是 Android 8（API 26）引入的，
      // 更低版本沿用全局「未知来源」开关，查了也没意义。
      if (info.version.sdkInt < 26) return true;
      return await Permission.requestInstallPackages.isGranted;
    } catch (e) {
      debugPrint('install permission check failed: $e');
      // 查不出来就别拦着，让后面的安装器自己给结果。
      return true;
    }
  }

  /// 弹说明对话框，讲清为什么要这个权限、在哪儿开，用户确认后跳系统设置页。
  Future<void> _promptInstallPermission() async {
    if (!mounted) return;
    setState(() => _stage = _UpdateStage.awaitingPermission);

    final go = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('update_install_permission_title'.tr),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('update_install_permission_body'.tr),
            const SizedBox(height: 12),
            Text(_installHintText, style: Theme.of(ctx).textTheme.bodySmall),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: Text('update_later'.tr),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(true),
            child: Text('update_go_settings'.tr),
          ),
        ],
      ),
    );

    if (go != true) {
      // 用户在权限说明这一步放弃：这正是线上安卓装不上的主因，必须能在统计里
      // 看到有多少人卡在这里，而不是只看到「下载成功」。
      unawaited(
        AppUpdateService.reportDownload(
          buildNumber: update.buildNumber,
          platform: 'android',
          errorMsg: UpdateErrorCode.permissionBlocked,
        ),
      );
      return;
    }
    await _requestInstallPermission();
  }

  Future<void> _requestInstallPermission() async {
    try {
      // permission_handler 在安卓上会跳 ACTION_MANAGE_UNKNOWN_APP_SOURCES，
      // 并在用户返回时带回结果；返回后再查一次状态兜底。
      await Permission.requestInstallPackages.request();
    } catch (e) {
      debugPrint('requestInstallPackages failed: $e');
    }
    if (!mounted) return;
    if (await _isInstallPermissionGranted()) {
      await _beginDownload();
      return;
    }
    if (!mounted) return;
    // 仍未授权：停在 awaitingPermission，didChangeAppLifecycleState 会在下次
    // 回到前台时继续查，同时对话框上留着「用浏览器下载」兜底。
    setState(() {});
  }

  Future<void> _beginDownload() async {
    if (_stage == _UpdateStage.downloading ||
        _stage == _UpdateStage.verifying) {
      return;
    }
    final url = _resolveUpdateUrl();
    if (url.isEmpty) return;

    if (!mounted) return;
    setState(() {
      _stage = _UpdateStage.downloading;
      _received = 0;
      _total = update.fileSize;
      _failureKey = '';
    });

    final stopwatch = Stopwatch()..start();
    String savePath;
    try {
      final dir = await getTemporaryDirectory();
      savePath =
          '${dir.path}/${AppUpdateService._apkFilePrefix}'
          '${update.version}-${update.buildNumber}.apk';

      final free = await AndroidUpdateSupport.freeSpaceBytes(dir.path);
      if (!AndroidUpdateSupport.hasEnoughSpace(
        freeBytes: free,
        fileSize: update.fileSize,
      )) {
        stopwatch.stop();
        await _fail(
          UpdateErrorCode.lowStorage,
          'update_low_storage',
          stopwatch.elapsedMilliseconds,
        );
        return;
      }
    } catch (e) {
      debugPrint('APK temp dir failed: $e');
      stopwatch.stop();
      await _fail(
        UpdateErrorCode.downloadFailed,
        'update_download_failed',
        stopwatch.elapsedMilliseconds,
      );
      return;
    }

    final cancelToken = CancelToken();
    _cancelToken = cancelToken;
    final downloader = ApkDownloader();

    // 失败重试保留已下载的部分，第二次请求带 Range 续传；只有服务端明确拒绝
    // Range（416）时才从头来过。
    String? errorCode;
    for (var attempt = 1; attempt <= 2; attempt++) {
      try {
        await downloader.download(
          url: url,
          savePath: savePath,
          cancelToken: cancelToken,
          onProgress: (received, total) {
            if (!mounted || _stage != _UpdateStage.downloading) return;
            setState(() {
              _received = received;
              if (total > 0) _total = total;
            });
          },
        );
        errorCode = null;
        break;
      } on DownloadIdleTimeoutException catch (e) {
        debugPrint('APK download attempt $attempt/2 idle timeout: $e');
        errorCode = UpdateErrorCode.downloadTimeout;
      } on DownloadRangeNotSatisfiableException {
        debugPrint('APK download attempt $attempt/2: range rejected, restart');
        errorCode = UpdateErrorCode.downloadFailed;
      } on DioException catch (e) {
        if (CancelToken.isCancel(e)) return; // 用户取消，保留半成品供续传
        debugPrint('APK download attempt $attempt/2 failed: $e');
        errorCode =
            e.type == DioExceptionType.connectionTimeout ||
                e.type == DioExceptionType.receiveTimeout ||
                e.type == DioExceptionType.sendTimeout
            ? UpdateErrorCode.downloadTimeout
            : UpdateErrorCode.downloadFailed;
      } catch (e) {
        debugPrint('APK download attempt $attempt/2 failed: $e');
        errorCode = UpdateErrorCode.downloadFailed;
      }
      if (attempt < 2) await Future.delayed(const Duration(milliseconds: 500));
    }
    _cancelToken = null;

    if (errorCode != null) {
      stopwatch.stop();
      await _fail(
        errorCode,
        errorCode == UpdateErrorCode.downloadTimeout
            ? 'update_download_timeout'
            : 'update_download_failed',
        stopwatch.elapsedMilliseconds,
      );
      return;
    }

    if (mounted) setState(() => _stage = _UpdateStage.verifying);

    if (update.sha256.isNotEmpty) {
      final file = File(savePath);
      final fileHash = await _computeFileSha256(file);
      if (fileHash != update.sha256.toLowerCase()) {
        debugPrint(
          'APK SHA256 mismatch: expected=${update.sha256}, got=$fileHash',
        );
        // 内容对不上就没有续传价值，删掉让下次整包重来。
        try {
          file.deleteSync();
        } catch (_) {}
        stopwatch.stop();
        await _fail(
          UpdateErrorCode.sha256Mismatch,
          'update_integrity_failed',
          stopwatch.elapsedMilliseconds,
        );
        return;
      }
    }

    stopwatch.stop();
    final fromBuild = await _currentBuildNumber();
    await AppUpdateService.markPendingInstall(
      buildNumber: update.buildNumber,
      fromBuild: fromBuild,
    );
    unawaited(
      AppUpdateService.reportDownload(
        buildNumber: update.buildNumber,
        platform: 'android',
        durationMs: stopwatch.elapsedMilliseconds,
        fromBuild: fromBuild,
      ),
    );

    final result = await OpenFilex.open(savePath);
    if (result.type != ResultType.done) {
      debugPrint('OpenFilex failed: ${result.type} ${result.message}');
      // 权限在这一步被拒和「机器上没有安装器」是两回事，前者才是我们要盯的那条。
      final blockedByPermission = result.type == ResultType.permissionDenied;
      unawaited(
        AppUpdateService.reportDownload(
          buildNumber: update.buildNumber,
          platform: 'android',
          stage: 'install',
          errorMsg: blockedByPermission
              ? UpdateErrorCode.permissionBlocked
              : UpdateErrorCode.installerNotFound,
          fromBuild: fromBuild,
        ),
      );
      // 这一步的失败已经如实报过了，撤掉待装标记，免得 24 小时后启动对账再
      // 用 install_not_completed 把同一次失败重复报一遍。
      unawaited(AppUpdateService.clearPendingInstall());
      if (mounted) {
        setState(() {
          _stage = _UpdateStage.failed;
          _failureKey = blockedByPermission
              ? 'update_install_permission_body'
              : 'update_installer_not_found';
        });
      }
      _launchUrl(url);
      return;
    }

    if (mounted) setState(() => _stage = _UpdateStage.launched);
  }

  Future<void> _fail(
    String errorCode,
    String messageKey,
    int durationMs,
  ) async {
    unawaited(
      AppUpdateService.reportDownload(
        buildNumber: update.buildNumber,
        platform: 'android',
        errorMsg: errorCode,
        durationMs: durationMs,
      ),
    );
    if (!mounted) return;
    setState(() {
      _stage = _UpdateStage.failed;
      _failureKey = messageKey;
    });
  }

  Future<int?> _currentBuildNumber() async {
    try {
      final info = await PackageInfo.fromPlatform();
      return int.tryParse(info.buildNumber);
    } catch (_) {
      return null;
    }
  }

  void _cancelDownload() {
    _cancelToken?.cancel('user cancelled');
    _cancelToken = null;
    if (!mounted) return;
    // 半成品文件留着，下次点更新会带 Range 接着下。
    setState(() => _stage = _UpdateStage.idle);
  }

  void _openInBrowser() {
    final url = _resolveUpdateUrl();
    if (url.isEmpty) return;
    _launchUrl(url);
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
