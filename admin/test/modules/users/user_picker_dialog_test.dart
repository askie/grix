import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix_admin/app/theme/app_theme.dart';
import 'package:grix_admin/core/network/api_client.dart';
import 'package:grix_admin/modules/users/admin_user_item.dart';
import 'package:grix_admin/modules/users/user_picker_dialog.dart';

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

Map<String, dynamic> _listResponse(List<Map<String, dynamic>> items) => {
  'code': 0,
  'msg': 'success',
  'data': {'items': items, 'total': items.length, 'page': 1, 'page_size': 20},
};

void main() {
  testWidgets('UserPickerDialog 多选模式：勾选并确认返回所选用户', (tester) async {
    ApiClient.instance.httpClientAdapter = _FakeAdapter(
      (options) => _json(
        _listResponse([
          {
            'id': '1',
            'username': 'alice',
            'nickname': 'Alice',
            'email': '',
            'status': 1,
          },
          {
            'id': '2',
            'username': 'bob',
            'nickname': 'Bob',
            'email': '',
            'status': 1,
          },
        ]),
      ),
    );

    Future<List<AdminUserItem>?>? picked;
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.light,
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: TextButton(
                onPressed: () {
                  picked = UserPickerDialog.show(
                    title: '选择用户',
                    mode: UserPickerMode.multiple,
                  );
                },
                child: const Text('OPEN'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('OPEN'));
    await tester.pumpAndSettle();

    // 标题与候选用户均已渲染
    expect(find.text('选择用户'), findsOneWidget);
    expect(find.text('Alice'), findsOneWidget);
    expect(find.text('Bob'), findsOneWidget);

    // 勾选两个用户
    await tester.tap(find.text('Alice'));
    await tester.pump();
    await tester.tap(find.text('Bob'));
    await tester.pump();

    expect(find.text('已选 2 人'), findsOneWidget);

    // 确认
    await tester.tap(find.widgetWithText(FilledButton, '确定'));
    await tester.pumpAndSettle();

    final result = await picked!;
    expect(result, isNotNull);
    expect(result!.map((u) => u.id).toList(), ['1', '2']);
  });

  testWidgets('UserPickerDialog 单选模式：第二次点击替换前一个选择', (tester) async {
    ApiClient.instance.httpClientAdapter = _FakeAdapter(
      (options) => _json(
        _listResponse([
          {
            'id': '10',
            'username': 'alice',
            'nickname': 'Alice',
            'email': '',
            'status': 1,
          },
          {
            'id': '20',
            'username': 'bob',
            'nickname': 'Bob',
            'email': '',
            'status': 1,
          },
        ]),
      ),
    );

    Future<List<AdminUserItem>?>? picked;
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.light,
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: TextButton(
                onPressed: () {
                  picked = UserPickerDialog.show(
                    title: '挑一个',
                    mode: UserPickerMode.single,
                    confirmText: '使用',
                  );
                },
                child: const Text('OPEN'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('OPEN'));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Alice'));
    await tester.pump();
    await tester.tap(find.text('Bob'));
    await tester.pump();

    await tester.tap(find.widgetWithText(FilledButton, '使用'));
    await tester.pumpAndSettle();

    final result = await picked!;
    expect(result, isNotNull);
    expect(result!.length, 1);
    expect(result.first.id, '20');
  });

  testWidgets('UserPickerDialog 取消返回 null', (tester) async {
    ApiClient.instance.httpClientAdapter = _FakeAdapter(
      (options) => _json(_listResponse(const [])),
    );

    Future<List<AdminUserItem>?>? picked;
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.light,
        home: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: TextButton(
                onPressed: () {
                  picked = UserPickerDialog.show(title: '空');
                },
                child: const Text('OPEN'),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('OPEN'));
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(TextButton, '取消'));
    await tester.pumpAndSettle();

    expect(await picked!, isNull);
  });
}
