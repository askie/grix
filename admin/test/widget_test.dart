import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix_admin/app/theme/app_theme.dart';
import 'package:grix_admin/modules/auth/login_controller.dart';
import 'package:grix_admin/modules/auth/login_view.dart';

void main() {
  testWidgets('登录页正常渲染账号/密码/登录按钮', (tester) async {
    Get.put(LoginController());
    addTearDown(Get.reset);

    await tester.pumpWidget(
      GetMaterialApp(theme: AppTheme.light, home: const LoginView()),
    );

    expect(find.text('塘主'), findsOneWidget);
    expect(find.text('管理员账号'), findsOneWidget);
    expect(find.text('密码'), findsOneWidget);
    expect(find.byType(FilledButton), findsOneWidget);
  });
}
