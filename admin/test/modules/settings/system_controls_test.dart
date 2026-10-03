import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix_admin/core/network/api_client.dart';
import 'package:grix_admin/modules/auth/auth_service.dart';
import 'package:grix_admin/modules/settings/settings_controller.dart';
import 'package:grix_admin/modules/settings/settings_models.dart';
import 'package:grix_admin/modules/settings/settings_view.dart';
import 'package:grix_admin/modules/settings/system_controls/system_controls_controller.dart';
import 'package:grix_admin/modules/settings/system_controls/system_controls_models.dart';
import 'package:grix_admin/modules/settings/system_controls/system_controls_service.dart';
import 'package:grix_admin/modules/settings/system_controls/system_controls_view.dart';

SystemControl item(bool value) => SystemControl(
  key: 'registration_enabled',
  label: '允许用户注册',
  description: '仅控制新账号开户，已有账号仍可登录。',
  valueType: 'boolean',
  value: value,
);

class FakeService extends SystemControlsService {
  bool value = false;
  bool failLoad = false;
  bool failSave = false;
  Completer<List<SystemControl>>? pendingLoad;
  Completer<SystemControl>? pendingSave;
  @override
  Future<List<SystemControl>> get() async {
    if (pendingLoad != null) return pendingLoad!.future;
    if (failLoad) throw Exception('read error');
    return [item(value)];
  }

  @override
  Future<SystemControl> update(String key, bool next) async {
    if (pendingSave != null) return pendingSave!.future;
    if (failSave) throw Exception('write error');
    value = next;
    return item(next);
  }
}

class Adapter implements HttpClientAdapter {
  Adapter(this.handler);
  final ResponseBody Function(RequestOptions) handler;
  @override
  void close({bool force = false}) {}
  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async => handler(options);
}

class LoadedSettingsController extends SettingsController {
  // Test fixture supplies existing settings without issuing network requests.
  @override
  // ignore: must_call_super
  void onInit() {
    auth.value = AuthSettings(autoAddCustomerUserId: '0');
    inviteThresholdCtrl.text = '10';
  }
}

void main() {
  tearDown(Get.reset);
  test('API preserves boolean false and directory metadata', () async {
    final requests = <RequestOptions>[];
    ApiClient.instance.httpClientAdapter = Adapter((request) {
      requests.add(request);
      final response = {
        'key': 'registration_enabled',
        'label': '允许用户注册',
        'description': '新账号',
        'value_type': 'boolean',
        'value': false,
      };
      return ResponseBody.fromString(
        jsonEncode({
          'code': 0,
          'data': request.method == 'GET'
              ? {
                  'items': [response],
                }
              : response,
        }),
        200,
        headers: {
          Headers.contentTypeHeader: [Headers.jsonContentType],
        },
      );
    });
    final service = SystemControlsService();
    expect((await service.get()).single.value, false);
    expect((await service.update('registration_enabled', false)).value, false);
    expect(
      requests.last.path,
      '/settings/system-controls/registration_enabled',
    );
    expect(requests.last.data, {'value': false});
    expect(
      () => SystemControl.fromJson({
        'key': 'x',
        'label': 'x',
        'description': 'x',
        'value_type': 'boolean',
        'value': null,
      }),
      throwsFormatException,
    );
  });
  test(
    'controller keeps drafts across refresh, serializes load/save and retries failure',
    () async {
      final service = FakeService();
      final c = SystemControlsController(service: service);
      await c.load();
      c.edit(c.items.single, true);
      await c.load();
      expect(c.drafts['registration_enabled'], true);
      expect(c.items.single.value, false);
      service.failSave = true;
      await c.save(c.items.single);
      expect(c.items.single.value, false);
      expect(c.savedKeys, isEmpty);
      expect(c.saveErrors, isNotEmpty);
      service.failSave = false;
      service.pendingSave = Completer<SystemControl>();
      final saving = c.save(c.items.single);
      await c.load(); // blocked while saving
      c.edit(c.items.single, false); // disabled while saving
      expect(c.loading.value, false);
      expect(c.drafts['registration_enabled'], true);
      service.pendingSave!.complete(item(true));
      await saving;
      expect(c.items.single.value, true);
      expect(c.savedKeys, contains('registration_enabled'));
      service.value = true;
      service.pendingLoad = Completer<List<SystemControl>>();
      final loading = c.load();
      await c.save(c.items.single); // blocked while reading
      c.edit(c.items.single, false);
      expect(c.savingKey.value, null);
      expect(c.drafts['registration_enabled'], true);
      service.pendingLoad!.complete([item(false)]);
      await loading;
      expect(c.drafts['registration_enabled'], false);
    },
  );
  testWidgets(
    'settings entry retains existing modules and opens system controls',
    (tester) async {
      Get.put(AuthService());
      Get.put<SettingsController>(LoadedSettingsController());
      await tester.pumpWidget(
        GetMaterialApp(
          home: const SettingsView(),
          getPages: [
            GetPage(
              name: '/settings/system-controls',
              page: () => const Scaffold(body: Text('control destination')),
            ),
          ],
        ),
      );
      expect(find.text('系统控制'), findsOneWidget);
      expect(find.text('认证设置'), findsOneWidget);
      expect(find.text('手机号短信登录注册'), findsOneWidget);
      await tester.tap(find.text('系统控制'));
      await tester.pumpAndSettle();
      expect(find.text('control destination'), findsOneWidget);
    },
  );
  for (final size in [const Size(390, 844), const Size(1280, 900)]) {
    testWidgets('read save true/false refresh reopen and errors at $size', (
      tester,
    ) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final service = FakeService()..failLoad = true;
      final c = Get.put(SystemControlsController(service: service));
      Get.put(AuthService());
      await tester.pumpWidget(const GetMaterialApp(home: SystemControlsView()));
      await tester.pumpAndSettle();
      expect(find.textContaining('读取失败'), findsOneWidget);
      expect(find.byType(SwitchListTile), findsNothing);
      service.failLoad = false;
      await tester.tap(find.text('重试读取'));
      await tester.pumpAndSettle();
      expect(find.text('当前已生效：禁止'), findsOneWidget);
      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();
      service.failSave = true;
      await tester.tap(find.text('保存'));
      await tester.pumpAndSettle();
      expect(find.textContaining('保存失败'), findsOneWidget);
      expect(find.textContaining('已保存：'), findsNothing);
      expect(find.text('当前已生效：禁止'), findsOneWidget);
      service.failSave = false;
      await tester.tap(find.text('重试保存'));
      await tester.pumpAndSettle();
      expect(find.text('已保存：允许'), findsOneWidget);
      await tester.tap(find.byTooltip('刷新'));
      await tester.pumpAndSettle();
      expect(find.text('当前已生效：允许'), findsOneWidget);
      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();
      await tester.tap(find.text('保存'));
      await tester.pumpAndSettle();
      expect(find.text('已保存：禁止'), findsOneWidget);
      final reopened = SystemControlsController(service: service);
      await reopened.load();
      expect(reopened.items.single.value, false);
      service.failLoad = true;
      await c.load();
      await tester.pumpAndSettle();
      expect(find.textContaining('读取失败'), findsOneWidget);
      expect(find.text('上次确认：禁止'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  }
}
