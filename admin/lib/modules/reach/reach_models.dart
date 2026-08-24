/// 公告单语言文案：title+body 组成站内信，email_subject/email_intro 供邮件使用。
class ReachAnnouncementLocale {
  ReachAnnouncementLocale({
    this.title = '',
    this.body = '',
    this.emailSubject = '',
    this.emailIntro = '',
  });

  final String title;
  final String body;
  final String emailSubject;
  final String emailIntro;

  bool get isEmpty =>
      title.isEmpty && body.isEmpty && emailSubject.isEmpty && emailIntro.isEmpty;

  factory ReachAnnouncementLocale.fromJson(Map<String, dynamic> j) =>
      ReachAnnouncementLocale(
        title: (j['title'] ?? '').toString(),
        body: (j['body'] ?? '').toString(),
        emailSubject: (j['email_subject'] ?? '').toString(),
        emailIntro: (j['email_intro'] ?? '').toString(),
      );

  Map<String, dynamic> toJson() => {
        'title': title,
        'body': body,
        'email_subject': emailSubject,
        'email_intro': emailIntro,
      };
}

/// 公告双语文案快照（对应后端 reach_tasks.content）。
class ReachAnnouncementContent {
  ReachAnnouncementContent({required this.zh, required this.en});

  final ReachAnnouncementLocale zh;
  final ReachAnnouncementLocale en;

  bool get isEmpty => zh.isEmpty && en.isEmpty;

  factory ReachAnnouncementContent.fromJson(Map<String, dynamic> j) {
    ReachAnnouncementLocale parse(dynamic raw) => raw is Map
        ? ReachAnnouncementLocale.fromJson(raw.cast<String, dynamic>())
        : ReachAnnouncementLocale();
    return ReachAnnouncementContent(zh: parse(j['zh']), en: parse(j['en']));
  }

  Map<String, dynamic> toJson() => {'zh': zh.toJson(), 'en': en.toJson()};
}

class ReachTask {
  ReachTask({
    required this.id,
    required this.kind,
    required this.eventKey,
    required this.templateId,
    required this.channels,
    required this.audience,
    required this.content,
    required this.status,
    this.scheduledAt,
    required this.stats,
    required this.region,
    required this.abGroupId,
    required this.abVariant,
    required this.createdBy,
    required this.createdAt,
    required this.updatedAt,
  });

  final String id;
  final String kind;
  final String eventKey;
  final String templateId;
  final List<String> channels;
  final Map<String, dynamic> audience;
  final ReachAnnouncementContent content;
  final String status;
  final DateTime? scheduledAt;
  final Map<String, dynamic> stats;
  final String region;
  final String abGroupId;
  final String abVariant;
  final String createdBy;
  final DateTime createdAt;
  final DateTime updatedAt;

  bool get isSending => status == 'sending';
  bool get isPaused => status == 'paused';
  bool get isDraft => status == 'draft';
  bool get canPause => status == 'sending';
  bool get canCancel =>
      status == 'draft' || status == 'sending' || status == 'paused';
  bool get canResume => status == 'paused';
  bool get isMarketing => kind == 'marketing';
  bool get isSystemEvent => kind == 'system_event';
  bool get isABTest => abGroupId.isNotEmpty;

  /// 待发送的系统公告草稿：可编辑文案、可手动发送。
  bool get isEditableAnnouncement => isSystemEvent && isDraft;

  String get statusLabel => switch (status) {
    'draft' => '待发送',
    'scheduled' => '定时',
    'sending' => '发送中',
    'sent' => '已完成',
    'paused' => '已暂停',
    'cancelled' => '已取消',
    _ => status,
  };

  String get kindLabel => switch (kind) {
    'system_event' => '系统事件',
    'marketing' => '营销',
    _ => kind,
  };

  factory ReachTask.fromJson(Map<String, dynamic> j) {
    final rawChannels = j['channels'];
    List<String> channels;
    if (rawChannels is List) {
      channels = rawChannels.map((e) => e.toString()).toList();
    } else {
      channels = const [];
    }

    final rawAudience = j['audience'];
    Map<String, dynamic> audience;
    if (rawAudience is Map) {
      audience = rawAudience.cast<String, dynamic>();
    } else {
      audience = const {};
    }

    final rawStats = j['stats'];
    Map<String, dynamic> stats;
    if (rawStats is Map) {
      stats = rawStats.cast<String, dynamic>();
    } else {
      stats = const {};
    }

    final rawContent = j['content'];
    final content = rawContent is Map
        ? ReachAnnouncementContent.fromJson(rawContent.cast<String, dynamic>())
        : ReachAnnouncementContent.fromJson(const {});

    return ReachTask(
      id: (j['id'] ?? '').toString(),
      kind: (j['kind'] ?? '').toString(),
      eventKey: (j['event_key'] ?? '').toString(),
      templateId: (j['template_id'] ?? '0').toString(),
      channels: channels,
      audience: audience,
      content: content,
      status: (j['status'] ?? '').toString(),
      scheduledAt: j['scheduled_at'] == null
          ? null
          : DateTime.tryParse(j['scheduled_at'].toString()),
      stats: stats,
      region: (j['region'] ?? '').toString(),
      abGroupId: (j['ab_group_id'] ?? '').toString(),
      abVariant: (j['ab_variant'] ?? '').toString(),
      createdBy: (j['created_by'] ?? '0').toString(),
      createdAt: DateTime.tryParse((j['created_at'] ?? '').toString()) ??
          DateTime.now(),
      updatedAt: DateTime.tryParse((j['updated_at'] ?? '').toString()) ??
          DateTime.now(),
    );
  }
}

