import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../shared/widgets/app_dialog_style.dart';
import 'agent_client_type_meta.dart';
import 'grix_connector_service.dart';

/// Agent 安装器独立组件
///
/// 状态机: idle → checking → installing → done / error
/// 调用 connector 的 install API 完成安装、轮询进度、处理错误。
class AgentInstallerView extends StatefulWidget {
  const AgentInstallerView({
    super.key,
    required this.service,
    required this.meta,
    this.onInstalled,
  });

  final GrixConnectorService service;
  final AgentClientTypeMeta meta;
  final VoidCallback? onInstalled;

  @override
  State<AgentInstallerView> createState() => _AgentInstallerViewState();
}

class _AgentInstallerViewState extends State<AgentInstallerView> {
  GrixConnectorService get _service => widget.service;

  _InstallPhase _phase = _InstallPhase.idle;
  String _message = '';
  double _progress = 0;
  String _error = '';
  Timer? _pollTimer;

  /// 操作代数，用于取消时让进行中的异步链自行终止
  int _generation = 0;

  /// 前置依赖检查结果（缓存，避免重复请求）
  PrerequisiteResult? _lastPrereq;

  @override
  void dispose() {
    _pollTimer?.cancel();
    super.dispose();
  }

  Future<void> _startInstall() async {
    final gen = ++_generation;
    _lastPrereq = null;

    setState(() {
      _phase = _InstallPhase.checking;
      _message = 'agent_installer_checking'.tr;
      _error = '';
    });

    // 检查前置依赖
    final prereq = await _service.checkPrerequisite(widget.meta.clientType);
    if (!mounted || gen != _generation) return;

    _lastPrereq = prereq;
    if (!prereq.ok) {
      setState(() {
        _phase = _InstallPhase.prerequisiteMissing;
        _message = prereq.message ?? 'agent_installer_prereq_unmet'.tr;
        _error = prereq.installCommand ?? prereq.installHint ?? '';
      });
      return;
    }

    setState(() {
      _phase = _InstallPhase.installing;
      _message = 'agent_installer_submitting'.tr;
      _progress = 0;
    });

    // 触发安装。
    // connector 的 POST /api/install 是同步阻塞接口：返回成功即代表安装
    // （含安装后校验）已全部完成。此时 connector 端的进度记录已被清除，
    // 再去轮询 GET /api/install/:agent 只会得到 status=unknown，进而误报
    // 「安装超时/安装失败」。因此 POST 返回成功后直接判定完成，不再轮询。
    setState(
      () => _message = 'agent_installer_installing'.trParams({
        'name': widget.meta.label,
      }),
    );
    final ok = await _service.installAgentViaApi(widget.meta.clientType);
    if (!mounted || gen != _generation) return;
    if (!ok) {
      setState(() {
        _phase = _InstallPhase.error;
        _error = _service.lastError.value.isNotEmpty
            ? _service.lastError.value
            : 'agent_installer_request_failed'.tr;
      });
      return;
    }

    _pollTimer?.cancel();
    setState(() {
      _phase = _InstallPhase.done;
      _message = 'agent_installer_done'.tr;
      _progress = 1;
    });
    await _service.probeAll(fresh: true);
    if (!mounted || gen != _generation) return;
    widget.onInstalled?.call();
  }

  void _cancelInstall() {
    _generation++; // 使进行中的异步操作失效
    _pollTimer?.cancel();
    setState(() {
      _phase = _InstallPhase.idle;
      _message = '';
      _error = '';
      _progress = 0;
      _lastPrereq = null;
    });
  }

  Future<void> _installPrerequisite() async {
    final gen = ++_generation;
    final prereq =
        _lastPrereq ?? await _service.checkPrerequisite(widget.meta.clientType);
    if (!mounted || gen != _generation) return;

    if (!prereq.ok && prereq.installCommand != null) {
      setState(() {
        _phase = _InstallPhase.installingPrereq;
        _message = 'agent_installer_installing_prereq'.tr;
        _error = '';
      });
      final ok = await _service.installPrerequisite(prereq.installCommand!);
      if (!mounted || gen != _generation) return;
      if (ok) {
        _lastPrereq = null; // 重新检查
        _startInstall();
      } else {
        setState(() {
          _phase = _InstallPhase.error;
          _error = _service.lastError.value.isNotEmpty
              ? _service.lastError.value
              : 'agent_installer_prereq_failed'.tr;
        });
      }
    }
  }

  /// 前置依赖是否可自动安装
  bool get _canAutoInstallPrereq =>
      _lastPrereq != null &&
      !_lastPrereq!.ok &&
      _lastPrereq!.installCommand != null;

