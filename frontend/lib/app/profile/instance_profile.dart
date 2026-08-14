/// 桌面端实例 Profile：多实例多账号隔离的命名空间。
///
/// 每个桌面进程启动时确定一个 profile 名，账号态持久化（登录凭证、
/// 区域端点、本地聊天库）都落在该 profile 的专属目录下。
/// 移动端与网页版不启用 profile 隔离，恒为 [defaultName]。
class InstanceProfile {
  InstanceProfile._(this.name);

  static const String defaultName = 'default';

  /// 全小写：Windows/macOS 文件系统不区分大小写，命名层先消除歧义。
  static final RegExp _namePattern = RegExp(r'^[a-z0-9_-]{1,32}$');

  static InstanceProfile _current = InstanceProfile._(defaultName);

  /// 当前进程的 profile，进程生命周期内不可变。
  static InstanceProfile get current => _current;

  final String name;

  bool get isDefault => name == defaultName;

  /// 规范化 profile 名：转小写并校验；非法返回 null。
  static String? tryNormalize(String? raw) {
    final trimmed = raw?.trim().toLowerCase() ?? '';
    if (trimmed.isEmpty) return null;
    if (!_namePattern.hasMatch(trimmed)) return null;
    return trimmed;
  }

  /// 固化当前进程的 profile 名。启动早期调用一次；
  /// 传入非法名返回 false（调用方应拒绝启动）。
  static bool initialize(String? raw) {
    if (raw == null || raw.trim().isEmpty) {
      _current = InstanceProfile._(defaultName);
      return true;
    }
    final normalized = tryNormalize(raw);
    if (normalized == null) return false;
    _current = InstanceProfile._(normalized);
    return true;
  }

  /// 仅测试用：还原为 default。
  static void resetForTest() {
    _current = InstanceProfile._(defaultName);
  }
}
