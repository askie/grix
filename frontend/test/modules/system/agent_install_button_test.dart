import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/system/agent_client_toolbar_view.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

/// 回归测试：未安装 agent 的弹窗里，"安装/下载"按钮必须可见。
///
/// 历史 bug：弹窗 actions 里把"安装"+"新增"包成一个 Row 当作单个 action，
/// AlertDialog 的 OverflowBar + IntrinsicWidth 在宽屏下会触发布局异常，正式包
/// 静默吞错导致整块按钮不渲染。修复方式是把按钮平铺为独立的 action。
void main() {
  testWidgets('未安装 agent 弹窗显示安装按钮且无布局异常', (tester) async {
    tester.view.physicalSize = const Size(1306, 1000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(() => tester.view.resetPhysicalSize());

    // 收集运行期错误：SVG 资源缺失等可忽略，但布局类错误必须为空。
    final errors = <FlutterErrorDetails>[];
    final prev = FlutterError.onError;
    FlutterError.onError = errors.add;
    addTearDown(() => FlutterError.onError = prev);

    // 直接 new，不走 Get.put，避免 onInit 的健康轮询 Timer。
    final svc = GrixConnectorService();
    svc.installedClients.value = const [
      InstalledClientCommand(clientType: 'gemini', installed: false),
    ];

    await tester.pumpWidget(GetMaterialApp(
      theme: AppTheme.lightTheme,
      translations: AppTranslations(),
      locale: const Locale('zh', 'CN'),
      fallbackLocale: const Locale('zh', 'CN'),
      home: Scaffold(
        body: AgentClientToolbarView(service: svc, compact: true),
      ),
    ));
    await tester.pumpAndSettle();

    // 点开 Gemini 图标 → 打开类型弹窗。
    await tester.tap(find.byType(InkWell).first);
    await tester.pumpAndSettle();

    // 弹窗提示文案在位。
    expect(find.textContaining('尚未安装'), findsOneWidget);
    // 关键：安装按钮必须可见。
    expect(find.text('安装'), findsOneWidget, reason: '安装/下载按钮缺失');
    // 新增按钮也应在位。
    expect(find.text('新增'), findsOneWidget);

    // 不允许出现布局类异常（RenderFlex / OverflowBar / hasSize）。
    final layoutErrors = errors.where((e) {
      final s = e.exceptionAsString();
      return s.contains('RenderFlex') ||
          s.contains('OverflowBar') ||
          s.contains('hasSize') ||
          s.contains('BoxConstraints');
    }).toList();
    expect(layoutErrors, isEmpty,
        reason: '出现布局异常：${layoutErrors.map((e) => e.exceptionAsString()).join("\n")}');
  });
}
