import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart' hide Response;

import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/egg_market_service.dart';

class _FakeAuthService extends AuthService {
  @override
  void attachAuthInterceptor(Dio dio) {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    Get.put<AuthService>(_FakeAuthService());
  });

  tearDown(() {
    Get.reset();
  });

  test('installEgg parses choose_executor response candidates', () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
      ..interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) {
            handler.resolve(
              Response<dynamic>(
                requestOptions: options,
                statusCode: 200,
                data: {
                  'code': 0,
                  'data': {
                    'status': 'choose_executor',
                    'candidates': [
                      {'agent_id': '101', 'agent_name': 'Main A'},
                      {'agent_id': '102', 'agent_name': 'Main B'},
                    ],
                  },
                },
              ),
            );
          },
        ),
      );

    final service = await EggMarketService(dio: dio).init();
    final result = await service.installEgg(
      eggID: 'lobster.executor',
      version: 1,
      idempotencyKey: 'egg-install-service-1',
      installMode: EggInstallMode.createNew,
    );

    expect(result.requiresExecutorSelection, isTrue);
    expect(result.sessionID, isEmpty);
    expect(result.executorAgentID, isEmpty);
    expect(result.candidates, hasLength(2));
    expect(result.candidates.first.agentID, '101');
    expect(result.candidates.first.agentName, 'Main A');
    expect(result.candidates.last.agentID, '102');
    expect(result.candidates.last.agentName, 'Main B');
  });

  test('installEgg sends executor_agent_id when provided', () async {
    Map<String, dynamic>? requestBody;
    final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
      ..interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) {
            requestBody = Map<String, dynamic>.from(options.data as Map);
            handler.resolve(
              Response<dynamic>(
                requestOptions: options,
                statusCode: 200,
                data: {
                  'code': 0,
                  'data': {
                    'install_id': 'install-1',
                    'status': 'running',
                    'session_id': 'session-1',
                    'executor_agent_id': '202',
                  },
                },
              ),
            );
          },
        ),
      );

    final service = await EggMarketService(dio: dio).init();
    final result = await service.installEgg(
      eggID: 'lobster.executor',
      version: 2,
      idempotencyKey: 'egg-install-service-2',
      installMode: EggInstallMode.existingAgent,
      targetAgentID: 'target-9',
      executorAgentID: '202',
    );

    expect(requestBody, isNotNull);
    expect(requestBody!['egg_id'], 'lobster.executor');
    expect(requestBody!['version'], 2);
    expect(requestBody!['idempotency_key'], 'egg-install-service-2');
    expect(requestBody!['install_mode'], EggInstallMode.existingAgent);
    expect(requestBody!['target_agent_id'], 'target-9');
    expect(requestBody!['executor_agent_id'], '202');
    expect(result.requiresExecutorSelection, isFalse);
    expect(result.sessionID, 'session-1');
    expect(result.executorAgentID, '202');
  });
}
