import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart' hide Response;

import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';

class _FakeAuthService extends AuthService {
  @override
  void attachAuthInterceptor(Dio dio) {}
}

// loadAgents 内会顺带调用 /agents/shared-with-me；测试只关心 /agents/list 的去重/竞态,
// 对 shared-with-me 直接返回空,不计入计数,也不挤占 pendingResponses 序列。
const _sharedWithMePath = '/agents/shared-with-me';

bool _isSharedWithMeRequest(RequestOptions options) {
  return options.path.startsWith(_sharedWithMePath);
}

Response<dynamic> _emptyOkResponse(RequestOptions options) {
  return Response<dynamic>(
    requestOptions: options,
    statusCode: 200,
    data: {'code': 0, 'data': <Map<String, dynamic>>[]},
  );
}

Dio _buildDelayedSuccessDio({
  required void Function() onRequest,
  required dynamic data,
}) {
  final dio = Dio(BaseOptions(baseUrl: 'https://example.com'));
  dio.interceptors.add(
    InterceptorsWrapper(
      onRequest: (options, handler) async {
        if (_isSharedWithMeRequest(options)) {
          handler.resolve(_emptyOkResponse(options));
          return;
        }
        onRequest();
        await Future<void>.delayed(const Duration(milliseconds: 10));
        handler.resolve(
          Response<dynamic>(
            requestOptions: options,
            statusCode: 200,
            data: data,
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

  test('loadAgents de-duplicates concurrent requests', () async {
    var requestCount = 0;
    final service = AgentService(
      dio: _buildDelayedSuccessDio(
        onRequest: () {
          requestCount++;
        },
        data: {'code': 0, 'data': <Map<String, dynamic>>[]},
      ),
    );
    await service.init();

    await Future.wait<void>([service.loadAgents(), service.loadAgents()]);

    expect(requestCount, 1);
  });

  test(
    'loadAgents ignores stale response from an older category request',
    () async {
      final pendingResponses = <Completer<dynamic>>[
        Completer<dynamic>(),
        Completer<dynamic>(),
      ];
      var requestIndex = 0;

      final dio = Dio(BaseOptions(baseUrl: 'https://example.com'));
      dio.interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) async {
            if (_isSharedWithMeRequest(options)) {
              handler.resolve(_emptyOkResponse(options));
              return;
            }
            final currentIndex = requestIndex++;
            final data = await pendingResponses[currentIndex].future;
            handler.resolve(
              Response<dynamic>(
                requestOptions: options,
                statusCode: 200,
                data: data,
              ),
            );
          },
        ),
      );

      final service = AgentService(dio: dio);
      await service.init();

      final firstLoad = service.loadAgents();
      final secondLoad = service.loadAgents(categoryId: 'cat-1');

      pendingResponses[1].complete({
        'code': 0,
        'data': <Map<String, dynamic>>[
          {
            'id': 'agent-new',
            'agent_name': 'Newest Agent',
            'provider_type': 3,
            'category_id': 'cat-1',
          },
        ],
      });
      await secondLoad;

      expect(service.agents, hasLength(1));
      expect(service.agents.single.id, 'agent-new');

      pendingResponses[0].complete({
        'code': 0,
        'data': <Map<String, dynamic>>[
          {
            'id': 'agent-old',
            'agent_name': 'Older Agent',
            'provider_type': 3,
            'category_id': '0',
          },
        ],
      });
      await firstLoad;

      expect(service.agents, hasLength(1));
      expect(service.agents.single.id, 'agent-new');
    },
  );

  test('updateAgent syncs matching private agent sessions in memory', () async {
    final imService = Get.put<ImService>(ImService());
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session-agent-sync-1',
        title: 'Old Agent Name',
        type: 'private',
        peerId: 'agent-sync-1',
        peerType: 2,
        peerNickname: 'Old Agent Name',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
      SessionModel(
        sessionId: 'session-agent-sync-2',
        title: 'Custom Topic',
        type: 'private',
        peerId: 'agent-sync-1',
        peerType: 2,
        peerNickname: 'Old Agent Name',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
      SessionModel(
        sessionId: 'session-agent-sync-3',
        title: 'Fresh Session Name',
        type: 'private',
        peerId: 'agent-sync-1',
        peerType: 2,
        peerNickname: 'Fresh Session Name',
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    ]);

    final service = AgentService(
      dio: _buildDelayedSuccessDio(
        onRequest: () {},
        data: {
          'code': 0,
          'data': {
            'id': 'agent-sync-1',
            'agent_name': 'New Agent Name',
            'provider_type': 3,
          },
        },
      ),
    );
    await service.init();
    service.agents.assignAll([
      AgentModel(
        id: 'agent-sync-1',
        agentName: 'Old Agent Name',
        providerType: 3,
      ),
    ]);

    final updated = await service.updateAgent('agent-sync-1', {
      'agent_name': 'New Agent Name',
    });

    expect(updated?.agentName, 'New Agent Name');
    expect(imService.sessions[0].peerNickname, 'New Agent Name');
    expect(imService.sessions[0].title, 'New Agent Name');
    expect(imService.sessions[1].peerNickname, 'New Agent Name');
    expect(imService.sessions[1].title, 'Custom Topic');
    expect(imService.sessions[2].peerNickname, 'Fresh Session Name');
    expect(imService.sessions[2].title, 'Fresh Session Name');
  });
}
