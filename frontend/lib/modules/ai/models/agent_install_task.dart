/// Resolves a backend-authored install task template into a copyable task.
///
/// The template carries `{{agent_name}} / {{agent_id}} / {{api_key}} /
/// {{api_endpoint}}` placeholders. Returns an empty string when any referenced
/// placeholder has no value — never a half-filled task that still carries a
/// literal `{{api_key}}`.
String resolveAgentInstallTask({
  required String template,
  required String agentName,
  required String agentId,
  required String apiKey,
  required String apiEndpoint,
}) {
  var resolved = template.trim();
  if (resolved.isEmpty) {
    return '';
  }
  final values = <String, String>{
    'agent_name': agentName.trim(),
    'agent_id': agentId.trim(),
    'api_key': apiKey.trim(),
    'api_endpoint': apiEndpoint.trim(),
  };
  for (final entry in values.entries) {
    final placeholder = '{{${entry.key}}}';
    if (!resolved.contains(placeholder)) {
      continue;
    }
    if (entry.value.isEmpty) {
      return '';
    }
    resolved = resolved.replaceAll(placeholder, entry.value);
  }
  return resolved;
}
