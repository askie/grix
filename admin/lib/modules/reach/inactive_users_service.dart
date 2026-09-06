import '../../core/network/api_client.dart';

/// 近 N 天没有任何 agent 连接过的「沉默用户」。手机号只有末四位脱敏串，本期不发短信。
class InactiveAgentUser {
  InactiveAgentUser({
    required this.userId,
    required this.nickname,
    required this.email,
    required this.phoneMasked,
    required this.agentTotal,
    required this.createdAt,
    required this.lastAgentConnectedAt,
  });

  final String userId,
      nickname,
      email,
      phoneMasked,
      createdAt,
      lastAgentConnectedAt;
  final int agentTotal;

  bool get hasEmail => email.isNotEmpty;
  bool get neverConnected => lastAgentConnectedAt.isEmpty;

  factory InactiveAgentUser.fromJson(Map<String, dynamic> j) =>
      InactiveAgentUser(
        userId: (j['user_id'] ?? '').toString(),
        nickname: (j['nickname'] ?? '').toString(),
        email: (j['email'] ?? '').toString(),
        phoneMasked: (j['phone_masked'] ?? '').toString(),
        agentTotal: (j['agent_total'] as num?)?.toInt() ?? 0,
        createdAt: (j['created_at'] ?? '').toString(),
        lastAgentConnectedAt: (j['last_agent_connected_at'] ?? '').toString(),
      );
}

/// 发送前预览：阿里云模板正文替换 {name}/{body} 之后的结果。
class InactiveUsersService {
  static Future<
    ({List<InactiveAgentUser> users, int total, int defaultTemplateId})
  >
  listInactiveUsers({
    required int noAgentDays,
    String? region,
    int page = 1,
    int pageSize = 20,
  }) async {
    final data = await ApiClient.instance.get(
      '/users/inactive-agent-users',
      query: {
        'no_agent_days': noAgentDays,
        if (region != null && region.isNotEmpty) 'region': region,
        'page': page,
        'page_size': pageSize,
      },
    );
    final m = (data as Map).cast<String, dynamic>();
    final list = ((m['users'] as List?) ?? [])
        .map(
          (e) => InactiveAgentUser.fromJson((e as Map).cast<String, dynamic>()),
        )
        .toList();
    return (
      users: list,
      total: (m['total'] as num?)?.toInt() ?? 0,
      defaultTemplateId: (m['default_email_template_id'] as num?)?.toInt() ?? 0,
    );
  }
}
