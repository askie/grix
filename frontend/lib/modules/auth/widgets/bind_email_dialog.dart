// 绑定邮箱弹窗：手机号注册的账号没有邮箱，这里一屏完成「填邮箱 → 发码 → 验证码 → 绑定」。
//
// 邮箱是找回账号的唯一凭据，所以按半强制处理：可以关掉，但只要还没绑，
// 下次冷启动仍会再弹（见 bind_email_prompt.dart）。

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../data/providers/auth_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/widgets/app_dialog_style.dart';

/// 展示绑定邮箱弹窗；返回 true 表示已绑定成功。
Future<bool> showBindEmailDialog(BuildContext context) async {
  final bound = await showAppDialog<bool>(
    context: context,
    barrierDismissible: false,
    builder: (_) => const _BindEmailDialog(),
  );
  return bound == true;
}

class _BindEmailDialog extends StatefulWidget {
  const _BindEmailDialog();

  @override
  State<_BindEmailDialog> createState() => _BindEmailDialogState();
}

class _BindEmailDialogState extends State<_BindEmailDialog> {
  /// 与后端 IP+邮箱发码冷却（emailCodeSendCooldownTTL，5 分钟）保持一致，
  /// 否则用户按亮的"重发"必然被后端拒绝。注册页同样取 300 秒。
  static const int _resendCooldownSeconds = 300;

  final _emailController = TextEditingController();
  final _codeController = TextEditingController();

  /// 最近一次成功发码的目标邮箱。
  String _codeIssuedTo = '';

  Timer? _cooldownTimer;
  int _cooldown = 0;
  bool _sending = false;
  bool _submitting = false;

  @override
  void dispose() {
    _cooldownTimer?.cancel();
    _emailController.dispose();
    _codeController.dispose();
    super.dispose();
  }

  AuthService get _auth => Get.find<AuthService>();

  String get _email => _emailController.text.trim();
  String get _code => _codeController.text.trim();

  /// 只做基础形态校验，真正的合法性以后端为准。
  bool get _emailLooksValid {
    final email = _email;
    final at = email.indexOf('@');
    if (at <= 0 || at == email.length - 1) return false;
    final domain = email.substring(at + 1);
    return domain.contains('.') &&
        !domain.startsWith('.') &&
        !domain.endsWith('.') &&
        !email.contains(' ');
  }

  bool get _canSend => !_sending && _cooldown == 0 && _emailLooksValid;
  bool get _canSubmit => !_submitting && _emailLooksValid && _code.length >= 4;

  void _startCooldown() {
    _cooldownTimer?.cancel();
    setState(() => _cooldown = _resendCooldownSeconds);
    _cooldownTimer = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (!mounted) {
        timer.cancel();
        return;
      }
      setState(() => _cooldown -= 1);
      if (_cooldown <= 0) timer.cancel();
    });
  }

  /// 换了邮箱就清掉已输的验证码：那串码是发给上一个邮箱的。
  void _onEmailChanged(String value) {
    setState(() {
      if (value.trim() != _codeIssuedTo && _codeController.text.isNotEmpty) {
        _codeController.clear();
      }
    });
  }

  Future<void> _sendCode() async {
    if (!_canSend) return;
    setState(() => _sending = true);
    try {
      final result = await _auth.sendBindEmailCode(email: _email);
      if (!mounted) return;
      if (result.ok) {
        _codeIssuedTo = _email;
        _startCooldown();
        CustomToast.show('email_bind_code_sent'.tr, isError: false);
        return;
      }
      CustomToast.show(
        result.message.isNotEmpty ? result.message : 'auth_send_code_failed'.tr,
        isError: true,
      );
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  Future<void> _submit() async {
    if (!_canSubmit) return;
    setState(() => _submitting = true);
    try {
      final result = await _auth.bindEmail(email: _email, code: _code);
      if (!mounted) return;
      if (result.ok) {
        // 刷新 profile，让 user.email 落到本地缓存，下次启动不再弹。
        await _auth.fetchCurrentUserProfile();
        if (!mounted) return;
        Navigator.of(context).pop(true);
        CustomToast.show('email_bind_success_body'.tr, isError: false);
        return;
      }
      CustomToast.show(
        result.message.isNotEmpty ? result.message : 'email_bind_failed'.tr,
        isError: true,
      );
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final compact = isCompactDialogWidth(context);
    return AlertDialog(
      title: Text('email_bind_title'.tr),
      content: _boundedContent(
        context,
        Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              'email_bind_subtitle'.tr,
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _emailController,
              keyboardType: TextInputType.emailAddress,
              autocorrect: false,
              enabled: !_submitting,
              decoration: InputDecoration(
                labelText: 'email_bind_email_label'.tr,
                hintText: 'email_bind_email_hint'.tr,
              ),
              onChanged: _onEmailChanged,
            ),
            const SizedBox(height: 12),
            Row(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                Expanded(
                  child: TextField(
                    controller: _codeController,
                    keyboardType: TextInputType.number,
                    enabled: !_submitting,
                    decoration: InputDecoration(
                      labelText: 'email_bind_code_label'.tr,
                    ),
                    onChanged: (_) => setState(() {}),
                  ),
                ),
                const SizedBox(width: 8),
                TextButton(
                  onPressed: _canSend ? _sendCode : null,
                  child: Text(
                    _cooldown > 0
                        ? 'email_bind_code_resend'.trParams({
                            'seconds': '$_cooldown',
                          })
                        : 'email_bind_code_send'.tr,
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _submitting
              ? null
              : () => Navigator.of(context).pop(false),
          child: Text('email_bind_later'.tr),
        ),
        TextButton(
          onPressed: _canSubmit ? _submit : null,
          child: Text(
            _submitting ? 'email_bind_submitting'.tr : 'email_bind_submit'.tr,
          ),
        ),
      ],
      actionsPadding: EdgeInsets.symmetric(
        horizontal: compact ? 12 : 16,
        vertical: 8,
      ),
    );
  }

  Widget _boundedContent(BuildContext context, Widget child) {
    return ConstrainedBox(
      constraints: resolveDialogConstraints(
        context,
        size: AppDialogSize.compact,
      ),
      child: SingleChildScrollView(child: child),
    );
  }
}
