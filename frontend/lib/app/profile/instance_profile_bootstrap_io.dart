import 'dart:io';

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter/services.dart' show MissingPluginException;
import 'package:shared_preferences/shared_preferences.dart';

import 'desktop_runtime.dart';
import 'instance_profile.dart';
import 'profile_instance_guard.dart';
import 'profile_local_store.dart';

/// 一次性迁移的全局旧 keys：与 auth_service.dart 的 _authSessionKeys
/// 和 app_storage_service.dart 的区域端点 keys 对应。这些 key 名是
/// 持久化契约，历史不可变，此处按字面固化。
const List<String> _legacyGlobalKeys = <String>[
  'access_token',
  'refresh_token',
  'access_expires_at_ms',
  'user_id',
  'username',
  'email',
  'nickname',
  'introduction',
  'avatar_url',
  'username_modified',
  'phone_e164',
  'phone_country',
  'app_region',
  'app_api_endpoint',
  'app_ws_endpoint',
];

const String _migrationDonePreference = 'legacy_prefs_migrated';

Future<void> bootstrapInstanceProfile(List<String> args) async {
  // 移动端不启用 profile 隔离；测试环境（FLUTTER_TEST）同样跳过，
  // 避免锁/迁移误走 path_provider 平台通道（FakeAsync 区会挂死）。
  if (!isDesktopRuntime) return;

  final raw = _resolveRawProfile(args);
  if (!InstanceProfile.initialize(raw)) {
    stderr.writeln(
      'Invalid --profile / GRIX_PROFILE value: "$raw" '
      '(expected [a-z0-9_-]{1,32})',
    );
    exit(64);
  }

  try {
    final acquired = await ProfileInstanceGuard.acquire();
    if (!acquired) {
      // 同 profile 已有实例：把它带到前台，本进程静默退出。
      await ProfileInstanceGuard.activateExisting();
      exit(0);
    }
    await _migrateLegacyGlobalPrefs();
  } on MissingPluginException {
    // 纯 Dart VM（单元测试）没有 path_provider 宿主实现：跳过锁与迁移。
    debugPrint('⚠️ path_provider unavailable, profile guard skipped');
  }
}

String? _resolveRawProfile(List<String> args) {
  // 环境变量为主通道（App 内"添加账号"拉起子进程时设置，三平台一致），
  // 启动参数为辅（高级用户/运维手动多开）。参数显式给出时优先。
  for (final arg in args) {
    if (arg.startsWith('--profile=')) {
      return arg.substring('--profile='.length);
    }
  }
  return Platform.environment['GRIX_PROFILE'];
}

/// default profile 首次启动时，把旧版本存在全局 SharedPreferences 里的
/// 账号态（凭证 + 区域端点）搬入 profile 存储并清除旧值。
/// 失败兜底 = 用户重新登录一次，聊天库不受影响。
Future<void> _migrateLegacyGlobalPrefs() async {
  if (!InstanceProfile.current.isDefault) return; // 旧数据只属于 default
  try {
    final store = await ProfileLocalStore.instance();
    if (store.getBool(_migrationDonePreference) == true) return;

    final prefs = await SharedPreferences.getInstance();
    final migrated = <String>[];
    for (final key in _legacyGlobalKeys) {
      if (store.containsKey(key)) continue; // 新存储已有值，不回写
      final value = prefs.get(key);
      if (value == null) continue;
      if (value is String || value is int || value is bool) {
        await store.set(key, value);
        migrated.add(key);
      }
    }
    await store.set(_migrationDonePreference, true);
    for (final key in migrated) {
      await prefs.remove(key);
    }
    if (migrated.isNotEmpty) {
      debugPrint(
        '✅ Migrated ${migrated.length} legacy auth keys into profile store',
      );
    }
  } catch (e) {
    debugPrint('⚠️ Legacy prefs migration failed (will re-login): $e');
  }
}
