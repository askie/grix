import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../data/providers/oss_service.dart';
import '../../../data/providers/report_service.dart';
import '../../../shared/utils/hardware_facade.dart';
import '../../../shared/utils/toast_util.dart';
import '../../chat/models/chat_attachment_type.dart';
import '../../chat/services/chat_attachment_payload_builder.dart';
import '../../chat/widgets/chat_attachment_source_sheet.dart';
import '../models/report_attachment_draft.dart';
import '../models/report_target_args.dart';

class ReportReasonOption {
  const ReportReasonOption({required this.code, required this.labelKey});

  final String code;
  final String labelKey;
}

class ReportController extends GetxController {
  ReportController({
    Map<String, dynamic>? initialArguments,
    ReportService? reportService,
    OssService? ossService,
  }) : _initialArguments = initialArguments,
       _reportService = reportService ?? Get.find<ReportService>(),
       _ossService = ossService ?? Get.find<OssService>();

  static const int maxAttachments = 3;
  static const int maxDescriptionRunes = 500;
  static const Set<String> _supportedContentTypes = <String>{
    'image/jpeg',
    'image/png',
    'image/webp',
  };

  static const List<ReportReasonOption> reasonOptions = <ReportReasonOption>[
    ReportReasonOption(
      code: 'harassment',
      labelKey: 'report_reason_harassment',
    ),
    ReportReasonOption(
      code: 'pornography',
      labelKey: 'report_reason_pornography',
    ),
    ReportReasonOption(code: 'violence', labelKey: 'report_reason_violence'),
    ReportReasonOption(code: 'fraud', labelKey: 'report_reason_fraud'),
    ReportReasonOption(code: 'spam', labelKey: 'report_reason_spam'),
    ReportReasonOption(
      code: 'impersonation',
      labelKey: 'report_reason_impersonation',
    ),
    ReportReasonOption(code: 'illegal', labelKey: 'report_reason_illegal'),
    ReportReasonOption(code: 'other', labelKey: 'report_reason_other'),
  ];

  final Map<String, dynamic>? _initialArguments;
  final ReportService _reportService;
  final OssService _ossService;

  final TextEditingController descriptionController = TextEditingController();
  final RxString selectedReasonCode = ''.obs;
  final RxList<ReportAttachmentDraft> attachments =
      <ReportAttachmentDraft>[].obs;
  final RxBool isSubmitting = false.obs;
  final RxnString feedbackMessage = RxnString();
  final RxBool feedbackIsError = true.obs;

  late final ReportTargetArgs targetArgs;

  @override
  void onInit() {
    super.onInit();
    targetArgs = _parseTargetArgs(_initialArguments ?? _readRouteArguments());
    descriptionController.addListener(clearFeedback);
  }

  @override
  void onClose() {
    descriptionController.removeListener(clearFeedback);
    descriptionController.dispose();
    super.onClose();
  }

  bool get isTargetValid {
    final title = targetArgs.title.trim();
    if (title.isEmpty) {
      return false;
    }
    return switch (targetArgs.targetType) {
      ReportTargetType.user => targetArgs.targetUserId.trim().isNotEmpty,
      ReportTargetType.group => targetArgs.targetSessionId.trim().isNotEmpty,
    };
  }

  String get targetTypeLabelKey {
    return switch (targetArgs.targetType) {
      ReportTargetType.user => 'report_target_type_user',
      ReportTargetType.group => 'report_target_type_group',
    };
  }

  String get submitButtonTextKey {
    return isSubmitting.value ? 'common_loading' : 'report_submit';
  }

  Future<void> pickScreenshot(BuildContext context) async {
    if (attachments.length >= maxAttachments) {
      CustomToast.show(
        'report_screenshot_limit'.trParams({'count': '$maxAttachments'}),
      );
      return;
    }

    final source = await ChatAttachmentSourceSheet.show(context);
    if (source == null) {
      return;
    }

    final file = await HardwareFacade.pickImage(
      fromCamera: source == ChatAttachmentSourceAction.camera,
    );
    if (file == null) {
      return;
    }

    final fileName = ChatAttachmentPayloadBuilder.resolveFileName(
      file.name,
      type: ChatAttachmentType.image,
    );
    final contentType = ChatAttachmentPayloadBuilder.resolveContentType(
      fileName,
      type: ChatAttachmentType.image,
    );
    if (!_supportedContentTypes.contains(contentType)) {
      CustomToast.show('report_screenshot_format_unsupported'.tr);
      return;
    }

    final bytes = await file.readAsBytes();
    if (bytes.isEmpty) {
      CustomToast.show('report_upload_failed'.tr);
      return;
    }

    attachments.add(
      ReportAttachmentDraft(
        fileName: fileName,
        contentType: contentType,
        bytes: bytes,
      ),
    );
  }

