class DashboardStats {
  const DashboardStats({
    required this.totalUsers,
    required this.onlineUsers,
    required this.onlineAgents,
    required this.dailyRegistrants,
  });

  final int totalUsers;
  final int onlineUsers;
  final int onlineAgents;
  final List<DailyRegistrationStat> dailyRegistrants;

  factory DashboardStats.fromJson(Map<String, dynamic> json) {
    return DashboardStats(
      totalUsers: (json['total_users'] as num?)?.toInt() ?? 0,
      onlineUsers: (json['online_users'] as num?)?.toInt() ?? 0,
      onlineAgents: (json['online_agents'] as num?)?.toInt() ?? 0,
      dailyRegistrants: (json['daily_registrants'] as List? ?? const [])
          .whereType<Map>()
          .map((e) => DailyRegistrationStat.fromJson(e.cast<String, dynamic>()))
          .toList(),
    );
  }
}

class DailyRegistrationStat {
  const DailyRegistrationStat({required this.date, required this.count});

  final String date;
  final int count;

  factory DailyRegistrationStat.fromJson(Map<String, dynamic> json) {
    return DailyRegistrationStat(
      date: (json['date'] ?? '').toString(),
      count: (json['count'] as num?)?.toInt() ?? 0,
    );
  }
}
