import 'dart:async';
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../../app/themes/app_theme.dart';
import '../../../../shared/utils/app_external_links.dart';
import '../models/chat_agent_question_card_data.dart';
import '../models/chat_agent_status_card_data.dart';
import '../models/chat_message_card_action.dart';
import '../services/chat_agent_card_action_encoder.dart';
import '../../services/chat_managed_input.dart';
import 'chat_agent_interaction_result_panel.dart';

class _OptimisticQuestionSubmission {
  const _OptimisticQuestionSubmission({
    this.answer = '',
    this.acceptText = '',
    this.cancelText = '',
  });

  final String answer;
  final String acceptText;
  final String cancelText;
}

class ChatAgentQuestionCardView extends StatefulWidget {
  const ChatAgentQuestionCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
    this.managedInputBinding,
    this.onQuickAnswerTap,
    this.nowProvider,
  });

  final ChatAgentQuestionCardData card;
  final bool isMine;
  final double fontScale;
  final ChatManagedInputBinding? managedInputBinding;
  final Future<ChatMessageCardActionResult> Function(String answer)?
  onQuickAnswerTap;

  /// 时间源，仅测试注入用；默认取系统当前时间。
  final DateTime Function()? nowProvider;

  @override
  State<ChatAgentQuestionCardView> createState() =>
      _ChatAgentQuestionCardViewState();
}

class _ChatAgentQuestionCardViewState extends State<ChatAgentQuestionCardView> {
  final Object _inputRegionGroupId = Object();
  final GlobalKey _cardRootKey = GlobalKey(
    debugLabel: 'chat_agent_question_card_root',
  );
  bool _isSubmitting = false;
  bool _hasPendingSubmission = false;
  bool _reportedInputFocus = false;
  GlobalKey? _lastReportedTargetKey;
  String _submitError = '';
  final Map<int, TextEditingController> _answerControllers = {};
  final Map<int, FocusNode> _answerFocusNodes = {};
  final Map<int, GlobalKey> _answerInputTargetKeys = {};
  final Map<int, Set<String>> _selectedOptionsByQuestion = {};
  _OptimisticQuestionSubmission? _pendingSubmission;
  Timer? _countdownTimer;

  int get _nowMs {
    return (widget.nowProvider?.call() ?? DateTime.now())
        .millisecondsSinceEpoch;
  }

  bool get _isExpired {
    final expiresAt = widget.card.expiresAtMs;
    return expiresAt > 0 && _nowMs >= expiresAt;
  }

  Duration get _remainingTime {
    final expiresAt = widget.card.expiresAtMs;
    if (expiresAt <= 0) {
      return Duration.zero;
    }
    final diffMs = expiresAt - _nowMs;
    return diffMs > 0 ? Duration(milliseconds: diffMs) : Duration.zero;
  }

  /// 提交交互是否被锁定：提交中，或卡片已超时且尚未提交。
  bool get _interactionsLocked {
    return _isSubmitting || (_isExpired && !_hasSubmittedPayload);
  }

  void _syncCountdownTimer() {
    final shouldTick =
        widget.card.hasExpiry && !_hasSubmittedPayload && !_isExpired;
    if (shouldTick && _countdownTimer == null) {
      _countdownTimer = Timer.periodic(const Duration(seconds: 1), (_) {
        if (!mounted) {
          return;
        }
        setState(() {});
        if (_isExpired || _hasSubmittedPayload) {
          _countdownTimer?.cancel();
          _countdownTimer = null;
        }
      });
    } else if (!shouldTick && _countdownTimer != null) {
      _countdownTimer?.cancel();
      _countdownTimer = null;
    }
  }

  String _formatRemainingTime(Duration remaining) {
    final totalSeconds = remaining.inSeconds;
    final hours = totalSeconds ~/ 3600;
    final minutes = (totalSeconds % 3600) ~/ 60;
    final seconds = totalSeconds % 60;
    String two(int value) => value.toString().padLeft(2, '0');
    if (hours > 0) {
      return '$hours:${two(minutes)}:${two(seconds)}';
    }
    return '${two(minutes)}:${two(seconds)}';
  }

