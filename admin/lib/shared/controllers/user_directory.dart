import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../modules/users/admin_user_item.dart';
import '../../modules/users/user_service.dart';

/// 全局用户目录：把用户 ID 解析成用户资料（昵称/头像/状态）。
///
/// 供 [UserRef] 等组件在渲染期调用 [resolve]：命中缓存直接返回；
/// 未命中的 ID 收集起来，在一个微批量窗口内合并成一次
/// `/users/lookup` 请求，避免列表页同屏几十个 ID 各打一枪。
class UserDirectory extends GetxService {
  static UserDirectory get instance {
    if (!Get.isRegistered<UserDirectory>()) {
      Get.put(UserDirectory(), permanent: true);
    }
    return Get.find<UserDirectory>();
  }

  /// 批量合并窗口。
  static const _batchWindow = Duration(milliseconds: 80);

  /// 单次批量上限（与后端 lookupMaxIDs 对齐）。
  static const _batchLimit = 100;

  /// 批量查询实现；测试时可替换为假实现。
  @visibleForTesting
  Future<List<AdminUserItem>> Function(List<String> ids) lookupFn =
      UserService.lookup;

  /// 失败自动重试的退避间隔；测试时可调小。
  @visibleForTesting
  Duration retryDelay = const Duration(seconds: 2);

  /// 每个 ID 的失败自动重试上限；超限后仍可由界面重建触发的 resolve 再试。
  static const _maxAutoRetries = 2;

  final Map<String, int> _retryCount = <String, int>{};

  /// 已解析缓存；value 为 null 表示查过但用户不存在（负缓存，防止反复请求）。
  final RxMap<String, AdminUserItem?> _cache = RxMap<String, AdminUserItem?>();

  final Set<String> _pending = <String>{};
  Timer? _flushTimer;

  /// 在 Obx 中调用：返回缓存的用户资料；未缓存时返回 null 并
  /// 安排一次批量拉取，数据到位后 RxMap 变更会触发调用方重建。
  AdminUserItem? resolve(String userId) {
    final id = userId.trim();
    if (id.isEmpty) return null;
    if (_cache.containsKey(id)) return _cache[id];
    if (_pending.add(id)) _scheduleFlush();
    return null;
  }

  /// 是否已有该 ID 的解析结果（含"不存在"负缓存）。
  bool isResolved(String userId) => _cache.containsKey(userId.trim());

  /// 单个用户的强制拉取（详情弹窗用，绕过缓存拿最新状态）。
  Future<AdminUserItem?> fetch(String userId) async {
    final id = userId.trim();
    if (id.isEmpty) return null;
    final items = await lookupFn([id]);
    final item = items.isEmpty ? null : items.first;
    _cache[id] = item;
    return item;
  }

  /// 操作（封禁/解封等）之后使某个 ID 的缓存失效，下次渲染重新拉取。
  void invalidate(String userId) {
    _cache.remove(userId.trim());
  }

  void _scheduleFlush() {
    _flushTimer ??= Timer(_batchWindow, () {
      _flushTimer = null;
      _flush();
    });
  }

  Future<void> _flush() async {
    if (_pending.isEmpty) return;
    final batch = _pending.take(_batchLimit).toList();
    _pending.removeAll(batch);
    if (_pending.isNotEmpty) _scheduleFlush();

    try {
      final items = await lookupFn(batch);
      final byId = {for (final it in items) it.id: it};
      // 一次性回填：命中的写资料，未命中的写负缓存。
      _cache.addAll({for (final id in batch) id: byId[id]});
      batch.forEach(_retryCount.remove);
    } catch (_) {
      // 拉取失败不写负缓存；限次自动重试，避免滚出屏幕的行永远停在裸 ID。
      final retryable = batch
          .where((id) => (_retryCount[id] ?? 0) < _maxAutoRetries)
          .toList();
      if (retryable.isEmpty) return;
      for (final id in retryable) {
        _retryCount[id] = (_retryCount[id] ?? 0) + 1;
        _pending.add(id);
      }
      _flushTimer ??= Timer(retryDelay, () {
        _flushTimer = null;
        _flush();
      });
    }
  }

  @override
  void onClose() {
    _flushTimer?.cancel();
    super.onClose();
  }
}
