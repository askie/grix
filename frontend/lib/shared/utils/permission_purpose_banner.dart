import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:permission_handler/permission_handler.dart';

import '../../app/themes/app_theme.dart';

/// Android-only top banner shown while the system runtime permission dialog
/// is visible, explaining why the permission is requested (Huawei 789-0).
class PermissionPurposeBanner {
  PermissionPurposeBanner._();

  static OverlayEntry? _entry;

  /// Test hook: override overlay lookup (Get navigator may be unset in tests).
  @visibleForTesting
  static OverlayState? Function()? debugOverlayResolver;

  /// Whether a banner is currently inserted (for tests).
  @visibleForTesting
  static bool get isVisible => _entry != null && (_entry?.mounted ?? false);

  /// Resolve localized title/body for a permission type.
  static ({String title, String body}) copyFor(Permission permission) {
    if (permission == Permission.camera) {
      return (
        title: 'android_permission_purpose_camera_title'.tr,
        body: 'android_permission_purpose_camera_body'.tr,
      );
    }
    if (permission == Permission.microphone) {
      return (
        title: 'android_permission_purpose_microphone_title'.tr,
        body: 'android_permission_purpose_microphone_body'.tr,
      );
    }
    if (permission == Permission.notification) {
      return (
        title: 'android_permission_purpose_notification_title'.tr,
        body: 'android_permission_purpose_notification_body'.tr,
      );
    }
    if (permission == Permission.photos ||
        permission == Permission.videos ||
        permission == Permission.audio ||
        permission == Permission.storage ||
        permission == Permission.manageExternalStorage ||
        permission == Permission.mediaLibrary) {
      return (
        title: 'android_permission_purpose_photos_title'.tr,
        body: 'android_permission_purpose_photos_body'.tr,
      );
    }
    return (
      title: 'android_permission_purpose_generic_title'.tr,
      body: 'android_permission_purpose_generic_body'.tr,
    );
  }

  static OverlayState? _resolveOverlay() {
    final override = debugOverlayResolver;
    if (override != null) {
      return override();
    }
    return Get.key.currentState?.overlay;
  }

  /// Insert the purpose banner above the app UI. No-op if overlay unavailable
  /// or a banner is already showing.
  static void show(Permission permission) {
    if (_entry != null) return;

    final overlay = _resolveOverlay();
    if (overlay == null || !overlay.mounted) {
      return;
    }

    final copy = copyFor(permission);
    final entry = OverlayEntry(
      builder: (context) {
        final topPadding = MediaQuery.maybeOf(context)?.padding.top ?? 0;
        return Positioned(
          top: topPadding,
          left: 0,
          right: 0,
          child: Material(
            color: Colors.transparent,
            child: Container(
              width: double.infinity,
              margin: const EdgeInsets.fromLTRB(12, 8, 12, 0),
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
              decoration: BoxDecoration(
                color: AppTheme.lightCard,
                borderRadius: BorderRadius.circular(10),
                border: Border.all(color: AppTheme.lightDivider),
                boxShadow: const [
                  BoxShadow(
                    color: Colors.black26,
                    blurRadius: 8,
                    offset: Offset(0, 2),
                  ),
                ],
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    copy.title,
                    key: const Key('android_permission_purpose_banner_title'),
                    style: const TextStyle(
                      color: AppTheme.lightTextPrimary,
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                      height: 1.25,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    copy.body,
                    key: const Key('android_permission_purpose_banner_body'),
                    style: const TextStyle(
                      color: AppTheme.lightTextSecondary,
                      fontSize: 13,
                      height: 1.35,
                    ),
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );

    _entry = entry;
    overlay.insert(entry);
  }

  /// Remove the banner if present. Safe to call multiple times.
  static void dismiss() {
    final entry = _entry;
    _entry = null;
    if (entry == null) return;
    if (entry.mounted) {
      entry.remove();
    }
  }

  @visibleForTesting
  static void debugReset() {
    dismiss();
    debugOverlayResolver = null;
  }
}
