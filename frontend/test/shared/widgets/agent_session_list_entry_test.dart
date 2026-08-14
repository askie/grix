import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/agent_session_list/agent_session_list.dart';

void main() {
  group('AgentSessionBindingEntry.fromMap', () {
    test('parses integer timestamps', () {
      final entry = AgentSessionBindingEntry.fromMap({
        'agentSessionId': 'sess-1',
        'updatedAt': 1781177482074,
        'createdAt': 1781177000000,
      });

      expect(entry.updatedAt, 1781177482074);
      expect(entry.createdAt, 1781177000000);
    });

    test('parses double timestamps from older connectors by truncating', () {
      // 旧版 connector 直接发送文件 mtime，带亚毫秒小数
      final entry = AgentSessionBindingEntry.fromMap({
        'agentSessionId': 'sess-2',
        'updatedAt': 1781177116690.9026,
        'createdAt': 1781177116690.9026,
      });

      expect(entry.updatedAt, 1781177116690);
      expect(entry.createdAt, 1781177116690);
    });

    test('parses numeric strings with decimals', () {
      final entry = AgentSessionBindingEntry.fromMap({
        'agentSessionId': 'sess-3',
        'updatedAt': '1781177116690.9026',
      });

      expect(entry.updatedAt, 1781177116690);
    });

    test('falls back to 0 for missing or invalid values', () {
      final entry = AgentSessionBindingEntry.fromMap({
        'agentSessionId': 'sess-4',
        'updatedAt': 'not-a-number',
      });

      expect(entry.updatedAt, 0);
      expect(entry.createdAt, 0);
    });
  });
}
