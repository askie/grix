import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/system/agent_probe_grouping.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

void main() {
  group('agent probe grouping', () {
    test('groups only supported system types in metadata order', () {
      const results = [
        AgentProbeResult(agentName: 'qwen-1', clientType: 'qwen'),
        AgentProbeResult(agentName: 'cursor-1', clientType: 'cursor'),
        AgentProbeResult(agentName: 'openclaw-1', clientType: 'openclaw'),
      ];

      final groups = buildAgentProbeGroups(results);

      expect(groups.map((group) => group.meta.clientType).toList(), [
        'openclaw',
        'qwen',
      ]);
    });

    test('can include supported types without probe results', () {
      final groups = buildAgentProbeGroups(const [], includeEmpty: true);

      expect(groups, hasLength(14));
      expect(groups.first.meta.clientType, 'openclaw');
      expect(groups.last.meta.clientType, 'kimi');
      expect(groups.every((group) => group.results.isEmpty), isTrue);
      expect(groups.every((group) => group.status == 'unavailable'), isTrue);
    });

    test('includes installed clients without deployed agents', () {
      final groups = buildAgentProbeGroups(
        const [],
        installedClients: const [
          InstalledClientCommand(clientType: 'claude', installed: true),
          InstalledClientCommand(clientType: 'cursor', installed: true),
        ],
      );

      expect(groups.map((group) => group.meta.clientType).toList(), ['claude']);
      expect(groups.single.status, 'installed');
      expect(groups.single.installedClient?.clientType, 'claude');
    });

    test('hides internal sentry status entries', () {
      const results = [
        AgentProbeResult(agentName: 'sentry', clientType: 'claude'),
        AgentProbeResult(
          agentName: 'codex-1',
          clientType: 'codex',
          cli: ProbeCliInfo(command: 'sentry', installed: true),
        ),
        AgentProbeResult(agentName: 'qwen-1', clientType: 'qwen'),
      ];

      final groups = buildAgentProbeGroups(
        results,
        installedClients: const [
          InstalledClientCommand(clientType: 'sentry', command: 'sentry'),
          InstalledClientCommand(clientType: 'claude', command: 'claude'),
        ],
      );

      expect(groups.map((group) => group.meta.clientType).toList(), [
        'claude',
        'qwen',
      ]);
      expect(groups.first.results, isEmpty);
      expect(groups.first.installedClient?.clientType, 'claude');
      expect(groups.last.results.single.agentName, 'qwen-1');
    });

    test('aggregates status by most severe state', () {
      expect(aggregateProbeStatus(['healthy', 'degraded']), 'degraded');
      expect(aggregateProbeStatus(['healthy', 'unavailable']), 'unavailable');
      expect(aggregateProbeStatus(['degraded', 'error']), 'error');
    });
  });
}