  void removeAttachmentAt(int index) {
    if (index < 0 || index >= attachments.length) {
      return;
    }
    attachments.removeAt(index);
    clearFeedback();
  }

  void selectReason(String code) {
    final normalizedCode = code.trim();
    if (normalizedCode.isEmpty) {
      return;
    }
    selectedReasonCode.value = normalizedCode;
    clearFeedback();
  }

  void clearFeedback() {
    if (feedbackMessage.value == null) {
      return;
    }
    feedbackMessage.value = null;
  }

  void _setFeedback(String message, {bool isError = true}) {
    final normalizedMessage = message.trim();
    if (normalizedMessage.isEmpty) {
      return;
    }
    feedbackIsError.value = isError;
    feedbackMessage.value = normalizedMessage;
  }

  Future<void> submit() async {
    if (isSubmitting.value) {
      return;
    }
    if (!isTargetValid) {
      _setFeedback('report_target_invalid'.tr);
      return;
    }

    final selectedReason = selectedReasonCode.value.trim();
    if (selectedReason.isEmpty) {
      _setFeedback('report_reason_required'.tr);
      return;
    }
    if (attachments.isEmpty) {
      _setFeedback('report_screenshot_required'.tr);
      return;
    }
    if (attachments.length > maxAttachments) {
      _setFeedback(
        'report_screenshot_limit'.trParams({'count': '$maxAttachments'}),
      );
      return;
    }

    final description = descriptionController.text.trim();
    if (description.runes.length > maxDescriptionRunes) {
      _setFeedback(
        'report_description_too_long'.trParams({
          'count': '$maxDescriptionRunes',
        }),
      );
      return;
    }

    _setFeedback('common_loading'.tr, isError: false);
    isSubmitting.value = true;
    try {
      final assetKeys = <String>[];
      for (final attachment in attachments) {
        final presign = await _reportService.presignAsset(
          filename: attachment.fileName,
          contentType: attachment.contentType,
        );
        if (!presign.ok || presign.data == null) {
          _setFeedback(presign.message);
          return;
        }

        final uploaded = await _ossService.uploadToOss(
          presign.data!.uploadUrl,
          attachment.bytes,
          contentType: attachment.contentType,
        );
        if (!uploaded) {
          _setFeedback('report_upload_failed'.tr);
          return;
        }

        assetKeys.add(presign.data!.assetKey);
      }

      final result = await _reportService.createReport(
        targetType: targetArgs.targetType.name,
        targetUserId: targetArgs.targetUserId,
        targetSessionId: targetArgs.targetSessionId,
        sourceSessionId: targetArgs.sourceSessionId,
        reasonCode: selectedReason,
        description: description,
        assetKeys: assetKeys,
      );
      if (!result.ok) {
        _setFeedback(result.message);
        return;
      }

      _setFeedback('report_submit_success'.tr, isError: false);
      CustomToast.show('report_submit_success'.tr, isError: false);
      Get.back(result: true);
    } finally {
      isSubmitting.value = false;
    }
  }

  Map<String, dynamic> _readRouteArguments() {
    final args = Get.arguments;
    if (args is Map<String, dynamic>) {
      return args;
    }
    if (args is Map) {
      return Map<String, dynamic>.from(args);
    }
    return <String, dynamic>{};
  }

  ReportTargetArgs _parseTargetArgs(Map<String, dynamic> args) {
    final rawTargetType = args['target_type']?.toString().trim().toLowerCase();
    final targetType = rawTargetType == 'group'
        ? ReportTargetType.group
        : ReportTargetType.user;
    return ReportTargetArgs(
      targetType: targetType,
      targetUserId: args['target_user_id']?.toString().trim() ?? '',
      targetSessionId: args['target_session_id']?.toString().trim() ?? '',
      sourceSessionId: args['source_session_id']?.toString().trim() ?? '',
      title: args['title']?.toString().trim() ?? '',
      subtitle: args['subtitle']?.toString().trim() ?? '',
      avatarUrl: args['avatar_url']?.toString().trim() ?? '',
    );
  }
}
