import 'package:intl/intl.dart';
import 'package:get/get.dart';

class TimeFormatter {
  static String formatChatTime(int? timestampMs) {
    if (timestampMs == null || timestampMs <= 0) return '';

    final normalizedTs = _normalizeTimestampMs(timestampMs);
    if (normalizedTs <= 0) return '';

    final date = DateTime.fromMillisecondsSinceEpoch(normalizedTs);
    final now = DateTime.now();
    final difference = now.difference(date);

    // Today
    if (date.year == now.year &&
        date.month == now.month &&
        date.day == now.day) {
      return DateFormat('HH:mm').format(date);
    }

    // Yesterday
    final yesterday = now.subtract(const Duration(days: 1));
    if (date.year == yesterday.year &&
        date.month == yesterday.month &&
        date.day == yesterday.day) {
      return '${'common_yesterday'.tr} ${DateFormat('HH:mm').format(date)}';
    }

    // Within this week (less than 7 days) and not same day
    if (difference.inDays < 7 && date.weekday != now.weekday) {
      final String weekdayStr = _getWeekdayString(date.weekday).tr;
      return '$weekdayStr ${DateFormat('HH:mm').format(date)}';
    }

    // Same year
    if (date.year == now.year) {
      return DateFormat('MM-dd HH:mm').format(date);
    }

    // Different year
    return DateFormat('yyyy-MM-dd HH:mm').format(date);
  }

  static int _normalizeTimestampMs(int rawTs) {
    if (rawTs <= 0) return 0;
    // Second-based timestamp.
    if (rawTs < 10000000000) {
      return rawTs * 1000;
    }
    // Microsecond-based timestamp.
    if (rawTs >= 1000000000000000) {
      return rawTs ~/ 1000;
    }
    return rawTs;
  }

  static String _getWeekdayString(int weekday) {
    switch (weekday) {
      case DateTime.monday:
        return 'time_monday';
      case DateTime.tuesday:
        return 'time_tuesday';
      case DateTime.wednesday:
        return 'time_wednesday';
      case DateTime.thursday:
        return 'time_thursday';
      case DateTime.friday:
        return 'time_friday';
      case DateTime.saturday:
        return 'time_saturday';
      case DateTime.sunday:
        return 'time_sunday';
      default:
        return '';
    }
  }
}
