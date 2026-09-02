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
class ReachEmailPreview {
  ReachEmailPreview({
    required this.templateId,
    required this.subject,
    required this.html,
    required this.error,
  });
  final String subject, html, error;
  final int templateId;

  factory ReachEmailPreview.fromJson(Map<String, dynamic> j) =>
      ReachEmailPreview(
        templateId: (j['template_id'] as num?)?.toInt() ?? 0,
        subject: (j['subject'] ?? '').toString(),
        html: (j['html'] ?? '').toString(),
        error: (j['error'] ?? '').toString(),
      );
}

/// 单个用户的发送结果。
class InactiveReachResult {
  InactiveReachResult({
    required this.userId,
    required this.channel,
    required this.status,
    required this.error,
  });
  final String userId, channel, status, error;

  bool get isSent => status == 'sent';

  String get statusLabel {
    switch (status) {
      case 'sent':
        return '已发送';
      case 'skipped':
        return '跳过';
      case 'failed':
        return '失败';
      default:
        return status.isEmpty ? '未知' : status;
    }
  }

  /// /reach/direct 的返回体：{task, channel, status, attempts}。
  /// 整单失败时顶层没有渠道，取最后一次尝试的错误做展示。
  factory InactiveReachResult.fromResponse(
    String userId,
    Map<String, dynamic> j,
  ) {
    final attempts = ((j['attempts'] as List?) ?? const [])
        .whereType<Map>()
        .map((e) => e.cast<String, dynamic>())
        .toList();
    var error = '';
    for (final a in attempts) {
      final e = (a['error'] ?? '').toString();
      if (e.isNotEmpty) error = e;
    }
    var channel = (j['channel'] ?? '').toString();
    if (channel.isEmpty && attempts.isNotEmpty) {
      channel = (attempts.last['channel'] ?? '').toString();
    }
    return InactiveReachResult(
      userId: userId,
      channel: channel,
      status: (j['status'] ?? '').toString(),
      error: error,
    );
  }
}

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

  static Future<ReachEmailPreview> previewEmail({
    required String title,
    required String body,
    int? emailTemplateId,
    String? sampleUserId,
  }) async {
    final data = await ApiClient.instance.post(
      '/reach/email-preview',
      data: {
        'title': title,
        'body': body,
        if (emailTemplateId != null && emailTemplateId > 0)
          'email_template_id': emailTemplateId,
        if (sampleUserId != null && sampleUserId.isNotEmpty)
          'sample_user_id': sampleUserId,
      },
    );
    final m = (data as Map).cast<String, dynamic>();
    return ReachEmailPreview.fromJson(
      (m['preview'] as Map).cast<String, dynamic>(),
    );
  }

  /// 逐人调用 /reach/direct 发邮件。channels 固定 ['email']，不兜底短信；
  /// marketing=true 让后端按营销订阅口径跳过未 opt-in 的用户。
  static Future<InactiveReachResult> sendOne({
    required InactiveAgentUser user,
    required String title,
    required String body,
    required String dedupKey,
    int? emailTemplateId,
  }) async {
    final data = await ApiClient.instance.post(
      '/reach/direct',
      data: {
        'user_id': user.userId,
        'title': title,
        'long_text': body,
        'event_key': 'inactive_agent_marketing',
        'dedup_key': dedupKey,
        'channels': ['email'],
        'marketing': true,
        if (emailTemplateId != null && emailTemplateId > 0)
          'email_template_id': emailTemplateId,
      },
    );
    return InactiveReachResult.fromResponse(
      user.userId,
      (data as Map).cast<String, dynamic>(),
    );
  }
}
