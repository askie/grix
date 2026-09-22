import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:permission_handler/permission_handler.dart';
import 'package:image_picker/image_picker.dart';

import 'permission_purpose_banner.dart';

class HardwareFacade {
  static final ImagePicker _picker = ImagePicker();

  /// Test hooks: override status / request / platform gates.
  @visibleForTesting
  static Future<PermissionStatus> Function(Permission)? debugStatusResolver;
  @visibleForTesting
  static Future<PermissionStatus> Function(Permission)? debugRequestResolver;
  @visibleForTesting
  static bool? debugForceRuntimePermissionGate;
  @visibleForTesting
  static bool? debugForceAndroidPurposeBanner;

  static bool _isPermissionUsable(PermissionStatus status) {
    return status.isGranted || status.isLimited;
  }

  static List<Permission> _resolveImagePickPermissions({
    required bool fromCamera,
  }) {
    if (kIsWeb) {
      return const [];
    }

    if (fromCamera) {
      return const [Permission.camera];
    }

    switch (defaultTargetPlatform) {
      case TargetPlatform.iOS:
        return const [Permission.photos];
      case TargetPlatform.android:
      case TargetPlatform.fuchsia:
      case TargetPlatform.linux:
      case TargetPlatform.macOS:
      case TargetPlatform.windows:
        return const [];
    }
  }

  static List<Permission> _resolveVideoPickPermissions({
    required bool fromCamera,
  }) {
    if (kIsWeb) {
      return const [];
    }

    if (fromCamera) {
      return const [Permission.camera, Permission.microphone];
    }

    switch (defaultTargetPlatform) {
      case TargetPlatform.iOS:
        return const [Permission.photos];
      case TargetPlatform.android:
      case TargetPlatform.fuchsia:
      case TargetPlatform.linux:
      case TargetPlatform.macOS:
      case TargetPlatform.windows:
        return const [];
    }
  }

  static bool get _requiresRuntimePermissionGate {
    if (debugForceRuntimePermissionGate != null) {
      return debugForceRuntimePermissionGate!;
    }
    if (kIsWeb) {
      return false;
    }
    switch (defaultTargetPlatform) {
      case TargetPlatform.android:
      case TargetPlatform.iOS:
        return true;
      case TargetPlatform.fuchsia:
      case TargetPlatform.linux:
      case TargetPlatform.macOS:
      case TargetPlatform.windows:
        return false;
    }
  }

  static bool get _shouldShowAndroidPurposeBanner {
    if (debugForceAndroidPurposeBanner != null) {
      return debugForceAndroidPurposeBanner!;
    }
    if (kIsWeb) {
      return false;
    }
    return defaultTargetPlatform == TargetPlatform.android;
  }

  static Future<PermissionStatus> _readStatus(Permission permission) {
    final override = debugStatusResolver;
    if (override != null) {
      return override(permission);
    }
    return permission.status;
  }

  static Future<PermissionStatus> _requestStatus(Permission permission) {
    final override = debugRequestResolver;
    if (override != null) {
      return override(permission);
    }
    return permission.request();
  }

  /// 统一的权限申请与拦截门面
  /// [permission] 具体的权限如 Permission.camera
  ///
  /// On Android, when a system runtime permission dialog is about to appear,
  /// a top-of-app purpose banner is shown for the duration of `request()`.
  static Future<bool> requestPermission(Permission permission) async {
    if (!_requiresRuntimePermissionGate) {
      return true;
    }

    try {
      final status = await _readStatus(permission);
      if (_isPermissionUsable(status)) return true;
      if (status.isRestricted || status.isPermanentlyDenied) return false;

      final showBanner = _shouldShowAndroidPurposeBanner;
      if (showBanner) {
        PermissionPurposeBanner.show(permission);
      }
      try {
        // 只有在此处且必要时才发起申请
        final result = await _requestStatus(permission);
        return _isPermissionUsable(result);
      } finally {
        if (showBanner) {
          PermissionPurposeBanner.dismiss();
        }
      }
    } on MissingPluginException catch (e) {
      PermissionPurposeBanner.dismiss();
      debugPrint('HardwareFacade permission plugin missing: $e');
      return false;
    } on PlatformException catch (e) {
      PermissionPurposeBanner.dismiss();
      debugPrint('HardwareFacade permission platform error: $e');
      return false;
    }
  }

  static Future<bool> _requestPermissions(List<Permission> permissions) async {
    for (final permission in permissions) {
      final granted = await requestPermission(permission);
      if (!granted) {
        return false;
      }
    }
    return true;
  }

  /// 统一的相册/相机资源拾取入口
  static Future<XFile?> pickImage({bool fromCamera = false}) async {
    final granted = await _requestPermissions(
      _resolveImagePickPermissions(fromCamera: fromCamera),
    );
    if (!granted) {
      return null;
    }

    try {
      if (fromCamera) {
        return await _picker.pickImage(source: ImageSource.camera);
      } else {
        return await _picker.pickImage(source: ImageSource.gallery);
      }
    } catch (e) {
      debugPrint('HardwareFacade error: $e');
      return null;
    }
  }

  static Future<List<XFile>> pickImages({bool fromCamera = false}) async {
    final granted = await _requestPermissions(
      _resolveImagePickPermissions(fromCamera: fromCamera),
    );
    if (!granted) {
      return const <XFile>[];
    }

    try {
      if (fromCamera) {
        final image = await _picker.pickImage(source: ImageSource.camera);
        if (image == null) {
          return const <XFile>[];
        }
        return <XFile>[image];
      }
      return await _picker.pickMultiImage();
    } catch (e) {
      debugPrint('HardwareFacade multi image error: $e');
      return const <XFile>[];
    }
  }

  static Future<XFile?> pickVideo({bool fromCamera = false}) async {
    final granted = await _requestPermissions(
      _resolveVideoPickPermissions(fromCamera: fromCamera),
    );
    if (!granted) {
      return null;
    }

    try {
      if (fromCamera) {
        return await _picker.pickVideo(source: ImageSource.camera);
      } else {
        return await _picker.pickVideo(source: ImageSource.gallery);
      }
    } catch (e) {
      debugPrint('HardwareFacade video error: $e');
      return null;
    }
  }

  @visibleForTesting
  static void debugReset() {
    debugStatusResolver = null;
    debugRequestResolver = null;
    debugForceRuntimePermissionGate = null;
    debugForceAndroidPurposeBanner = null;
    PermissionPurposeBanner.dismiss();
  }
}