class ReachTemplate {
  ReachTemplate({
    required this.id,
    required this.name,
    required this.title,
    required this.inAppBody,
    required this.pushBody,
    required this.emailHtml,
    this.smsBody = '',
    required this.createdAt,
    required this.updatedAt,
  });

  final String id;
  final String name;
  final String title;
  final String inAppBody;
  final String pushBody;
  final String emailHtml;
  final String smsBody;
  final DateTime createdAt;
  final DateTime updatedAt;

  factory ReachTemplate.fromJson(Map<String, dynamic> j) => ReachTemplate(
        id: (j['id'] ?? '').toString(),
        name: (j['name'] ?? '').toString(),
        title: (j['title'] ?? '').toString(),
        inAppBody: (j['in_app_body'] ?? '').toString(),
        pushBody: (j['push_body'] ?? '').toString(),
        emailHtml: (j['email_html'] ?? '').toString(),
        smsBody: (j['sms_body'] ?? '').toString(),
        createdAt: DateTime.tryParse((j['created_at'] ?? '').toString()) ??
            DateTime.now(),
        updatedAt: DateTime.tryParse((j['updated_at'] ?? '').toString()) ??
            DateTime.now(),
      );
}

class ReachSendLog {
  ReachSendLog({
    required this.id,
    required this.taskId,
    required this.userId,
    required this.channel,
    required this.status,
    required this.error,
    required this.createdAt,
    this.openedAt,
    this.clickedAt,
  });

  final String id;
  final String taskId;
  final String userId;
  final String channel;
  final String status;
  final String error;
  final DateTime createdAt;
  final DateTime? openedAt;
  final DateTime? clickedAt;

  String get channelLabel => switch (channel) {
    'in_app' => '站内信',
    'push' => '推送',
    'email' => '邮件',
    _ => channel,
  };

  String get statusLabel => switch (status) {
    'pending' => '待发',
    'sent' => '已发',
    'failed' => '失败',
    'skipped' => '跳过',
    _ => status,
  };

  factory ReachSendLog.fromJson(Map<String, dynamic> j) => ReachSendLog(
        id: (j['id'] ?? '').toString(),
        taskId: (j['task_id'] ?? '').toString(),
        userId: (j['user_id'] ?? '').toString(),
        channel: (j['channel'] ?? '').toString(),
        status: (j['status'] ?? '').toString(),
        error: (j['error'] ?? '').toString(),
        createdAt: DateTime.tryParse((j['created_at'] ?? '').toString()) ??
            DateTime.now(),
        openedAt: j['opened_at'] == null
            ? null
            : DateTime.tryParse(j['opened_at'].toString()),
        clickedAt: j['clicked_at'] == null
            ? null
            : DateTime.tryParse(j['clicked_at'].toString()),
      );
}

class ReachTaskDetail {
  ReachTaskDetail({required this.task, required this.sendLogs});

  final ReachTask task;
  final List<ReachSendLog> sendLogs;

  String get id => task.id;
  String get kind => task.kind;
  String get status => task.status;
  String get statusLabel => task.statusLabel;
  ReachAnnouncementContent get content => task.content;
  bool get isEditableAnnouncement => task.isEditableAnnouncement;
  String get eventKey => task.eventKey;
  String get templateId => task.templateId;
  List<String> get channels => task.channels;
  String get region => task.region;
  String get abGroupId => task.abGroupId;
  String get abVariant => task.abVariant;
  String get createdBy => task.createdBy;
  DateTime get createdAt => task.createdAt;
  DateTime get updatedAt => task.updatedAt;
  DateTime? get scheduledAt => task.scheduledAt;

  factory ReachTaskDetail.fromJson(Map<String, dynamic> j) {
    final rawLogs = (j['send_logs'] as List?) ?? const [];
    return ReachTaskDetail(
      task: ReachTask.fromJson(j),
      sendLogs: rawLogs
          .map((e) => ReachSendLog.fromJson((e as Map).cast<String, dynamic>()))
          .toList(),
    );
  }
}

