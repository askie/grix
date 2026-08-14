import 'package:flutter/material.dart';
import 'package:get/get.dart';

/// 通用确认对话框。返回 true 表示用户确认。
/// 可选 [withReason]：让用户填写一段原因（用于封号等）。
class ConfirmDialog {
  ConfirmDialog._();

  static Future<bool> show({
    required String title,
    required String message,
    String confirmText = '确定',
    String cancelText = '取消',
    bool danger = false,
  }) async {
    final result = await Get.dialog<bool>(
      AlertDialog(
        title: Text(title),
        content: Text(message),
        actions: [
          TextButton(
            onPressed: () => Get.back(result: false),
            child: Text(cancelText),
          ),
          FilledButton(
            style: danger
                ? FilledButton.styleFrom(backgroundColor: Get.theme.colorScheme.error)
                : null,
            onPressed: () => Get.back(result: true),
            child: Text(confirmText),
          ),
        ],
      ),
    );
    return result ?? false;
  }

  /// 带原因输入的确认框。返回非 null 表示确认（内容为输入的原因，可能为空串）。
  static Future<String?> showWithReason({
    required String title,
    String hint = '请输入原因（可选）',
    String confirmText = '确定',
    bool danger = true,
  }) async {
    final ctrl = TextEditingController();
    final result = await Get.dialog<bool>(
      AlertDialog(
        title: Text(title),
        content: TextField(
          controller: ctrl,
          maxLines: 3,
          decoration: InputDecoration(hintText: hint),
        ),
        actions: [
          TextButton(
            onPressed: () => Get.back(result: false),
            child: const Text('取消'),
          ),
          FilledButton(
            style: danger
                ? FilledButton.styleFrom(backgroundColor: Get.theme.colorScheme.error)
                : null,
            onPressed: () => Get.back(result: true),
            child: Text(confirmText),
          ),
        ],
      ),
    );
    final confirmed = result ?? false;
    final text = ctrl.text.trim();
    ctrl.dispose();
    return confirmed ? text : null;
  }
}

/// 轻量提示（成功/失败）。
class Toast {
  Toast._();

  static void success(String message) {
    Get.snackbar('成功', message,
        snackPosition: SnackPosition.BOTTOM,
        margin: const EdgeInsets.all(16),
        duration: const Duration(seconds: 2));
  }

  static void error(String message) {
    Get.snackbar('出错了', message,
        snackPosition: SnackPosition.BOTTOM,
        backgroundColor: Get.theme.colorScheme.errorContainer,
        colorText: Get.theme.colorScheme.onErrorContainer,
        margin: const EdgeInsets.all(16),
        duration: const Duration(seconds: 3));
  }
}
