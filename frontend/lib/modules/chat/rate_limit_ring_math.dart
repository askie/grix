/// 工具栏用量双环：外环 = 已用%，内环 = 重置窗口内已过时间%。

/// 识别用量 item 的重置窗口长度。非 rate-limit progress 返回 null。
///
/// Cursor 的 M / API 复用 connector 的 fiveHour / sevenDay 槽位，但语义是月度套餐
/// 与 API 分项（cursor-rate-limits-v1）；窗口按月计。
Duration? resolveRateLimitWindowDuration({
  required String localAction,
  required String itemId,
  required String centerText,
  double progressWindowMinutes = 0,
}) {
  if (localAction != 'get_rate_limits') return null;
  if (progressWindowMinutes.isFinite && progressWindowMinutes > 0) {
    return Duration(minutes: progressWindowMinutes.round());
  }
  final id = itemId.toLowerCase();
  final center = centerText.trim().toLowerCase();
  if (id.contains('5h') || center == '5h') {
    return const Duration(hours: 5);
  }
  if (id.contains('7d') || center == '7d') {
    return const Duration(days: 7);
  }
  if (id == 'rate_limit_primary') {
    return const Duration(hours: 5);
  }
  if (id == 'rate_limit_secondary') {
    return const Duration(days: 7);
  }
  if (id == 'rate_limit_monthly' ||
      id.contains('monthly') ||
      center == 'm') {
    return const Duration(days: 30);
  }
  if (id == 'rate_limit_api' || center == 'api') {
    return const Duration(days: 30);
  }
  return null;
}

double computeRateLimitTimePercent(
  DateTime resetTime,
  Duration windowDuration,
  DateTime now,
) {
  final totalMs = windowDuration.inMilliseconds;
  if (totalMs <= 0) return 0;
  final remainMs = resetTime.difference(now).inMilliseconds;
  if (remainMs <= 0) return 100;
  if (remainMs >= totalMs) return 0;
  return (1.0 - remainMs / totalMs) * 100.0;
}

DateTime? parseRateLimitResetTime(String raw) {
  final unix = int.tryParse(raw.trim());
  if (unix != null && unix > 0) {
    // connector / Cursor 工具栏下发秒级 unix；偶发毫秒（>= 1e12）直接用。
    if (unix >= 1000000000000) {
      return DateTime.fromMillisecondsSinceEpoch(unix);
    }
    if (unix > 1000000000) {
      return DateTime.fromMillisecondsSinceEpoch(unix * 1000);
    }
  }
  return DateTime.tryParse(raw);
}
