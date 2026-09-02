import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

/// 桌面端直连本地Connector改造：验证服务端签发的明文凭证能正确转手交给本地
/// Connector 的凭证应用接口，且响应语义（ok/busy/offline/failed）如实透传给调用方。
class _FakeAdapter implements HttpClientAdapter {
  _FakeAdapter(this.respond);

  final ResponseBody Function(RequestOptions options) respond;
  final requestedUris = <String>[];
  final requestedBodies = <dynamic>[];

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requestedUris.add(options.uri.toString());
    requestedBodies.add(options.data);
    return respond(options);
  }
}

class _ThrowingAdapter implements HttpClientAdapter {
  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    throw DioException(requestOptions: options, error: 'connection refused');
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

void main() {
  tearDown(Get.reset);

  test('applyRelayCredential 把凭证 PUT 到本地Connector的正确端点并原样透传 ok', () async {
    final adapter = _FakeAdapter((_) => _json({'enabled': true}, 200));
    final service = GrixConnectorService()..httpAdapter = adapter;

    final result = await service.applyRelayCredential(
      'claude-local-1',
      agentId: '12345',
      virtualKey: 'gvk_plaintext_example',
      anthropicBaseUrl: 'https://grix.dhf.pub/anthropic/v1',
      openaiBaseUrl: 'https://grix.dhf.pub/openai/v1',
    );

    expect(result, GrixApplyRelayCredentialResult.ok);
    expect(
      adapter.requestedUris.single,
      'http://127.0.0.1:19580/api/proxy/agents/claude-local-1/relay-credential',
    );
    expect(adapter.requestedBodies.single, {
      'agent_id': '12345',
      'virtual_key': 'gvk_plaintext_example',
      'anthropic_base_url': 'https://grix.dhf.pub/anthropic/v1',
      'openai_base_url': 'https://grix.dhf.pub/openai/v1',
    });
  });

  test('connector 答 busy=true：如实报 okButBusy，不能说成完全生效', () async {
    final adapter = _FakeAdapter(
      (_) => _json({'enabled': true, 'busy': true}, 200),
    );
    final service = GrixConnectorService()..httpAdapter = adapter;

    final result = await service.applyRelayCredential(
      'claude-local-1',
      agentId: '12345',
      virtualKey: 'gvk_plaintext_example',
      anthropicBaseUrl: '',
      openaiBaseUrl: '',
    );

    expect(result, GrixApplyRelayCredentialResult.okButBusy);
  });

  test('connector 答非200：如实报 failed，不能误判成离线', () async {
    final adapter = _FakeAdapter((_) => _json({'error': 'bad request'}, 400));
    final service = GrixConnectorService()..httpAdapter = adapter;

    final result = await service.applyRelayCredential(
      'claude-local-1',
      agentId: '12345',
      virtualKey: 'gvk_plaintext_example',
      anthropicBaseUrl: '',
      openaiBaseUrl: '',
    );

    expect(result, GrixApplyRelayCredentialResult.failed);
    expect(service.lastError.value, contains('400'));
  });

  test('连不上本地Connector（无响应）：报 offline', () async {
    final service = GrixConnectorService()..httpAdapter = _ThrowingAdapter();

    final result = await service.applyRelayCredential(
      'claude-local-1',
      agentId: '12345',
      virtualKey: 'gvk_plaintext_example',
      anthropicBaseUrl: '',
      openaiBaseUrl: '',
    );

    expect(result, GrixApplyRelayCredentialResult.offline);
  });

  test('失败时 lastError 不得包含明文凭证——凭证只能经请求体传递，不能落进日志', () async {
    final adapter = _FakeAdapter((_) => _json({'error': 'bad request'}, 400));
    final service = GrixConnectorService()..httpAdapter = adapter;

    await service.applyRelayCredential(
      'claude-local-1',
      agentId: '12345',
      virtualKey: 'gvk_super_secret_plaintext',
      anthropicBaseUrl: '',
      openaiBaseUrl: '',
    );

    expect(
      service.lastError.value,
      isNot(contains('gvk_super_secret_plaintext')),
    );
  });
}
