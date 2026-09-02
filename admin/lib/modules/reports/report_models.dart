/// 举报列表项，对应后端 ReportListItem。
class ReportListItem {
  ReportListItem({
    required this.id,
    required this.status,
    required this.statusText,
    required this.targetType,
    required this.targetTypeText,
    required this.reasonCode,
    required this.reasonText,
    required this.reporterName,
    required this.reporterInfo,
    required this.targetTitle,
    required this.targetInfo,
    required this.createdAt,
    required this.resolvedAt,
  });

  final String id;
  final int status; // 见后端：pending/review/resolved
  final String statusText;
  final int targetType; // user/group
  final String targetTypeText;
  final String reasonCode;
  final String reasonText;
  final String reporterName;
  final String reporterInfo;
  final String targetTitle;
  final String targetInfo;
  final DateTime? createdAt;
  final DateTime? resolvedAt;

  bool get isResolved => statusText == '已处理' || resolvedAt != null;

  factory ReportListItem.fromJson(Map<String, dynamic> json) {
    DateTime? t(dynamic v) =>
        v == null ? null : DateTime.tryParse(v.toString());
    return ReportListItem(
      id: (json['id'] ?? '').toString(),
      status: (json['status'] as num?)?.toInt() ?? 0,
      statusText: (json['status_text'] ?? '').toString(),
      targetType: (json['target_type'] as num?)?.toInt() ?? 0,
      targetTypeText: (json['target_type_text'] ?? '').toString(),
      reasonCode: (json['reason_code'] ?? '').toString(),
      reasonText: (json['reason_text'] ?? '').toString(),
      reporterName: (json['reporter_name'] ?? '').toString(),
      reporterInfo: (json['reporter_info'] ?? '').toString(),
      targetTitle: (json['target_title'] ?? '').toString(),
      targetInfo: (json['target_info'] ?? '').toString(),
      createdAt: t(json['created_at']),
      resolvedAt: t(json['resolved_at']),
    );
  }
}

class ReportPerson {
  ReportPerson({
    required this.userId,
    required this.username,
    required this.nickname,
    required this.avatarUrl,
    required this.displayName,
  });

  final String userId;
  final String username;
  final String nickname;
  final String avatarUrl;
  final String displayName;

  factory ReportPerson.fromJson(Map<String, dynamic> j) => ReportPerson(
    userId: (j['user_id'] ?? '').toString(),
    username: (j['username'] ?? '').toString(),
    nickname: (j['nickname'] ?? '').toString(),
    avatarUrl: (j['avatar_url'] ?? '').toString(),
    displayName: (j['display_name'] ?? '').toString(),
  );
}

class ReportTarget {
  ReportTarget({
    required this.userId,
    required this.username,
    required this.sessionId,
    required this.title,
    required this.subtitle,
    required this.avatarUrl,
    required this.ownerId,
    required this.memberCount,
  });

  final String userId;
  final String username;
  final String sessionId;
  final String title;
  final String subtitle;
  final String avatarUrl;
  final String ownerId;
  final int memberCount;

  factory ReportTarget.fromJson(Map<String, dynamic> j) => ReportTarget(
    userId: (j['user_id'] ?? '').toString(),
    username: (j['username'] ?? '').toString(),
    sessionId: (j['session_id'] ?? '').toString(),
    title: (j['title'] ?? '').toString(),
    subtitle: (j['subtitle'] ?? '').toString(),
    avatarUrl: (j['avatar_url'] ?? '').toString(),
    ownerId: (j['owner_id'] ?? '').toString(),
    memberCount: (j['member_count'] as num?)?.toInt() ?? 0,
  );
}

class ReportAttachment {
  ReportAttachment({
    required this.id,
    required this.slotNo,
    required this.mimeType,
    required this.sizeBytes,
  });

  final String id;
  final int slotNo;
  final String mimeType;
  final int sizeBytes;

  bool get isImage => mimeType.startsWith('image/');

  factory ReportAttachment.fromJson(Map<String, dynamic> j) => ReportAttachment(
    id: (j['id'] ?? '').toString(),
    slotNo: (j['slot_no'] as num?)?.toInt() ?? 0,
    mimeType: (j['mime_type'] ?? '').toString(),
    sizeBytes: (j['size_bytes'] as num?)?.toInt() ?? 0,
  );
}

