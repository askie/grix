import 'package:flutter/material.dart';

@immutable
class AgentClientTypeMeta {
  const AgentClientTypeMeta({
    required this.clientType,
    required this.label,
    required this.logoAsset,
    required this.command,
    required this.sortOrder,
    this.monochrome = false,
  });

  final String clientType;
  final String label;
  final String logoAsset;
  final String command;
  final int sortOrder;

  /// Whether this logo is a single-color SVG without explicit fills.
  /// When true, a theme-aware color filter is applied for visibility.
  final bool monochrome;
}

const kSystemAgentClientTypes = <AgentClientTypeMeta>[
  AgentClientTypeMeta(
    clientType: 'openclaw',
    label: 'OpenClaw',
    logoAsset: 'assets/icons/agent_clients/openclaw.svg',
    command: 'openclaw',
    sortOrder: 0,
  ),
  AgentClientTypeMeta(
    clientType: 'claude',
    label: 'Claude',
    logoAsset: 'assets/icons/agent_clients/anthropic.svg',
    command: 'claude',
    sortOrder: 1,
    monochrome: true,
  ),
  AgentClientTypeMeta(
    clientType: 'codex',
    label: 'Codex',
    logoAsset: 'assets/icons/agent_clients/openai.svg',
    command: 'codex',
    sortOrder: 2,
    monochrome: true,
  ),
  AgentClientTypeMeta(
    clientType: 'gemini',
    label: 'Gemini',
    logoAsset: 'assets/icons/agent_clients/googlegemini.svg',
    command: 'gemini',
    sortOrder: 3,
    monochrome: true,
  ),
  AgentClientTypeMeta(
    clientType: 'qwen',
    label: 'Qwen',
    logoAsset: 'assets/icons/agent_clients/qwen.svg',
    command: 'qwen',
    sortOrder: 4,
    monochrome: true,
  ),
  AgentClientTypeMeta(
    clientType: 'pi',
    label: 'Pi',
    logoAsset: 'assets/icons/agent_clients/pi.svg',
    command: 'pi',
    sortOrder: 5,
  ),
  AgentClientTypeMeta(
    clientType: 'hermes',
    label: 'Hermes',
    logoAsset: 'assets/icons/agent_clients/hermes.svg',
    command: 'hermes',
    sortOrder: 6,
  ),
  AgentClientTypeMeta(
    clientType: 'reasonix',
    label: 'Reasonix',
    logoAsset: 'assets/icons/agent_clients/reasonix.svg',
    command: 'reasonix',
    sortOrder: 7,
  ),
  AgentClientTypeMeta(
    clientType: 'codewhale',
    label: 'CodeWhale',
    logoAsset: 'assets/icons/agent_clients/codewhale.svg',
    command: 'codewhale',
    sortOrder: 8,
  ),
  AgentClientTypeMeta(
    clientType: 'opencode',
    label: 'OpenCode',
    logoAsset: 'assets/icons/agent_clients/opencode.svg',
    command: 'opencode',
    sortOrder: 9,
  ),
  AgentClientTypeMeta(
    clientType: 'kiro',
    label: 'Kiro',
    logoAsset: 'assets/icons/agent_clients/kiro.svg',
    command: 'kiro-cli',
    sortOrder: 10,
  ),
  AgentClientTypeMeta(
    clientType: 'copilot',
    label: 'GitHub Copilot',
    logoAsset: 'assets/icons/agent_clients/github-copilot.svg',
    command: 'gh',
    sortOrder: 11,
    monochrome: true,
  ),
  AgentClientTypeMeta(
    clientType: 'agy',
    label: 'Antigravity',
    logoAsset: 'assets/icons/agent_clients/agy.svg',
    command: 'agy',
    sortOrder: 12,
    monochrome: true,
  ),
  AgentClientTypeMeta(
    clientType: 'kimi',
    label: 'Kimi',
    logoAsset: 'assets/icons/agent_clients/kimi.svg',
    command: 'kimi',
    sortOrder: 13,
    monochrome: true,
  ),
  AgentClientTypeMeta(
    clientType: 'deepseek',
    label: 'DeepSeek',
    logoAsset: 'assets/icons/agent_clients/deepseek.svg',
    command: 'dsh',
    sortOrder: 14,
  ),
];

final Map<String, AgentClientTypeMeta> kSystemAgentClientTypeMetaByType = {
  for (final meta in kSystemAgentClientTypes) meta.clientType: meta,
};

AgentClientTypeMeta? systemAgentClientTypeMeta(String rawClientType) {
  final normalized = rawClientType.trim().toLowerCase();
  if (normalized.isEmpty) return null;
  return kSystemAgentClientTypeMetaByType[normalized];
}

bool isSupportedSystemAgentClientType(String rawClientType) {
  return systemAgentClientTypeMeta(rawClientType) != null;
}
