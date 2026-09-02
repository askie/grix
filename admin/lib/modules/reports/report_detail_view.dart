import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:intl/intl.dart';

import '../../app/theme/app_palette.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/user_ref.dart';
import 'report_detail_controller.dart';
import 'report_models.dart';

/// 举报详情页：信息 / 附件查看 / 处理 / 处理历史。
class ReportDetailView extends GetView<ReportDetailController> {
  const ReportDetailView({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Obx(() => Text('举报详情 #${controller.detail.value?.id ?? ''}')),
      ),
      body: Obx(
        () => AsyncView(
          loading: controller.loading.value,
          error: controller.error.value,
          isEmpty: controller.detail.value == null,
          onRetry: controller.load,
          builder: (_) => _Detail(d: controller.detail.value!, c: controller),
        ),
      ),
      bottomNavigationBar: Obx(() {
        final d = controller.detail.value;
        if (d == null || !d.canResolve) return const SizedBox.shrink();
        return _ActionBar(d: d, c: controller);
      }),
    );
  }
}

class _ActionBar extends StatelessWidget {
  const _ActionBar({required this.d, required this.c});

  final ReportDetail d;
  final ReportDetailController c;

  @override
  Widget build(BuildContext context) {
    return Material(
      elevation: 8,
      color: AppPalette.surface,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
          child: Wrap(
            spacing: 8,
            runSpacing: 8,
            alignment: WrapAlignment.end,
            children: [
              OutlinedButton(
                onPressed: () => c.resolve('reject', '驳回'),
                child: const Text('驳回'),
              ),
              OutlinedButton(
                onPressed: () => c.resolve('no_action', '不处理'),
                child: const Text('不处理'),
              ),
              OutlinedButton(
                onPressed: () => c.resolve('duplicate', '标记重复'),
                child: const Text('重复举报'),
              ),
              if (d.canBanUser)
                FilledButton.icon(
                  style: FilledButton.styleFrom(
                    backgroundColor: AppPalette.danger,
                  ),
                  onPressed: () => c.resolve('ban_user', '封禁用户'),
                  icon: const Icon(Icons.block, size: 18),
                  label: const Text('封禁用户'),
                ),
              if (d.canBanGroup)
                FilledButton.icon(
                  style: FilledButton.styleFrom(
                    backgroundColor: AppPalette.danger,
                  ),
                  onPressed: () => c.resolve('ban_group', '封禁群组'),
                  icon: const Icon(Icons.block, size: 18),
                  label: const Text('封禁群组'),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class _Detail extends StatelessWidget {
  const _Detail({required this.d, required this.c});

  final ReportDetail d;
  final ReportDetailController c;

  @override
  Widget build(BuildContext context) {
    final df = DateFormat('yyyy-MM-dd HH:mm');
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        _card('概览', [
          _kv('状态', d.statusText),
          if (d.resolutionText.isNotEmpty) _kv('处理结果', d.resolutionText),
          _kv('对象类型', d.targetTypeText),
          _kv('举报原因', d.reasonText),
          if (d.createdAt != null)
            _kv('举报时间', df.format(d.createdAt!.toLocal())),
          if (d.resolvedAt != null)
            _kv('处理时间', df.format(d.resolvedAt!.toLocal())),
          if (d.resolvedAdmin.isNotEmpty) _kv('处理人', d.resolvedAdmin),
        ]),
        const SizedBox(height: 12),
        _card('举报人', [
          if (d.reporter.userId.isNotEmpty)
            _kvw(
              '用户',
              UserRef(
                d.reporter.userId,
                placeholderName: d.reporter.displayName,
                showId: true,
              ),
            )
          else
            _kv('昵称', d.reporter.displayName),
          if (d.reporter.username.isNotEmpty) _kv('账号', d.reporter.username),
        ]),
        const SizedBox(height: 12),
        _card('被举报对象', [
          _kv('标题', d.target.title),
          if (d.target.subtitle.isNotEmpty) _kv('说明', d.target.subtitle),
          if (d.target.username.isNotEmpty) _kv('账号', d.target.username),
          if (d.target.userId.isNotEmpty)
            _kvw('用户', UserRef(d.target.userId, showId: true)),
          if (d.target.sessionId.isNotEmpty) _kv('会话ID', d.target.sessionId),
          if (d.isGroupTarget) _kv('成员数', '${d.target.memberCount}'),
        ]),
        if (d.description.isNotEmpty) ...[
          const SizedBox(height: 12),
          _card('举报描述', [Text(d.description)]),
        ],
        if (d.attachments.isNotEmpty) ...[
          const SizedBox(height: 12),
          _card('举报截图（${d.attachments.length}）', [
            Wrap(
              spacing: 10,
              runSpacing: 10,
              children: d.attachments
                  .map((a) => _Attachment(attachment: a, controller: c))
                  .toList(),
            ),
          ]),
        ],
        if (d.actionLogs.isNotEmpty) ...[
          const SizedBox(height: 12),
          _card('处理历史', [for (final log in d.actionLogs) _LogRow(log: log)]),
        ],
        const SizedBox(height: 80),
      ],
    );
  }

  Widget _card(String title, List<Widget> children) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              title,
              style: const TextStyle(
                fontSize: 14,
                fontWeight: FontWeight.w700,
                color: AppPalette.textPrimary,
              ),
            ),
            const SizedBox(height: 12),
            ...children,
          ],
        ),
      ),
    );
  }

  /// 值为任意组件的 kv 行（用户名片等）。
  Widget _kvw(String k, Widget v) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 72,
            child: Text(
              k,
              style: const TextStyle(
                color: AppPalette.textSecondary,
                fontSize: 13,
              ),
            ),
          ),
          Expanded(
            child: Align(
              alignment: Alignment.centerLeft,
              child: DefaultTextStyle.merge(
                style: const TextStyle(fontSize: 13),
                child: v,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _kv(String k, String v) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 72,
            child: Text(
              k,
              style: const TextStyle(
                color: AppPalette.textSecondary,
                fontSize: 13,
              ),
            ),
          ),
          Expanded(child: Text(v, style: const TextStyle(fontSize: 13))),
        ],
      ),
    );
  }
}

