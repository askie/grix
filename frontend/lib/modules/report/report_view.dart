import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/themes/app_theme.dart';
import '../../shared/widgets/session_avatar.dart';
import 'controllers/report_controller.dart';
import 'models/report_attachment_draft.dart';

class ReportView extends GetView<ReportController> {
  const ReportView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      backgroundColor: theme.scaffoldBackgroundColor,
      appBar: AppBar(
        title: Text(
          'report_title'.tr,
          style: theme.textTheme.titleLarge?.copyWith(fontSize: 18),
        ),
      ),
      body: SafeArea(
        child: Obx(
          () => ListView(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 24),
            children: [
              _TargetCard(controller: controller),
              const SizedBox(height: 16),
              _ReasonSection(controller: controller),
              const SizedBox(height: 16),
              _DescriptionSection(controller: controller),
              const SizedBox(height: 16),
              _EvidenceSection(controller: controller),
              const SizedBox(height: 16),
              _SubmitFeedback(controller: controller),
              const SizedBox(height: 24),
              SizedBox(
                width: double.infinity,
                child: ElevatedButton(
                  onPressed: controller.isSubmitting.value
                      ? null
                      : controller.submit,
                  child: controller.isSubmitting.value
                      ? Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            SizedBox(
                              width: 18,
                              height: 18,
                              child: CircularProgressIndicator(
                                strokeWidth: 2.2,
                                valueColor: AlwaysStoppedAnimation<Color>(
                                  theme.colorScheme.onPrimary,
                                ),
                              ),
                            ),
                            const SizedBox(width: 10),
                            Text(controller.submitButtonTextKey.tr),
                          ],
                        )
                      : Text(controller.submitButtonTextKey.tr),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _SubmitFeedback extends StatelessWidget {
  const _SubmitFeedback({required this.controller});

  final ReportController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final message = controller.feedbackMessage.value;
      if (message == null || message.trim().isEmpty) {
        return const SizedBox.shrink();
      }

      final theme = Theme.of(context);
      final isError = controller.feedbackIsError.value;
      final foregroundColor = isError
          ? theme.colorScheme.error
          : AppTheme.successColor;
      final backgroundColor = foregroundColor.withValues(alpha: 0.12);

      return Container(
        width: double.infinity,
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        decoration: BoxDecoration(
          color: backgroundColor,
          borderRadius: BorderRadius.circular(14),
          border: Border.all(color: foregroundColor.withValues(alpha: 0.28)),
        ),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(
              isError
                  ? Icons.error_outline_rounded
                  : Icons.info_outline_rounded,
              size: 18,
              color: foregroundColor,
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                message,
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: foregroundColor,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
          ],
        ),
      );
    });
  }
}

class _TargetCard extends StatelessWidget {
  const _TargetCard({required this.controller});

  final ReportController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Row(
        children: [
          SessionAvatar(
            isGroup: false,
            avatarTitle: controller.targetArgs.title,
            avatarColor: AppTheme.getAvatarColor(
              controller.targetArgs.targetUserId.isNotEmpty
                  ? controller.targetArgs.targetUserId
                  : controller.targetArgs.targetSessionId,
            ),
            avatarUrl: controller.targetArgs.avatarUrl,
            size: 48,
            borderRadius: 12,
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  controller.targetArgs.title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  controller.targetArgs.subtitle.isNotEmpty
                      ? controller.targetArgs.subtitle
                      : controller.targetTypeLabelKey.tr,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: theme.colorScheme.secondary,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
            decoration: BoxDecoration(
              color: theme.colorScheme.primary.withValues(alpha: 0.12),
              borderRadius: BorderRadius.circular(999),
            ),
            child: Text(
              controller.targetTypeLabelKey.tr,
              style: theme.textTheme.labelMedium?.copyWith(
                color: theme.colorScheme.primary,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _ReasonSection extends StatelessWidget {
  const _ReasonSection({required this.controller});

  final ReportController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final theme = Theme.of(context);
      return _SectionCard(
        title: 'report_reason_title'.tr,
        child: Wrap(
          spacing: 8,
          runSpacing: 8,
          children: ReportController.reasonOptions
              .map((option) {
                final selected =
                    controller.selectedReasonCode.value == option.code;
                return _ReasonOptionChip(
                  key: Key('report_reason_${option.code}'),
                  label: option.labelKey.tr,
                  selected: selected,
                  onTap: () => controller.selectReason(option.code),
                  theme: theme,
                );
              })
              .toList(growable: false),
        ),
      );
    });
  }
}

class _ReasonOptionChip extends StatelessWidget {
  const _ReasonOptionChip({
    required super.key,
    required this.label,
    required this.selected,
    required this.onTap,
    required this.theme,
  });

  final String label;
  final bool selected;
  final VoidCallback onTap;
  final ThemeData theme;

  @override
  Widget build(BuildContext context) {
    final selectedBorderColor = theme.colorScheme.primary;
    final unselectedBorderColor = theme.colorScheme.onSurface;
    final backgroundColor = selected
        ? theme.colorScheme.primary.withValues(alpha: 0.1)
        : theme.colorScheme.surface;
    final textColor = selected
        ? theme.colorScheme.primary
        : theme.colorScheme.onSurface;

    return Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(16),
        child: AnimatedContainer(
          key: Key('${(key as ValueKey<String>).value}_container'),
          duration: const Duration(milliseconds: 160),
          curve: Curves.easeOut,
          padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 14),
          decoration: BoxDecoration(
            color: backgroundColor,
            borderRadius: BorderRadius.circular(16),
            border: Border.all(
              color: selected ? selectedBorderColor : unselectedBorderColor,
              width: selected ? 2 : 1.5,
            ),
          ),
          child: Text(
            label,
            style: theme.textTheme.titleMedium?.copyWith(
              color: textColor,
              fontWeight: FontWeight.w700,
            ),
          ),
        ),
      ),
    );
  }
}

