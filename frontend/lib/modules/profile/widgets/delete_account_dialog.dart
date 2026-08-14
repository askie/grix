import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../app/routes/root_route_navigator.dart';
import '../../../data/providers/auth_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/widgets/app_dialog_style.dart';

Future<void> showDeleteAccountDialog(BuildContext context) async {
  final authService = Get.find<AuthService>();
  final expectedValue = (authService.user?.username.trim().isNotEmpty ?? false)
      ? authService.user!.username.trim()
      : 'DELETE';
  final textController = TextEditingController();
  final deleting = false.obs;

  try {
    await showAppGetDialog(
      AlertDialog(
        title: Text(
          'account_delete_title'.tr,
          style: const TextStyle(fontWeight: FontWeight.w700),
        ),
        content: Obx(() {
          final busy = deleting.value;
          return Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('account_delete_description'.tr),
              const SizedBox(height: 12),
              Text(
                'account_delete_confirmation_hint'.trParams({
                  'value': expectedValue,
                }),
                style: TextStyle(
                  fontSize: 13,
                  color: Theme.of(context).colorScheme.secondary,
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: textController,
                enabled: !busy,
                decoration: InputDecoration(
                  labelText: 'account_delete_confirmation_label'.tr,
                ),
              ),
              const SizedBox(height: 12),
              Text(
                'account_delete_irreversible'.tr,
                style: const TextStyle(
                  fontSize: 12,
                  color: Color(0xFFB3261E),
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          );
        }),
        actions: [
          Obx(() {
            final busy = deleting.value;
            return TextButton(
              onPressed: busy ? null : () => Get.back(),
              child: Text('common_cancel'.tr),
            );
          }),
          Obx(() {
            final busy = deleting.value;
            return ElevatedButton(
              style: ElevatedButton.styleFrom(
                backgroundColor: const Color(0xFFB3261E),
                foregroundColor: Colors.white,
              ),
              onPressed: busy
                  ? null
                  : () async {
                      if (textController.text.trim() != expectedValue) {
                        CustomToast.show(
                          'account_delete_confirmation_mismatch'.tr,
                        );
                        return;
                      }

                      deleting.value = true;
                      final result = await authService.deleteAccount();
                      deleting.value = false;
                      if (!result.ok) {
                        CustomToast.show(
                          result.message.isNotEmpty
                              ? result.message
                              : 'account_delete_failed'.tr,
                        );
                        return;
                      }

                      if (Get.isDialogOpen ?? false) {
                        Get.back();
                      }
                      CustomToast.show(
                        'account_delete_success'.tr,
                        isError: false,
                      );
                      RootRouteNavigator.toLogin();
                    },
              child: busy
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: Colors.white,
                      ),
                    )
                  : Text('account_delete_action'.tr),
            );
          }),
        ],
      ),
      barrierDismissible: false,
    );
  } finally {
    textController.dispose();
  }
}
