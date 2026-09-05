/// Agent 自定义斜杠命令。内置命令随工具栏快照下发、不在这里；
/// 这个模型只对应主人自己加的那部分（REST /agents/:id/slash-commands）。
class AgentSlashCommandEntry {
  const AgentSlashCommandEntry({
    required this.id,
    required this.name,
    required this.description,
  });

  final String id;
  final String name;
  final String description;

  factory AgentSlashCommandEntry.fromJson(Map<String, dynamic> json) {
    return AgentSlashCommandEntry(
      id: json['id']?.toString().trim() ?? '',
      name: json['name']?.toString().trim() ?? '',
      description: json['description']?.toString().trim() ?? '',
    );
  }
}
