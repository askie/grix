import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart' hide Response;

import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';

class _FakeAuthService extends AuthService {
  @override
  void attachAuthInterceptor(Dio dio) {}
}

AgentModel _agent(String id, {String name = ''}) {
  return AgentModel.fromJson({
    'id': id,
    'agent_name': name.isEmpty ? 'agent_$id' : name,
    'owner_id': '1',
    'status': 1,
    'provider_type': 3,
  });
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

  group('allAccessibleAgents', () {
    test('空 sharedAgents 返回 agents 副本', () {
      final svc = AgentService();
      svc.agents.assignAll([_agent('a1'), _agent('a2')]);

      final merged = svc.allAccessibleAgents;

      expect(merged.map((a) => a.id).toList(), ['a1', 'a2']);
    });

    test('空 agents 仅返回 sharedAgents', () {
      final svc = AgentService();
      svc.sharedAgents.assignAll([_agent('s1'), _agent('s2')]);

      final merged = svc.allAccessibleAgents;

      expect(merged.map((a) => a.id).toList(), ['s1', 's2']);
    });

    test('agents + sharedAgents 合并保持 agents 在前', () {
      final svc = AgentService();
      svc.agents.assignAll([_agent('owned_1'), _agent('owned_2')]);
      svc.sharedAgents.assignAll([_agent('shared_1')]);

      final ids = svc.allAccessibleAgents.map((a) => a.id).toList();

      expect(ids, ['owned_1', 'owned_2', 'shared_1']);
    });

    test('id 重复时 sharedAgents 不会覆盖 owner 版本', () {
      final svc = AgentService();
      svc.agents.assignAll([_agent('dup', name: 'owner-name')]);
      svc.sharedAgents.assignAll([_agent('dup', name: 'shared-name')]);

      final merged = svc.allAccessibleAgents;

      expect(merged.length, 1);
      expect(merged.single.agentName, 'owner-name');
    });

    test('sharedAgents 内重复 id 自身也去重', () {
      final svc = AgentService();
      svc.sharedAgents.assignAll([_agent('s1'), _agent('s1', name: 'second')]);

      final merged = svc.allAccessibleAgents;

      expect(merged.length, 1);
      expect(merged.single.id, 's1');
    });

    test('id 为空白时跳过 不进入合并结果', () {
      final svc = AgentService();
      svc.agents.assignAll([_agent('valid')]);
      svc.sharedAgents.assignAll([_agent('  '), _agent('shared_ok')]);

      final ids = svc.allAccessibleAgents.map((a) => a.id).toList();

      expect(ids.contains('  '), isFalse, reason: '空白 id 必须被剔除');
      expect(ids, ['valid', 'shared_ok']);
    });
  });
}
