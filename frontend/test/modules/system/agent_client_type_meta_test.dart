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
          'traecli',
          'omp',
          'codebuddy',
          'grok',
          'qwenpaw',
          'zeroclaw',
        ],
      );
    });

    test('resolves supported types case-insensitively', () {
      expect(systemAgentClientTypeMeta(' Qwen ')?.label, 'Qwen');
      expect(systemAgentClientTypeMeta('COPILOT')?.label, 'GitHub Copilot');
      expect(systemAgentClientTypeMeta('cursor'), isNull);
      expect(systemAgentClientTypeMeta('openhuman'), isNull);
      expect(systemAgentClientTypeMeta('pi')?.label, 'Pi');
      expect(systemAgentClientTypeMeta(' TraeCLI ')?.label, 'TraeCLI');
      expect(systemAgentClientTypeMeta(' Grok ')?.label, 'Grok');
      expect(systemAgentClientTypeMeta('QWENPAW')?.label, 'QwenPaw');
      expect(systemAgentClientTypeMeta('zeroclaw')?.label, 'ZeroClaw');
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
      // pi.svg/dim.svg 都是同款 32x32 全幅圆角底板占位图（同 omp.svg），三者的
      // selfContained 必须一致，否则插在共享圆形底盘上会出现两层底盘叠加。
      expect(systemAgentClientTypeMeta('pi')?.selfContained, isTrue);
      expect(systemAgentClientTypeMeta('dim')?.selfContained, isTrue);
    });

    test('round3: omp/codebuddy resolve with the expected assets', () {
      expect(systemAgentClientTypeMeta('OMP')?.label, 'Oh-My-Pi');
      expect(systemAgentClientTypeMeta('omp')?.command, 'omp');
      expect(
        systemAgentClientTypeMeta('omp')?.logoAsset,
        'assets/icons/agent_clients/omp.svg',
      );
      // 32x32 全幅底板占位图（同 pi.svg/dim.svg 样式），插在共享圆形底盘上要满幅铺开。
      expect(systemAgentClientTypeMeta('omp')?.selfContained, isTrue);
      expect(systemAgentClientTypeMeta('omp')?.monochrome, isFalse);

      expect(
        systemAgentClientTypeMeta(' CodeBuddy ')?.label,
        'CodeBuddy Code',
      );
      expect(systemAgentClientTypeMeta('codebuddy')?.command, 'codebuddy');
      expect(
        systemAgentClientTypeMeta('codebuddy')?.logoAsset,
        'assets/icons/agent_clients/codebuddy.svg',
      );
      // 官方 Simple Icons 单色路径已带自己的品牌色，不需要运行时着色/满幅处理。
      expect(systemAgentClientTypeMeta('codebuddy')?.monochrome, isFalse);
      expect(systemAgentClientTypeMeta('codebuddy')?.selfContained, isFalse);
    });
  });
}
