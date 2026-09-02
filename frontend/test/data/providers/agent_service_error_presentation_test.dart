import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:get/get.dart';

class _FakeAuthService extends AuthService {
  @override
  void attachAuthInterceptor(Dio dio) {}
}

Dio _buildAlwaysFailingDio() {
  final dio = Dio(BaseOptions(baseUrl: 'http://127.0.0.1:1/v1'));
  dio.interceptors.add(
    InterceptorsWrapper(
      onRequest: (options, handler) {
        handler.reject(
          DioException(
            requestOptions: options,
            type: DioExceptionType.connectionError,
            error: 'forced connection error',
          ),
        );
      },
    ),
  );
  return dio;
}

void main() {
  setUp(() {
    Get.testMode = true;
    Get.reset();
    Get.put<AuthService>(_FakeAuthService());
  });

  tearDown(() {
    Get.reset();
  });

  test('loadAgents does not open snackbar when request fails', () async {
    final service = AgentService(dio: _buildAlwaysFailingDio());
    await service.init();

    await service.loadAgents();

    expect(service.agents, isEmpty);
    expect(Get.isSnackbarOpen, isFalse);
  });
}
