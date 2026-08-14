import 'dart:io';

import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import 'instance_profile.dart';

/// 桌面端 profile 目录布局（仅 io 平台使用）：
///
/// ```
/// <ApplicationSupport>/
/// ├── databases/                 # default profile 的聊天库（历史位置，不迁移）
/// └── profiles/<name>/
///     ├── auth_session.json      # 凭证 + 区域端点
///     ├── profile.lock           # 运行锁
///     ├── ipc_port               # 实例激活 IPC 端口
///     └── databases/             # 非 default profile 的聊天库
/// ```
class ProfilePaths {
  /// 所有 profile 的根目录（不自动创建，供实例列表扫描）。
  static Future<Directory> profilesBase() async {
    final support = await getApplicationSupportDirectory();
    return Directory(p.join(support.path, 'profiles'));
  }

  /// 指定 profile 的专属目录，确保已创建。
  static Future<Directory> dirOf(String profileName) async {
    final base = await profilesBase();
    final dir = Directory(p.join(base.path, profileName));
    await dir.create(recursive: true);
    return dir;
  }

  /// 当前进程 profile 的专属目录，确保已创建。
  static Future<Directory> currentDir() {
    return dirOf(InstanceProfile.current.name);
  }

  /// 当前 profile 的聊天库目录。default 保持历史位置
  /// `<ApplicationSupport>/databases`（存量用户零迁移），
  /// 其余 profile 落在自己目录下。
  static Future<Directory> currentDatabasesDir() async {
    final support = await getApplicationSupportDirectory();
    final base = InstanceProfile.current.isDefault
        ? support.path
        : (await currentDir()).path;
    final dir = Directory(p.join(base, 'databases'));
    await dir.create(recursive: true);
    return dir;
  }
}
