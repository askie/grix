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
          'qodercli',
          'qoderclicn',
          'mcode',
          'dim',
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

    test('round2a: qodercli/qoderclicn/mcode/dim resolve with the expected assets', () {
      expect(systemAgentClientTypeMeta('qodercli')?.label, 'Qoder CLI');
      expect(systemAgentClientTypeMeta('qodercli')?.command, 'qodercli');
      expect(
        systemAgentClientTypeMeta('qodercli')?.logoAsset,
        'assets/icons/agent_clients/qoder.svg',
      );
      expect(systemAgentClientTypeMeta('qodercli')?.monochrome, isTrue);

      expect(systemAgentClientTypeMeta(' QoderCliCN ')?.label, 'Qoder CLI CN');
      expect(
        systemAgentClientTypeMeta('qoderclicn')?.logoAsset,
        'assets/icons/agent_clients/qoder.svg',
      );

      expect(systemAgentClientTypeMeta('MCODE')?.label, 'MiniMax Code');
      expect(systemAgentClientTypeMeta('mcode')?.command, 'mcode');
      expect(
        systemAgentClientTypeMeta('mcode')?.logoAsset,
        'assets/icons/agent_clients/minimax.svg',
      );

      expect(systemAgentClientTypeMeta('dim')?.label, 'DimAgent');
      expect(
        systemAgentClientTypeMeta('dim')?.logoAsset,
        'assets/icons/agent_clients/dim.svg',
      );
    });
  });
}