class ReachTaskStats {
  ReachTaskStats({
    required this.taskId,
    required this.status,
    required this.channels,
    required this.regions,
    required this.totalLogs,
    required this.opened,
    required this.clicked,
    required this.openRate,
    required this.clickRate,
  });

  final String taskId;
  final String status;
  final Map<String, int> channels;
  final Map<String, int> regions;
  final int totalLogs;
  final int opened;
  final int clicked;
  final double openRate;
  final double clickRate;

  factory ReachTaskStats.fromJson(Map<String, dynamic> j) {
    Map<String, int> parseIntMap(dynamic raw) {
      if (raw is! Map) return const {};
      return raw
          .cast<String, dynamic>()
          .map((k, v) => MapEntry(k, (v as num?)?.toInt() ?? 0));
    }

    return ReachTaskStats(
      taskId: (j['task_id'] ?? '').toString(),
      status: (j['status'] ?? '').toString(),
      channels: parseIntMap(j['channel_breakdown']),
      regions: parseIntMap(j['region_breakdown']),
      totalLogs: (j['total_logs'] as num?)?.toInt() ?? 0,
      opened: (j['opened'] as num?)?.toInt() ?? 0,
      clicked: (j['clicked'] as num?)?.toInt() ?? 0,
      openRate: (j['open_rate'] as num?)?.toDouble() ?? 0,
      clickRate: (j['click_rate'] as num?)?.toDouble() ?? 0,
    );
  }
}

class ReachSubscriptionOverview {
  ReachSubscriptionOverview({
    required this.totalSubscriptions,
    required this.subscribed,
    required this.unsubscribed,
  });

  final int totalSubscriptions;
  final int subscribed;
  final int unsubscribed;

  factory ReachSubscriptionOverview.fromJson(Map<String, dynamic> j) =>
      ReachSubscriptionOverview(
        totalSubscriptions:
            (j['total_subscriptions'] as num?)?.toInt() ?? 0,
        subscribed: (j['subscribed'] as num?)?.toInt() ?? 0,
        unsubscribed: (j['unsubscribed'] as num?)?.toInt() ?? 0,
      );
}

class ABVariantStats {
  ABVariantStats({
    required this.variant,
    required this.taskId,
    required this.status,
    required this.sent,
    required this.opened,
    required this.clicked,
    required this.openRate,
    required this.clickRate,
    required this.channelBreakdown,
  });

  final String variant;
  final String taskId;
  final String status;
  final int sent;
  final int opened;
  final int clicked;
  final double openRate;
  final double clickRate;
  final Map<String, int> channelBreakdown;

  factory ABVariantStats.fromJson(Map<String, dynamic> j) {
    final rawBreakdown = j['channel_breakdown'];
    Map<String, int> breakdown;
    if (rawBreakdown is Map) {
      breakdown = rawBreakdown
          .cast<String, dynamic>()
          .map((k, v) => MapEntry(k, (v as num?)?.toInt() ?? 0));
    } else {
      breakdown = const {};
    }

    return ABVariantStats(
      variant: (j['variant'] ?? '').toString(),
      taskId: (j['task_id'] ?? '').toString(),
      status: (j['status'] ?? '').toString(),
      sent: (j['sent'] as num?)?.toInt() ?? 0,
      opened: (j['opened'] as num?)?.toInt() ?? 0,
      clicked: (j['clicked'] as num?)?.toInt() ?? 0,
      openRate: (j['open_rate'] as num?)?.toDouble() ?? 0,
      clickRate: (j['click_rate'] as num?)?.toDouble() ?? 0,
      channelBreakdown: breakdown,
    );
  }
}

class ABTestStats {
  ABTestStats({required this.abGroupId, required this.variants});

  final String abGroupId;
  final List<ABVariantStats> variants;

  factory ABTestStats.fromJson(Map<String, dynamic> j) {
    final rawVariants = (j['variants'] as List?) ?? const [];
    return ABTestStats(
      abGroupId: (j['ab_group_id'] ?? '').toString(),
      variants: rawVariants
          .map((e) =>
              ABVariantStats.fromJson((e as Map).cast<String, dynamic>()))
          .toList(),
    );
  }
}

class ABTestResult {
  ABTestResult({required this.abGroupId, required this.tasks});

  final String abGroupId;
  final List<ReachTask> tasks;

  factory ABTestResult.fromJson(Map<String, dynamic> j) {
    final rawTasks = (j['tasks'] as List?) ?? const [];
    return ABTestResult(
      abGroupId: (j['ab_group_id'] ?? '').toString(),
      tasks: rawTasks
          .map((e) => ReachTask.fromJson((e as Map).cast<String, dynamic>()))
          .toList(),
    );
  }
}
