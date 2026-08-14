import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/platform/desktop/desktop_autostart_service.dart';
import 'package:grix/platform/desktop/desktop_window_service.dart';
import 'package:grix/modules/system/system_settings_view.dart';

void main() {
  setUp(() {
    Get.put(DesktopWindowService());
    Get.put(DesktopAutostartService());
  });

  tearDown(Get.reset);

  Future<void> pumpSettingsTab(WidgetTester tester) async {
    await tester.pumpWidget(
      const MaterialApp(home: Scaffold(body: SystemSettingsTab())),
    );
    await tester.pump();
  }

  testWidgets('设置页展示窗口行为与开机自启开关', (tester) async {
    await pumpSettingsTab(tester);

    expect(find.text('system_close_to_tray'), findsOneWidget);
    expect(find.text('system_auto_start'), findsOneWidget);
  });

  testWidgets('设置页不再暴露连接器自动重启开关（内部默认行为）', (tester) async {
    await pumpSettingsTab(tester);

    expect(find.text('system_connector'), findsNothing);
    expect(find.text('system_auto_restart'), findsNothing);
    expect(find.text('system_auto_restart_desc'), findsNothing);
  });
}