  String get _effectiveSubmittedAnswer {
    final submittedAnswer = widget.card.displaySubmittedAnswer;
    if (submittedAnswer.isNotEmpty) {
      return submittedAnswer;
    }
    return _pendingSubmission?.answer.trim() ?? '';
  }

  String get _effectiveSubmittedAcceptText {
    final submittedAcceptText = widget.card.displaySubmittedAcceptText;
    if (submittedAcceptText.isNotEmpty) {
      return submittedAcceptText;
    }
    return _pendingSubmission?.acceptText.trim() ?? '';
  }

  String get _effectiveSubmittedCancelText {
    final submittedCancelText = widget.card.displaySubmittedCancelText;
    if (submittedCancelText.isNotEmpty) {
      return submittedCancelText;
    }
    return _pendingSubmission?.cancelText.trim() ?? '';
  }

  ChatAgentStatusCardData? get _submissionStatus {
    if (_hasPendingSubmission) {
      return null;
    }
    return widget.card.submissionStatus;
  }

  bool get _hasSubmittedPayload {
    return _effectiveSubmittedAnswer.isNotEmpty ||
        _effectiveSubmittedAcceptText.isNotEmpty ||
        _effectiveSubmittedCancelText.isNotEmpty;
  }

  bool get _canRetrySubmission {
    final submissionStatus = _submissionStatus;
    return submissionStatus != null &&
        submissionStatus.displayStatus != 'success';
  }

  bool _shouldKeepFocusOnOutsidePointer(PointerDownEvent event) {
    final platform = defaultTargetPlatform;
    final isDesktopPlatform =
        platform == TargetPlatform.linux ||
        platform == TargetPlatform.macOS ||
        platform == TargetPlatform.windows;
    return isDesktopPlatform && event.kind == ui.PointerDeviceKind.mouse;
  }

  void _handleAnswerTapOutside(PointerDownEvent event) {
    if (_shouldKeepFocusOnOutsidePointer(event)) {
      return;
    }
    FocusManager.instance.primaryFocus?.unfocus();
  }

  String get _effectiveSubmissionSummary {
    if (_effectiveSubmittedAnswer.isNotEmpty) {
      return _effectiveSubmittedAnswer;
    }
    if (_effectiveSubmittedAcceptText.isNotEmpty) {
      return _effectiveSubmittedAcceptText;
    }
    return _effectiveSubmittedCancelText;
  }

  @override
  void initState() {
    super.initState();
    _syncDraftsFromCard();
    widget.managedInputBinding?.registerTarget(_cardRootKey);
    _syncCountdownTimer();
  }

  @override
  void didUpdateWidget(covariant ChatAgentQuestionCardView oldWidget) {
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
    if (_hasPendingSubmission &&
        _shouldClearPendingSubmission(oldWidget.card)) {
      _hasPendingSubmission = false;
      _pendingSubmission = null;
    }
    if (_shouldResyncDrafts(oldWidget.card, widget.card)) {
      _syncDraftsFromCard();
    }
    _syncCountdownTimer();
  }

