import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:permission_handler/permission_handler.dart';
import 'package:image_picker/image_picker.dart';

class HardwareFacade {
  static final ImagePicker _picker = ImagePicker();

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

  /// 统一的权限申请与拦截门面
  /// [permission] 具体的权限如 Permission.camera
  static Future<bool> requestPermission(Permission permission) async {
    if (!_requiresRuntimePermissionGate) {
      return true;
    }

    try {
      final status = await permission.status;
      if (_isPermissionUsable(status)) return true;
      if (status.isRestricted || status.isPermanentlyDenied) return false;

      // 只有在此处且必要时才发起申请
      final result = await permission.request();
      return _isPermissionUsable(result);
    } on MissingPluginException catch (e) {
      debugPrint('HardwareFacade permission plugin missing: $e');
      return false;
    } on PlatformException catch (e) {
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
}
