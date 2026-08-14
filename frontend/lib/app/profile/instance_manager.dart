import 'instance_manager_impl_stub.dart'
    if (dart.library.io) 'instance_manager_impl_io.dart'
    as impl;
import 'profile_instance_info.dart';

/// 桌面端多实例管理：列出实例、打开/前台化实例、新开账号窗口。
///
/// profile 的分配、环境变量传递、拉起子进程全部在此完成，
/// 用户层面只有"添加账号"与实例列表两个动作，不接触任何参数。
class InstanceManager {
  /// 仅桌面平台支持（网页版/移动端为 false，UI 不渲染入口）。
  static bool get isSupported => impl.instanceManagerSupported();

  static String get currentProfileName => impl.currentProfileName();

  static Future<List<ProfileInstanceInfo>> listInstances() =>
      impl.listInstances();

  /// 打开指定 profile：已在运行则把它的窗口带到前台，否则拉起新进程。
  static Future<void> openInstance(String name) => impl.openInstance(name);

  /// 分配一个未占用的 profile 并拉起新窗口（登录其他账号）。
  static Future<void> launchNewInstance() => impl.launchNewInstance();
}
