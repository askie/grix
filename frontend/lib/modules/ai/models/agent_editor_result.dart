enum AgentEditorResult {
  saved('common_save_success'),
  deleted('ai_agent_delete_success');

  const AgentEditorResult(this.toastKey);

  final String toastKey;
}