class ReportActionLog {
  ReportActionLog({
    required this.actionText,
    required this.resolutionText,
    required this.note,
    required this.adminName,
    required this.createdAt,
  });

  final String actionText;
  final String resolutionText;
  final String note;
  final String adminName;
  final DateTime? createdAt;

  factory ReportActionLog.fromJson(Map<String, dynamic> j) => ReportActionLog(
    actionText: (j['action_text'] ?? '').toString(),
    resolutionText: (j['resolution_text'] ?? '').toString(),
    note: (j['note'] ?? '').toString(),
    adminName: (j['admin_name'] ?? '').toString(),
    createdAt: j['created_at'] == null
        ? null
        : DateTime.tryParse(j['created_at'].toString()),
  );
}

/// 举报详情，对应后端 reportDetailToJSON。
class ReportDetail {
  ReportDetail({
    required this.id,
    required this.statusText,
    required this.resolutionText,
    required this.targetTypeText,
    required this.reasonText,
    required this.description,
    required this.sourceSessionId,
    required this.reporter,
    required this.target,
    required this.attachments,
    required this.actionLogs,
    required this.resolvedNote,
    required this.assignedAdmin,
    required this.resolvedAdmin,
    required this.createdAt,
    required this.resolvedAt,
    required this.isUserTarget,
    required this.isGroupTarget,
    required this.canResolve,
    required this.canBanUser,
    required this.canBanGroup,
  });

  final String id;
  final String statusText;
  final String resolutionText;
  final String targetTypeText;
  final String reasonText;
  final String description;
  final String sourceSessionId;
  final ReportPerson reporter;
  final ReportTarget target;
  final List<ReportAttachment> attachments;
  final List<ReportActionLog> actionLogs;
  final String resolvedNote;
  final String assignedAdmin;
  final String resolvedAdmin;
  final DateTime? createdAt;
  final DateTime? resolvedAt;
  final bool isUserTarget;
  final bool isGroupTarget;
  final bool canResolve;
  final bool canBanUser;
  final bool canBanGroup;

  factory ReportDetail.fromJson(Map<String, dynamic> j) {
    DateTime? t(dynamic v) =>
        v == null ? null : DateTime.tryParse(v.toString());
    List<Map<String, dynamic>> listOf(dynamic v) => ((v as List?) ?? const [])
        .map((e) => (e as Map).cast<String, dynamic>())
        .toList();
    return ReportDetail(
      id: (j['id'] ?? '').toString(),
      statusText: (j['status_text'] ?? '').toString(),
      resolutionText: (j['resolution_text'] ?? '').toString(),
      targetTypeText: (j['target_type_text'] ?? '').toString(),
      reasonText: (j['reason_text'] ?? '').toString(),
      description: (j['description'] ?? '').toString(),
      sourceSessionId: (j['source_session_id'] ?? '').toString(),
      reporter: ReportPerson.fromJson(
        ((j['reporter'] as Map?) ?? {}).cast<String, dynamic>(),
      ),
      target: ReportTarget.fromJson(
        ((j['target'] as Map?) ?? {}).cast<String, dynamic>(),
      ),
      attachments: listOf(
        j['attachments'],
      ).map(ReportAttachment.fromJson).toList(),
      actionLogs: listOf(
        j['action_logs'],
      ).map(ReportActionLog.fromJson).toList(),
      resolvedNote: (j['resolved_note'] ?? '').toString(),
      assignedAdmin: (j['assigned_admin'] ?? '').toString(),
      resolvedAdmin: (j['resolved_admin'] ?? '').toString(),
      createdAt: t(j['created_at']),
      resolvedAt: t(j['resolved_at']),
      isUserTarget: j['is_user_target'] == true,
      isGroupTarget: j['is_group_target'] == true,
      canResolve: j['can_resolve'] == true,
      canBanUser: j['can_ban_user'] == true,
      canBanGroup: j['can_ban_group'] == true,
    );
  }
}
