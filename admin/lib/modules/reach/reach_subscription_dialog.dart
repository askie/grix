import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../shared/widgets/dialog_content_box.dart';
import 'reach_models.dart';
import 'reach_service.dart';

class ReachSubscriptionDialog extends StatefulWidget {
  const ReachSubscriptionDialog({super.key});

  static Future<void> show() {
    return Get.dialog(const ReachSubscriptionDialog());
  }

  @override
  State<ReachSubscriptionDialog> createState() =>
      _ReachSubscriptionDialogState();
}

class _ReachSubscriptionDialogState extends State<ReachSubscriptionDialog> {
  ReachSubscriptionOverview? _data;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final result = await ReachService.getSubscriptionOverview();
      if (!mounted) return;
      setState(() {
        _data = result;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      insetPadding: kDialogInsetPadding,
      title: const Text('订阅概览'),
      content: DialogContentBox(
        child: _loading
            ? const SizedBox(
                height: 80,
                child: Center(child: CircularProgressIndicator()),
              )
            : _error != null
            ? Text(_error!, style: TextStyle(color: theme.colorScheme.error))
            : _buildStats(theme),
      ),
      actions: [
        TextButton(onPressed: () => Get.back(), child: const Text('关闭')),
      ],
    );
  }

  Widget _buildStats(ThemeData theme) {
    final d = _data!;
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        _StatRow(Icons.people, '总订阅记录', '${d.totalSubscriptions}', theme),
        const Divider(height: 24),
        _StatRow(
          Icons.check_circle_outline,
          '已订阅',
          '${d.subscribed}',
          theme,
          color: Colors.green,
        ),
        const SizedBox(height: 8),
        _StatRow(
          Icons.cancel_outlined,
          '已退订',
          '${d.unsubscribed}',
          theme,
          color: Colors.red,
        ),
      ],
    );
  }
}

class _StatRow extends StatelessWidget {
  const _StatRow(this.icon, this.label, this.value, this.theme, {this.color});
  final IconData icon;
  final String label;
  final String value;
  final ThemeData theme;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Icon(icon, size: 22, color: color ?? theme.hintColor),
        const SizedBox(width: 12),
        Expanded(child: Text(label, style: theme.textTheme.bodyMedium)),
        Text(
          value,
          style: theme.textTheme.titleMedium?.copyWith(
            fontWeight: FontWeight.w600,
          ),
        ),
      ],
    );
  }
}