class _DescriptionSection extends StatelessWidget {
  const _DescriptionSection({required this.controller});

  final ReportController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return _SectionCard(
      title: 'report_description_title'.tr,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          TextField(
            controller: controller.descriptionController,
            maxLines: 5,
            minLines: 4,
            maxLength: ReportController.maxDescriptionRunes,
            decoration: InputDecoration(
              hintText: 'report_description_hint'.tr,
              alignLabelWithHint: true,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'report_description_helper'.tr,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.secondary,
            ),
          ),
        ],
      ),
    );
  }
}

class _EvidenceSection extends StatelessWidget {
  const _EvidenceSection({required this.controller});

  final ReportController controller;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final theme = Theme.of(context);
      return _SectionCard(
        title: 'report_evidence_title'.tr,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'report_evidence_hint'.trParams({
                'count': '${ReportController.maxAttachments}',
              }),
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.secondary,
              ),
            ),
            const SizedBox(height: 12),
            GridView.builder(
              shrinkWrap: true,
              physics: const NeverScrollableScrollPhysics(),
              itemCount: controller.attachments.length + 1,
              gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                crossAxisCount: 3,
                crossAxisSpacing: 12,
                mainAxisSpacing: 12,
                childAspectRatio: 1,
              ),
              itemBuilder: (context, index) {
                if (index == controller.attachments.length) {
                  final isFull =
                      controller.attachments.length >=
                      ReportController.maxAttachments;
                  return _AddEvidenceTile(
                    enabled: !isFull && !controller.isSubmitting.value,
                    onTap: () => controller.pickScreenshot(context),
                  );
                }
                final attachment = controller.attachments[index];
                return _AttachmentPreviewTile(
                  attachment: attachment,
                  index: index,
                  onRemove: controller.isSubmitting.value
                      ? null
                      : () => controller.removeAttachmentAt(index),
                );
              },
            ),
          ],
        ),
      );
    });
  }
}

class _SectionCard extends StatelessWidget {
  const _SectionCard({required this.title, required this.child});

  final String title;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 12),
          child,
        ],
      ),
    );
  }
}

class _AddEvidenceTile extends StatelessWidget {
  const _AddEvidenceTile({required this.enabled, required this.onTap});

  final bool enabled;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: enabled ? onTap : null,
        borderRadius: BorderRadius.circular(14),
        child: Container(
          decoration: BoxDecoration(
            color: theme.colorScheme.primary.withValues(alpha: 0.06),
            borderRadius: BorderRadius.circular(14),
            border: Border.all(
              color: theme.colorScheme.primary.withValues(alpha: 0.2),
            ),
          ),
          padding: const EdgeInsets.all(10),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(
                Icons.add_a_photo_outlined,
                color: enabled
                    ? theme.colorScheme.primary
                    : theme.colorScheme.outline,
              ),
              const SizedBox(height: 8),
              Text(
                'report_add_screenshot'.tr,
                textAlign: TextAlign.center,
                style: theme.textTheme.labelMedium?.copyWith(
                  color: enabled
                      ? theme.colorScheme.primary
                      : theme.colorScheme.outline,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _AttachmentPreviewTile extends StatelessWidget {
  const _AttachmentPreviewTile({
    required this.attachment,
    required this.onRemove,
    required this.index,
  });

  final ReportAttachmentDraft attachment;
  final VoidCallback? onRemove;
  final int index;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ClipRRect(
      borderRadius: BorderRadius.circular(14),
      child: Stack(
        fit: StackFit.expand,
        children: [
          Image.memory(
            attachment.bytes,
            fit: BoxFit.cover,
            errorBuilder: (_, __, ___) => Container(
              color: theme.colorScheme.surfaceContainerHighest,
              alignment: Alignment.center,
              child: const Icon(Icons.broken_image_outlined),
            ),
          ),
          Align(
            alignment: Alignment.topRight,
            child: Padding(
              padding: const EdgeInsets.all(4),
              child: GestureDetector(
                key: Key('report_attachment_remove_$index'),
                behavior: HitTestBehavior.opaque,
                onTap: onRemove,
                child: Container(
                  width: 40,
                  height: 40,
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    color: Colors.black.withValues(alpha: 0.55),
                    shape: BoxShape.circle,
                  ),
                  child: const Icon(
                    Icons.close_rounded,
                    size: 22,
                    color: Colors.white,
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
