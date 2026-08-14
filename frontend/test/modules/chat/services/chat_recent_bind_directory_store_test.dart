import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:grix/modules/chat/services/chat_recent_bind_directory_store.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late ChatRecentBindDirectoryStore store;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    store = ChatRecentBindDirectoryStore();
  });

  group('ChatRecentBindDirectoryStore', () {
    test('record 后新条目排在最前（MRU）', () async {
      await store.record(path: '/a', agentId: 'agent1', nowMs: 1);
      await store.record(path: '/b', agentId: 'agent1', nowMs: 2);

      final entries = await store.listForAgent('agent1');
      expect(entries.map((e) => e.path).toList(), ['/b', '/a']);
    });

    test('同 agent 同 path 重复绑定去重并上浮', () async {
      await store.record(path: '/a', agentId: 'agent1', nowMs: 1);
      await store.record(path: '/b', agentId: 'agent1', nowMs: 2);
      await store.record(path: '/a', agentId: 'agent1', nowMs: 3);

      final entries = await store.listForAgent('agent1');
      expect(entries.map((e) => e.path).toList(), ['/a', '/b']);
    });

    test('路径归一化：去空白与尾部分隔符后视为同一条', () async {
      await store.record(path: ' /a/b/ ', agentId: 'agent1', nowMs: 1);
      await store.record(path: '/a/b', agentId: 'agent1', nowMs: 2);

      final entries = await store.listForAgent('agent1');
      expect(entries, hasLength(1));
      expect(entries.first.path, '/a/b');
    });

    test('空路径与空 agent 均可安全调用', () async {
      await store.record(path: '   ', agentId: 'agent1');
      expect(await store.listForAgent('agent1'), isEmpty);
    });

    test('存储上限 30 条，最旧的被淘汰', () async {
      for (var i = 0; i < 35; i++) {
        await store.record(path: '/dir$i', agentId: 'agent1', nowMs: i);
      }
      final entries = await store.listForAgent('agent1', limit: 100);
      expect(entries, hasLength(ChatRecentBindDirectoryStore.maxStoredEntries));
      expect(entries.first.path, '/dir34');
      expect(entries.last.path, '/dir5');
    });

    test('listForAgent 默认最多返回 10 条', () async {
      for (var i = 0; i < 15; i++) {
        await store.record(path: '/dir$i', agentId: 'agent1', nowMs: i);
      }
      final entries = await store.listForAgent('agent1');
      expect(
        entries,
        hasLength(ChatRecentBindDirectoryStore.defaultDisplayLimit),
      );
    });

    test('当前 agent 的记录优先，同机器其他 agent 的补在后面', () async {
      await store.record(
        path: '/other-new',
        agentId: 'agent2',
        hostname: 'mac1',
        nowMs: 10,
      );
      await store.record(
        path: '/mine-old',
        agentId: 'agent1',
        hostname: 'mac1',
        nowMs: 1,
      );

      final entries = await store.listForAgent('agent1', hostname: 'mac1');
      expect(entries.map((e) => e.path).toList(), ['/mine-old', '/other-new']);
    });

    test('跨机器的其他 agent 记录不补位', () async {
      await store.record(
        path: '/on-mac2',
        agentId: 'agent2',
        hostname: 'mac2',
        nowMs: 10,
      );
      await store.record(
        path: '/mine',
        agentId: 'agent1',
        hostname: 'mac1',
        nowMs: 1,
      );

      final entries = await store.listForAgent('agent1', hostname: 'mac1');
      expect(entries.map((e) => e.path).toList(), ['/mine']);
    });

    test('当前 agent 机器名未知时不跨 agent 补位，仅展示自己的记录', () async {
      await store.record(
        path: '/other',
        agentId: 'agent2',
        hostname: 'mac1',
        nowMs: 10,
      );
      await store.record(path: '/mine', agentId: 'agent1', nowMs: 1);

      final entries = await store.listForAgent('agent1');
      expect(entries.map((e) => e.path).toList(), ['/mine']);
    });

    test('其他 agent 记录缺机器名时同样不补位', () async {
      await store.record(path: '/other', agentId: 'agent2', nowMs: 10);
      await store.record(
        path: '/mine',
        agentId: 'agent1',
        hostname: 'mac1',
        nowMs: 1,
      );

      final entries = await store.listForAgent('agent1', hostname: 'mac1');
      expect(entries.map((e) => e.path).toList(), ['/mine']);
    });

    test('同机器跨 agent 同 path 只展示一次', () async {
      await store.record(
        path: '/same',
        agentId: 'agent1',
        hostname: 'mac1',
        nowMs: 1,
      );
      await store.record(
        path: '/same',
        agentId: 'agent2',
        hostname: 'mac1',
        nowMs: 2,
      );

      final entries = await store.listForAgent('agent1', hostname: 'mac1');
      expect(entries, hasLength(1));
      expect(entries.first.agentId, 'agent1');
    });

    test('缓存损坏时按空列表处理', () async {
      SharedPreferences.setMockInitialValues({
        ChatRecentBindDirectoryStore.storageKey: 'not-json{{{',
      });
      final broken = ChatRecentBindDirectoryStore();
      expect(await broken.listForAgent('agent1'), isEmpty);
      // 损坏后仍可正常写入新记录。
      await broken.record(path: '/a', agentId: 'agent1', nowMs: 1);
      expect(await broken.listForAgent('agent1'), hasLength(1));
    });

    test('记录跨会话持久化（同一 SharedPreferences 重新构造可读回）', () async {
      await store.record(path: '/persist', agentId: 'agent1', nowMs: 1);
      final reopened = ChatRecentBindDirectoryStore();
      final entries = await reopened.listForAgent('agent1');
      expect(entries.map((e) => e.path).toList(), ['/persist']);
    });
  });

  group('RecentBindDirectoryEntry.displayName', () {
    test('取路径最后一段', () {
      const entry = RecentBindDirectoryEntry(
        path: '/Users/me/projects/aibot',
        agentId: 'a',
        hostname: '',
        updatedAtMs: 0,
      );
      expect(entry.displayName, 'aibot');
    });

    test('兼容 Windows 反斜杠路径', () {
      const entry = RecentBindDirectoryEntry(
        path: r'C:\work\repo',
        agentId: 'a',
        hostname: '',
        updatedAtMs: 0,
      );
      expect(entry.displayName, 'repo');
    });

    test('根目录回退为原始路径', () {
      const entry = RecentBindDirectoryEntry(
        path: '/',
        agentId: 'a',
        hostname: '',
        updatedAtMs: 0,
      );
      expect(entry.displayName, '/');
    });
  });

  group('isDirectoryBoundAgentClientType', () {
    test('排除名单之外的接入类型均展示', () {
      expect(isDirectoryBoundAgentClientType('claude'), isTrue);
      expect(isDirectoryBoundAgentClientType(' Codex '), isTrue);
      expect(isDirectoryBoundAgentClientType('pi'), isTrue);
      expect(isDirectoryBoundAgentClientType('codewhale'), isTrue);
      expect(isDirectoryBoundAgentClientType('agy'), isTrue);
    });

    test('hermes/openclaw/空类型不展示', () {
      expect(isDirectoryBoundAgentClientType('hermes'), isFalse);
      expect(isDirectoryBoundAgentClientType(' OpenClaw '), isFalse);
      expect(isDirectoryBoundAgentClientType(''), isFalse);
      expect(isDirectoryBoundAgentClientType('   '), isFalse);
    });
  });
}
