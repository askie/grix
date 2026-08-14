import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';

import '../../app/theme/app_palette.dart';
import '../../modules/auth/auth_service.dart';
import '../../modules/users/admin_user_item.dart';
import '../../modules/users/user_service.dart';
import '../controllers/user_directory.dart';
import 'confirm_dialog.dart';

/// 用户详情卡：展示用户资料，并提供封禁/解封等管理动作。
///
/// 由 [UserRef] 点击弹出，也可在任何地方 `UserDetailSheet.show(userId)`。
/// 动作按钮仅对拥有 `users` 权限的管理员展示，其余管理员只读。
class UserDetailSheet {
  UserDetailSheet._();

  static Future<void> show(String userId) {
    return Get.bottomSheet(
      _UserDetailSheetBody(userId: userId),
      isScrollControlled: true,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      backgroundColor: AppPalette.surface,
    );
  }
}

class _UserDetailSheetBody extends StatefulWidget {
  const _UserDetailSheetBody({required this.userId});

  final String userId;

  @override
  State<_UserDetailSheetBody> createState() => _UserDetailSheetBodyState();
}

class _UserDetailSheetBodyState extends State<_UserDetailSheetBody> {
  AdminUserItem? _user;
  bool _loading = true;
  String _error = '';

  bool get _canManage =>
      AuthService.to.profile.value?.hasPermission('users') ?? false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = '';
    });
    try {
      final user = await UserDirectory.instance.fetch(widget.userId);
      if (!mounted) return;
      setState(() {
        _user = user;
        _loading = false;
        if (user == null) _error = '用户不存在（可能已注销）';
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e.toString();
      });
    }
  }

  /// 执行一个管理动作并刷新详情；目录缓存同步失效，列表处的名字/状态跟着更新。
  Future<void> _runAction(
      Future<void> Function() action, String successMessage) async {
    try {
      await action();
      Toast.success(successMessage);
      UserDirectory.instance.invalidate(widget.userId);
      await _load();
    } catch (e) {
      Toast.error(e.toString());
    }
  }

  Future<void> _ban(AdminUserItem user) async {
    final reason = await ConfirmDialog.showWithReason(
      title: '封禁用户 ${user.displayName}',
      hint: '请输入封禁原因（可选）',
      confirmText: '封禁',
    );
    if (reason == null) return;
    await _runAction(() => UserService.ban(user.id, reason), '用户已封禁');
  }

  Future<void> _unban(AdminUserItem user) async {
    final ok = await ConfirmDialog.show(
      title: '解封用户',
      message: '确定要解封 ${user.displayName} 吗？',
      confirmText: '解封',
    );
    if (!ok) return;
    await _runAction(() => UserService.unban(user.id), '用户已恢复');
  }

  Future<void> _unlockLogin(AdminUserItem user) async {
    final ok = await ConfirmDialog.show(
      title: '解除登录锁定',
      message: '确定解除 ${user.displayName} 的登录锁定吗？',
      confirmText: '解除',
    );
    if (!ok) return;
    await _runAction(() => UserService.unlockLogin(user.id), '登录锁定已解除');
  }

  Future<void> _unmuteModeration(AdminUserItem user) async {
    final ok = await ConfirmDialog.show(
      title: '解除审核禁言',
      message: '确定解除 ${user.displayName} 的内容审查禁言吗？',
      confirmText: '解除',
    );
    if (!ok) return;
    await _runAction(
        () => UserService.unmuteModeration(user.id), '审查禁言已解除');
  }

  Future<void> _unbindPhone(AdminUserItem user) async {
    final ok = await ConfirmDialog.show(
      title: '解绑手机号',
      message: '确定解绑 ${user.displayName} 的手机号 ${user.phoneE164} 吗？\n'
          '该号码将释放，用户可重新用同号注册或绑定到其他账户。',
      confirmText: '解绑',
      danger: true,
    );
    if (!ok) return;
    await _runAction(() => UserService.unbindPhone(user.id), '手机号已解绑');
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 520),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(20, 16, 20, 20),
            child: _loading
                ? const SizedBox(
                    height: 160,
                    child: Center(child: CircularProgressIndicator()),
                  )
                : _user == null
                    ? _buildError()
                    : _buildDetail(context, _user!),
          ),
        ),
      ),
    );
  }

  Widget _buildError() {
    return SizedBox(
      height: 160,
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Text(_error.isEmpty ? '加载失败' : _error,
              style: const TextStyle(color: AppPalette.textSecondary)),
          const SizedBox(height: 12),
          OutlinedButton(onPressed: _load, child: const Text('重试')),
        ],
      ),
    );
  }

  Widget _buildDetail(BuildContext context, AdminUserItem user) {
    final df = DateFormat('yyyy-MM-dd HH:mm');
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            CircleAvatar(
              radius: 24,
              backgroundColor: AppPalette.brandSoft,
              foregroundImage:
                  user.avatarUrl.isNotEmpty ? NetworkImage(user.avatarUrl) : null,
              child: Text(
                user.displayName.isEmpty
                    ? '?'
                    : String.fromCharCode(user.displayName.runes.first),
                style: const TextStyle(
                    fontSize: 18, color: AppPalette.brandDark),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(user.displayName,
                      style: Theme.of(context).textTheme.titleMedium,
                      overflow: TextOverflow.ellipsis),
                  const SizedBox(height: 2),
                  Text('@${user.username}',
                      style: const TextStyle(
                          fontSize: 12, color: AppPalette.textSecondary)),
                ],
              ),
            ),
            _statusPill(user),
          ],
        ),
        const SizedBox(height: 14),
        const Divider(height: 1, color: AppPalette.divider),
        const SizedBox(height: 12),
        _kv('用户ID', user.id, copyable: true),
        if (user.email.isNotEmpty) _kv('邮箱', user.email),
        if (user.phoneE164.isNotEmpty) _kv('手机号', user.phoneE164),
        if (user.createdAt != null) _kv('注册时间', df.format(user.createdAt!.toLocal())),
        if (user.isBanned && user.bannedReason.isNotEmpty)
          _kv('封禁原因', user.bannedReason),
        if (user.loginLocked)
          _kv('登录锁定',
              user.lockRemaining.isEmpty ? '锁定中' : '剩余 ${user.lockRemaining}'),
        if (user.moderationMuted)
          _kv('审查禁言', '${user.moderationMuteSessionCount} 个会话禁言中'),
        if (_canManage) ...[
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              if (user.isBanned)
                FilledButton.tonal(
                  onPressed: () => _unban(user),
                  child: const Text('解封'),
                )
              else
                FilledButton(
                  style: FilledButton.styleFrom(
                      backgroundColor: Theme.of(context).colorScheme.error),
                  onPressed: () => _ban(user),
                  child: const Text('封禁'),
                ),
              if (user.loginLocked)
                OutlinedButton(
                  onPressed: () => _unlockLogin(user),
                  child: const Text('解除登录锁定'),
                ),
              if (user.moderationMuted)
                OutlinedButton(
                  onPressed: () => _unmuteModeration(user),
                  child: const Text('解除审核禁言'),
                ),
              if (user.phoneE164.isNotEmpty)
                OutlinedButton(
                  onPressed: () => _unbindPhone(user),
                  child: const Text('解绑手机号'),
                ),
            ],
          ),
        ],
      ],
    );
  }

  Widget _statusPill(AdminUserItem user) {
    final banned = user.isBanned;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: banned ? AppPalette.dangerSoft : AppPalette.brandSoft,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        banned ? '已封禁' : '正常',
        style: TextStyle(
          fontSize: 12,
          color: banned ? AppPalette.danger : AppPalette.brandDark,
        ),
      ),
    );
  }

  Widget _kv(String k, String v, {bool copyable = false}) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 72,
            child: Text(k,
                style: const TextStyle(
                    color: AppPalette.textSecondary, fontSize: 13)),
          ),
          Expanded(child: Text(v, style: const TextStyle(fontSize: 13))),
          if (copyable)
            InkWell(
              onTap: () {
                Clipboard.setData(ClipboardData(text: v));
                Toast.success('已复制');
              },
              child: const Padding(
                padding: EdgeInsets.symmetric(horizontal: 4),
                child: Icon(Icons.copy, size: 14, color: AppPalette.textSecondary),
              ),
            ),
        ],
      ),
    );
  }
}
