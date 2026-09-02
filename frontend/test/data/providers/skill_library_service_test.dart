import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/skill_library_service.dart';

class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.respond);
  final ResponseBody Function(RequestOptions options) respond;
  final requests = <String>[];

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add('${options.method} ${options.uri.path}');
    return respond(options);
  }
}

ResponseBody _json(Map<String, dynamic> body, int status) =>
    ResponseBody.fromString(
      jsonEncode(body),
      status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );

SkillLibraryService _serviceWith(_FakeAdapter adapter) {
  final dio = Dio(
    BaseOptions(baseUrl: 'http://x/v1', validateStatus: (_) => true),
  )..httpClientAdapter = adapter;
  return SkillLibraryService(dio: dio);
}

void main() {
  tearDown(Get.reset);

  test('refresh 解析技能列表', () async {
    final adapter = _FakeAdapter(
      (_) => _json({
        'code': 0,
        'data': {
          'items': [
            {'id': '10', 'name': '报告规范', 'version': '2', 'digest': 'd'},
          ],
        },
      }, 200),
    );
    final service = _serviceWith(adapter);
    final ok = await service.refresh();
    expect(ok, true);
    expect(service.skills, hasLength(1));
    expect(service.skills.first.name, '报告规范');
    expect(service.skills.first.version, 2);
  });

  test('owner_id 解析与系统内置技能识别', () async {
    final adapter = _FakeAdapter(
      (_) => _json({
        'code': 0,
        'data': {
          'items': [
            {
              'id': '1',
              'name': '系统技能',
              'owner_id': '0',
              'version': '1',
              'digest': 'd',
            },
            {
              'id': '2',
              'name': '我的技能',
              'owner_id': '1001',
              'version': '1',
              'digest': 'd',
            },
          ],
        },
      }, 200),
    );
    final service = _serviceWith(adapter);
    await service.refresh();
    expect(service.skills.first.isSystem, true);
    expect(service.skills.last.isSystem, false);
  });

  test('create 成功后回刷', () async {
    var listItems = <Map<String, dynamic>>[];
    final adapter = _FakeAdapter((options) {
      if (options.method == 'POST') {
        listItems = [
          {'id': '11', 'name': 'x', 'version': '1', 'digest': 'd'},
        ];
        return _json({
          'code': 0,
          'data': {'id': '11', 'name': 'x'},
        }, 200);
      }
      return _json({
        'code': 0,
        'data': {'items': listItems},
      }, 200);
    });
    final service = _serviceWith(adapter);
    final ok = await service.create('x', '# x');
    expect(ok, true);
    expect(service.skills.map((s) => s.name), contains('x'));
  });

  test('后端错误码返回 false 且记录 lastError', () async {
    final adapter = _FakeAdapter(
      (_) => _json({'code': 27003, 'msg': '同名技能已存在'}, 409),
    );
    final service = _serviceWith(adapter);
    final ok = await service.create('dup', 'c');
    expect(ok, false);
    expect(service.lastError, contains('同名'));
  });

  test('delete 调用 DELETE 端点', () async {
    final adapter = _FakeAdapter(
      (_) => _json({
        'code': 0,
        'data': {'items': []},
      }, 200),
    );
    final service = _serviceWith(adapter);
    final ok = await service.remove('10');
    expect(ok, true);
    expect(
      adapter.requests.any(
        (r) => r.startsWith('DELETE') && r.contains('/skills/10'),
      ),
      true,
    );
  });
}
