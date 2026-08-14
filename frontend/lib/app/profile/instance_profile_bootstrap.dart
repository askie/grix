import 'instance_profile_bootstrap_stub.dart'
    if (dart.library.io) 'instance_profile_bootstrap_io.dart'
    as impl;

/// 启动早期确定当前进程的实例 profile（桌面端多实例多账号隔离）。
///
/// 桌面端：解析 profile 名（环境变量 `GRIX_PROFILE` 为主、启动参数
/// `--profile=<name>` 为辅）→ 抢占 profile 运行锁 → 迁移旧全局凭证。
/// 同 profile 已有实例在运行时，会通知其前台化并直接结束本进程（不返回）。
/// 移动端与网页版为空操作。
Future<void> bootstrapInstanceProfile(List<String> args) {
  return impl.bootstrapInstanceProfile(args);
}
