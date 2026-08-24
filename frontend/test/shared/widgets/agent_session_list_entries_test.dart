import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/agent_session_list/agent_session_list.dart';

AgentSessionBindingEntry _entry({
  String aibotSessionId = '',
  String agentSessionId = '',
  String cwd = '',
  String? title,
}) {
  return AgentSessionBindingEntry(
    aibotSessionId: aibotSessionId,
    agentSessionId: agentSessionId,
    cwd: cwd,
    workerStatus: 'ready',
    createdAt: 1,
    updatedAt: 2,
    archived: false,
    title: title,
  );
}

void main() {
  group('resolveAgentSessionEntries', () {
    test('未绑定条目原样保留', () {
      final out = resolveAgentSessionEntries(
        [_entry(agentSessionId: 'thread-1', cwd: '/w', title: 'raw')],
        sessionExists: (_) => false,
        localTitleFor: (_) => null,
      );
      expect(out, hasLength(1));
      expect(out.single.hasAibotSession, isFalse);
      expect(out.single.title, 'raw');
    });

    test('已绑定且会话存在时用本地标题覆盖', () {
      final out = resolveAgentSessionEntries(
        [
          _entry(
            aibotSessionId: 's1',
            agentSessionId: 'thread-1',
            cwd: '/w',
            title: 'provider title',
          ),
        ],
        sessionExists: (sid) => sid == 's1',
        localTitleFor: (_) => 'local title',
      );
      expect(out.single.title, 'local title');
      expect(out.single.aibotSessionId, 's1');
    });

    // 守卫：App 侧会话被删除后，电脑端会话不得从列表永久消失——
    // 必须降级为未绑定条目，保留 provider 会话身份与 cwd 供重新导入。
    test('会话已删除时降级为可重新导入的未绑定条目', () {
      final out = resolveAgentSessionEntries(
        [
          _entry(
            aibotSessionId: 'deleted-session',
            agentSessionId: 'thread-1',
            cwd: '/workspace',
            title: 'old chat',
          ),
        ],
        sessionExists: (_) => false,
        localTitleFor: (_) => null,
      );
      expect(out, hasLength(1));
      final entry = out.single;
      expect(entry.hasAibotSession, isFalse);
      expect(entry.agentSessionId, 'thread-1');
      expect(entry.cwd, '/workspace');
      expect(entry.workerStatus, 'inactive');
      expect(entry.title, 'old chat');
    });

    test('孤儿绑定且会话已删时丢弃', () {
      final out = resolveAgentSessionEntries(
        [
          _entry(aibotSessionId: 'deleted-session'),
          _entry(aibotSessionId: 'deleted-session', agentSessionId: 'thread-2'),
        ],
        sessionExists: (_) => false,
        localTitleFor: (_) => null,
      );
      // 两条都没有完整的重建信息（缺 agentSessionId 或缺 cwd），全部丢弃。
      expect(out, isEmpty);
    });
  });
}
