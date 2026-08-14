import 'dart:io';

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter/services.dart' show MissingPluginException;
import 'package:path/path.dart' as p;

import 'desktop_runtime.dart';
import 'instance_profile.dart';
import 'profile_instance_guard.dart';
import 'profile_instance_info.dart';
import 'profile_local_store.dart';
import 'profile_paths.dart';

// 测试环境（FLUTTER_TEST）不支持：widget 测试渲染设置页时
// 误走 path_provider 平台通道会在 FakeAsync 区挂死。
bool instanceManagerSupported() => isDesktopRuntime;

String currentProfileName() => InstanceProfile.current.name;

Future<List<ProfileInstanceInfo>> listInstances() async {
  if (!instanceManagerSupported()) return const <ProfileInstanceInfo>[];
  try {
    final names = <String>{
      InstanceProfile.defaultName,
      InstanceProfile.current.name,
    };
    final base = await ProfilePaths.profilesBase();
    if (await base.exists()) {
      await for (final entry in base.list(followLinks: false)) {
        if (entry is! Directory) continue;
        final name = InstanceProfile.tryNormalize(p.basename(entry.path));
        if (name != null) names.add(name);
      }
    }
    final infos = await Future.wait(names.map(_loadInstanceInfo));
    infos.sort((a, b) {
      if (a.isCurrent != b.isCurrent) return a.isCurrent ? -1 : 1;
      if (a.name == InstanceProfile.defaultName) return -1;
      if (b.name == InstanceProfile.defaultName) return 1;
      return a.name.compareTo(b.name);
    });
    return infos;
  } on MissingPluginException {
    return const <ProfileInstanceInfo>[];
  }
}

Future<ProfileInstanceInfo> _loadInstanceInfo(String name) async {
  String? nickname;
  String? avatarUrl;
  try {
    final base = await ProfilePaths.profilesBase();
    final file = File(p.join(base.path, name, ProfileLocalStore.fileName));
    if (await file.exists()) {
      final store = await ProfileLocalStore.open(file);
      nickname = store.getString('nickname');
      avatarUrl = store.getString('avatar_url');
      // 有 user_id 但没昵称时，退化用用户名兜底展示。
      nickname ??= store.getString('username');
    }
  } catch (e) {
    debugPrint('⚠️ Load instance info for "$name" failed: $e');
  }
  final isCurrent = name == InstanceProfile.current.name;
  final running = isCurrent || await ProfileInstanceGuard.pingProfile(name);
  return ProfileInstanceInfo(
    name: name,
    isCurrent: isCurrent,
    running: running,
    nickname: nickname,
    avatarUrl: avatarUrl,
  );
}

Future<void> openInstance(String name) async {
  final normalized = InstanceProfile.tryNormalize(name);
  if (normalized == null || normalized == InstanceProfile.current.name) return;
  // 通知目标实例前，先由当前前台进程让渡前台权限，
  // 否则目标进程在 Windows 上抢不到焦点、窗口弹不出来（切换失败）。
  ProfileInstanceGuard.allowForegroundHandoff();
  final activated = await ProfileInstanceGuard.pingProfile(
    normalized,
    activate: true,
  );
  if (activated) return;
  await _spawn(normalized);
}

Future<void> launchNewInstance() async {
  final existing = (await listInstances()).map((info) => info.name).toSet();
  var index = 2;
  while (existing.contains('profile-$index')) {
    index++;
  }
  await _spawn('profile-$index');
}

Future<void> _spawn(String profileName) async {
  // 环境变量为主通道、--profile 参数为兜底，双通道同时传，三平台一致。
  await Process.start(
    Platform.resolvedExecutable,
    <String>['--profile=$profileName'],
    environment: <String, String>{'GRIX_PROFILE': profileName},
    mode: ProcessStartMode.detached,
  );
  debugPrint('✅ Spawned instance for profile "$profileName"');
}
