import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

/// 打开 Agent 介绍的全屏编辑器。
///
/// 与页面上的小输入框共用同一份 [textController]，文字实时同步；
/// 焦点节点独立（Flutter 不允许两个输入框共用 FocusNode）。
Future<void> openAgentIntroductionExpandedEditor({
  required TextEditingController textController,
  required Future<void> Function(BuildContext context) onInsertContact,
  int? maxLength,
}) async {
  await Get.to<void>(
    () => _AgentIntroductionExpandedEditorPage(
      textController: textController,
      onInsertContact: onInsertContact,
      maxLength: maxLength,
    ),
    opaque: false,
    fullscreenDialog: true,
    transition: Transition.fadeIn,
    duration: const Duration(milliseconds: 160),
  );
}

class _AgentIntroductionExpandedEditorPage extends StatefulWidget {
  const _AgentIntroductionExpandedEditorPage({
    required this.textController,
    required this.onInsertContact,
    this.maxLength,
  });

  final TextEditingController textController;
  final Future<void> Function(BuildContext context) onInsertContact;
  final int? maxLength;

  @override
  State<_AgentIntroductionExpandedEditorPage> createState() =>
      _AgentIntroductionExpandedEditorPageState();
}

class _AgentIntroductionExpandedEditorPageState
    extends State<_AgentIntroductionExpandedEditorPage> {
  static const double _widePanelBreakpoint = 700;

  final FocusNode _focusNode = FocusNode();

  @override
  void dispose() {
    _focusNode.dispose();
    super.dispose();
  }

  void _collapse() {
    Get.back();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isWide = MediaQuery.sizeOf(context).width >= _widePanelBreakpoint;

    final panel = Container(
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: isWide ? BorderRadius.circular(16) : BorderRadius.zero,
        boxShadow: isWide
            ? [
                BoxShadow(
                  color: Colors.black.withValues(alpha: 0.2),
                  blurRadius: 24,
                  offset: const Offset(0, 8),
                ),
              ]
            : null,
      ),
      clipBehavior: isWide ? Clip.antiAlias : Clip.none,
      child: SafeArea(
        child: TextFieldTapRegion(
          child: Column(
            children: [
              Padding(
                padding: const EdgeInsetsDirectional.only(start: 16, end: 4),
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        'ai_agent_introduction_expanded_title'.tr,
                        style: TextStyle(
                          fontSize: 15,
                          fontWeight: FontWeight.w600,
                          color: theme.colorScheme.onSurface,
                        ),
                      ),
                    ),
                    IconButton(
                      key: const Key('agent_introduction_collapse_button'),
                      tooltip: 'chat_input_collapse'.tr,
                      icon: Icon(
                        Icons.close_fullscreen_rounded,
                        size: 20,
                        color: theme.colorScheme.secondary.withValues(
                          alpha: 0.8,
                        ),
                      ),
                      onPressed: _collapse,
                    ),
                  ],
                ),
              ),
              Divider(
                height: 1,
                thickness: 0.5,
                color: theme.dividerColor.withValues(alpha: 0.4),
              ),
              Align(
                alignment: Alignment.centerLeft,
                child: Padding(
                  padding: const EdgeInsetsDirectional.only(
                    start: 8,
                    end: 8,
                    top: 4,
                  ),
                  child: TextButton.icon(
                    key: const Key('agent_insert_id_button_expanded'),
                    onPressed: () => widget.onInsertContact(context),
                    icon: const Icon(Icons.person_add_alt_1, size: 18),
                    label: Text('ai_agent_insert_id'.tr),
                  ),
                ),
              ),
              Expanded(
                child: TextField(
                  key: const Key('agent_introduction_expanded_field'),
                  controller: widget.textController,
                  focusNode: _focusNode,
                  autofocus: true,
                  maxLines: null,
                  expands: true,
                  textAlignVertical: TextAlignVertical.top,
                  keyboardType: TextInputType.multiline,
                  textCapitalization: TextCapitalization.sentences,
                  autofillHints: const <String>[],
                  inputFormatters: [
                    if (widget.maxLength != null)
                      LengthLimitingTextInputFormatter(widget.maxLength),
                  ],
                  decoration: InputDecoration(
                    hintText: 'ai_agent_introduction_hint'.tr,
                    hintStyle: TextStyle(
                      color: theme.colorScheme.secondary.withValues(alpha: 0.4),
                      fontSize: 15,
                    ),
                    filled: false,
                    border: InputBorder.none,
                    contentPadding: const EdgeInsets.symmetric(
                      horizontal: 16,
                      vertical: 12,
                    ),
                  ),
                  style: const TextStyle(fontSize: 15, height: 1.5),
                  textInputAction: TextInputAction.newline,
                ),
              ),
            ],
          ),
        ),
      ),
    );

    return Scaffold(
      backgroundColor: isWide
          ? Colors.black.withValues(alpha: 0.35)
          : theme.colorScheme.surface,
      body: isWide
          ? Stack(
              children: [
                Positioned.fill(
                  child: GestureDetector(
                    behavior: HitTestBehavior.opaque,
                    onTap: _collapse,
                    child: const SizedBox.expand(),
                  ),
                ),
                Center(
                  child: ConstrainedBox(
                    constraints: BoxConstraints(
                      maxWidth: 720,
                      maxHeight: MediaQuery.sizeOf(context).height * 0.8,
                    ),
                    child: panel,
                  ),
                ),
              ],
            )
          : panel,
    );
  }
}
