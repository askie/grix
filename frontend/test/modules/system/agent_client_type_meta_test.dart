import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/system/agent_client_type_meta.dart';

void main() {
  group('system agent client type metadata', () {
    test('uses the expected supported types in display order', () {
      expect(
        kSystemAgentClientTypes.map((meta) => meta.clientType).toList(),
        const [
          'openclaw',
          'claude',
          'codex',
          'gemini',
          'qwen',
          'pi',
          'hermes',
          'reasonix',
          'codewhale',
          'opencode',
          'kiro',
          'copilot',
          'agy',
          'kimi',
          'deepseek',
        ],
      );
    });

    test('resolves supported types case-insensitively', () {
      expect(systemAgentClientTypeMeta(' Qwen ')?.label, 'Qwen');
      expect(systemAgentClientTypeMeta('COPILOT')?.label, 'GitHub Copilot');
      expect(systemAgentClientTypeMeta('cursor'), isNull);
      expect(systemAgentClientTypeMeta('openhuman'), isNull);
      expect(systemAgentClientTypeMeta('pi')?.label, 'Pi');
    });
  });
}
