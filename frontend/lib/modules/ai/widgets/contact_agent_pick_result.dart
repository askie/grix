/// 联系人 / Agent 选择结果：界面展示用 [displayName]，落库用 [id]。
class ContactAgentPickResult {
  const ContactAgentPickResult({
    required this.id,
    required this.displayName,
    this.avatarUrl = '',
  });

  final String id;
  final String displayName;
  final String avatarUrl;
}
