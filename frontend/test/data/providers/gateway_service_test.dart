import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart' hide Response;
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/gateway_service.dart';

class _FakeAuthService extends AuthService {
  final attached = <Dio>[];

  @override
  void attachAuthInterceptor(Dio dio) {
    attached.add(dio);
  }
}

Dio _mockDio(Response<dynamic> Function(RequestOptions options) responder) {
  final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
    ..interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) {
          handler.resolve(responder(options));
        },
      ),
    );
  return dio;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    Get.put<AuthService>(_FakeAuthService());
  });

  // 充值面板与 Agent 工具栏都用 Get.put(GatewayService()) 懒注册，从不调用 init()。
  // 鉴权拦截器若只挂在 init() 里，这条路径上的请求就不带 Authorization 头，
  // 网关接口一律 401——线上表现为充值下单永远失败、只提示"请联系管理员"。
  test('Get.put 注册（不调用 init）时也必须挂上鉴权拦截器', () {
    final auth = Get.find<AuthService>() as _FakeAuthService;
    expect(auth.attached, isEmpty);

    Get.put(GatewayService());

    expect(
      auth.attached,
      hasLength(1),
      reason: 'Get.put 走 onInit，必须挂上鉴权拦截器，否则请求不带 token 必然 401',
    );
  });

  tearDown(() {
    Get.reset();
  });

  test(
    'configureAgentProvider posts to /gateway/agents/:id/provider and returns true on success',
    () async {
      String? capturedPath;
      final dio = _mockDio((options) {
        capturedPath = options.path;
        return Response<dynamic>(
          requestOptions: options,
          statusCode: 200,
          data: {
            'code': 0,
            'data': {'already_configured': false},
          },
        );
      });

      final service = await GatewayService(dio: dio).init();
      final ok = await service.configureAgentProvider('12345');

      expect(ok, isTrue);
      expect(capturedPath, '/gateway/agents/12345/provider');
    },
  );

  test('configureAgentProvider returns false on business error', () async {
    final dio = _mockDio((options) {
      return Response<dynamic>(
        requestOptions: options,
        statusCode: 403,
        data: {'code': 20003, 'msg': '无权操作该 Agent'},
      );
    });

    final service = await GatewayService(dio: dio).init();
    final ok = await service.configureAgentProvider('12345');

    expect(ok, isFalse);
  });

  test('configureAgentProvider returns false when request throws', () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
      ..interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) {
            handler.reject(
              DioException(requestOptions: options, error: 'network down'),
            );
          },
        ),
      );

    final service = await GatewayService(dio: dio).init();
    final ok = await service.configureAgentProvider('12345');

    expect(ok, isFalse);
  });

  test(
    'issueAgentRelayCredential posts to /gateway/agents/:id/relay-credential and parses the credential',
    () async {
      String? capturedPath;
      final dio = _mockDio((options) {
        capturedPath = options.path;
        return Response<dynamic>(
          requestOptions: options,
          statusCode: 200,
          data: {
            'code': 0,
            'data': {
              'virtual_key': 'gvk_plaintext_example',
              'anthropic_base_url': 'https://grix.dhf.pub/anthropic/v1',
              'openai_base_url': 'https://grix.dhf.pub/openai/v1',
            },
          },
        );
      });

      final service = await GatewayService(dio: dio).init();
      final credential = await service.issueAgentRelayCredential('12345');

      expect(capturedPath, '/gateway/agents/12345/relay-credential');
      expect(credential, isNotNull);
      expect(credential!.virtualKey, 'gvk_plaintext_example');
      expect(credential.anthropicBaseUrl, 'https://grix.dhf.pub/anthropic/v1');
      expect(credential.openaiBaseUrl, 'https://grix.dhf.pub/openai/v1');
    },
  );

  test('issueAgentRelayCredential returns null on business error', () async {
    final dio = _mockDio((options) {
      return Response<dynamic>(
        requestOptions: options,
        statusCode: 403,
        data: {'code': 20003, 'msg': '无权操作该 Agent'},
      );
    });

    final service = await GatewayService(dio: dio).init();
    final credential = await service.issueAgentRelayCredential('12345');

    expect(credential, isNull);
    // 服务端拒绝原因要透出去给用户看（如 26004/26006 的模型校验提示），不能吞掉。
    expect(service.lastRelayCredentialError, '无权操作该 Agent');
  });

  test('issueAgentRelayCredential 带 model 时随 body 传给服务端，成功后清空错误', () async {
    Object? capturedData;
    final dio = _mockDio((options) {
      capturedData = options.data;
      return Response<dynamic>(
        requestOptions: options,
        statusCode: 200,
        data: {
          'code': 0,
          'data': {
            'virtual_key': 'gvk_plaintext_example',
            'anthropic_base_url': 'https://grix.dhf.pub/anthropic/v1',
            'openai_base_url': 'https://grix.dhf.pub/openai/v1',
          },
        },
      );
    });

    final service = await GatewayService(dio: dio).init();
    service.lastRelayCredentialError = '上次的残留';
    final credential = await service.issueAgentRelayCredential(
      '12345',
      model: 'deepseek-v4-pro',
    );

    expect(credential, isNotNull);
    expect(capturedData, {'model': 'deepseek-v4-pro'});
    expect(service.lastRelayCredentialError, isNull);
  });

  test('issueAgentRelayCredential returns null when request throws', () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
      ..interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) {
            handler.reject(
              DioException(requestOptions: options, error: 'network down'),
            );
          },
        ),
      );

    final service = await GatewayService(dio: dio).init();
    final credential = await service.issueAgentRelayCredential('12345');

    expect(credential, isNull);
  });

  test('getWallet parses balance from response', () async {
    final dio = _mockDio((options) {
      return Response<dynamic>(
        requestOptions: options,
        statusCode: 200,
        data: {
          'code': 0,
          'data': {
            'wallet': {
              'id': '999',
              'owner_id': '111',
              'balance': '12.500000000000',
            },
          },
        },
      );
    });

    final service = await GatewayService(dio: dio).init();
    final wallet = await service.getWallet();

    expect(wallet, isNotNull);
    expect(wallet!.id, '999');
    expect(wallet.balance, '12.500000000000');
  });

  test('listLedger parses items from response', () async {
    final dio = _mockDio((options) {
      return Response<dynamic>(
        requestOptions: options,
        statusCode: 200,
        data: {
          'code': 0,
          'data': {
            'items': [
              {
                'provider': 'deepseek',
                'model': 'deepseek-v4-flash',
                'cost': '0.0021',
                'status': 'settled',
                'created_at': '2026-07-05T00:00:00Z',
              },
            ],
            'total': 1,
            'page': 1,
            'page_size': 20,
          },
        },
      );
    });

    final service = await GatewayService(dio: dio).init();
    final items = await service.listLedger();

    expect(items, hasLength(1));
    expect(items.first.provider, 'deepseek');
    expect(items.first.cost, '0.0021');
  });

  test('listTopups returns empty list on error response', () async {
    final dio = _mockDio((options) {
      return Response<dynamic>(
        requestOptions: options,
        statusCode: 500,
        data: {'code': 50001, 'msg': '服务端内部异常'},
      );
    });

    final service = await GatewayService(dio: dio).init();
    final items = await service.listTopups();

    expect(items, isEmpty);
  });

  test('createTopup posts payload and returns pay url on success', () async {
    String? capturedPath;
    Object? capturedData;
    final dio = _mockDio((options) {
      capturedPath = options.path;
      capturedData = options.data;
      return Response<dynamic>(
        requestOptions: options,
        statusCode: 200,
        data: {
          'code': 0,
          'data': {
            'topup_order_id': '987654',
            'pay_url': 'https://pay.example.com/cashier?order=987654',
            'status': 'pending',
          },
        },
      );
    });

    final service = await GatewayService(dio: dio).init();
    final order = await service.createTopup(
      amount: '10.50',
      currency: 'CNY',
      channel: 'alipay',
      returnUrl: 'https://grix.dhf.pub/',
    );

    expect(capturedPath, '/gateway/wallet/topup');
    expect(capturedData, {
      'amount': '10.50',
      'currency': 'CNY',
      'channel': 'alipay',
      'return_url': 'https://grix.dhf.pub/',
    });
    expect(order, isNotNull);
    expect(order!.topupOrderId, '987654');
    expect(order.payUrl, 'https://pay.example.com/cashier?order=987654');
  });

  test(
    'createTopup omits empty return_url and returns null on error',
    () async {
      Object? capturedData;
      final dio = _mockDio((options) {
        capturedData = options.data;
        return Response<dynamic>(
          requestOptions: options,
          statusCode: 500,
          data: {'code': 50001, 'msg': '服务端内部异常'},
        );
      });

      final service = await GatewayService(dio: dio).init();
      final order = await service.createTopup(
        amount: '1',
        currency: 'USD',
        channel: 'paypal',
      );

      expect(capturedData, {
        'amount': '1',
        'currency': 'USD',
        'channel': 'paypal',
      });
      expect(order, isNull);
    },
  );
}
