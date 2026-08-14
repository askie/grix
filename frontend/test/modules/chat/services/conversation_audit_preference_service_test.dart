import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/conversation_audit_preference_service.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('server state is shared as one reactive value per agent', () {
    final service = ConversationAuditPreferenceService();

    service.applyServerState(agentId: 'agent-1', enabled: true);

    final first = service.stateForAgent(agentId: 'agent-1');
    final second = service.stateForAgent(agentId: 'agent-1');
    expect(identical(first, second), isTrue);
    expect(first.value, isTrue);

    // 任意后续快照直接刷新同一个镜像。
    service.applyServerState(agentId: 'agent-1', enabled: false);
    expect(first.value, isFalse);

    // 其他 agent 不受影响。
    expect(service.stateForAgent(agentId: 'agent-2').value, isFalse);
  });

  test('toggle is optimistic and goes through the sender', () async {
    final service = ConversationAuditPreferenceService();
    service.applyServerState(agentId: 'agent-1', enabled: false);
    final sent = <Map<String, Object>>[];
    service.serverStateSender = (String sessionId, String agentId, bool enabled) {
      sent.add(<String, Object>{
        'session_id': sessionId,
        'agent_id': agentId,
        'enabled': enabled,
      });
      return true;
    };

    await service.toggle(agentId: 'agent-1', sessionId: 'session-1');

    expect(sent, hasLength(1));
    expect(sent.single['session_id'], 'session-1');
    expect(sent.single['agent_id'], 'agent-1');
    expect(sent.single['enabled'], isTrue);
    expect(service.stateForAgent(agentId: 'agent-1').value, isTrue);
  });

  test('toggle reverts when the packet cannot be sent', () async {
    final service = ConversationAuditPreferenceService();
    service.applyServerState(agentId: 'agent-1', enabled: false);
    service.serverStateSender =
        (String sessionId, String agentId, bool enabled) => false;

    await service.toggle(agentId: 'agent-1');

    expect(service.stateForAgent(agentId: 'agent-1').value, isFalse);
  });

  test('toggle without a wired sender reverts and sends nothing', () async {
    final service = ConversationAuditPreferenceService();
    service.applyServerState(agentId: 'agent-1', enabled: true);

    await service.toggle(agentId: 'agent-1');

    expect(service.stateForAgent(agentId: 'agent-1').value, isTrue);
  });

  test('empty agent id is ignored', () async {
    final service = ConversationAuditPreferenceService();
    service.applyServerState(agentId: ' ', enabled: true);
    await service.setEnabled(agentId: '', enabled: true);
    expect(service.stateForAgent(agentId: '').value, isFalse);
  });

  test('resetForAccountSwitch clears mirror and sender', () async {
    final service = ConversationAuditPreferenceService();
    service.applyServerState(agentId: 'agent-1', enabled: true);
    service.serverStateSender =
        (String sessionId, String agentId, bool enabled) => true;

    service.resetForAccountSwitch();

    expect(service.stateForAgent(agentId: 'agent-1').value, isFalse);
    expect(service.serverStateSender, isNull);
    // 重置后 toggle 因 sender 未接线回滚，不产生跨用户的误切换。
    await service.toggle(agentId: 'agent-1');
    expect(service.stateForAgent(agentId: 'agent-1').value, isFalse);
  });
}
