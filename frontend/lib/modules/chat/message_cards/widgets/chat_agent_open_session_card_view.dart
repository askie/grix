import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../../app/themes/app_theme.dart';
import '../models/chat_agent_open_session_card_data.dart';
import '../models/chat_agent_status_card_data.dart';
import '../models/chat_message_card_action.dart';
import '../services/chat_agent_card_action_encoder.dart';
import '../services/chat_agent_card_text_localizer.dart';
import '../services/chat_open_session_draft_store.dart';
import '../../services/chat_managed_input.dart';
import 'chat_agent_interaction_result_panel.dart';

class ChatAgentOpenSessionCardView extends StatefulWidget {
  const ChatAgentOpenSessionCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
    this.onSubmit,
    this.managedInputBinding,
    this.pickRemoteDirectory,
    this.platform,
  });

  final ChatAgentOpenSessionCardData card;
  final bool isMine;
  final double fontScale;
  final Future<ChatMessageCardActionResult> Function(String command)? onSubmit;
  final ChatManagedInputBinding? managedInputBinding;
  final Future<String?> Function()? pickRemoteDirectory;
  final TargetPlatform? platform;

  @override
  State<ChatAgentOpenSessionCardView> createState() =>
      _ChatAgentOpenSessionCardViewState();
}

class _ChatAgentOpenSessionCardViewState
    extends State<ChatAgentOpenSessionCardView>
    with AutomaticKeepAliveClientMixin {
  final Object _inputRegionGroupId = Object();
  final GlobalKey _cardRootKey = GlobalKey(
    debugLabel: 'chat_agent_open_session_card_root',
  );
  final GlobalKey _pathInputTargetKey = GlobalKey(
    debugLabel: 'chat_agent_open_session_card_input_target',
  );
  late final TextEditingController _pathController;
  late final FocusNode _pathFocusNode;
  String _pendingSubmittedPath = '';
  bool _hasPendingSubmission = false;
  bool _isSubmitting = false;
  bool _isPickingRemoteDirectory = false;
  bool _reportedInputFocus = false;
  GlobalKey? _lastReportedTargetKey;
  String _submitError = '';
  bool _wasResetByBackend = false;
  String get _effectiveSubmittedPath {
    final submittedPath = widget.card.displaySubmittedPath;
    if (submittedPath.isNotEmpty) {
      return submittedPath;
    }
    return _pendingSubmittedPath;
  }

  ChatAgentStatusCardData? get _submissionStatus {
    if (_hasPendingSubmission) {
      return null;
    }
    return widget.card.submissionStatus;
  }

  bool get _canRetrySubmission {
    final submissionStatus = _submissionStatus;
    return submissionStatus != null &&
        submissionStatus.displayStatus != 'success';
  }

  bool _shouldKeepFocusOnOutsidePointer(PointerDownEvent event) {
    final platform = widget.platform ?? defaultTargetPlatform;
    final isDesktopPlatform =
        platform == TargetPlatform.linux ||
        platform == TargetPlatform.macOS ||
        platform == TargetPlatform.windows;
    return isDesktopPlatform && event.kind == ui.PointerDeviceKind.mouse;
  }

  void _handlePathTapOutside(PointerDownEvent event) {
    if (_shouldKeepFocusOnOutsidePointer(event)) {
      return;
    }
    FocusManager.instance.primaryFocus?.unfocus();
  }

  void _setPathText(String value) {
    _pathController.value = TextEditingValue(
      text: value,
      selection: TextSelection.collapsed(offset: value.length),
    );
    _persistDraft();
  }

  // 卡片已提交则不恢复草稿，否则优先恢复未提交草稿，再回退到初始目录。
  // 草稿凭卡片实例 id 暂存在 State 之外，使列表重建/State 重建后路径不丢。
  String _resolveInitialPathText() {
    if (widget.card.displaySubmittedPath.isNotEmpty) {
      return widget.card.displayInitialCwd;
    }
    final draft = ChatOpenSessionDraftStore.read(
      widget.card.displayCardInstanceId,
    );
    if (draft != null && draft.isNotEmpty) {
      return draft;
    }
    return widget.card.displayInitialCwd;
  }

  void _persistDraft() {
    ChatOpenSessionDraftStore.write(
      widget.card.displayCardInstanceId,
      _pathController.text.trim(),
    );
  }

  @override
  void initState() {
    super.initState();
    _pathController = TextEditingController(text: _resolveInitialPathText());
    _pathFocusNode = FocusNode();
    _pathFocusNode.addListener(_handlePathFocusChanged);
    widget.managedInputBinding?.registerTarget(_cardRootKey);
  }

  @override
  void didUpdateWidget(covariant ChatAgentOpenSessionCardView oldWidget) {
    super.didUpdateWidget(oldWidget);
    final previousBinding = oldWidget.managedInputBinding;
    final nextBinding = widget.managedInputBinding;
    if (previousBinding?.inputId != nextBinding?.inputId) {
      previousBinding?.reportFocusChange(false);
      previousBinding?.unregister();
      _reportedInputFocus = false;
      _lastReportedTargetKey = null;
      nextBinding?.registerTarget(_cardRootKey);
      _syncManagedInputFocus();
    }
    if (_hasPendingSubmission) {
      if (widget.card.displaySubmittedPath == _pendingSubmittedPath &&
          widget.card.submissionStatus != null) {
        _hasPendingSubmission = false;
        _pendingSubmittedPath = '';
      }
    }
    // Detect backend card content change (retry form) regardless of
    // _hasPendingSubmission — the edit may arrive before _handleSubmit
    // sets _hasPendingSubmission to true (race condition).
    if (widget.card.displaySummaryText != oldWidget.card.displaySummaryText ||
        widget.card.displayDetailText != oldWidget.card.displayDetailText) {
      _hasPendingSubmission = false;
      _pendingSubmittedPath = '';
      _wasResetByBackend = true;
    }
    if (oldWidget.card.displayInitialCwd != widget.card.displayInitialCwd &&
        _pathController.text.trim().isEmpty &&
        _effectiveSubmittedPath.isEmpty) {
      _setPathText(widget.card.displayInitialCwd);
    }
  }

  @override
  void dispose() {
    widget.managedInputBinding?.reportFocusChange(false);
    widget.managedInputBinding?.unregister();
    _pathFocusNode.removeListener(_handlePathFocusChanged);
    _pathFocusNode.dispose();
    _pathController.dispose();
    super.dispose();
  }

  @override
  bool get wantKeepAlive => true;

  @override
  Widget build(BuildContext context) {
    super.build(context);
    final theme = Theme.of(context);
    final viewportWidth = MediaQuery.sizeOf(context).width;
    final accentColor = widget.isMine
        ? theme.colorScheme.primary
        : theme.colorScheme.secondary;
    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.labelMedium?.copyWith(
            fontSize: 11 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ) ??
          TextStyle(
            fontSize: 11 * widget.fontScale,
            fontWeight: FontWeight.w700,
            color: accentColor.withValues(alpha: 0.9),
            letterSpacing: 0.2,
          ),
    );
    final bodyStyle = AppTheme.applyTextFont(
      theme.textTheme.bodySmall?.copyWith(
            fontSize: 12 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.8),
            height: 1.45,
          ) ??
          TextStyle(
            fontSize: 12 * widget.fontScale,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.8),
            height: 1.45,
          ),
    );
    final submittedPath = _effectiveSubmittedPath;
    final submissionStatus = _submissionStatus;
    final showPendingResult =
        (_hasPendingSubmission ||
            (submittedPath.isNotEmpty && submissionStatus == null)) &&
        !_wasResetByBackend;
    final showInlineForm =
        submittedPath.isEmpty || _canRetrySubmission || _wasResetByBackend;
    final inputEnabled = !_isSubmitting && showInlineForm;

    return TextFieldTapRegion(
      groupId: _inputRegionGroupId,
      child: KeyedSubtree(
        key: _cardRootKey,
        child: Container(
          key: const Key('chat_message_card_agent_open_session'),
          constraints: BoxConstraints(
            minWidth: 240,
            maxWidth: viewportWidth * 0.8,
          ),
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: accentColor.withValues(alpha: 0.08),
            borderRadius: BorderRadius.circular(12),
            border: Border.all(color: accentColor.withValues(alpha: 0.18)),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Row(
                children: [
                  Container(
                    width: 34,
                    height: 34,
                    decoration: BoxDecoration(
                      color: accentColor.withValues(alpha: 0.14),
                      borderRadius: BorderRadius.circular(10),
                    ),
                    child: Icon(
                      Icons.folder_open_rounded,
                      size: 18,
                      color: accentColor,
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Text(
                      'chat_message_card_agent_open_session_label'.tr,
                      style: titleStyle,
                    ),
                  ),
                ],
              ),
              if (widget.card.displaySummaryText.isNotEmpty) ...[
                const SizedBox(height: 10),
                Text(
                  ChatAgentCardTextLocalizer.localize(
                    widget.card.displaySummaryText,
                  ),
                  style: bodyStyle,
                ),
              ],
              if (widget.card.displayDetailText.isNotEmpty) ...[
                const SizedBox(height: 8),
                Text(
                  ChatAgentCardTextLocalizer.localize(
                    widget.card.displayDetailText,
                  ),
                  style: bodyStyle,
                ),
              ],
              if (showInlineForm) ...[
                const SizedBox(height: 10),
                Container(
                  key: _pathInputTargetKey,
                  child: TextField(
                    key: const Key(
                      'chat_message_card_agent_open_session_input',
                    ),
                    groupId: _inputRegionGroupId,
                    controller: _pathController,
                    focusNode: _pathFocusNode,
                    enabled: inputEnabled,
                    minLines: 1,
                    maxLines: 2,
                    scrollPadding: const EdgeInsets.only(bottom: 320),
                    onTapOutside: _handlePathTapOutside,
                    onChanged: (_) {
                      _persistDraft();
                      if (_submitError.isEmpty) {
                        return;
                      }
                      setState(() {
                        _submitError = '';
                      });
                    },
                    decoration: InputDecoration(
                      isDense: true,
                      hintText:
                          'chat_message_card_agent_open_session_input_hint'.tr,
                      border: const OutlineInputBorder(),
                      suffixIcon: widget.pickRemoteDirectory != null
                          ? IconButton(
                              key: const Key(
                                'chat_message_card_agent_open_session_browse',
                              ),
                              icon: _isPickingRemoteDirectory
                                  ? const SizedBox(
                                      width: 16,
                                      height: 16,
                                      child: CircularProgressIndicator(
                                        strokeWidth: 2,
                                      ),
                                    )
                                  : const Icon(
                                      Icons.folder_open_rounded,
                                      size: 18,
                                    ),
                              tooltip:
                                  'chat_message_card_agent_open_session_browse'
                                      .tr,
                              onPressed:
                                  inputEnabled && !_isPickingRemoteDirectory
                                  ? _handlePickRemoteDirectory
                                  : null,
                            )
                          : null,
                    ),
                  ),
                ),
                const SizedBox(height: 10),
                Row(
                  children: [
                    Expanded(
                      child: FilledButton(
                        key: const Key(
                          'chat_message_card_agent_open_session_submit',
                        ),
                        onPressed: inputEnabled ? _handleSubmit : null,
                        child: _isSubmitting
                            ? Row(
                                mainAxisAlignment: MainAxisAlignment.center,
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  const SizedBox(
                                    width: 16,
                                    height: 16,
                                    child: CircularProgressIndicator(
                                      strokeWidth: 2,
                                    ),
                                  ),
                                  const SizedBox(width: 8),
                                  Text(
                                    'chat_message_card_agent_open_session_submit_loading'
                                        .tr,
                                  ),
                                ],
                              )
                            : Text(
                                'chat_message_card_agent_open_session_submit'
                                    .tr,
                              ),
                      ),
                    ),
                  ],
                ),
                if (_submitError.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text(
                    _submitError,
                    key: const Key(
                      'chat_message_card_agent_open_session_submit_error',
                    ),
                    style: bodyStyle.copyWith(color: theme.colorScheme.error),
                  ),
                ],
              ] else ...[
                const SizedBox(height: 10),
                Text(
                  'chat_message_card_agent_open_session_submitted'.trParams({
                    'path': submittedPath,
                  }),
                  key: const Key(
                    'chat_message_card_agent_open_session_submitted',
                  ),
                  style: bodyStyle.copyWith(fontWeight: FontWeight.w600),
                ),
              ],
              if (showPendingResult) ...[
                const SizedBox(height: 10),
                ChatAgentInteractionResultPanel(
                  key: const Key('chat_message_card_agent_open_session_result'),
                  summary:
                      'chat_message_card_agent_open_session_submit_loading'.tr,
                  fontScale: widget.fontScale,
                  accentColor: accentColor,
                  tone: ChatAgentInteractionResultTone.pending,
                ),
              ],
              if (submissionStatus != null) ...[
                const SizedBox(height: 10),
                // 关联 ID 不再展示；详情是否下发由后端决定：绑定成功卡
                // 已不携带技术详情，where/status 查询卡仍携带工作区详情。
                ChatAgentInteractionResultPanel(
                  key: const Key('chat_message_card_agent_open_session_result'),
                  summary: submissionStatus.displaySummary,
                  detailText: submissionStatus.displayDetailText,
                  fontScale: widget.fontScale,
                  accentColor: accentColor,
                  tone: _mapStatusTone(submissionStatus.displayStatus),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }

  void _handlePathFocusChanged() {
    _syncManagedInputFocus();
  }

  void _syncManagedInputFocus() {
    final binding = widget.managedInputBinding;
    if (binding == null) {
      return;
    }
    final targetKey = _pathFocusNode.hasFocus
        ? _pathInputTargetKey
        : _cardRootKey;
    if (!identical(_lastReportedTargetKey, targetKey)) {
      binding.updateTargetKey(targetKey);
      _lastReportedTargetKey = targetKey;
    }
    if (_reportedInputFocus == _pathFocusNode.hasFocus) {
      return;
    }
    _reportedInputFocus = _pathFocusNode.hasFocus;
    binding.reportFocusChange(_reportedInputFocus);
  }

  Future<void> _handlePickRemoteDirectory() async {
    final pickRemoteDirectory = widget.pickRemoteDirectory;
    if (pickRemoteDirectory == null) return;
    setState(() {
      _isPickingRemoteDirectory = true;
      _submitError = '';
    });
    try {
      final selectedPath = (await pickRemoteDirectory())?.trim() ?? '';
      if (!mounted || selectedPath.isEmpty) return;
      setState(() {
        _setPathText(selectedPath);
      });
    } finally {
      if (mounted) {
        setState(() {
          _isPickingRemoteDirectory = false;
        });
      }
    }
  }

  Future<void> _handleSubmit() async {
    final onSubmit = widget.onSubmit;
    if (onSubmit == null || _isSubmitting) {
      return;
    }
    final cwd = _pathController.text.trim();
    if (cwd.isEmpty) {
      setState(() {
        _submitError = 'chat_message_card_agent_open_session_submit_invalid'.tr;
      });
      return;
    }

    String command;
    try {
      command = ChatAgentCardActionEncoder.buildOpenSessionAction(
        widget.card,
        cwd,
      );
    } catch (_) {
      setState(() {
        _submitError = 'chat_message_card_agent_open_session_submit_invalid'.tr;
      });
      return;
    }

    setState(() {
      _isSubmitting = true;
      _submitError = '';
      _wasResetByBackend = false;
    });

    final result = await onSubmit(command);
    if (!mounted) {
      return;
    }

    switch (result.status) {
      case ChatMessageCardActionStatus.submitted:
        ChatOpenSessionDraftStore.clear(widget.card.displayCardInstanceId);
        setState(() {
          _isSubmitting = false;
          _pendingSubmittedPath = cwd;
          _hasPendingSubmission = true;
        });
        return;
      case ChatMessageCardActionStatus.ignored:
        setState(() {
          _isSubmitting = false;
        });
        return;
      case ChatMessageCardActionStatus.failed:
        setState(() {
          _isSubmitting = false;
          _submitError = result.message.isNotEmpty
              ? result.message
              : 'chat_message_card_agent_open_session_submit_failed'.tr;
        });
        return;
    }
  }

  ChatAgentInteractionResultTone _mapStatusTone(String status) {
    switch (status) {
      case 'success':
        return ChatAgentInteractionResultTone.success;
      case 'warning':
        return ChatAgentInteractionResultTone.warning;
      case 'error':
        return ChatAgentInteractionResultTone.error;
      default:
        return ChatAgentInteractionResultTone.info;
    }
  }
}
