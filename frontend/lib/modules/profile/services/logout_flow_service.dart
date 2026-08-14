import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../app/routes/app_routes.dart';
import '../../../app/routes/root_route_navigator.dart';
import '../../../app/themes/app_theme.dart';
import '../../../data/providers/auth_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../shared/widgets/app_dialog_style.dart';

Future<void> showLogoutConfirmDialog() async {
  await showAppGetDialog<void>(
    AlertDialog(
      title: Text(
        'me_logout'.tr,
        style: const TextStyle(fontWeight: FontWeight.w600),
      ),
      content: Text('me_logout_confirm'.tr),
      actions: [
        TextButton(
          onPressed: () => Get.back(),
          child: Text('me_logout_cancel'.tr),
        ),
        ElevatedButton(
          onPressed: () async {
            Get.back();
            await performLogout();
          },
          style: ElevatedButton.styleFrom(
            backgroundColor: AppTheme.errorColor,
            foregroundColor: Colors.white,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(10),
            ),
          ),
          child: Text('me_logout'.tr),
        ),
      ],
    ),
  );
}

Future<void> performLogout() async {
  Get.find<ImService>().disconnect();
  await Get.find<AuthService>().logout();
  if (Get.currentRoute != AppRoutes.login) {
    RootRouteNavigator.toLogin();
  }
}