  bool get _isBusy =>
      _phase == _InstallPhase.checking ||
      _phase == _InstallPhase.installingPrereq ||
      _phase == _InstallPhase.installing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [_buildStatusArea(theme), _buildActions(theme)],
    );
  }

  Widget _buildStatusArea(ThemeData theme) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 12),
      child: Column(
        children: [
          _buildStatusIcon(theme),
          const SizedBox(height: 12),
          Text(
            _phase == _InstallPhase.idle
                ? 'agent_installer_ready'.trParams({'name': widget.meta.label})
                : _message,
            textAlign: TextAlign.center,
            style: TextStyle(
              fontSize: 13,
              color: _phase == _InstallPhase.error
                  ? theme.colorScheme.error
                  : theme.colorScheme.onSurface.withValues(alpha: 0.8),
            ),
          ),
          // 进度条
          if (_phase == _InstallPhase.installing ||
              _phase == _InstallPhase.installingPrereq)
            Padding(
              padding: const EdgeInsets.only(top: 12),
              child: LinearProgressIndicator(
                value: _progress > 0 ? _progress : null,
                backgroundColor: theme.colorScheme.surfaceContainerHighest,
              ),
            ),
          // 错误详情（可选择复制）
          if (_phase == _InstallPhase.error && _error.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Container(
                width: double.infinity,
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: theme.colorScheme.errorContainer.withValues(
                    alpha: 0.3,
                  ),
                  borderRadius: BorderRadius.circular(8),
                ),
                child: SelectableText(
                  _error,
                  style: TextStyle(
                    fontSize: 12,
                    fontFamily: 'monospace',
                    color: theme.colorScheme.onErrorContainer,
                  ),
                ),
              ),
            ),
          // 前置依赖修复提示（可选择复制）
          if (_phase == _InstallPhase.prerequisiteMissing && _error.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Container(
                width: double.infinity,
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: theme.colorScheme.surfaceContainerHighest.withValues(
                    alpha: 0.5,
                  ),
                  borderRadius: BorderRadius.circular(8),
                ),
                child: SelectableText(
                  _error,
                  style: TextStyle(
                    fontSize: 12,
                    fontFamily: 'monospace',
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.7),
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildStatusIcon(ThemeData theme) {
    switch (_phase) {
      case _InstallPhase.idle:
        return Icon(
          Icons.download_rounded,
          size: 36,
          color: theme.colorScheme.primary,
        );
      case _InstallPhase.checking:
      case _InstallPhase.installingPrereq:
        return SizedBox(
          width: 36,
          height: 36,
          child: CircularProgressIndicator(
            strokeWidth: 3,
            color: theme.colorScheme.primary,
          ),
        );
      case _InstallPhase.installing:
        return SizedBox(
          width: 36,
          height: 36,
          child: CircularProgressIndicator(
            value: _progress > 0 ? _progress : null,
            strokeWidth: 3,
            color: theme.colorScheme.primary,
          ),
        );
      case _InstallPhase.done:
        return const Icon(
          Icons.check_circle_rounded,
          size: 36,
          color: Colors.green,
        );
      case _InstallPhase.error:
        return Icon(
          Icons.error_outline_rounded,
          size: 36,
          color: theme.colorScheme.error,
        );
      case _InstallPhase.prerequisiteMissing:
        return const Icon(
          Icons.warning_amber_rounded,
          size: 36,
          color: Colors.orange,
        );
    }
  }

  Widget _buildActions(ThemeData theme) {
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.end,
        children: [
          // 安装中 → 取消按钮
          if (_isBusy)
            TextButton(
              onPressed: _cancelInstall,
              child: Text('common_cancel'.tr),
            ),

          // idle → 开始安装
          if (_phase == _InstallPhase.idle)
            FilledButton.icon(
              onPressed: _startInstall,
              icon: const Icon(Icons.download_rounded, size: 18),
              label: Text('agent_installer_start'.tr),
            ),

          // 前置缺失 → 根据是否有自动安装命令显示不同按钮
          if (_phase == _InstallPhase.prerequisiteMissing) ...[
            if (_canAutoInstallPrereq)
              FilledButton.icon(
                onPressed: _installPrerequisite,
                icon: const Icon(Icons.build_rounded, size: 18),
                label: Text('agent_installer_install_prereq'.tr),
              )
            else
              OutlinedButton.icon(
                onPressed: _startInstall,
                icon: const Icon(Icons.refresh, size: 18),
                label: Text('agent_installer_recheck'.tr),
              ),
          ],

          // 错误 → 重试
          if (_phase == _InstallPhase.error) ...[
            TextButton(
              onPressed: () => setState(() {
                _phase = _InstallPhase.idle;
                _error = '';
                _message = '';
              }),
              child: Text('common_cancel'.tr),
            ),
            FilledButton.icon(
              onPressed: _startInstall,
              icon: const Icon(Icons.refresh, size: 18),
              label: Text('common_retry'.tr),
            ),
          ],

          // 完成 → 完成（关闭 dialog）
          if (_phase == _InstallPhase.done)
            FilledButton.icon(
              onPressed: () => Navigator.pop(context),
              icon: const Icon(Icons.check_rounded, size: 18),
              label: Text('common_done'.tr),
            ),
        ],
      ),
    );
  }
}

enum _InstallPhase {
  idle,
  checking,
  prerequisiteMissing,
  installingPrereq,
  installing,
  done,
  error,
}

/// 在 dialog 中展示安装器
Future<void> showAgentInstallerDialog({
  required BuildContext context,
  required GrixConnectorService service,
  required AgentClientTypeMeta meta,
  VoidCallback? onInstalled,
}) {
  return showAppDialog<void>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: Row(
        children: [
          Icon(
            Icons.download_rounded,
            size: 20,
            color: Theme.of(ctx).colorScheme.primary,
          ),
          const SizedBox(width: 10),
          Text('agent_installer_title'.trParams({'name': meta.label})),
        ],
      ),
      content: SizedBox(
        width: resolveDialogConstraints(
          ctx,
          size: AppDialogSize.standard,
        ).maxWidth,
        child: AgentInstallerView(
          service: service,
          meta: meta,
          onInstalled: onInstalled,
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(ctx),
          child: Text('common_close'.tr),
        ),
      ],
    ),
  );
}
