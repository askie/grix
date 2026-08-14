import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/agent_service.dart';

Dio _buildScopeDio(Map<String, dynamic> data) {
  final dio = Dio(BaseOptions(baseUrl: 'http://example.test/v1'));
  dio.interceptors.add(
    InterceptorsWrapper(
      onRequest: (options, handler) {
        handler.resolve(
          Response(
            requestOptions: options,
            statusCode: 200,
            data: {'code': 0, 'data': data},
          ),
        );
      },
    ),
  );
  return dio;
}

void main() {
  test('getAgentScopes derives available scopes from backend items', () async {
    final service = AgentService(
      dio: _buildScopeDio({
        'scopes': ['future.scope'],
        'available_scope_items': [
          {
            'scope': 'future.scope',
            'label': 'Future Scope',
            'description': 'Server supplied text.',
          },
        ],
      }),
    );

    final result = await service.getAgentScopes('9992');

    expect(result.ok, isTrue);
    expect(result.data?.scopes, ['future.scope']);
    expect(result.data?.availableScopes, ['future.scope']);
    expect(result.data?.availableScopeItems.single.label, 'Future Scope');
    expect(
      result.data?.availableScopeItems.single.description,
      'Server supplied text.',
    );
  });
}
