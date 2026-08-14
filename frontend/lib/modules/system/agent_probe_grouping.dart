import 'agent_client_type_meta.dart';
import 'grix_connector_service.dart';

class AgentProbeGroup {
  const AgentProbeGroup({
    required this.meta,
    required this.results,
    required this.status,
    this.installedClient,
  });

  final AgentClientTypeMeta meta;
  final List<AgentProbeResult> results;
  final String status;
  final InstalledClientCommand? installedClient;
}

List<AgentProbeGroup> buildAgentProbeGroups(
  Iterable<AgentProbeResult> results, {
  Iterable<InstalledClientCommand> installedClients = const [],
  bool includeEmpty = false,
}) {
  final byType = <String, List<AgentProbeResult>>{};
  for (final result in results) {
    if (isHiddenAgentProbeResult(result)) continue;
    final meta = systemAgentClientTypeMeta(result.clientType);
    if (meta == null) continue;
    byType.putIfAbsent(meta.clientType, () => <AgentProbeResult>[]).add(result);
  }
  final installedByType = <String, InstalledClientCommand>{};
  for (final client in installedClients) {
    if (isHiddenInstalledClientCommand(client)) continue;
    final meta = systemAgentClientTypeMeta(client.clientType);
    if (meta == null) continue;
    installedByType[meta.clientType] = client;
  }

  final groups = <AgentProbeGroup>[];
  for (final meta in kSystemAgentClientTypes) {
    final list = byType[meta.clientType] ?? const <AgentProbeResult>[];
    final installedClient = installedByType[meta.clientType];
    if (list.isEmpty && installedClient == null && !includeEmpty) continue;
    groups.add(
      AgentProbeGroup(
        meta: meta,
        results: List<AgentProbeResult>.unmodifiable(list),
        status: list.isEmpty
            ? (installedClient == null
                  ? 'unavailable'
                  : installedClient.installed
                  ? 'installed'
                  : 'not_installed')
            : aggregateProbeStatus(list.map((item) => item.status)),
        installedClient: installedClient,
      ),
    );
  }
  return List<AgentProbeGroup>.unmodifiable(groups);
}

bool isHiddenAgentProbeResult(AgentProbeResult result) {
  return _isInternalStatusName(result.clientType) ||
      _isInternalStatusName(result.agentName) ||
      _isInternalStatusName(result.cli?.command ?? '');
}

bool isHiddenInstalledClientCommand(InstalledClientCommand client) {
  return _isInternalStatusName(client.clientType) ||
      _isInternalStatusName(client.command);
}

bool _isInternalStatusName(String value) {
  return value.trim().toLowerCase() == 'sentry';
}

String aggregateProbeStatus(Iterable<String> statuses) {
  var selected = 'healthy';
  var selectedRank = -1;
  for (final status in statuses) {
    final rank = probeStatusRank(status);
    if (rank > selectedRank) {
      selected = status;
      selectedRank = rank;
    }
  }
  return selected;
}

int probeStatusRank(String status) {
  switch (status) {
    case 'error':
      return 4;
    case 'degraded':
      return 3;
    case 'installed':
      return 2;
    case 'unavailable':
      return 2;
    case 'not_installed':
      return 2;
    case 'healthy':
      return 1;
    default:
      return 0;
  }
}