  @override
  void dispose() {
    _countdownTimer?.cancel();
    widget.managedInputBinding?.reportFocusChange(false);
    widget.managedInputBinding?.unregister();
    for (final controller in _answerControllers.values) {
      controller.dispose();
    }
    for (final focusNode in _answerFocusNodes.values) {
      focusNode.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
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
    final codeStyle =
        theme.textTheme.bodyMedium?.copyWith(
          fontSize: 12 * widget.fontScale,
          color: theme.colorScheme.onSurface,
          height: 1.45,
          fontFamily: 'monospace',
        ) ??
        TextStyle(
          fontSize: 12 * widget.fontScale,
          color: theme.colorScheme.onSurface,
          height: 1.45,
          fontFamily: 'monospace',
        );
    final submissionStatus = _submissionStatus;
    final showPendingResult =
        _hasPendingSubmission ||
        (_hasSubmittedPayload && submissionStatus == null);
    final showInlineForm = !_hasSubmittedPayload || _canRetrySubmission;

    return TextFieldTapRegion(
      groupId: _inputRegionGroupId,
      child: KeyedSubtree(
        key: _cardRootKey,
        child: Container(
          key: const Key('chat_message_card_agent_question'),
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
                      Icons.help_outline_rounded,
                      size: 18,
                      color: accentColor,
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          'chat_message_card_agent_question_label'.tr,
                          style: titleStyle,
                        ),
                        const SizedBox(height: 4),
                        Text(
                          '${'chat_message_card_agent_question_request_id'.tr}: ${widget.card.displayRequestId}',
                          style: bodyStyle,
                        ),
                      ],
                    ),
                  ),
                ],
              ),
              if (widget.card.isUrlMode) ...[
                const SizedBox(height: 10),
                if (widget.card.displayMessage.isNotEmpty) ...[
                  Text(widget.card.displayMessage, style: bodyStyle),
                  const SizedBox(height: 8),
                ],
                Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: theme.colorScheme.surface,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(
                      color: theme.colorScheme.outline.withValues(alpha: 0.12),
                    ),
                  ),
                  child: SelectableText(
                    widget.card.displayUrl,
                    style: codeStyle,
                  ),
                ),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    OutlinedButton.icon(
                      key: const Key(
                        'chat_message_card_agent_question_open_url',
                      ),
                      onPressed: _interactionsLocked ? null : _handleOpenUrl,
                      icon: const Icon(Icons.open_in_new_rounded, size: 16),
                      label: Text(
                        widget.card.displayOpenUrlLabel.isNotEmpty
                            ? widget.card.displayOpenUrlLabel
                            : 'chat_message_card_agent_question_open_url'.tr,
                      ),
                    ),
                    if (showInlineForm && widget.onQuickAnswerTap != null)
                      FilledButton(
                        key: const Key(
                          'chat_message_card_agent_question_complete',
                        ),
                        onPressed: _interactionsLocked ? null : _handleUrlComplete,
                        child: Text('common_done'.tr),
                      ),
                    if (showInlineForm && widget.onQuickAnswerTap != null)
                      OutlinedButton(
                        key: const Key(
                          'chat_message_card_agent_question_cancel',
                        ),
                        onPressed: _interactionsLocked ? null : _handleUrlCancel,
                        child: Text('common_cancel'.tr),
                      ),
                  ],
                ),
              ],
              if (!widget.card.isUrlMode) ...[
                for (final question in widget.card.questions) ...[
                  const SizedBox(height: 10),
                  Text(
                    '${question.index}. ${question.displayHeader}',
                    style: titleStyle,
                  ),
                  const SizedBox(height: 4),
                  Text(question.displayPrompt, style: bodyStyle),
                  if (question.displayOptions.isNotEmpty) ...[
                    const SizedBox(height: 6),
                    Wrap(
                      spacing: 6,
                      runSpacing: 6,
                      children: question.displayOptions
                          .map(
                            (option) => _buildOptionChip(
                              question: question,
                              option: option,
                              bodyStyle: bodyStyle,
                              readOnly: !showInlineForm || _interactionsLocked,
                            ),
                          )
                          .toList(growable: false),
                    ),
                  ],
                  if (question.multiSelect) ...[
                    const SizedBox(height: 4),
                    Text(
                      'chat_message_card_agent_question_multi_hint'.tr,
                      style: bodyStyle,
                    ),
                  ],
                  if (showInlineForm && widget.onQuickAnswerTap != null) ...[
                    const SizedBox(height: 8),
                    Container(
                      key: _inputTargetKeyFor(question.index),
                      child: TextField(
                        key: Key(
                          'chat_message_card_agent_question_input_${question.index}',
                        ),
                        groupId: _inputRegionGroupId,
                        controller: _controllerFor(question.index),
                        focusNode: _focusNodeFor(question.index),
                        enabled: !_interactionsLocked,
                        minLines: 1,
                        maxLines: 4,
                        scrollPadding: const EdgeInsets.only(bottom: 320),
                        onTapOutside: _handleAnswerTapOutside,
                        onChanged: (value) =>
                            _syncSelectionFromText(question, value),
                        decoration: InputDecoration(
                          isDense: true,
                          hintText:
                              'chat_message_card_agent_question_input_hint'
                                  .trParams({'index': '${question.index}'}),
                          border: const OutlineInputBorder(),
                        ),
                      ),
                    ),
                  ],
                ],
              ],
              if (_hasSubmittedPayload) ...[
                const SizedBox(height: 10),
                Text(
                  'chat_message_card_agent_question_answered'.trParams({
                    'answer': _effectiveSubmissionSummary,
                  }),
                  key: const Key('chat_message_card_agent_question_answered'),
                  style: bodyStyle.copyWith(fontWeight: FontWeight.w600),
                ),
              ],
              if (!widget.card.isUrlMode &&
                  showInlineForm &&
                  widget.card.supportsQuickOptionReplies &&
                  widget.onQuickAnswerTap != null) ...[
                const SizedBox(height: 8),
                Text(
                  'chat_message_card_agent_question_quick_reply'.tr,
                  style: titleStyle,
                ),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: widget.card.quickReplyOptions
                      .asMap()
                      .entries
                      .map(
                        (entry) => FilledButton(
                          key: Key(
                            'chat_message_card_agent_question_option_${entry.key}',
                          ),
                          onPressed: _interactionsLocked
                              ? null
                              : () => _handleQuickAnswer(entry.value),
                          child: Text(entry.value),
                        ),
                      )
                      .toList(growable: false),
                ),
              ],
              if (!widget.card.isUrlMode &&
                  showInlineForm &&
                  widget.onQuickAnswerTap != null &&
                  widget.card.supportsStructuredReplies) ...[
                const SizedBox(height: 10),
                Align(
                  alignment: Alignment.centerLeft,
                  child: FilledButton.icon(
                    key: const Key('chat_message_card_agent_question_submit'),
                    onPressed: _interactionsLocked ? null : _handleStructuredSubmit,
                    icon: _isSubmitting
                        ? const SizedBox(
                            width: 16,
                            height: 16,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.send_rounded, size: 16),
                    label: Text('chat_message_card_agent_question_submit'.tr),
                  ),
                ),
              ],
              if (widget.card.hasExpiry &&
                  showInlineForm &&
                  !_hasSubmittedPayload) ...[
                const SizedBox(height: 10),
                if (_isExpired)
                  Row(
                    key: const Key(
                      'chat_message_card_agent_question_expired',
                    ),
                    children: [
                      Icon(
                        Icons.timer_off_outlined,
                        size: 14,
                        color: theme.colorScheme.error,
                      ),
                      const SizedBox(width: 6),
                      Expanded(
                        child: Text(
                          'chat_message_card_agent_question_expired'.tr,
                          style: bodyStyle.copyWith(
                            color: theme.colorScheme.error,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                    ],
                  )
                else
                  Row(
                    key: const Key(
                      'chat_message_card_agent_question_countdown',
                    ),
                    children: [
                      Icon(
                        Icons.timer_outlined,
                        size: 14,
                        color: _remainingTime.inSeconds <= 60
                            ? theme.colorScheme.error
                            : accentColor,
                      ),
                      const SizedBox(width: 6),
                      Expanded(
                        child: Text(
                          'chat_message_card_agent_question_countdown'
                              .trParams({
                                'time': _formatRemainingTime(_remainingTime),
                              }),
                          style: bodyStyle.copyWith(
                            color: _remainingTime.inSeconds <= 60
                                ? theme.colorScheme.error
                                : null,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                    ],
                  ),
              ],
              if (showPendingResult) ...[
                const SizedBox(height: 10),
                ChatAgentInteractionResultPanel(
                  key: const Key('chat_message_card_agent_question_result'),
                  summary: 'chat_message_card_agent_question_submitting'.tr,
                  fontScale: widget.fontScale,
                  accentColor: accentColor,
                  tone: ChatAgentInteractionResultTone.pending,
                ),
              ],
              if (submissionStatus != null) ...[
                const SizedBox(height: 10),
                ChatAgentInteractionResultPanel(
                  key: const Key('chat_message_card_agent_question_result'),
                  summary: submissionStatus.displaySummary,
                  detailText: submissionStatus.displayDetailText,
                  fontScale: widget.fontScale,
                  accentColor: accentColor,
                  tone: _mapStatusTone(submissionStatus.displayStatus),
                ),
              ],
              if (widget.card.displayFooterText.isNotEmpty) ...[
                const SizedBox(height: 10),
                Text(widget.card.displayFooterText, style: bodyStyle),
              ],
              if (_submitError.isNotEmpty) ...[
                const SizedBox(height: 10),
                Text(
                  _submitError,
                  key: const Key(
                    'chat_message_card_agent_question_submit_error',
                  ),
                  style: bodyStyle.copyWith(color: theme.colorScheme.error),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _handleQuickAnswer(String answer) async {
    final callback = widget.onQuickAnswerTap;
    if (callback == null || _interactionsLocked) {
      return;
    }
    if (widget.card.questions.isNotEmpty) {
      _selectSingleOption(widget.card.questions.first, answer, true);
    }
    late final String action;
    try {
      action = ChatAgentCardActionEncoder.buildQuestionQuickReplyAction(
        widget.card,
        answer,
      );
    } catch (_) {
      setState(() {
        _submitError = 'chat_message_card_agent_question_submit_invalid'.tr;
      });
      return;
    }
    setState(() {
      _isSubmitting = true;
      _submitError = '';
    });
    final result = await callback(action);
    if (!mounted) {
      return;
    }
    switch (result.status) {
      case ChatMessageCardActionStatus.submitted:
        setState(() {
          _isSubmitting = false;
          _hasPendingSubmission = true;
          _pendingSubmission = _OptimisticQuestionSubmission(answer: answer);
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
              : 'chat_message_card_agent_question_submit_failed'.tr;
        });
        return;
    }
  }

  Future<void> _handleStructuredSubmit() async {
    final callback = widget.onQuickAnswerTap;
    if (callback == null || _interactionsLocked) {
      return;
    }
    final answersByIndex = <int, String>{};
    for (final question in widget.card.questions) {
      answersByIndex[question.index] = _controllerFor(
        question.index,
      ).text.trim();
    }
    late final String command;
    try {
      command = ChatAgentCardActionEncoder.buildQuestionStructuredReplyAction(
        widget.card,
        answersByIndex,
      );
    } catch (_) {
      setState(() {
        _submitError = 'chat_message_card_agent_question_submit_invalid'.tr;
      });
      return;
    }
    setState(() {
      _isSubmitting = true;
      _submitError = '';
    });
    final result = await callback(command);
    if (!mounted) {
      return;
    }
    switch (result.status) {
      case ChatMessageCardActionStatus.submitted:
        setState(() {
          _isSubmitting = false;
          _hasPendingSubmission = true;
          _pendingSubmission = _OptimisticQuestionSubmission(
            answer: _buildStructuredSubmissionSummary(answersByIndex),
          );
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
              : 'chat_message_card_agent_question_submit_failed'.tr;
        });
        return;
    }
  }

  Future<void> _handleOpenUrl() async {
    final opened = await AppExternalLinks.open(widget.card.displayUrl);
    if (!mounted || opened) {
      return;
    }
    setState(() {
      _submitError = 'chat_message_card_agent_question_open_url_failed'.tr;
    });
  }

  Future<void> _handleUrlComplete() async {
    late final String command;
    try {
      command = ChatAgentCardActionEncoder.buildQuestionUrlCompleteAction(
        widget.card,
      );
    } catch (_) {
      setState(() {
        _submitError = 'chat_message_card_agent_question_submit_invalid'.tr;
      });
      return;
    }
    await _handleUrlCommand(command, isAccept: true);
  }

  Future<void> _handleUrlCancel() async {
    late final String command;
    try {
      command = ChatAgentCardActionEncoder.buildQuestionUrlCancelAction(
        widget.card,
      );
    } catch (_) {
      setState(() {
        _submitError = 'chat_message_card_agent_question_submit_invalid'.tr;
      });
      return;
    }
    await _handleUrlCommand(command, isAccept: false);
  }

  Future<void> _handleUrlCommand(
    String command, {
    required bool isAccept,
  }) async {
    final callback = widget.onQuickAnswerTap;
    if (callback == null || _isSubmitting) {
      return;
    }
    setState(() {
      _isSubmitting = true;
      _submitError = '';
    });
    final result = await callback(command);
    if (!mounted) {
      return;
    }
    switch (result.status) {
      case ChatMessageCardActionStatus.submitted:
        setState(() {
          _isSubmitting = false;
          _hasPendingSubmission = true;
          _pendingSubmission = isAccept
              ? _OptimisticQuestionSubmission(acceptText: 'common_done'.tr)
              : _OptimisticQuestionSubmission(cancelText: 'common_cancel'.tr);
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
              : 'chat_message_card_agent_question_submit_failed'.tr;
        });
        return;
    }
  }

  Widget _buildOptionChip({
    required ChatAgentQuestionPrompt question,
    required String option,
    required TextStyle bodyStyle,
    required bool readOnly,
  }) {
    if (widget.onQuickAnswerTap == null || readOnly) {
      return Chip(
        label: Text(option, style: bodyStyle),
        visualDensity: VisualDensity.compact,
      );
    }
    if (question.multiSelect) {
      final selected =
          _selectedOptionsByQuestion[question.index]?.contains(option) ?? false;
      return FilterChip(
        key: Key(
          'chat_message_card_agent_question_option_${question.index}_${option.hashCode}',
        ),
        label: Text(option, style: bodyStyle),
        selected: selected,
        onSelected: _isSubmitting
            ? null
            : (next) => _toggleMultiSelectOption(question, option, next),
        visualDensity: VisualDensity.compact,
      );
    }
    final selected = _controllerFor(question.index).text.trim() == option;
    return ChoiceChip(
      key: Key(
        'chat_message_card_agent_question_option_${question.index}_${option.hashCode}',
      ),
      label: Text(option, style: bodyStyle),
      selected: selected,
      onSelected: _isSubmitting
          ? null
          : (next) => _selectSingleOption(question, option, next),
      visualDensity: VisualDensity.compact,
    );
  }

  TextEditingController _controllerFor(int questionIndex) {
    return _answerControllers.putIfAbsent(
      questionIndex,
      () => TextEditingController(),
    );
  }

  GlobalKey _inputTargetKeyFor(int questionIndex) {
    return _answerInputTargetKeys.putIfAbsent(
      questionIndex,
      () => GlobalKey(
        debugLabel: 'chat_agent_question_input_target_$questionIndex',
      ),
    );
  }

  FocusNode _focusNodeFor(int questionIndex) {
    return _answerFocusNodes.putIfAbsent(questionIndex, () {
      final focusNode = FocusNode();
      focusNode.addListener(_syncManagedInputFocus);
      return focusNode;
    });
  }

  bool get _hasFocusedAnswerInput =>
      _answerFocusNodes.values.any((focusNode) => focusNode.hasFocus);

  GlobalKey get _activeInputTargetKey {
    for (final entry in _answerFocusNodes.entries) {
      if (entry.value.hasFocus) {
        return _inputTargetKeyFor(entry.key);
      }
    }
    return _cardRootKey;
  }

  void _syncManagedInputFocus() {
    final binding = widget.managedInputBinding;
    if (binding == null) {
      return;
    }
    final targetKey = _activeInputTargetKey;
    if (!identical(_lastReportedTargetKey, targetKey)) {
      binding.updateTargetKey(targetKey);
      _lastReportedTargetKey = targetKey;
    }
    if (_reportedInputFocus == _hasFocusedAnswerInput) {
      return;
    }
    _reportedInputFocus = _hasFocusedAnswerInput;
    binding.reportFocusChange(_reportedInputFocus);
  }

  void _syncDraftsFromCard() {
    final nextIndexes = widget.card.questions
        .map((question) => question.index)
        .toSet();
    final staleIndexes = _answerControllers.keys
        .where((index) => !nextIndexes.contains(index))
        .toList(growable: false);
    for (final index in staleIndexes) {
      _answerControllers.remove(index)?.dispose();
      _answerFocusNodes.remove(index)?.dispose();
      _answerInputTargetKeys.remove(index);
      _selectedOptionsByQuestion.remove(index);
    }
    for (final question in widget.card.questions) {
      _controllerFor(question.index);
      _selectedOptionsByQuestion.putIfAbsent(question.index, () => <String>{});
    }
    _hasPendingSubmission = false;
    _pendingSubmission = null;
    _submitError = '';
    _isSubmitting = false;
  }

  bool _shouldClearPendingSubmission(ChatAgentQuestionCardData oldCard) {
    final statusChanged =
        oldCard.submissionStatus != widget.card.submissionStatus;
    final submissionChanged =
        oldCard.submittedAnswer != widget.card.submittedAnswer ||
        oldCard.submittedAcceptText != widget.card.submittedAcceptText ||
        oldCard.submittedCancelText != widget.card.submittedCancelText;
    if (!statusChanged && !submissionChanged) {
      return false;
    }
    return widget.card.submissionStatus != null ||
        _hasServerSubmission(widget.card);
  }

  bool _hasServerSubmission(ChatAgentQuestionCardData card) {
    return card.displaySubmittedAnswer.isNotEmpty ||
        card.displaySubmittedAcceptText.isNotEmpty ||
        card.displaySubmittedCancelText.isNotEmpty;
  }

  bool _shouldResyncDrafts(
    ChatAgentQuestionCardData previous,
    ChatAgentQuestionCardData next,
  ) {
    if (previous.requestId != next.requestId ||
        previous.mode != next.mode ||
        previous.message != next.message ||
        previous.url != next.url ||
        previous.openUrlLabel != next.openUrlLabel ||
        previous.footerText != next.footerText ||
        previous.submittedAcceptText != next.submittedAcceptText ||
        previous.submittedCancelText != next.submittedCancelText ||
        previous.questions.length != next.questions.length) {
      return true;
    }
    for (var index = 0; index < previous.questions.length; index++) {
      final left = previous.questions[index];
      final right = next.questions[index];
      if (left.index != right.index ||
          left.header != right.header ||
          left.prompt != right.prompt ||
          left.multiSelect != right.multiSelect ||
          left.options.length != right.options.length) {
        return true;
      }
      for (
        var optionIndex = 0;
        optionIndex < left.options.length;
        optionIndex++
      ) {
        if (left.options[optionIndex] != right.options[optionIndex]) {
          return true;
        }
      }
    }
    return false;
  }

  String _buildStructuredSubmissionSummary(Map<int, String> answersByIndex) {
    if (widget.card.questions.length == 1) {
      return answersByIndex[widget.card.questions.first.index]?.trim() ?? '';
    }
    final segments = <String>[];
    for (final question in widget.card.questions) {
      final answer = answersByIndex[question.index]?.trim() ?? '';
      if (answer.isEmpty) {
        continue;
      }
      segments.add('${question.displayHeader}: $answer');
    }
    return segments.join('\n');
  }

  void _selectSingleOption(
    ChatAgentQuestionPrompt question,
    String option,
    bool selected,
  ) {
    final controller = _controllerFor(question.index);
    final nextText = selected ? option : '';
    controller.value = TextEditingValue(
      text: nextText,
      selection: TextSelection.collapsed(offset: nextText.length),
      composing: TextRange.empty,
    );
    _selectedOptionsByQuestion[question.index] = selected
        ? {option}
        : <String>{};
    setState(() {});
  }

  void _toggleMultiSelectOption(
    ChatAgentQuestionPrompt question,
    String option,
    bool selected,
  ) {
    final selectedOptions = _selectedOptionsByQuestion.putIfAbsent(
      question.index,
      () => <String>{},
    );
    if (selected) {
      selectedOptions.add(option);
    } else {
      selectedOptions.remove(option);
    }
    final orderedSelections = question.displayOptions
        .where(selectedOptions.contains)
        .toList(growable: false);
    final controller = _controllerFor(question.index);
    final nextText = orderedSelections.join(', ');
    controller.value = TextEditingValue(
      text: nextText,
      selection: TextSelection.collapsed(offset: nextText.length),
      composing: TextRange.empty,
    );
    setState(() {});
  }

  void _syncSelectionFromText(ChatAgentQuestionPrompt question, String value) {
    final normalized = value.trim();
    if (question.multiSelect) {
      final selections = normalized
          .split(',')
          .map((entry) => entry.trim())
          .where((entry) => question.displayOptions.contains(entry))
          .toSet();
      _selectedOptionsByQuestion[question.index] = selections;
    } else {
      _selectedOptionsByQuestion[question.index] =
          question.displayOptions.contains(normalized)
          ? {normalized}
          : <String>{};
    }
    setState(() {});
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
