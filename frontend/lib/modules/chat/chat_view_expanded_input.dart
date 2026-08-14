part of 'chat_view.dart';

/// 打开输入框全屏编辑器。
///
/// 与底部小输入框共用同一份 [ChatController.inputController]，文字实时同步；
/// 焦点节点独立（Flutter 不允许两个输入框共用 FocusNode）。
Future<void> openChatExpandedInputEditor(
  ChatController controller, {
  required double fontScale,
}) async {
  if (controller.isExpandedInputEditorOpen) {
    return;
  }
  controller.isExpandedInputEditorOpen = true;
  try {
    await Get.to<void>(
      () => _ChatExpandedInputEditorPage(
        controller: controller,
        fontScale: fontScale,
      ),
      opaque: false,
      fullscreenDialog: true,
      transition: Transition.fadeIn,
      duration: const Duration(milliseconds: 160),
    );
  } finally {
    controller.isExpandedInputEditorOpen = false;
  }
}

class _ChatExpandedInputEditorPage extends StatefulWidget {
  const _ChatExpandedInputEditorPage({
    required this.controller,
    required this.fontScale,
  });

  final ChatController controller;
  final double fontScale;

  @override
  State<_ChatExpandedInputEditorPage> createState() =>
      _ChatExpandedInputEditorPageState();
}

class _ChatExpandedInputEditorPageState
    extends State<_ChatExpandedInputEditorPage> {
  static const double _widePanelBreakpoint = 700;

  final FocusNode _focusNode = FocusNode();

  @override
  void initState() {
    super.initState();
    widget.controller.expandedInputFocusNodeOverride = _focusNode;
  }

  @override
  void dispose() {
    if (identical(
      widget.controller.expandedInputFocusNodeOverride,
      _focusNode,
    )) {
      widget.controller.expandedInputFocusNodeOverride = null;
    }
    _focusNode.dispose();
    super.dispose();
  }

  void _collapse() {
    Get.back();
  }

  void _send() {
    final sent = widget.controller.dispatchCurrentInputMessage();
    if (sent) {
      Get.back();
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final fontScale = widget.fontScale;
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
                        'chat_input_expanded_title'.tr,
                        style: TextStyle(
                          fontSize: 15 * fontScale,
                          fontWeight: FontWeight.w600,
                          color: theme.colorScheme.onSurface,
                        ),
                      ),
                    ),
                    IconButton(
                      key: const Key('chat_input_collapse_button'),
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
              buildPinnedMentionBar(
                widget.controller,
                context,
                fontScale: fontScale,
              ),
              Expanded(
                child: Focus(
                  canRequestFocus: false,
                  onKeyEvent: (node, event) {
                    if (event is KeyDownEvent) {
                      final isEnterKey =
                          event.logicalKey == LogicalKeyboardKey.enter ||
                          event.logicalKey == LogicalKeyboardKey.numpadEnter;
                      if (isEnterKey &&
                          (HardwareKeyboard.instance.isMetaPressed ||
                              HardwareKeyboard.instance.isControlPressed) &&
                          !widget.controller.isInputComposing) {
                        _send();
                        return KeyEventResult.handled;
                      }
                    }
                    return KeyEventResult.ignored;
                  },
                  child: Obx(() {
                    final uploading = widget.controller.isUploadingImage.value;
                    return TextField(
                      key: const Key('chat_expanded_input_field'),
                      controller: widget.controller.inputController,
                      focusNode: _focusNode,
                      autofocus: true,
                      contextMenuBuilder: _buildLocalizedInputContextMenu,
                      readOnly: uploading,
                      maxLines: null,
                      expands: true,
                      textAlignVertical: TextAlignVertical.top,
                      keyboardType: TextInputType.multiline,
                      textCapitalization: TextCapitalization.sentences,
                      autofillHints: const <String>[],
                      decoration: InputDecoration(
                        hintText: 'chat_send_placeholder'.tr,
                        hintStyle: TextStyle(
                          color: theme.colorScheme.secondary.withValues(
                            alpha: 0.4,
                          ),
                          fontSize: 15 * fontScale,
                        ),
                        filled: false,
                        border: InputBorder.none,
                        contentPadding: const EdgeInsets.symmetric(
                          horizontal: 16,
                          vertical: 12,
                        ),
                      ),
                      style: TextStyle(fontSize: 15 * fontScale, height: 1.5),
                      textInputAction: TextInputAction.newline,
                    );
                  }),
                ),
              ),
              buildChatMentionList(
                widget.controller,
                context,
                fontScale: fontScale,
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.end,
                  children: [
                    Obx(() {
                      final uploading =
                          widget.controller.isUploadingImage.value;
                      final overLimit =
                          widget.controller.isInputOverLengthLimit.value;
                      final disabled = uploading || overLimit;
                      return Material(
                        color: disabled
                            ? theme.colorScheme.surfaceContainerHighest
                            : theme.primaryColor,
                        borderRadius: BorderRadius.circular(20),
                        child: InkWell(
                          key: const Key('chat_expanded_input_send_button'),
                          onTap: disabled ? null : _send,
                          borderRadius: BorderRadius.circular(20),
                          child: Container(
                            width: 40,
                            height: 40,
                            alignment: Alignment.center,
                            child: uploading
                                ? SizedBox(
                                    width: 20,
                                    height: 20,
                                    child: CircularProgressIndicator(
                                      strokeWidth: 2,
                                      color: theme.colorScheme.onSurface
                                          .withValues(alpha: 0.5),
                                    ),
                                  )
                                : const Icon(
                                    Icons.arrow_upward_rounded,
                                    color: Colors.white,
                                    size: 22,
                                  ),
                          ),
                        ),
                      );
                    }),
                  ],
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
