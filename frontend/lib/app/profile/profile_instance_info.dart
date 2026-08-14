/// 一个实例 profile 的展示信息（实例列表用）。
class ProfileInstanceInfo {
  const ProfileInstanceInfo({
    required this.name,
    required this.isCurrent,
    required this.running,
    this.nickname,
    this.avatarUrl,
  });

  final String name;

  /// 是否当前进程所在 profile。
  final bool isCurrent;

  /// 是否已有实例进程在运行。
  final bool running;

  /// 该 profile 上次登录账号的昵称（未登录过为 null）。
  final String? nickname;

  final String? avatarUrl;
}
