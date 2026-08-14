enum ReportTargetType { user, group }

class ReportTargetArgs {
  const ReportTargetArgs({
    required this.targetType,
    required this.targetUserId,
    required this.targetSessionId,
    required this.sourceSessionId,
    required this.title,
    required this.subtitle,
    required this.avatarUrl,
  });

  final ReportTargetType targetType;
  final String targetUserId;
  final String targetSessionId;
  final String sourceSessionId;
  final String title;
  final String subtitle;
  final String avatarUrl;
}
