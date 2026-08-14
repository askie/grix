import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'auth_service.dart';
import '../../app/routes/app_routes.dart';
import '../../shared/widgets/dialog_content_box.dart';

/// 修改密码对话框。
///
/// 输入当前密码 + 新密码 + 确认新密码，提交后自动登出并跳转登录页。
Future<void> showChangePasswordDialog(BuildContext context) {
  final currentCtrl = TextEditingController();
  final newCtrl = TextEditingController();
  final confirmCtrl = TextEditingController();
  final loading = false.obs;
  final error = RxnString();

  return showDialog<void>(
    context: context,
    barrierDismissible: false,
    builder: (ctx) {
      return Obx(() => AlertDialog(
            insetPadding: kDialogInsetPadding,
            scrollable: true,
            title: const Text('修改密码'),
            content: DialogContentBox(
              maxWidth: 360,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  TextField(
                    controller: currentCtrl,
                    obscureText: true,
                    autofocus: true,
                    decoration: const InputDecoration(
                      labelText: '当前密码',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 16),
                  TextField(
                    controller: newCtrl,
                    obscureText: true,
                    decoration: const InputDecoration(
                      labelText: '新密码',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  const SizedBox(height: 16),
                  TextField(
                    controller: confirmCtrl,
                    obscureText: true,
                    decoration: const InputDecoration(
                      labelText: '确认新密码',
                      border: OutlineInputBorder(),
                    ),
                    onSubmitted: (_) => _submit(
                      currentCtrl: currentCtrl,
                      newCtrl: newCtrl,
                      confirmCtrl: confirmCtrl,
                      loading: loading,
                      error: error,
                    ),
                  ),
                  if (error.value != null) ...[
                    const SizedBox(height: 12),
                    Text(error.value!,
                        style: TextStyle(
                            color: Theme.of(ctx).colorScheme.error,
                            fontSize: 13)),
                  ],
                ],
              ),
            ),
            actions: [
              TextButton(
                onPressed:
                    loading.value ? null : () => Navigator.of(ctx).pop(),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: loading.value
                    ? null
                    : () => _submit(
                          currentCtrl: currentCtrl,
                          newCtrl: newCtrl,
                          confirmCtrl: confirmCtrl,
                          loading: loading,
                          error: error,
                        ),
                child: loading.value
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child:
                            CircularProgressIndicator(strokeWidth: 2, color: Colors.white),
                      )
                    : const Text('确认'),
              ),
            ],
          ));
    },
  ).then((_) {
    currentCtrl.dispose();
    newCtrl.dispose();
    confirmCtrl.dispose();
  });
}

Future<void> _submit({
  required TextEditingController currentCtrl,
  required TextEditingController newCtrl,
  required TextEditingController confirmCtrl,
  required RxBool loading,
  required RxnString error,
}) async {
  final current = currentCtrl.text;
  final next = newCtrl.text;
  final confirm = confirmCtrl.text;

  if (current.isEmpty || next.isEmpty || confirm.isEmpty) {
    error.value = '请填写所有字段';
    return;
  }
  if (next != confirm) {
    error.value = '两次输入的新密码不一致';
    return;
  }
  if (next.length < 6) {
    error.value = '新密码至少 6 位';
    return;
  }

  error.value = null;
  loading.value = true;
  try {
    await AuthService.to.changePassword(current, next);
    // 关闭对话框，跳转登录页
    Get.back();
    Get.offAllNamed(AppRoutes.login);
    Get.snackbar('成功', '密码已修改，请重新登录', snackPosition: SnackPosition.BOTTOM);
  } catch (e) {
    error.value = e.toString().replaceFirst('Exception: ', '');
  } finally {
    loading.value = false;
  }
}
