import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix_admin/app/theme/app_theme.dart';
import 'package:grix_admin/core/network/api_client.dart';
import 'package:grix_admin/modules/auth/auth_service.dart';
import 'package:grix_admin/modules/feature_gates/feature_gates_controller.dart';
import 'package:grix_admin/modules/feature_gates/feature_gates_view.dart';

/// 伪网络适配器：按请求返回预设响应。
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.handler);

  final ResponseBody Function(RequestOptions options) handler;

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async => handler(options);
}

ResponseBody _json(Map<String, dynamic> body, {int status = 200}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

Map<String, dynamic> _gatesResponse() => {
  'code': 0,
  'msg': 'success',
  'data': {
    'gates': [
      {
        'key': 'voice_call',
        'display_name': '语音通话',
        'status': 'disabled',
        'whitelist_user_count': 2,
        'public_only': false,
      },
      {
        'key': 'region_select',
        'display_name': '区域选择',
        'status': 'enabled',
        'whitelist_user_count': 0,
        'public_only': true,
      },
    ],
    'available': [],
  },
};

void main() {
  tearDown(Get.reset);

  testWidgets('public_only 开关不显示白名单相关按钮，普通开关正常显示', (tester) async {
    // 必须先装上伪网络适配器，再注册控制器（onInit 即发起加载请求）
    ApiClient.instance.httpClientAdapter = _FakeAdapter(
      (options) => _json(_gatesResponse()),
    );
    Get.put(AuthService());
    Get.put(FeatureGatesController());

    await tester.pumpWidget(
      GetMaterialApp(theme: AppTheme.light, home: const FeatureGatesView()),
    );
    // AsyncView 的加载动画是持续动画，pumpAndSettle 会超时，用有限次 pump
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump(const Duration(milliseconds: 300));

    // 两张卡片都已渲染
    expect(find.text('语音通话'), findsOneWidget);
    expect(find.text('区域选择'), findsOneWidget);

    // 普通开关：白名单切换 + 用户管理按钮齐全
    expect(find.text('白名单'), findsOneWidget);
    expect(find.text('添加用户'), findsOneWidget);
    expect(find.text('移除用户'), findsOneWidget);

    // 普通开关显示白名单人数，public_only 开关不显示
    expect(find.text('key: voice_call　白名单用户: 2'), findsOneWidget);
    expect(find.text('key: region_select'), findsOneWidget);

    // "全量开启"出现 2 次：普通开关的按钮 + public_only 开关的状态标签；
    // "关闭"出现 2 次：普通开关的状态标签 + public_only 开关的按钮。
    // public_only 开关若多出"白名单/添加用户/移除用户"按钮，上面的 findsOneWidget 会失败。
    expect(find.text('全量开启'), findsNWidgets(2));
    expect(find.text('关闭'), findsNWidgets(2));
  });
}