class _LogRow extends StatelessWidget {
  const _LogRow({required this.log});

  final ReportActionLog log;

  @override
  Widget build(BuildContext context) {
    final df = DateFormat('yyyy-MM-dd HH:mm');
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                '${log.actionText} ${log.resolutionText}',
                style: const TextStyle(
                  fontWeight: FontWeight.w600,
                  fontSize: 13,
                ),
              ),
              const Spacer(),
              if (log.createdAt != null)
                Text(
                  df.format(log.createdAt!.toLocal()),
                  style: const TextStyle(
                    color: AppPalette.textTertiary,
                    fontSize: 12,
                  ),
                ),
            ],
          ),
          if (log.note.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 2),
              child: Text(
                '备注：${log.note}',
                style: const TextStyle(
                  color: AppPalette.textSecondary,
                  fontSize: 12,
                ),
              ),
            ),
          if (log.adminName.isNotEmpty)
            Text(
              '操作人：${log.adminName}',
              style: const TextStyle(
                color: AppPalette.textTertiary,
                fontSize: 12,
              ),
            ),
        ],
      ),
    );
  }
}

class _Attachment extends StatelessWidget {
  const _Attachment({required this.attachment, required this.controller});

  final ReportAttachment attachment;
  final ReportDetailController controller;

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<String>(
      future: controller.attachmentUrl(attachment.id),
      builder: (context, snap) {
        const size = 96.0;
        if (snap.connectionState != ConnectionState.done) {
          return _box(
            const Center(
              child: SizedBox(
                width: 20,
                height: 20,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
            ),
          );
        }
        final url = snap.data ?? '';
        if (url.isEmpty || !attachment.isImage) {
          return _box(
            const Icon(
              Icons.insert_drive_file_outlined,
              color: AppPalette.textTertiary,
            ),
          );
        }
        return InkWell(
          onTap: () => _openFull(context, url),
          child: ClipRRect(
            borderRadius: BorderRadius.circular(8),
            child: CachedNetworkImage(
              imageUrl: url,
              width: size,
              height: size,
              fit: BoxFit.cover,
              placeholder: (_, _) => _box(const SizedBox()),
              errorWidget: (_, _, _) =>
                  _box(const Icon(Icons.broken_image_outlined)),
            ),
          ),
        );
      },
    );
  }

  Widget _box(Widget child) {
    return Container(
      width: 96,
      height: 96,
      decoration: BoxDecoration(
        color: AppPalette.surfaceAlt,
        border: Border.all(color: AppPalette.border),
        borderRadius: BorderRadius.circular(8),
      ),
      child: child,
    );
  }

  void _openFull(BuildContext context, String url) {
    showDialog<void>(
      context: context,
      builder: (_) => Dialog(
        backgroundColor: Colors.black,
        insetPadding: const EdgeInsets.all(16),
        child: Stack(
          children: [
            InteractiveViewer(
              child: Center(child: CachedNetworkImage(imageUrl: url)),
            ),
            Positioned(
              right: 4,
              top: 4,
              child: IconButton(
                icon: const Icon(Icons.close, color: Colors.white),
                onPressed: () => Navigator.of(context).pop(),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
