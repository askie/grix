part of 'chat_view.dart';

/// 打开某个 agent 的远程目录选择器，返回选中的目录绝对路径；取消返回 null。
/// 消息气泡内的绑定卡片与空白页快捷绑定组件共用此入口。
Future<String?> pickChatAgentRemoteDirectory(
  ChatController controller, {
  required String agentId,
}) async {
  final ctx = Get.context;
  if (ctx == null) return null;
  final sessionId = controller.sessionId;
  final imService = controller.imService;
  Future<RemoteFileListResult> listProvider(
    String? parentId,
    RemoteFileListQuery query,
  ) async {
    final resp = await imService.requestAgentFileList(
      agentId: agentId,
      sessionId: sessionId,
      parentId: parentId,
      showHidden: query.showHidden,
      allowedExtensions: query.allowedExtensions,
    );
    final nodes = resp.files.map((m) => mapAgentRemoteFileNode(m)).toList();
    return RemoteFileListResult(
      files: nodes,
      currentPath: resp.currentPath,
      machineName: resp.machineName,
    );
  }

  Future<RemoteFileNode> createFolderProvider(
    String? parentId,
    String name,
  ) async {
    final folder = await imService.requestAgentCreateFolder(
      agentId: agentId,
      sessionId: sessionId,
      parentId: parentId,
      name: name,
    );
    return RemoteFileNode(
      id: folder['id']?.toString() ?? '',
      name: folder['name']?.toString() ?? '',
      isDirectory: true,
    );
  }

  final result = await RemoteFilePicker.show(
    ctx,
    listProvider: listProvider,
    createFolderProvider: createFolderProvider,
    favoriteApi: UserFavoritePathService(),
    pickTarget: RemoteFilePickTarget.directories,
    selectionMode: RemoteFileSelectionMode.single,
    // 记忆路径按 agent 区分，避免不同机器的 agent 共用同一 key 串台
    // （详见附件入口同款注释）。
    storageKey: 'remote_file_picker_last_path_output_dir_$agentId',
  );
  if (result == null || result.selectedFiles.isEmpty) return null;
  final selectedPath = result.selectedFiles.first.id.trim();
  if (selectedPath.isEmpty) return null;
  return selectedPath;
}

/// 快捷绑定组件的揭示延迟：进会话后等这段时间再显示，避开初次消息加载窗口
/// （ChatController._initialMessageLoadDelay 约 330ms），防止空态误判导致的闪现。
/// 取略大于加载延迟的值，留出消息填充缓冲。
const Duration _quickBindRevealDelay = Duration(milliseconds: 500);

Widget buildChatEmptyState(
  ChatController controller,
  BuildContext context, {
  required double fontScale,
}) {
  final theme = Theme.of(context);
  final quickBindAgentId = controller.directoryBoundAgentId;
  return Center(
    child: SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 80,
            height: 80,
            decoration: BoxDecoration(
              color: theme.primaryColor.withValues(alpha: 0.08),
              shape: BoxShape.circle,
            ),
            child: Icon(
              Icons.chat_bubble_outline_rounded,
              size: 36,
              color: theme.primaryColor.withValues(alpha: 0.4),
            ),
          ),
          const SizedBox(height: 16),
          Text(
            'chat_empty'.tr,
            style: TextStyle(
              fontSize: 15 * fontScale,
              fontWeight: FontWeight.w600,
              color: theme.colorScheme.onSurface.withValues(alpha: 0.5),
            ),
          ),
          const SizedBox(height: 6),
          Text(
            'chat_empty_hint'.tr,
            style: TextStyle(
              fontSize: 13 * fontScale,
              color: theme.colorScheme.secondary.withValues(alpha: 0.5),
            ),
          ),
          if (quickBindAgentId.isNotEmpty) ...[
            const SizedBox(height: 20),
            ChatQuickBindDirectoryPanel(
              key: ValueKey('chat_quick_bind_$quickBindAgentId'),
              fontScale: fontScale,
              revealDelay: _quickBindRevealDelay,
              entriesLoader: () =>
                  ChatController.recentBindDirectoryStore.listForAgent(
                    quickBindAgentId,
                    hostname: controller.agentHostnameOf(quickBindAgentId),
                  ),
              onBindDirectory: controller.sendQuickBindDirectory,
              onPickDirectory: () => pickChatAgentRemoteDirectory(
                controller,
                agentId: quickBindAgentId,
              ),
            ),
          ],
        ],
      ),
    ),
  );
}

Widget buildChatMentionList(
  ChatController controller,
  BuildContext context, {
  required double fontScale,
}) {
  return Obx(() {
    _ChatViewDebugBuildCounter.hit('mention_list_outer_obx');
    if (!controller.showMentionList.value) return const SizedBox.shrink();

    final members = controller.filteredMentionList;
    if (members.isEmpty) return const SizedBox.shrink();
    final mentionRowHeight = (48.0 * fontScale).clamp(44.0, 56.0).toDouble();
    final currentUserId = controller.authService.userId?.trim() ?? '';
    final theme = Theme.of(context);
    final maxHeight = MediaQuery.sizeOf(context).height * 0.3;
    final listHeight = (mentionRowHeight * members.length)
        .clamp(mentionRowHeight, maxHeight)
        .toDouble();

    return Container(
      key: const Key('chat_mention_list_container'),
      constraints: BoxConstraints(maxHeight: maxHeight),
      margin: const EdgeInsets.symmetric(horizontal: 8),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(12),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.1),
            blurRadius: 10,
            offset: const Offset(0, -4),
          ),
        ],
      ),
      child: SizedBox(
        height: listHeight,
        child: ListView.builder(
          key: const Key('chat_mention_list_scrollable'),
          primary: false,
          padding: EdgeInsets.zero,
          itemCount: members.length,
          itemExtent: mentionRowHeight,
          itemBuilder: (context, index) {
            final member = members[index];
            final memberType = (member['member_type'] ?? 1) as int;
            final builtinKind = (member['builtin_kind'] ?? '').toString();
            final memberId = (member['member_id'] ?? '').toString().trim();
            final isBuiltinMentionAll = builtinKind == 'mention_all';
            return Obx(() {
              _ChatViewDebugBuildCounter.hit('mention_list_row_obx');
              if (!isBuiltinMentionAll && memberId.isNotEmpty) {
                controller.senderProfileVersionFor(
                  senderId: memberId,
                  senderType: memberType,
                  isMine: memberType == 1 && memberId == currentUserId,
                );
              }
              final displayName = controller.resolveGroupMemberDisplayName(
                member,
              );
              final isSelected = controller.mentionSelectedIndex.value == index;
              return ListTile(
                minTileHeight: mentionRowHeight,
                selected: isSelected,
                selectedTileColor: theme.colorScheme.primary.withValues(
                  alpha: 0.15,
                ),
                selectedColor: theme.colorScheme.primary,
                contentPadding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 0,
                ),
                visualDensity: VisualDensity.compact,
                leading: Icon(
                  isBuiltinMentionAll
                      ? Icons.alternate_email_rounded
                      : memberType == 2
                      ? Icons.smart_toy_rounded
                      : Icons.person_rounded,
                  size: 20,
                  color: isSelected
                      ? theme.colorScheme.primary
                      : theme.colorScheme.secondary,
                ),
                title: Text(
                  displayName,
                  style: TextStyle(
                    fontSize: 14 * fontScale,
                    fontWeight: isSelected ? FontWeight.w600 : FontWeight.w500,
                  ),
                ),
                trailing: Obx(() {
                  _ChatViewDebugBuildCounter.hit('mention_list_row_pin_obx');
                  final pinned = controller.isPinnedMention(memberId);
                  return IconButton(
                    key: Key('chat_mention_pin_${memberId}_$pinned'),
                    tooltip: pinned ? '取消固定' : '固定此成员',
                    visualDensity: VisualDensity.compact,
                    iconSize: 20,
                    icon: Icon(
                      pinned
                          ? Icons.push_pin_rounded
                          : Icons.push_pin_outlined,
                      color: pinned
                          ? theme.colorScheme.primary
                          : theme.colorScheme.secondary.withValues(alpha: 0.6),
                    ),
                    onPressed: () => controller.togglePinnedMention(member),
                  );
                }),
                onTap: () => controller.insertMention(member),
              );
            });
          },
        ),
      ),
    );
  });
}

Widget buildPinnedMentionBar(
  ChatController controller,
  BuildContext context, {
  required double fontScale,
}) {
  return Obx(() {
    _ChatViewDebugBuildCounter.hit('pinned_mention_bar_obx');
    final pinned = controller.pinnedMentions;
    if (pinned.isEmpty) return const SizedBox.shrink();
    final theme = Theme.of(context);
    return Container(
      key: const Key('chat_pinned_mention_bar'),
      margin: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 3),
      // 横向 ListView：人数多时可左右滑动；背景透明，沿用外层输入区底色。
      child: SizedBox(
        height: 26 * fontScale,
        child: ListView.separated(
          key: const Key('chat_pinned_mention_bar_scrollable'),
          scrollDirection: Axis.horizontal,
          primary: false,
          physics: const BouncingScrollPhysics(),
          itemCount: pinned.length,
          separatorBuilder: (_, __) => const SizedBox(width: 4),
          itemBuilder: (context, index) {
            final mention = pinned[index];
            return _PinnedMentionChip(
              key: Key('chat_pinned_${mention.memberId}'),
              controller: controller,
              mention: mention,
              theme: theme,
              fontScale: fontScale,
            );
          },
        ),
      ),
    );
  });
}

class _PinnedMentionChip extends StatelessWidget {
  const _PinnedMentionChip({
    super.key,
    required this.controller,
    required this.mention,
    required this.theme,
    required this.fontScale,
  });

  final ChatController controller;
  final PinnedMention mention;
  final ThemeData theme;
  final double fontScale;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: theme.colorScheme.primary.withValues(alpha: 0.12),
      borderRadius: BorderRadius.circular(13),
      child: InkWell(
        key: const Key('chat_pinned_mention_chip'),
        borderRadius: BorderRadius.circular(13),
        onTap: () => controller.removePinnedMention(mention.memberId),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 8),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                Icons.alternate_email_rounded,
                size: 12,
                color: theme.colorScheme.primary,
              ),
              const SizedBox(width: 3),
              Text(
                mention.displayName,
                style: TextStyle(
                  fontSize: 11 * fontScale,
                  fontWeight: FontWeight.w500,
                  color: theme.colorScheme.primary,
                  height: 1.1,
                ),
              ),
              const SizedBox(width: 2),
              Icon(
                Icons.close_rounded,
                size: 12,
                color: theme.colorScheme.primary.withValues(alpha: 0.7),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

Widget buildVisibleToPickerList(
  ChatController controller,
  BuildContext context, {
  required double fontScale,
}) {
  return Obx(() {
    _ChatViewDebugBuildCounter.hit('visible_to_picker_outer_obx');
    if (!controller.showVisibleToPicker.value) return const SizedBox.shrink();
    if (!controller.isGroupChat) return const SizedBox.shrink();

    final members = controller.groupMembers;
    final currentUserId = controller.authService.userId;
    final theme = Theme.of(context);
    final mentionRowHeight = (48.0 * fontScale).clamp(44.0, 56.0).toDouble();
    final maxHeight = MediaQuery.sizeOf(context).height * 0.35;

    // Filter out current user
    final selectableMembers = members
        .where((m) => m['member_id']?.toString() != currentUserId)
        .toList();

    return Container(
      key: const Key('visible_to_picker_container'),
      constraints: BoxConstraints(maxHeight: maxHeight),
      margin: const EdgeInsets.symmetric(horizontal: 8),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(12),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withValues(alpha: 0.1),
            blurRadius: 10,
            offset: const Offset(0, -4),
          ),
        ],
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          // Header row
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(
                  color: theme.colorScheme.secondary.withValues(alpha: 0.1),
                ),
              ),
            ),
            child: Row(
              children: [
                Icon(
                  Icons.lock_outline,
                  size: 16,
                  color: theme.colorScheme.secondary.withValues(alpha: 0.6),
                ),
                const SizedBox(width: 6),
                Text(
                  '仅谁可见',
                  style: TextStyle(
                    fontSize: 13 * fontScale,
                    fontWeight: FontWeight.w600,
                    color: theme.colorScheme.secondary.withValues(alpha: 0.8),
                  ),
                ),
                Obx(() {
                  final selectedCount = controller.visibleToUserIds.length;
                  if (selectedCount <= 0) {
                    return const SizedBox.shrink();
                  }
                  return Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const SizedBox(width: 6),
                      Container(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 6,
                          vertical: 1,
                        ),
                        decoration: BoxDecoration(
                          color: theme.colorScheme.primary.withValues(
                            alpha: 0.12,
                          ),
                          borderRadius: BorderRadius.circular(8),
                        ),
                        child: Text(
                          '$selectedCount',
                          style: TextStyle(
                            fontSize: 11 * fontScale,
                            fontWeight: FontWeight.w600,
                            color: theme.colorScheme.primary,
                          ),
                        ),
                      ),
                    ],
                  );
                }),
                const Spacer(),
                GestureDetector(
                  onTap: () => controller.showVisibleToPicker.value = false,
                  child: Icon(
                    Icons.expand_more_rounded,
                    size: 20,
                    color: theme.colorScheme.secondary.withValues(alpha: 0.5),
                  ),
                ),
              ],
            ),
          ),
          // Member list
          Flexible(
            child: ListView.builder(
              primary: false,
              shrinkWrap: true,
              padding: EdgeInsets.zero,
              itemCount: selectableMembers.length,
              itemExtent: mentionRowHeight,
              itemBuilder: (context, index) {
                final member = selectableMembers[index];
                final memberId = member['member_id']?.toString().trim() ?? '';
                final memberType = (member['member_type'] ?? 1) as int;
                return Obx(() {
                  _ChatViewDebugBuildCounter.hit('visible_to_picker_row_obx');
                  if (memberId.isNotEmpty) {
                    controller.senderProfileVersionFor(
                      senderId: memberId,
                      senderType: memberType,
                      isMine: memberType == 1 && memberId == currentUserId,
                    );
                  }
                  final displayName = controller.resolveGroupMemberDisplayName(
                    member,
                  );
                  final isSelected = controller.isMemberSelectedForVisibleTo(
                    memberId,
                  );
                  return ListTile(
                    minTileHeight: mentionRowHeight,
                    contentPadding: const EdgeInsets.symmetric(horizontal: 16),
                    visualDensity: VisualDensity.compact,
                    leading: Icon(
                      memberType == 2
                          ? Icons.smart_toy_rounded
                          : Icons.person_rounded,
                      size: 20,
                      color: isSelected
                          ? theme.colorScheme.primary
                          : theme.colorScheme.secondary,
                    ),
                    title: Text(
                      displayName,
                      style: TextStyle(
                        fontSize: 14 * fontScale,
                        fontWeight: isSelected
                            ? FontWeight.w600
                            : FontWeight.w500,
                      ),
                    ),
                    trailing: Icon(
                      isSelected
                          ? Icons.check_circle_rounded
                          : Icons.circle_outlined,
                      size: 20,
                      color: isSelected
                          ? theme.colorScheme.primary
                          : theme.colorScheme.secondary.withValues(alpha: 0.4),
                    ),
                    onTap: () => controller.toggleVisibleToMember(memberId),
                  );
                });
              },
            ),
          ),
        ],
      ),
    );
  });
}

Widget buildVisibleToIndicatorBar(
  ChatController controller,
  BuildContext context, {
  required double fontScale,
}) {
  return Obx(() {
    if (controller.visibleToUserIds.isEmpty) return const SizedBox.shrink();
    if (controller.showVisibleToPicker.value) return const SizedBox.shrink();

    final theme = Theme.of(context);
    final summary = controller.visibleToDisplaySummary;

    return Container(
      key: const Key('visible_to_indicator_bar'),
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        border: Border(
          top: BorderSide(
            color: theme.colorScheme.secondary.withValues(alpha: 0.1),
          ),
        ),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          Icon(
            Icons.lock_outline,
            size: 20,
            color: theme.colorScheme.secondary.withValues(alpha: 0.6),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              '仅 $summary 可见',
              style: TextStyle(
                fontSize: 12 * fontScale,
                fontWeight: FontWeight.w600,
                color: theme.colorScheme.secondary.withValues(alpha: 0.9),
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          IconButton(
            icon: Icon(
              Icons.close_rounded,
              size: 18,
              color: theme.colorScheme.secondary.withValues(alpha: 0.6),
            ),
            onPressed: controller.clearVisibleTo,
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(),
          ),
        ],
      ),
    );
  });
}

/// 「正在编辑排队任务」提示条（仿撤回重编辑条）：编辑态时显示在输入框上方，
/// 点 × 发 hold:false 退出编辑并还原草稿。
Widget buildChatQueueEditingBanner(
  ChatController controller,
  BuildContext context, {
  required double fontScale,
}) {
  return Obx(() {
    if (controller.editingQueueTaskEventId.value.isEmpty) {
      return const SizedBox.shrink();
    }

    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        border: Border(
          top: BorderSide(
            color: theme.colorScheme.secondary.withValues(alpha: 0.1),
          ),
        ),
      ),
      child: Row(
        children: [
          Icon(
            Icons.edit_note_rounded,
            size: 16,
            color: theme.colorScheme.primary.withValues(alpha: 0.7),
          ),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              'chat_editing_queue_task_hint'.tr,
              style: TextStyle(
                fontSize: 13 * fontScale,
                color: theme.colorScheme.secondary.withValues(alpha: 0.6),
              ),
            ),
          ),
          GestureDetector(
            onTap: controller.cancelQueueTaskEdit,
            child: Icon(
              Icons.close,
              size: 16,
              color: theme.colorScheme.secondary.withValues(alpha: 0.4),
            ),
          ),
        ],
      ),
    );
  });
}

Widget buildChatRevokedMessageBanner(
  ChatController controller,
  BuildContext context, {
  required double fontScale,
}) {
  return Obx(() {
    final content = controller.revokedMessageContent.value;
    if (content.isEmpty) return const SizedBox.shrink();

    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        border: Border(
          top: BorderSide(
            color: theme.colorScheme.secondary.withValues(alpha: 0.1),
          ),
        ),
      ),
      child: Row(
        children: [
          Icon(
            Icons.undo_rounded,
            size: 16,
            color: theme.colorScheme.secondary.withValues(alpha: 0.5),
          ),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              'chat_revoked_message_hint'.tr,
              style: TextStyle(
                fontSize: 13 * fontScale,
                color: theme.colorScheme.secondary.withValues(alpha: 0.6),
              ),
            ),
          ),
          GestureDetector(
            onTap: controller.restoreRevokedMessage,
            child: Text(
              'chat_revoked_message_reedit'.tr,
              style: TextStyle(
                fontSize: 13 * fontScale,
                color: theme.colorScheme.primary,
                fontWeight: FontWeight.w500,
              ),
            ),
          ),
          const SizedBox(width: 8),
          GestureDetector(
            onTap: controller.clearRevokedMessageContent,
            child: Icon(
              Icons.close,
              size: 16,
              color: theme.colorScheme.secondary.withValues(alpha: 0.4),
            ),
          ),
        ],
      ),
    );
  });
}

Widget buildChatReplyPreviewBlock(
  ChatController controller,
  BuildContext context, {
  required double fontScale,
}) {
  return Obx(() {
    final replyMsg = controller.replyingToMessage.value;
    if (replyMsg == null) return const SizedBox.shrink();

    final theme = Theme.of(context);
    final isMine = ChatMessageOwnerClassifier.isMineMessage(
      replyMsg,
      currentUserId: controller.authService.userId,
    );
    controller.senderProfileVersionFor(
      senderId: replyMsg.senderId,
      senderType: replyMsg.senderType,
      isMine: isMine,
    );
    final senderName = controller.resolveSenderName(
      senderId: replyMsg.senderId,
      isMine: isMine,
      isGroup: controller.isGroupChat,
      senderType: replyMsg.senderType,
    );

    return Container(
      key: const Key('chat_reply_preview_container'),
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        border: Border(
          top: BorderSide(
            color: theme.colorScheme.secondary.withValues(alpha: 0.1),
          ),
        ),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          Icon(
            Icons.reply_rounded,
            size: 20,
            color: theme.colorScheme.secondary.withValues(alpha: 0.6),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'chat_replying_to'.trParams({'name': senderName}),
                  style: TextStyle(
                    fontSize: 12 * fontScale,
                    fontWeight: FontWeight.w600,
                    color: theme.colorScheme.secondary.withValues(alpha: 0.9),
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
                const SizedBox(height: 2),
                Text(
                  ChatMessagePreview.summarize(
                    controller.formatMessageContentForDisplay(replyMsg.content),
                  ),
                  style: TextStyle(
                    fontSize: 12 * fontScale,
                    color: theme.colorScheme.secondary.withValues(alpha: 0.6),
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
          ),
          IconButton(
            icon: Icon(
              Icons.close_rounded,
              size: 18,
              color: theme.colorScheme.secondary.withValues(alpha: 0.6),
            ),
            onPressed: controller.cancelReply,
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(),
          ),
        ],
      ),
    );
  });
}

Widget buildChatInputArea(
  ChatController controller,
  BuildContext context, {
  required double fontScale,
  required VoidCallback onDismissKeyboard,
}) {
  final theme = Theme.of(context);
  // toolbar 独立于输入区域的 Obx，避免键盘/输入状态变化触发 toolbar 重建
  return Column(
    mainAxisSize: MainAxisSize.min,
    children: [
      _ChatAgentToolbarDock(controller: controller, fontScale: fontScale),
      _buildChatInputAreaBody(
        controller: controller,
        context: context,
        theme: theme,
        fontScale: fontScale,
        onDismissKeyboard: onDismissKeyboard,
      ),
    ],
  );
}

Widget _buildChatInputAreaBody({
  required ChatController controller,
  required BuildContext context,
  required ThemeData theme,
  required double fontScale,
  required VoidCallback onDismissKeyboard,
}) {
  return Obx(() {
    final viewPaddingBottom = MediaQuery.viewPaddingOf(context).bottom;
    final systemGestureInsetBottom = MediaQuery.systemGestureInsetsOf(
      context,
    ).bottom;
    final liveKeyboardInsetBottom = MediaQuery.viewInsetsOf(context).bottom;
    final effectiveLiveKeyboardInsetBottom =
        controller.shouldFollowKeyboardForInputDock
        ? liveKeyboardInsetBottom
        : 0.0;
    final bottomInsetResolution = ChatInputBottomInsetResolver.resolve(
      viewPaddingBottom: viewPaddingBottom,
      systemGestureInsetBottom: systemGestureInsetBottom,
      liveKeyboardInsetBottom: effectiveLiveKeyboardInsetBottom,
      retainedKeyboardInsetBottom: controller.inputLayoutKeyboardInsetBottom,
      platformViewportObstructionBottom:
          controller.platformViewportObstructionBottom,
      minBottomSpacing: 8,
    );

    return Padding(
      padding: EdgeInsets.only(bottom: bottomInsetResolution.keyboardInset),
      child: Container(
        key: const Key('chat_input_area_container'),
        padding: EdgeInsets.only(
          left: 8,
          right: 8,
          top: 8,
          bottom: bottomInsetResolution.inputBottomInset,
        ),
        decoration: BoxDecoration(
          color: theme.colorScheme.surface,
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: 0.04),
              blurRadius: 8,
              offset: const Offset(0, -2),
            ),
          ],
        ),
        child: TextFieldTapRegion(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Obx(() {
                final staged = controller.stagedAttachments;
                if (staged.isEmpty) {
                  return const SizedBox.shrink();
                }
                return _StagedAttachmentPreviewStrip(
                  attachments: staged.toList(),
                  onRemove: controller.removeStagedAttachment,
                  onEditImage: controller.editStagedImage,
                );
              }),
              Row(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  Obx(() {
                    final isUploading = controller.isUploadingImage.value;
                    return IconButton(
                      key: const Key('chat_attachment_menu_toggle_button'),
                      icon: isUploading
                          ? SizedBox(
                              width: 20,
                              height: 20,
                              child: CircularProgressIndicator(
                                strokeWidth: 2,
                                color: theme.colorScheme.secondary.withValues(
                                  alpha: 0.6,
                                ),
                              ),
                            )
                          : Icon(
                              Icons.add_rounded,
                              color: theme.colorScheme.secondary.withValues(
                                alpha: 0.72,
                              ),
                              size: 28,
                            ),
                      onPressed: isUploading
                          ? null
                          : controller.toggleAttachmentMenu,
                    );
                  }),

                  Expanded(
                    child: Stack(
                      children: [
                        Container(
                          constraints: const BoxConstraints(maxHeight: 120),
                          child: ScrollConfiguration(
                            behavior: const _InputFieldScrollBehavior(),
                            child: Focus(
                              canRequestFocus: false,
                              onKeyEvent: (node, event) {
                                controller.updateKeyboardModifierState(event);

                                if (event is KeyDownEvent) {
                                  // 桌面端 & iOS 外接键盘粘贴快捷键拦截
                                  if (!kIsWeb &&
                                      event.logicalKey ==
                                          LogicalKeyboardKey.keyV &&
                                      (defaultTargetPlatform ==
                                                  TargetPlatform.macOS ||
                                              defaultTargetPlatform ==
                                                  TargetPlatform.iOS
                                          ? HardwareKeyboard
                                                .instance
                                                .isMetaPressed
                                          : HardwareKeyboard
                                                .instance
                                                .isControlPressed)) {
                                    controller.handleDesktopPaste();
                                    return KeyEventResult.handled;
                                  }

                                  final isEnterKey =
                                      event.logicalKey ==
                                          LogicalKeyboardKey.enter ||
                                      event.logicalKey ==
                                          LogicalKeyboardKey.numpadEnter;
                                  final isArrowUp =
                                      event.logicalKey ==
                                      LogicalKeyboardKey.arrowUp;
                                  final isArrowDown =
                                      event.logicalKey ==
                                      LogicalKeyboardKey.arrowDown;
                                  if (!isEnterKey &&
                                      !isArrowUp &&
                                      !isArrowDown) {
                                    return KeyEventResult.ignored;
                                  }
                                  controller
                                      .clearPendingInputSubmitSuppressionForNewKeyPress();

                                  if (isEnterKey &&
                                      controller.isInputComposing) {
                                    controller.suppressNextInputSubmit();
                                    return KeyEventResult.ignored;
                                  }

                                  if (controller.showMentionList.value &&
                                      controller
                                          .filteredMentionList
                                          .isNotEmpty) {
                                    if (isArrowUp) {
                                      controller.mentionMoveUp();
                                      return KeyEventResult.handled;
                                    } else if (isArrowDown) {
                                      controller.mentionMoveDown();
                                      return KeyEventResult.handled;
                                    } else if (isEnterKey) {
                                      if (controller.mentionSelectCurrent()) {
                                        controller.suppressNextInputSubmit();
                                        return KeyEventResult.handled;
                                      }
                                    }
                                  }

                                  if (isEnterKey) {
                                    if (controller.isKeyboardModifierHeld) {
                                      controller
                                          .submitMessageFromHardwareEnter();
                                      return KeyEventResult.handled;
                                    }
                                    controller.suppressNextInputSubmit();
                                    controller.insertInputLineBreak();
                                    return KeyEventResult.handled;
                                  }
                                }
                                return KeyEventResult.ignored;
                              },
                              child: Obx(() {
                                final uploading =
                                    controller.isUploadingImage.value;
                                return TextField(
                                  controller: controller.inputController,
                                  focusNode: controller.focusNode,
                                  contextMenuBuilder:
                                      _buildLocalizedInputContextMenu,
                                  readOnly: uploading,
                                  maxLines: null,
                                  keyboardType: TextInputType.multiline,
                                  textCapitalization:
                                      TextCapitalization.sentences,
                                  autofillHints: const <String>[],
                                  onEditingComplete: () {},
                                  onTapOutside: (_) => onDismissKeyboard(),
                                  decoration: InputDecoration(
                                    hintText: 'chat_send_placeholder'.tr,
                                    hintStyle: TextStyle(
                                      color: theme.colorScheme.secondary
                                          .withValues(alpha: 0.4),
                                      fontSize: 14 * fontScale,
                                    ),
                                    contentPadding: const EdgeInsets.symmetric(
                                      horizontal: 16,
                                      vertical: 10,
                                    ),
                                  ),
                                  style: TextStyle(fontSize: 14 * fontScale),
                                  onSubmitted: (_) =>
                                      controller.submitMessageFromInputAction(),
                                  textInputAction: TextInputAction.newline,
                                );
                              }),
                            ),
                          ),
                        ),
                        PositionedDirectional(
                          top: 2,
                          end: 2,
                          child: Obx(() {
                            if (!controller.showInputExpandButton.value) {
                              return const SizedBox.shrink();
                            }
                            return Material(
                              color: theme.colorScheme.surface.withValues(
                                alpha: 0.88,
                              ),
                              shape: const CircleBorder(),
                              child: InkWell(
                                key: const Key('chat_input_expand_button'),
                                customBorder: const CircleBorder(),
                                onTap: () => openChatExpandedInputEditor(
                                  controller,
                                  fontScale: fontScale,
                                ),
                                child: Tooltip(
                                  message: 'chat_input_expand'.tr,
                                  child: Padding(
                                    padding: const EdgeInsets.all(5),
                                    child: Icon(
                                      Icons.open_in_full_rounded,
                                      size: 14,
                                      color: theme.colorScheme.secondary
                                          .withValues(alpha: 0.7),
                                    ),
                                  ),
                                ),
                              ),
                            );
                          }),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 4),
                  Obx(() {
                    final uploading = controller.isUploadingImage.value;
                    final overLimit = controller.isInputOverLengthLimit.value;
                    final disabled = uploading || overLimit;
                    return Container(
                      margin: const EdgeInsets.only(bottom: 2),
                      child: Material(
                        color: disabled
                            ? theme.colorScheme.surfaceContainerHighest
                            : theme.primaryColor,
                        borderRadius: BorderRadius.circular(20),
                        child: InkWell(
                          onTap: disabled ? null : controller.sendMessage,
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
                      ),
                    );
                  }),
                ],
              ),
              Obx(() {
                final isUploading = controller.isUploadingImage.value;
                final isMenuOpen = controller.isAttachmentMenuOpen.value;
                return AnimatedSize(
                  duration: const Duration(milliseconds: 180),
                  curve: Curves.easeOut,
                  alignment: Alignment.topCenter,
                  child: isMenuOpen
                      ? ChatAttachmentMenu(
                          enabled: !isUploading,
                          showHideSendAction: controller.isGroupChat,
                          isHideSendActive:
                              controller.visibleToUserIds.isNotEmpty ||
                              controller.showVisibleToPicker.value,
                          onHideSendTap: () {
                            controller.closeAttachmentMenu();
                            controller.toggleVisibleToPicker();
                          },
                          onImageTap: () =>
                              handleChatImageAttachmentTap(controller, context),
                          onVideoTap: () =>
                              handleChatVideoAttachmentTap(controller, context),
                          onFileTap: controller.pickAndSendFile,
                          // 语音通话按钮：仅 providerType==4 的语音大模型 agent 显示
                          onVoiceCallTap:
                              controller.isAgentPrivateChat &&
                                  Get.find<FeatureFlagService>().isEnabled(
                                    'voice_call',
                                  )
                              ? (() {
                                  final session = controller.imService
                                      .findSessionById(controller.sessionId);
                                  final agentId = session?.peerId.trim() ?? '';
                                  final agent = agentId.isNotEmpty
                                      ? controller.agentService.agents
                                            .firstWhereOrNull(
                                              (a) => a.id == agentId,
                                            )
                                      : null;
                                  // agent 已知且确定不是语音大模型 → 不显示按钮
                                  if (agent != null &&
                                      agent.providerType != 4) {
                                    return null;
                                  }
                                  // 语音大模型 或 agent 信息尚未就绪 → 显示按钮
                                  return () {
                                    controller.closeAttachmentMenu();
                                    controller.startVoiceCallForCurrentAgent();
                                  };
                                })()
                              : null,
                          // 语音大脑按钮：仅与文字 agent(providerType!=4) 私聊显示，
                          // 与上面的语音通话按钮按 peer 类型互斥。
                          onVoiceBrainTap:
                              controller.isAgentPrivateChat &&
                                  Get.find<FeatureFlagService>().isEnabled(
                                    'voice_brain',
                                  )
                              ? (() {
                                  final session = controller.imService
                                      .findSessionById(controller.sessionId);
                                  final agentId = session?.peerId.trim() ?? '';
                                  final agent = agentId.isNotEmpty
                                      ? controller.agentService.agents
                                            .firstWhereOrNull(
                                              (a) => a.id == agentId,
                                            )
                                      : null;
                                  // 仅文字 agent（已知且非 type=4）显示语音大脑；
                                  // type=4 走老的语音通话按钮（互斥），信息未就绪也不显示。
                                  if (agent == null ||
                                      agent.providerType == 4) {
                                    return null;
                                  }
                                  return () {
                                    controller.closeAttachmentMenu();
                                    controller.startVoiceBrainCall();
                                  };
                                })()
                              : null,
                          onBrowseFilesTap:
                              controller.isAgentPrivateChat ||
                                  controller
                                      .groupToolbarTargetAgentId
                                      .isNotEmpty
                              ? controller.pickRemoteFiles
                              : null,
                        )
                      : const SizedBox.shrink(),
                );
              }),
            ],
          ),
        ),
      ),
    );
  });
}

Widget _buildLocalizedInputContextMenu(
  BuildContext context,
  EditableTextState editableTextState,
) {
  final locale = Get.locale ?? Localizations.maybeLocaleOf(context);

  // iOS: 替换 "粘贴" 按钮为使用缓存的剪贴板读取，避免每次弹权限窗
  List<ContextMenuButtonItem> buttons =
      editableTextState.contextMenuButtonItems;
  if (Platform.isIOS) {
    buttons = buttons.map((btn) {
      if (btn.type == ContextMenuButtonType.paste) {
        return ContextMenuButtonItem(
          type: ContextMenuButtonType.paste,
          label: btn.label,
          onPressed: () {
            _iosCachedPaste(editableTextState);
            ContextMenuController.removeAny();
          },
        );
      }
      return btn;
    }).toList();
  }

  final toolbar = AdaptiveTextSelectionToolbar.buttonItems(
    anchors: editableTextState.contextMenuAnchors,
    buttonItems: buttons,
  );
  if (locale == null) {
    return toolbar;
  }
  return Localizations.override(
    context: context,
    locale: locale,
    child: toolbar,
  );
}

/// iOS 专用：通过 NativeClipboardService 读取缓存的剪贴板文本并插入到输入框。
/// changeCount 未变化时复用缓存，不触发 iOS 系统粘贴权限弹窗。
void _iosCachedPaste(EditableTextState editableTextState) async {
  final text = await NativeClipboardService.getText();
  if (text == null || text.isEmpty) return;

  final controller = editableTextState.widget.controller;
  final currentValue = controller.value;
  final selection = currentValue.selection;
  final newText = selection.isValid
      ? currentValue.text.replaceRange(selection.start, selection.end, text)
      : currentValue.text + text;
  final newOffset = selection.isValid
      ? selection.start + text.length
      : currentValue.text.length + text.length;

  controller.value = TextEditingValue(
    text: newText,
    selection: TextSelection.collapsed(offset: newOffset),
  );
}

class _ChatAgentToolbarDock extends StatelessWidget {
  const _ChatAgentToolbarDock({
    required this.controller,
    required this.fontScale,
  });

  final ChatController controller;
  final double fontScale;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      if (!controller.imService.isCurrentSession(controller.sessionId)) {
        return const SizedBox.shrink();
      }
      final toolbar = controller.imService.getAgentToolbar(
        controller.sessionId,
      );
      final displayToolbar = _composeChatToolbarForDisplay(
        controller,
        serverToolbar: toolbar,
      );
      if (displayToolbar == null) {
        return const SizedBox.shrink();
      }

      return Container(
        color: Theme.of(context).colorScheme.surface,
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: RepaintBoundary(
          child: _buildChatAgentToolbarContent(
            controller,
            context,
            toolbar: displayToolbar,
            fontScale: fontScale,
          ),
        ),
      );
    });
  }
}

AgentToolbarModel? _composeChatToolbarForDisplay(
  ChatController controller, {
  AgentToolbarModel? serverToolbar,
}) {
  final sessionId = controller.sessionId.trim();
  final hasMatchingServerToolbar =
      serverToolbar != null &&
      serverToolbar.sessionId.trim() == sessionId &&
      serverToolbar.visible;
  if (!hasMatchingServerToolbar) {
    return null;
  }
  final items = <AgentToolbarItemModel>[...serverToolbar.items];

  if (controller.canToggleConversationAudit) {
    final selected = controller.conversationAuditEnabled.value;
    items.add(
      AgentToolbarItemModel(
        itemId: 'conversation_audit_toggle',
        groupId: 'platform_common',
        kind: 'button',
        actionId: '',
        label: '',
        icon: 'audit',
        variant: selected ? 'primary' : 'neutral',
        disabled: false,
        loading: false,
        selected: selected,
        tooltip: selected
            ? 'chat_toolbar_audit_enabled_tooltip'.tr
            : 'chat_toolbar_audit_enable_tooltip'.tr,
        badgeText: '',
        confirmTitle: '',
        confirmText: '',
        value: '',
        placeholder: '',
        options: const <AgentToolbarOptionModel>[],
        percent: 0,
        centerText: '',
        progressDesc: '',
        progressDetail: '',
        localAction: 'conversation_audit_toggle',
      ),
    );
  }

  if (items.isEmpty) {
    return null;
  }
  return AgentToolbarModel(
    sessionId: sessionId,
    agentId: serverToolbar.agentId,
    toolbarId: serverToolbar.toolbarId,
    revision: serverToolbar.revision,
    visible: true,
    updatedAt: serverToolbar.updatedAt,
    items: items,
    librarySkills: serverToolbar.librarySkills,
  );
}

Future<bool> _showConversationAuditEnableDialog(BuildContext context) {
  return showAppConfirmDialog(
    context: context,
    title: 'chat_toolbar_audit_enable_dialog_title'.tr,
    message: 'chat_toolbar_audit_enable_dialog_body'.tr,
    confirmText: 'chat_toolbar_audit_enable_dialog_confirm'.tr,
  );
}

/// 合并后端 Agent 工具栏项与前端平台公共项后统一渲染。
Widget _buildChatAgentToolbarContent(
  ChatController controller,
  BuildContext context, {
  required AgentToolbarModel toolbar,
  required double fontScale,
}) {
  final iconOnlyCodexToolbar = _isIconOnlyCodexToolbar(toolbar);
  final metrics = _resolveChatToolbarMetrics(
    context,
    fontScale,
    preferCompact: iconOnlyCodexToolbar,
  );

  final groups = _groupChatToolbarItems(
    toolbar.items
        .map((item) {
          final display = controller.imService.getToolbarItemForDisplay(
            controller.sessionId,
            item,
          );
          // 队列按钮：动态注入本地队列数作为显示文本
          if (display.localAction == 'show_queue') {
            final count = controller.imService.queueCountForSession(
              controller.sessionId,
            );
            final text = count > 99 ? '99+' : '$count';
            return AgentToolbarItemModel(
              itemId: display.itemId,
              groupId: display.groupId,
              kind: display.kind,
              actionId: display.actionId,
              label: text,
              icon: display.icon,
              variant: display.variant,
              disabled: display.disabled,
              loading: display.loading,
              selected: display.selected,
              tooltip: display.tooltip,
              badgeText: '',
              confirmTitle: display.confirmTitle,
              confirmText: display.confirmText,
              value: display.value,
              placeholder: display.placeholder,
              options: display.options,
              percent: display.percent,
              centerText: display.centerText,
              progressDesc: display.progressDesc,
              progressDetail: display.progressDetail,
              localAction: display.localAction,
              commands: display.commands,
            );
          }
          return display;
        })
        .toList(growable: false),
  );
  if (groups.isEmpty) {
    return const SizedBox.shrink();
  }

  return SizedBox(
    key: const ValueKey('chat_agent_toolbar_container'),
    width: double.infinity,
    height: metrics.itemHeight,
    child: ScrollConfiguration(
      // 桌面/Web 全局滚动默认禁鼠标拖动（保文字选中）；工具栏横向条需单独放开。
      behavior: const HorizontalDragScrollBehavior(),
      child: ListView.builder(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.symmetric(horizontal: 12),
        addAutomaticKeepAlives: false,
        itemCount: groups.length,
        itemBuilder: (context, index) {
          final group = groups[index];
          final groupId = group.first.groupId.trim();
          final trailingGap =
              index == groups.length - 1 ? 0.0 : metrics.groupGap;
          return Padding(
            padding: EdgeInsets.only(right: trailingGap),
            child: Row(
              key: groupId.isEmpty
                  ? null
                  : ValueKey('chat_agent_toolbar_group_$groupId'),
              mainAxisSize: MainAxisSize.min,
              children: [
                for (var itemIndex = 0; itemIndex < group.length; itemIndex++)
                  Padding(
                    padding: EdgeInsets.only(
                      right: itemIndex == group.length - 1
                          ? 0
                          : metrics.itemGap,
                    ),
                    child: _buildChatAgentToolbarItem(
                      controller,
                      context,
                      toolbar,
                      group[itemIndex],
                      metrics: metrics,
                      iconOnlyCodexToolbar: iconOnlyCodexToolbar,
                    ),
                  ),
              ],
            ),
          );
        },
      ),
    ),
  );
}

Widget _buildChatAgentToolbarItem(
  ChatController controller,
  BuildContext context,
  AgentToolbarModel toolbar,
  AgentToolbarItemModel item, {
  required _ChatToolbarMetrics metrics,
  required bool iconOnlyCodexToolbar,
}) {
  if (item.isSelect) {
    return _buildChatAgentToolbarSelect(
      controller,
      context,
      toolbar,
      item,
      metrics: metrics,
      iconOnlyCodexToolbar: iconOnlyCodexToolbar,
    );
  }
  if (item.isProgress) {
    return _buildChatAgentToolbarProgress(
      controller,
      context,
      toolbar,
      item,
      metrics: metrics,
    );
  }
  return _buildChatAgentToolbarButton(
    controller,
    context,
    toolbar,
    item,
    metrics: metrics,
    iconOnlyCodexToolbar: iconOnlyCodexToolbar,
  );
}

Widget _buildChatAgentToolbarButton(
  ChatController controller,
  BuildContext context,
  AgentToolbarModel toolbar,
  AgentToolbarItemModel item, {
  required _ChatToolbarMetrics metrics,
  required bool iconOnlyCodexToolbar,
}) {
  final theme = Theme.of(context);
  final disabled = item.disabled || item.loading;
  final compactValueOnly = _isCompactValueOnlyGeminiToolbarItem(item);
  final palette = _resolveChatToolbarPalette(
    theme,
    item.variant,
    selected: item.selected,
  );
  final primaryLabel = iconOnlyCodexToolbar
      ? ''
      : compactValueOnly
      ? _resolveChatToolbarCompactValueText(item)
      : _resolveChatToolbarPrimaryLabel(item);
  final badgeText = iconOnlyCodexToolbar
      ? ''
      : compactValueOnly
      ? ''
      : _resolveChatToolbarBadgeText(item);
  Widget child = Material(
    color: Colors.transparent,
    borderRadius: BorderRadius.circular(metrics.itemRadius),
    child: InkWell(
      borderRadius: BorderRadius.circular(metrics.itemRadius),
      onTap: disabled
          ? null
          : () async {
              if (item.isClientToggleList) {
                showChatToggleListSheet(
                  context,
                  item: item,
                  toolbar: toolbar,
                  sessionId: controller.sessionId,
                  imService: controller.imService,
                );
                return;
              }
              if (item.isClientCommandList) {
                showChatCommandListSheet(
                  context,
                  title: item.isSkillsCommandList ? '' : item.label,
                  commands: item.commands,
                  commandListItemId: item.itemId,
                  librarySkills: toolbar.librarySkills,
                  agentId: toolbar.agentId,
                  sessionId: controller.sessionId,
                  imService: controller.imService,
                  // 仅技能弹窗带「技能库」Tab；命令弹窗只列命令，避免重复。
                  showSkillLibrary: item.isSkillsCommandList,
                  onSelected: (cmd) {
                    final normalized = _normalizeToolbarSkillCommand(cmd.exec);
                    if (normalized.isEmpty) {
                      return;
                    }
                    controller.insertText('$normalized ');
                  },
                  onLibrarySkillInserted: (text) {
                    if (text.trim().isEmpty) return;
                    controller.insertText(text);
                  },
                );
                return;
              }
              if (item.localAction == 'visitor_profile') {
                showChatVisitorInfoDialog(context, controller);
                return;
              }
              if (item.localAction == 'conversation_audit_toggle') {
                if (!controller.conversationAuditEnabled.value) {
                  final confirmed = await _showConversationAuditEnableDialog(
                    context,
                  );
                  if (!confirmed) return;
                }
                controller.toggleConversationAudit();
                return;
              }
              if (item.localAction == 'list_sessions') {
                final session = controller.imService.findSessionById(
                  controller.sessionId,
                );
                if (session != null) {
                  String agentId;
                  if (controller.isGroupChat) {
                    agentId = controller.groupToolbarTargetAgentId.trim();
                  } else {
                    if (session.peerType != 2) return;
                    agentId = session.peerId.trim();
                  }
                  if (agentId.isEmpty || agentId == '0') return;
                  final agent = controller.agentService.agents.firstWhereOrNull(
                    (a) => a.id == agentId,
                  );
                  AgentSessionList.show(
                    context,
                    agentId: agentId,
                    currentSessionId: controller.sessionId,
                    agentClientType: agent?.agentClientType ?? '',
                    bindingsProvider: (aid) async {
                      final raw = await controller.imService
                          .requestAgentSessionBindings(
                            agentId: aid,
                            sessionId: controller.sessionId,
                          );
                      final entries = <AgentSessionBindingEntry>[];
                      for (final m in raw) {
                        final entry = AgentSessionBindingEntry.fromMap(m);
                        if (entry.hasAibotSession) {
                          // 曾绑定过 App 会话：用 AI Bot 会话 ID 查本地库。
                          final localSession = controller.imService
                              .findSessionById(entry.aibotSessionId);
                          if (localSession == null) {
                            // 本地查不到 = 该会话已在 App 侧被删除，丢弃。
                            continue;
                          }
                          // 会话列表里每条都是同一个 Agent，用 peerNickname
                          // 作标题没有区分度；优先用 session.title（会话摘要），
                          // 为空再回退到 peer 显示名。
                          final sessionTitle = localSession.title.trim();
                          final localTitle = sessionTitle.isNotEmpty
                              ? sessionTitle
                              : controller.imService.resolveSessionDisplayTitle(
                                  localSession,
                                );
                          entries.add(entry.copyWith(title: localTitle));
                        } else {
                          // 仅存在于插件侧、尚未绑定 App 会话：保留，
                          // 供用户新建会话并绑定后继续往下聊。
                          entries.add(entry);
                        }
                      }
                      return entries;
                    },
                    bindProvider:
                        ({
                          required cwd,
                          agentSessionId = '',
                          title = '',
                        }) async {
                          final raw = await controller.imService
                              .requestAgentSessionBind(
                                agentId: agentId,
                                sessionId: controller.sessionId,
                                cwd: cwd,
                                agentSessionId: agentSessionId,
                                title: title,
                              );
                          return AgentSessionBindResult.fromMap(raw);
                        },
                  );
                }
                return;
              }
              if (item.localAction == 'browse_files') {
                controller.pickRemoteFiles();
                return;
              }
              if (item.localAction == 'show_queue') {
                showChatQueueSheet(
                  context,
                  imService: controller.imService,
                  sessionId: controller.sessionId,
                  controller: controller,
                );
                return;
              }
              if (item.localAction == 'visitor_close') {
                final confirmed = await showChatVisitorSessionActionConfirm(
                  context,
                  title: 'chat_visitor_close_title'.tr,
                  content: 'chat_visitor_close_confirm_content'.tr,
                  confirmText: 'chat_visitor_close_confirm'.tr,
                );
                if (!confirmed) {
                  return;
                }
                await controller.closeCurrentVisitorSession();
                return;
              }
              if (item.localAction == 'visitor_ban') {
                final confirmed = await showChatVisitorSessionActionConfirm(
                  context,
                  title: 'chat_visitor_ban_title'.tr,
                  content: 'chat_visitor_ban_confirm_content'.tr,
                  confirmText: 'chat_visitor_ban_confirm'.tr,
                  destructive: true,
                );
                if (!confirmed) {
                  return;
                }
                await controller.banCurrentVisitorSession();
                return;
              }
              final confirmed = await _confirmChatToolbarAction(
                context,
                title: item.confirmTitle,
                content: item.confirmText,
              );
              if (!confirmed) {
                return;
              }
              await controller.imService.sendAgentToolbarAction(
                sessionId: controller.sessionId,
                toolbar: toolbar,
                item: item,
                event: 'click',
              );
            },
      child: ConstrainedBox(
        constraints: BoxConstraints(
          minHeight: metrics.itemHeight,
          maxWidth: metrics.itemMaxWidth,
        ),
        child: AnimatedContainer(
          key: ValueKey('chat_agent_toolbar_item_${item.itemId}'),
          duration: const Duration(milliseconds: 160),
          padding: EdgeInsets.symmetric(
            horizontal: metrics.itemHorizontalPadding,
            vertical: metrics.itemVerticalPadding,
          ),
          decoration: BoxDecoration(
            color: disabled
                ? theme.disabledColor.withValues(alpha: 0.08)
                : palette.background,
            borderRadius: BorderRadius.circular(metrics.itemRadius),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (item.loading)
                SizedBox(
                  width: metrics.spinnerSize,
                  height: metrics.spinnerSize,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: palette.foreground,
                  ),
                )
              else if (item.icon.trim().isNotEmpty)
                Icon(
                  _resolveChatToolbarIcon(item.icon),
                  size: metrics.iconSize,
                  color: disabled ? theme.disabledColor : palette.foreground,
                ),
              if (primaryLabel.isNotEmpty) ...[
                SizedBox(
                  width: item.icon.trim().isNotEmpty ? metrics.iconGap : 0,
                ),
                Flexible(
                  child: Text(
                    primaryLabel,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: metrics.labelFontSize,
                      fontWeight: FontWeight.w600,
                      color: disabled
                          ? theme.disabledColor
                          : palette.foreground,
                    ),
                  ),
                ),
              ],
              if (badgeText.isNotEmpty) ...[
                SizedBox(width: metrics.badgeGap),
                _buildChatToolbarBadge(
                  badgeText,
                  theme,
                  palette: palette,
                  metrics: metrics,
                  disabled: disabled,
                ),
              ],
            ],
          ),
        ),
      ),
    ),
  );
  if (item.tooltip.isEmpty) {
    return child;
  }
  return Tooltip(message: item.tooltip, child: child);
}

Widget _buildChatAgentToolbarProgress(
  ChatController controller,
  BuildContext context,
  AgentToolbarModel toolbar,
  AgentToolbarItemModel item, {
  required _ChatToolbarMetrics metrics,
}) {
  final theme = Theme.of(context);
  final palette = _resolveChatToolbarPalette(theme, item.variant);
  final isThreadCompact = item.localAction == 'thread_compact';
  final hasBusyToolbarAction = toolbar.items.any((toolbarItem) {
    if (toolbarItem.itemId == item.itemId) {
      return false;
    }
    return toolbarItem.loading;
  });
  final hasRunningExecution = controller.hasRunningExecutionForSession;
  final compactLocked =
      isThreadCompact && (hasRunningExecution || hasBusyToolbarAction);
  final disabled = item.disabled || compactLocked;

  Future<void> handleTap() async {
    if (item.localAction == 'thread_compact') {
      if (compactLocked) {
        return;
      }
      final confirmed = await _confirmChatToolbarAction(
        context,
        title: _buildThreadCompactConfirmTitle(item),
        content: _buildThreadCompactConfirmContent(toolbar, item),
      );
      if (!confirmed) {
        return;
      }
      await controller.imService.sendAgentToolbarAction(
        sessionId: controller.sessionId,
        toolbar: toolbar,
        item: item,
        event: 'click',
      );
      return;
    }
    if (item.localAction == 'get_rate_limits') {
      await controller.imService.sendAgentToolbarAction(
        sessionId: controller.sessionId,
        toolbar: toolbar,
        item: item,
        event: 'click',
      );
      if (!context.mounted) {
        return;
      }
      final desc = item.progressDesc.tr;
      final detail = _buildRateLimitDetail(item.percent, item.progressDetail);
      ProgressDetailBottomSheet.show(
        context,
        description: desc,
        percent: item.percent,
        detail: detail,
        accentColor: palette.foreground,
      );
      return;
    }
    if (item.progressDesc.isEmpty && item.progressDetail.isEmpty) return;
    ProgressDetailBottomSheet.show(
      context,
      description: item.progressDesc,
      percent: item.percent,
      detail: item.progressDetail,
      accentColor: palette.foreground,
    );
  }

  final windowDuration = resolveRateLimitWindowDuration(
    localAction: item.localAction,
    itemId: item.itemId,
    centerText: item.centerText,
  );
  final resetTime = windowDuration == null
      ? null
      : parseRateLimitResetTime(item.progressDetail);

  final Widget child;
  if (windowDuration != null && resetTime != null) {
    child = _ChatToolbarTimeRingProgress(
      centerText: _resolveToolbarProgressCenterText(
        item.centerText,
        item.percent,
      ),
      percent: item.percent,
      ringColor: palette.foreground,
      size: metrics.itemHeight,
      strokeWidth: 2.5,
      disabled: disabled,
      onTap: handleTap,
      resetTime: resetTime,
      windowDuration: windowDuration,
    );
  } else {
    child = CircularProgressButton(
      centerText: _resolveToolbarProgressCenterText(
        item.centerText,
        item.percent,
      ),
      percent: item.percent,
      ringColor: palette.foreground,
      size: metrics.itemHeight,
      strokeWidth: 2.5,
      disabled: disabled,
      onTap: handleTap,
    );
  }

  if (item.tooltip.isEmpty) return child;
  return Tooltip(message: item.tooltip, child: child);
}

/// 用量圆环外层套一圈"时间已过百分比"绿色内环。每分钟自动重画。
class _ChatToolbarTimeRingProgress extends StatefulWidget {
  const _ChatToolbarTimeRingProgress({
    required this.centerText,
    required this.percent,
    required this.ringColor,
    required this.size,
    required this.strokeWidth,
    required this.disabled,
    required this.onTap,
    required this.resetTime,
    required this.windowDuration,
  });

  final String centerText;
  final double percent;
  final Color ringColor;
  final double size;
  final double strokeWidth;
  final bool disabled;
  final VoidCallback onTap;
  final DateTime resetTime;
  final Duration windowDuration;

  @override
  State<_ChatToolbarTimeRingProgress> createState() =>
      _ChatToolbarTimeRingProgressState();
}

class _ChatToolbarTimeRingProgressState
    extends State<_ChatToolbarTimeRingProgress> {
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(const Duration(seconds: 60), (_) {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final timePercent = computeRateLimitTimePercent(
      widget.resetTime,
      widget.windowDuration,
      DateTime.now(),
    );
    return CircularProgressButton(
      centerText: widget.centerText,
      percent: widget.percent,
      ringColor: widget.ringColor,
      size: widget.size,
      strokeWidth: widget.strokeWidth,
      disabled: widget.disabled,
      onTap: widget.onTap,
      innerPercent: timePercent,
    );
  }
}

String _resolveToolbarProgressCenterText(String raw, double percent) {
  final text = raw.trim();
  if (text != '压缩') return raw;
  final compactPercent = percent.floor().clamp(0, 99);
  return '$compactPercent';
}

String _buildRateLimitDetail(double percent, String rawDetail) {
  final used = percent.clamp(0.0, 100.0);
  final remaining = (100.0 - used).clamp(0.0, 100.0);
  final lines = <String>[
    '${'rate_limit_used'.tr}: ${used.toStringAsFixed(used == used.truncateToDouble() ? 0 : 1)}%',
    '${'rate_limit_remaining'.tr}: ${remaining.toStringAsFixed(remaining == remaining.truncateToDouble() ? 0 : 1)}%',
  ];
  if (rawDetail.isNotEmpty) {
    final resetTime = parseRateLimitResetTime(rawDetail);
    if (resetTime != null) {
      final now = DateTime.now();
      if (resetTime.isBefore(now)) {
        lines.add('rate_limit_already_reset'.tr);
      } else {
        final diff = resetTime.difference(now);
        lines.add(
          'rate_limit_resets_in'.tr.replaceAll(
            '\$time\$',
            _formatRelativeDuration(diff),
          ),
        );
      }
    }
  }
  return lines.join('\n');
}

String _buildThreadCompactConfirmTitle(AgentToolbarItemModel item) {
  final title = item.confirmTitle.trim();
  if (title.isNotEmpty) {
    return title;
  }
  return 'chat_toolbar_thread_compact_confirm_title'.tr;
}

String _buildThreadCompactConfirmContent(
  AgentToolbarModel toolbar,
  AgentToolbarItemModel compactItem,
) {
  final content = compactItem.confirmText.trim();
  if (content.isNotEmpty) {
    return content;
  }

  final percent = compactItem.percent;
  final used = percent.clamp(0.0, 100.0);
  final remaining = (100.0 - used).clamp(0.0, 100.0);
  final usageDetail = [
    '${'rate_limit_used'.tr}: ${used.toStringAsFixed(used == used.truncateToDouble() ? 0 : 1)}%',
    '${'rate_limit_remaining'.tr}: ${remaining.toStringAsFixed(remaining == remaining.truncateToDouble() ? 0 : 1)}%',
  ].join('\n');
  return [
    'chat_toolbar_thread_compact_confirm_intro'.tr,
    '',
    'chat_toolbar_thread_compact_confirm_usage'.tr,
    usageDetail,
    '',
    'chat_toolbar_thread_compact_confirm_continue'.tr,
  ].join('\n');
}

String _formatRelativeDuration(Duration d) {
  final hours = d.inHours;
  final minutes = d.inMinutes % 60;
  if (hours > 24) {
    final days = hours ~/ 24;
    final remainHours = hours % 24;
    if (remainHours > 0) {
      return '$days${'time_unit_day'.tr}$remainHours${'time_unit_hour'.tr}';
    }
    return '$days${'time_unit_day'.tr}';
  }
  if (hours > 0 && minutes > 0) {
    return '$hours${'time_unit_hour'.tr}$minutes${'time_unit_minute'.tr}';
  }
  if (hours > 0) return '$hours${'time_unit_hour'.tr}';
  if (minutes > 0) return '$minutes${'time_unit_minute'.tr}';
  return '${d.inSeconds}${'time_unit_second'.tr}';
}

Widget _buildChatAgentToolbarSelect(
  ChatController controller,
  BuildContext context,
  AgentToolbarModel toolbar,
  AgentToolbarItemModel item, {
  required _ChatToolbarMetrics metrics,
  required bool iconOnlyCodexToolbar,
}) {
  final theme = Theme.of(context);
  final disabled =
      item.disabled ||
      item.loading ||
      item.options.where((o) => !o.disabled).isEmpty;
  final compactValueOnly = _isCompactValueOnlyGeminiToolbarItem(item);
  final isFolderSelector = _isChatToolbarFolderSelectItem(item);
  final palette = _resolveChatToolbarPalette(
    theme,
    item.variant,
    selected: item.selected,
  );
  final primaryLabel = iconOnlyCodexToolbar
      ? ''
      : isFolderSelector
      ? ''
      : compactValueOnly
      ? _resolveChatToolbarCompactValueText(item)
      : _resolveChatToolbarPrimaryLabel(item);
  final badgeText = iconOnlyCodexToolbar
      ? ''
      : compactValueOnly
      ? ''
      : _resolveChatToolbarBadgeText(item);

  Future<void> selectOption(String optionId) async {
    await controller.imService.sendAgentToolbarAction(
      sessionId: controller.sessionId,
      toolbar: toolbar,
      item: item,
      event: 'select',
      optionId: optionId,
    );
  }

  final chip = ConstrainedBox(
    constraints: BoxConstraints(
      minHeight: metrics.itemHeight,
      maxWidth: metrics.itemMaxWidth,
    ),
    child: AnimatedContainer(
      key: ValueKey('chat_agent_toolbar_item_${item.itemId}'),
      duration: const Duration(milliseconds: 160),
      padding: EdgeInsets.symmetric(
        horizontal: metrics.itemHorizontalPadding,
        vertical: metrics.itemVerticalPadding,
      ),
      decoration: BoxDecoration(
        color: disabled
            ? theme.disabledColor.withValues(alpha: 0.08)
            : palette.background,
        borderRadius: BorderRadius.circular(metrics.itemRadius),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (item.loading)
            SizedBox(
              width: metrics.spinnerSize,
              height: metrics.spinnerSize,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: palette.foreground,
              ),
            )
          else
            Icon(
              _resolveChatToolbarIcon(item.icon),
              size: metrics.iconSize,
              color: disabled ? theme.disabledColor : palette.foreground,
            ),
          if (primaryLabel.isNotEmpty) ...[
            SizedBox(width: metrics.iconGap),
            Flexible(
              child: Text(
                primaryLabel,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                  fontSize: metrics.labelFontSize,
                  fontWeight: FontWeight.w600,
                  color: disabled ? theme.disabledColor : palette.foreground,
                ),
              ),
            ),
          ],
          // 设置应用失败：后端去掉文字后缀、改发 warning variant，这里渲染叹号图标。
          if (compactValueOnly &&
              item.variant.trim().toLowerCase() == 'warning') ...[
            SizedBox(width: metrics.badgeGap),
            Icon(
              Icons.priority_high_rounded,
              size: metrics.iconSize,
              color: disabled ? theme.disabledColor : palette.foreground,
            ),
          ],
          if (badgeText.isNotEmpty) ...[
            SizedBox(width: metrics.badgeGap),
            _buildChatToolbarBadge(
              badgeText,
              theme,
              palette: palette,
              metrics: metrics,
              disabled: disabled,
            ),
          ],
          SizedBox(width: metrics.chevronGap),
          Icon(
            Icons.keyboard_arrow_down_rounded,
            size: metrics.chevronSize,
            color: disabled ? theme.disabledColor : palette.foreground,
          ),
        ],
      ),
    ),
  );

  final Widget child;
  if (chatToolbarSelectUsesSheet(item.options.length)) {
    // 长列表（如 Cursor CLI 展开模型）走可搜索 BottomSheet，避免 PopupMenu 撑满裁切。
    child = Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: disabled
            ? null
            : () {
                final title = item.tooltip.trim().isNotEmpty
                    ? item.tooltip.trim()
                    : (item.label.trim().isNotEmpty ? item.label.trim() : '选择');
                showChatToolbarSelectSheet(
                  context: context,
                  title: title,
                  options: _buildChatToolbarSelectOptionsForMenu(item),
                  currentValue: item.value,
                  onSelected: selectOption,
                );
              },
        borderRadius: BorderRadius.circular(metrics.itemRadius),
        child: chip,
      ),
    );
  } else {
    child = PopupMenuButton<String>(
      enabled: !disabled,
      tooltip: item.tooltip,
      padding: EdgeInsets.zero,
      splashRadius: metrics.itemHeight,
      position: PopupMenuPosition.under,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      color: theme.colorScheme.surface,
      onSelected: selectOption,
      itemBuilder: (context) {
        final orderedOptions = _buildChatToolbarSelectOptionsForMenu(item);
        return orderedOptions.map((option) {
          final isCurrent = _isChatToolbarOptionCurrent(item, option);
          return PopupMenuItem<String>(
            height: metrics.menuItemHeight,
            value: option.optionId,
            enabled: !option.disabled,
            child: ConstrainedBox(
              constraints: BoxConstraints(
                minWidth: metrics.menuItemMinWidth,
                maxWidth: metrics.menuItemMaxWidth,
              ),
              child: Row(
                children: [
                  Icon(
                    isCurrent
                        ? Icons.check_circle_rounded
                        : Icons.circle_outlined,
                    size: 16,
                    color: option.disabled
                        ? theme.disabledColor
                        : isCurrent
                        ? palette.foreground
                        : theme.colorScheme.secondary.withValues(alpha: 0.52),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      option.label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: metrics.menuLabelFontSize,
                        fontWeight:
                            isCurrent ? FontWeight.w600 : FontWeight.w500,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          );
        }).toList();
      },
      child: chip,
    );
  }
  if (item.tooltip.isEmpty) {
    return child;
  }
  return Tooltip(message: item.tooltip, child: child);
}

Future<bool> _confirmChatToolbarAction(
  BuildContext context, {
  required String title,
  required String content,
}) async {
  if (title.trim().isEmpty || content.trim().isEmpty) {
    return true;
  }
  return showAppConfirmDialog(
    context: context,
    title: title,
    message: content,
    isDestructive: true,
  );
}

typedef _ChatToolbarMetrics = ({
  double groupGap,
  double itemGap,
  double itemHeight,
  double itemMaxWidth,
  double itemHorizontalPadding,
  double itemVerticalPadding,
  double itemRadius,
  double iconSize,
  double spinnerSize,
  double iconGap,
  double labelFontSize,
  double badgeGap,
  double badgeFontSize,
  double badgeHorizontalPadding,
  double badgeVerticalPadding,
  double badgeMaxWidth,
  double chevronSize,
  double chevronGap,
  double menuItemHeight,
  double menuItemMinWidth,
  double menuItemMaxWidth,
  double menuLabelFontSize,
});

_ChatToolbarMetrics _resolveChatToolbarMetrics(
  BuildContext context,
  double fontScale, {
  bool preferCompact = false,
}) {
  final width = MediaQuery.sizeOf(context).width;
  final compact = width < 420;
  final dense = preferCompact;
  return (
    groupGap: dense ? 6 : (compact ? 8 : 10),
    itemGap: dense ? 3 : (compact ? 4 : 5),
    itemHeight: dense ? (compact ? 32 : 34) : (compact ? 34 : 36),
    itemMaxWidth: dense
        ? (compact ? 108 : 124)
        : (width >= 900 ? 240 : (compact ? 164 : 188)),
    itemHorizontalPadding: dense ? 8 : (compact ? 10 : 11),
    itemVerticalPadding: dense ? 6 : (compact ? 7 : 8),
    itemRadius: 9,
    iconSize: dense ? (compact ? 14 : 15) : (compact ? 15 : 16),
    spinnerSize: dense ? (compact ? 12 : 13) : (compact ? 13 : 14),
    iconGap: dense ? 0 : (compact ? 6 : 7),
    labelFontSize: (12.5 * fontScale).clamp(11.5, 13.5).toDouble(),
    badgeGap: dense ? 0 : 6,
    badgeFontSize: (10.5 * fontScale).clamp(10.0, 11.5).toDouble(),
    badgeHorizontalPadding: dense ? 0 : (compact ? 6 : 7),
    badgeVerticalPadding: dense ? 0 : (compact ? 2 : 2.5),
    badgeMaxWidth: dense ? 0 : (compact ? 84 : 96),
    chevronSize: compact ? 16 : 18,
    chevronGap: dense ? 2 : (compact ? 4 : 5),
    menuItemHeight: compact ? 36 : 38,
    menuItemMinWidth: compact ? 148 : 164,
    menuItemMaxWidth: compact ? 220 : 260,
    menuLabelFontSize: (12.5 * fontScale).clamp(11.5, 13.0).toDouble(),
  );
}

bool _isIconOnlyCodexToolbar(AgentToolbarModel toolbar) {
  if (toolbar.toolbarId.trim().toLowerCase() != 'agent-toolbar:codex:v1') {
    return false;
  }
  return toolbar.items.every(
    (item) =>
        item.label.trim().isEmpty &&
        item.value.trim().isEmpty &&
        item.badgeText.trim().isEmpty,
  );
}

String _normalizeToolbarSkillCommand(String raw) {
  final trimmed = raw.trim();
  if (trimmed.isEmpty) {
    return '';
  }
  if (trimmed.startsWith('/')) {
    return trimmed;
  }
  return '/$trimmed';
}

List<List<AgentToolbarItemModel>> _groupChatToolbarItems(
  List<AgentToolbarItemModel> items,
) {
  final groups = <String, List<AgentToolbarItemModel>>{};
  final order = <String>[];
  for (final item in items) {
    final groupKey = item.groupId.isNotEmpty
        ? 'group:${item.groupId}'
        : 'item:${item.itemId}';
    if (!groups.containsKey(groupKey)) {
      groups[groupKey] = <AgentToolbarItemModel>[];
      order.add(groupKey);
    }
    groups[groupKey]!.add(item);
  }
  return order.map((key) => groups[key]!).toList(growable: false);
}

IconData _resolveChatToolbarIcon(String icon) {
  switch (icon.trim().toLowerCase()) {
    case 'stop':
      return Icons.stop_circle_outlined;
    case 'folder':
      return Icons.folder_open_outlined;
    case 'cpu':
      return Icons.memory_rounded;
    case 'status':
      return Icons.info_outline_rounded;
    case 'where':
      return Icons.near_me_outlined;
    case 'link':
      return Icons.link_rounded;
    case 'run':
      return Icons.play_circle_outline_rounded;
    case 'pause':
      return Icons.pause_circle_outline_rounded;
    case 'ban':
      return Icons.block_rounded;
    case 'refresh':
      return Icons.refresh_rounded;
    case 'settings':
      return Icons.tune_rounded;
    case 'check':
      return Icons.check_circle_outline_rounded;
    case 'warning':
      return Icons.warning_amber_rounded;
    case 'terminal':
      return Icons.terminal_rounded;
    case 'spark':
      return Icons.auto_awesome_rounded;
    case 'shield':
      return Icons.shield_outlined;
    case 'audit':
      return Icons.fact_check_outlined;
    case 'usage':
      return Icons.query_stats_rounded;
    default:
      return Icons.tune_rounded;
  }
}

String _resolveChatToolbarPrimaryLabel(AgentToolbarItemModel item) {
  // 访客工具栏由后端下发中文 label，前端按 localAction 做 i18n。
  switch (item.localAction.trim()) {
    case 'visitor_profile':
      return 'chat_visitor_info_title'.tr;
    case 'visitor_close':
      return 'chat_visitor_toolbar_close'.tr;
    case 'visitor_ban':
      return 'chat_visitor_ban_title'.tr;
    case 'client:toggle_list':
      return 'chat_toolbar_plugins'.tr;
  }
  final label = item.label.trim();
  if (label.isNotEmpty) {
    return label;
  }
  final value = item.value.trim();
  if (value.isNotEmpty) {
    return value;
  }
  final placeholder = item.placeholder.trim();
  if (placeholder.isNotEmpty) {
    return placeholder;
  }
  return '';
}

bool _isCompactValueOnlyGeminiToolbarItem(AgentToolbarItemModel item) {
  switch (item.actionId.trim().toLowerCase()) {
    case 'session_control':
    case 'select_preset':
    case 'select_provider':
    case 'select_model':
    case 'select_mode':
      return true;
    default:
      return false;
  }
}

String _resolveChatToolbarCompactValueText(AgentToolbarItemModel item) {
  // DeepSeek 供应商 chip 只留图标：供应商名放进 tooltip/下拉选项，
  // 不占 chip 主文本（后端 badge/value 仍照常下发，当前选中态靠 value 标记）。
  if (item.actionId.trim().toLowerCase() == 'select_provider') {
    return '';
  }
  final badge = item.badgeText.trim();
  if (badge.isNotEmpty) {
    return badge;
  }
  final value = item.value.trim();
  if (value.isEmpty) {
    return '';
  }
  for (final option in item.options) {
    if (option.optionId.trim().toLowerCase() == value.toLowerCase()) {
      final label = option.label.trim();
      if (label.isNotEmpty) {
        return label;
      }
    }
  }
  return value;
}

String _resolveChatToolbarBadgeText(AgentToolbarItemModel item) {
  final badge = item.badgeText.trim();
  if (badge.isNotEmpty) {
    return badge;
  }
  final value = item.value.trim();
  final label = item.label.trim();
  if (item.isSelect && value.isNotEmpty && value != label) {
    return value;
  }
  return '';
}

bool _isChatToolbarOptionCurrent(
  AgentToolbarItemModel item,
  AgentToolbarOptionModel option,
) {
  final current = item.value.trim().toLowerCase();
  if (current.isEmpty) {
    return false;
  }
  return current == option.optionId.trim().toLowerCase() ||
      current == option.label.trim().toLowerCase();
}

bool _isChatToolbarFolderSelectItem(AgentToolbarItemModel item) {
  if (!item.isSelect) {
    return false;
  }
  final icon = item.icon.trim().toLowerCase();
  final actionId = item.actionId.trim().toLowerCase();
  final itemId = item.itemId.trim().toLowerCase();
  return icon == 'folder' ||
      actionId.contains('folder') ||
      itemId.contains('folder');
}

List<AgentToolbarOptionModel> _buildChatToolbarSelectOptionsForMenu(
  AgentToolbarItemModel item,
) {
  if (!_isChatToolbarFolderSelectItem(item) || item.options.length <= 1) {
    return item.options;
  }
  final currentIndex = item.options.indexWhere(
    (option) => _isChatToolbarOptionCurrent(item, option),
  );
  if (currentIndex <= 0) {
    return item.options;
  }
  final options = List<AgentToolbarOptionModel>.of(item.options);
  final current = options.removeAt(currentIndex);
  options.insert(0, current);
  return options;
}

Widget _buildChatToolbarBadge(
  String text,
  ThemeData theme, {
  required ({Color background, Color foreground}) palette,
  required _ChatToolbarMetrics metrics,
  required bool disabled,
}) {
  return ConstrainedBox(
    constraints: BoxConstraints(maxWidth: metrics.badgeMaxWidth),
    child: Container(
      padding: EdgeInsets.symmetric(
        horizontal: metrics.badgeHorizontalPadding,
        vertical: metrics.badgeVerticalPadding,
      ),
      decoration: BoxDecoration(
        color: disabled
            ? theme.disabledColor.withValues(alpha: 0.08)
            : palette.foreground.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        text,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: TextStyle(
          fontSize: metrics.badgeFontSize,
          fontWeight: FontWeight.w600,
          color: disabled ? theme.disabledColor : palette.foreground,
        ),
      ),
    ),
  );
}

({Color background, Color foreground}) _resolveChatToolbarPalette(
  ThemeData theme,
  String variant, {
  bool selected = false,
}) {
  final token = variant.trim().toLowerCase();
  Color tone;
  switch (token) {
    case 'danger':
      tone = AppTheme.errorColor;
      break;
    case 'warning':
      tone = AppTheme.warningColor;
      break;
    case 'success':
      tone = AppTheme.successColor;
      break;
    case 'primary':
    case 'accent':
    case 'secondary':
      tone = theme.colorScheme.primary;
      break;
    case 'ghost':
    case 'neutral':
    case '':
      tone = theme.colorScheme.onSurface;
      break;
    default:
      tone = theme.colorScheme.onSurface;
      break;
  }

  if (token == 'ghost' || token == 'neutral' || token.isEmpty) {
    if (selected) {
      return (
        background: theme.colorScheme.primary.withValues(alpha: 0.10),
        foreground: theme.colorScheme.primary,
      );
    }
    return (
      background: theme.colorScheme.surface.withValues(alpha: 0.9),
      foreground: theme.colorScheme.onSurface,
    );
  }

  return (
    background: tone.withValues(alpha: selected ? 0.18 : 0.10),
    foreground: tone,
  );
}

Future<void> handleChatImageAttachmentTap(
  ChatController controller,
  BuildContext context,
) async {
  controller.closeAttachmentMenu();
  final action = await ChatAttachmentSourceSheet.show(context);
  if (action == null) {
    return;
  }
  switch (action) {
    case ChatAttachmentSourceAction.camera:
      await controller.pickAndSendImageFromCamera();
      return;
    case ChatAttachmentSourceAction.gallery:
      await controller.pickAndSendImage();
      return;
  }
}

Future<void> handleChatVideoAttachmentTap(
  ChatController controller,
  BuildContext context,
) async {
  controller.closeAttachmentMenu();
  final action = await ChatAttachmentSourceSheet.show(context);
  if (action == null) {
    return;
  }
  switch (action) {
    case ChatAttachmentSourceAction.camera:
      await controller.pickAndSendVideoFromCamera();
      return;
    case ChatAttachmentSourceAction.gallery:
      await controller.pickAndSendVideo();
      return;
  }
}

Widget buildChatMessageBubbleWithMenu({
  required ChatController controller,
  required BuildContext context,
  required MessageModel msg,
  required bool showVisibleToLock,
  required String visibleToTip,
  required double fontScale,
  required bool hasSenderMeta,
  required bool showAvatar,
  required bool isMine,
  required bool isStreaming,
  required String itemKey,
  required Map<String, MessageModel> messageByLookupId,
  ChatMessageCardData? messageCardDataOverride,
}) {
  final status = msg.status ?? '';
  final isSending = status.startsWith('sending');
  final isFailed = status.startsWith('failed');
  final canRevoke = controller.canRevokeMessage(
    message: msg,
    isMine: isMine,
    isSending: isSending,
    isFailed: isFailed,
    isStreaming: isStreaming,
  );
  final canReply = !isSending && !isFailed;
  final canCopy = msg.content.isNotEmpty;
  final canForward = controller.canForwardMessage(msg) && !isStreaming;

  final displayContent = controller.formatMessageContentForDisplay(msg.content);
  const shouldDeferMarkdownRender = false;

  final quotedMessageId = msg.quotedMessageId;
  MessageModel? repliedMsg;
  if (quotedMessageId?.isNotEmpty == true) {
    final targetQuotedMessageId = quotedMessageId!;
    repliedMsg = messageByLookupId[targetQuotedMessageId];
  }
  final displayRepliedMsg = repliedMsg?.copyWith(
    content: controller.formatMessageContentForDisplay(repliedMsg.content),
  );

  final sessionId = controller.sessionId;
  final agentId = () {
    // agent 发的消息，senderId 就是 agent_id
    if (msg.senderType == 2) return msg.senderId.trim();
    // 非 agent 消息：私聊从 session 取，群聊从 toolbar 取
    if (controller.isGroupChat) {
      return controller.groupToolbarTargetAgentId.trim();
    }
    final session = controller.imService.findSessionById(sessionId);
    if (session?.peerType == 2) return session!.peerId.trim();
    return '';
  }();
  final pickRemoteDirectory = agentId.isNotEmpty
      ? () => pickChatAgentRemoteDirectory(controller, agentId: agentId)
      : null;

  final bubble = MessageBubble(
    key: ValueKey('${itemKey}_bubble'),
    msgId: msg.msgId,
    initialContent: displayContent,
    messageExtra: msg.extra,
    messageCardDataOverride: messageCardDataOverride,
    isStreaming: isStreaming,
    isMine: isMine,
    isThinking: msg.isThinking,
    quotedMessageId: quotedMessageId,
    repliedMsg: displayRepliedMsg,
    onStreamUpdate: isStreaming ? controller.onStreamingMessageUpdated : null,
    deferMarkdownRender: shouldDeferMarkdownRender,
    markdownRenderDeferDuration: const Duration(milliseconds: 220),
    onMessageCardTap: controller.onMessageCardTap,
    onMessageCardAction: controller.onMessageCardAction,
    messageCardManagedInputBinding: controller
        .createMessageCardManagedInputBinding(msg.msgId),
    isExecApprovalPending: controller.isExecApprovalActionPending,
    pickRemoteDirectory: pickRemoteDirectory,
    margin: EdgeInsets.only(
      left: ChatView._messageContentHorizontalInset,
      right: ChatView._messageContentHorizontalInset,
      top: hasSenderMeta
          ? ChatView._messageBubbleTopMarginWithSender
          : ChatView._messageBubbleTopMargin,
      bottom: ChatView._messageBubbleVerticalMargin,
    ),
    borderRadius: _chatMessageBubbleBorderRadius(isMine: isMine),
  );
  final bubbleWithVisibleToLock = showVisibleToLock
      ? buildChatVisibleToLockBubble(
          bubble: bubble,
          tipMessage: visibleToTip,
          fontScale: fontScale,
        )
      : bubble;
  final audit = msg.extra['audit'];
  final isAuditedTurn =
      isMine &&
      audit is Map &&
      audit['enabled'] == true &&
      audit['scope']?.toString() == 'turn';
  final bubbleWithAuditBadge = isAuditedTurn
      ? buildChatAuditBadgeBubble(
          bubble: bubbleWithVisibleToLock,
          isMine: isMine,
          onTap: () => Get.to(
            () => ConversationAuditDetailPage(
              sessionId: msg.sessionId,
              msgId: msg.msgId,
            ),
          ),
        )
      : bubbleWithVisibleToLock;

  return Obx(() {
    if (controller.isForwardSelectionMode) {
      if (!canForward) {
        return bubbleWithAuditBadge;
      }
      final selected = controller.isForwardMessageSelectedByKey(itemKey);
      return ChatSelectableMessageBubble(
        isMine: isMine,
        selectionMode: true,
        selected: selected,
        onTap: () => controller.toggleForwardMessageSelection(msg),
        onLongPress: () => controller.toggleForwardMessageSelection(msg),
        child: bubbleWithAuditBadge,
      );
    }

    final hasContextAction = canRevoke || canReply || canCopy || canForward;
    if (!hasContextAction) {
      return bubbleWithAuditBadge;
    }

    return ChatSelectableMessageBubble(
      isMine: isMine,
      selectionMode: false,
      selected: false,
      onTap: null,
      onLongPress: hasContextAction
          ? () => showChatMessageContextMenu(
              controller: controller,
              context: context,
              msg: msg,
              canRevoke: canRevoke,
              canReply: canReply,
              canCopy: canCopy,
              canForward: canForward,
            )
          : null,
      child: bubbleWithAuditBadge,
    );
  });
}

Widget buildChatAuditBadgeBubble({
  required Widget bubble,
  required bool isMine,
  required VoidCallback onTap,
}) {
  final theme = Get.theme;
  return Stack(
    clipBehavior: Clip.none,
    children: [
      bubble,
      Positioned(
        bottom: 0,
        right: isMine ? 10 : null,
        left: isMine ? null : 10,
        child: Semantics(
          button: true,
          label: 'chat_message_audit_view_label'.tr,
          child: InkWell(
            borderRadius: BorderRadius.circular(999),
            onTap: onTap,
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
              decoration: BoxDecoration(
                color: theme.colorScheme.primaryContainer,
                borderRadius: BorderRadius.circular(999),
              ),
              child: Text(
                'chat_message_audit_badge'.tr,
                style: TextStyle(
                  fontSize: 10,
                  color: theme.colorScheme.onPrimaryContainer,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
          ),
        ),
      ),
    ],
  );
}

Widget buildChatVisibleToLockBubble({
  required Widget bubble,
  required String tipMessage,
  required double fontScale,
}) {
  final theme = Get.theme;
  return Stack(
    clipBehavior: Clip.none,
    children: [
      bubble,
      Positioned(
        top: 4,
        right: 6,
        child: Tooltip(
          triggerMode: TooltipTriggerMode.tap,
          message: tipMessage,
          child: Container(
            padding: const EdgeInsets.all(1),
            decoration: BoxDecoration(
              color: theme.colorScheme.surface,
              borderRadius: BorderRadius.circular(999),
            ),
            child: Icon(
              Icons.lock_outline,
              size: 12 * fontScale,
              color: theme.colorScheme.secondary.withValues(alpha: 0.7),
            ),
          ),
        ),
      ),
    ],
  );
}

Widget buildChatMessageSenderMeta({
  required String senderName,
  required int createdAt,
  required String senderVisualSeed,
  required bool isMine,
  required double fontScale,
  bool showSenderName = true,
  VoidCallback? onSenderTap,
  VoidCallback? onSenderLongPress,
}) {
  final theme = Get.theme;
  final timeLabel = TimeFormatter.formatChatTime(createdAt);
  return Padding(
    padding: EdgeInsets.only(
      left: isMine ? 0 : ChatView._messageContentHorizontalInset,
      right: isMine ? ChatView._messageContentHorizontalInset : 0,
      top: 4,
      bottom: ChatView._messageSenderMetaBottomSpacing,
    ),
    child: GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTap: onSenderTap,
      onLongPress: onSenderLongPress,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        textDirection: isMine ? TextDirection.rtl : TextDirection.ltr,
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          if (showSenderName) ...[
            Text(
              senderName,
              style: TextStyle(
                fontSize: 12 * fontScale,
                color: isMine
                    ? theme.primaryColor.withValues(alpha: 0.85)
                    : AppTheme.getAvatarColor(senderVisualSeed),
                fontWeight: FontWeight.w500,
              ),
            ),
            if (timeLabel.isNotEmpty) const SizedBox(width: 6),
          ],
          if (timeLabel.isNotEmpty)
            Text(
              timeLabel,
              style: TextStyle(
                fontSize: 10 * fontScale,
                color: theme.colorScheme.secondary.withValues(alpha: 0.5),
              ),
            ),
        ],
      ),
    ),
  );
}

Widget buildChatMessageBubbleWithAvatar({
  required Widget bubble,
  required Widget? senderMeta,
  required bool isMine,
  required int senderType,
  required String senderId,
  required String senderName,
  required String senderAvatarUrl,
  required String senderVisualSeed,
  required bool showAvatar,
  VoidCallback? onSenderTap,
  VoidCallback? onSenderLongPress,
}) {
  final messageContent = Column(
    crossAxisAlignment: isMine
        ? CrossAxisAlignment.end
        : CrossAxisAlignment.start,
    children: [if (senderMeta != null) senderMeta, bubble],
  );
  if (senderType != 1 && senderType != 2) {
    return messageContent;
  }

  final displayName = senderName.trim().isEmpty ? senderId : senderName;
  return Stack(
    clipBehavior: Clip.none,
    children: [
      Padding(
        padding: EdgeInsets.only(
          left: isMine ? 0 : ChatView._messageAvatarContentInset,
          right: isMine ? ChatView._messageAvatarContentInset : 0,
        ),
        child: messageContent,
      ),
      if (showAvatar)
        Positioned(
          left: isMine ? null : ChatView._messageAvatarEdgeInset,
          right: isMine ? ChatView._messageAvatarEdgeInset : null,
          top: 4,
          child: _MessageSenderAvatar(
            avatarUrl: senderAvatarUrl,
            displayName: displayName,
            avatarSeed: senderVisualSeed,
            size: 32,
            onTap: onSenderTap,
            onLongPress: onSenderLongPress,
          ),
        ),
    ],
  );
}

BorderRadius _chatMessageBubbleBorderRadius({required bool isMine}) {
  const rounded = Radius.circular(ChatView._messageBubbleCornerRadius);
  return BorderRadius.only(
    topLeft: isMine ? rounded : Radius.zero,
    topRight: isMine ? Radius.zero : rounded,
    bottomLeft: rounded,
    bottomRight: rounded,
  );
}

Future<void> showChatMessageContextMenu({
  required ChatController controller,
  required BuildContext context,
  required MessageModel msg,
  required bool canRevoke,
  required bool canReply,
  required bool canCopy,
  required bool canForward,
}) async {
  final action = await ChatMessageActionSheet.show(
    context,
    canCopy: canCopy,
    canReply: canReply,
    canRevoke: canRevoke,
    canForward: canForward,
    canSelectMultiple: canForward,
    onForwardLongPress: () {
      Clipboard.setData(ClipboardData(text: msg.msgId));
      CustomToast.show('消息 ID 已复制', isError: false);
    },
  );
  if (action == null) {
    return;
  }
  if (!context.mounted) {
    return;
  }

  switch (action) {
    case ChatMessageAction.forward:
      await forwardChatSingleMessage(
        controller: controller,
        context: context,
        message: msg,
      );
      return;
    case ChatMessageAction.selectMultiple:
      controller.beginForwardSelection(msg);
      return;
    case ChatMessageAction.copy:
      final copyText =
          ChatMessageCardCodec.buildCopyableText(msg.content) ?? msg.content;
      await copyChatMessageRawText(copyText);
      return;
    case ChatMessageAction.reply:
      controller.setReplyingToMessage(msg);
      return;
    case ChatMessageAction.revoke:
      confirmChatMessageRevoke(
        controller: controller,
        context: context,
        msg: msg,
      );
      return;
  }
}

Future<void> copyChatMessageRawText(String rawText) async {
  try {
    await Clipboard.setData(ClipboardData(text: rawText));
    CustomToast.show('chat_copy_success'.tr, isError: false);
  } catch (_) {
    CustomToast.show('common_error'.tr);
  }
}

Future<void> confirmChatMessageRevoke({
  required ChatController controller,
  required BuildContext context,
  required MessageModel msg,
}) async {
  final confirmed = await showAppConfirmDialog(
    context: context,
    title: 'chat_revoke_confirm_title'.tr,
    message: 'chat_revoke_confirm_content'.tr,
    confirmText: 'chat_revoke'.tr,
    isDestructive: true,
  );
  if (confirmed) {
    final isMine =
        msg.senderType == 1 && msg.senderId == controller.authService.userId;
    final originalContent = isMine && msg.msgType == 1 ? msg.content : '';
    controller.revokeMessage(msg.msgId, originalContent: originalContent);
  }
}

Future<void> forwardChatSingleMessage({
  required ChatController controller,
  required BuildContext context,
  required MessageModel message,
}) async {
  if (!controller.canForwardMessage(message)) {
    return;
  }
  await forwardChatMessages(
    controller: controller,
    context: context,
    messages: <MessageModel>[message],
    mode: ChatForwardDispatchMode.separate,
    clearSelectionOnSuccess: false,
  );
}

Future<void> forwardChatSelectedMessages({
  required ChatController controller,
  required BuildContext context,
  required ChatForwardDispatchMode mode,
}) async {
  final selectedMessages = controller.collectSelectedForwardMessages();
  if (selectedMessages.isEmpty) {
    CustomToast.show('chat_forward_select_hint'.tr);
    return;
  }
  await forwardChatMessages(
    controller: controller,
    context: context,
    messages: selectedMessages,
    mode: mode,
    clearSelectionOnSuccess: true,
  );
}

Future<void> forwardChatMessages({
  required ChatController controller,
  required BuildContext context,
  required List<MessageModel> messages,
  required ChatForwardDispatchMode mode,
  required bool clearSelectionOnSuccess,
}) async {
  final agentDraft = controller.buildForwardAgentDraft(messages);
  final target = await pickChatForwardTarget(
    controller,
    context,
    onSendToAgent: agentDraft.isEmpty
        ? null
        : () {
            unawaited(
              showSendMessageToAgentDialog(
                context,
                initialMessage: agentDraft,
              ),
            );
          },
  );
  if (target == null) {
    return;
  }
  if (!context.mounted) {
    return;
  }

  try {
    final sentCount = await controller.forwardMessages(
      messages: messages,
      targetSessionId: target.sessionId,
      mode: mode,
    );
    if (!context.mounted) {
      return;
    }
    if (sentCount <= 0) {
      CustomToast.show('chat_forward_failed'.tr);
      return;
    }

    if (clearSelectionOnSuccess) {
      controller.exitForwardSelectionMode();
    }
    CustomToast.show(
      'chat_forward_queued'.trParams({'count': '$sentCount'}),
      isError: false,
    );
  } catch (_) {
    if (!context.mounted) {
      return;
    }
    CustomToast.show('chat_forward_failed'.tr);
  }
}

Future<ChatForwardTargetOption?> pickChatForwardTarget(
  ChatController controller,
  BuildContext context, {
  VoidCallback? onSendToAgent,
}) async {
  final options = controller.buildForwardTargetOptions();
  if (options.isEmpty) {
    CustomToast.show('chat_forward_no_target'.tr);
    return null;
  }

  return ChatForwardTargetPickerSheet.show(
    context,
    options: options,
    onSendToAgent: onSendToAgent,
  );
}

/// 会话卡片转发 sheet 上 "+" 入口使用：把会话卡片信息整理成
/// "发给 Agent"对话框的预填文本（卡片 JSON 不适合直接给 Agent 读）。
String buildChatConversationCardAgentDraft(ChatController controller) {
  final lines = <String>[
    '会话卡片：',
    '名称：${controller.displayChatTitle}',
    '会话 ID：${controller.sessionId}',
    '类型：${controller.isGroupChat ? '群聊' : '私聊'}',
  ];
  return lines.join('\n');
}

Future<int> forwardChatConversationCard({
  required ChatController controller,
  required BuildContext context,
  required String targetSessionId,
  String accompanyingMessage = '',
}) async {
  if (!context.mounted) {
    return 0;
  }

  final sentCount = await controller.forwardConversationCard(
    targetSessionId: targetSessionId,
    accompanyingMessage: accompanyingMessage,
  );
  if (!context.mounted) {
    return 0;
  }
  if (sentCount <= 0) {
    CustomToast.show('chat_forward_failed'.tr);
    return 0;
  }

  CustomToast.show(
    'chat_forward_queued'.trParams({'count': '$sentCount'}),
    isError: false,
  );
  return sentCount;
}

Widget buildChatSystemMessageItem(
  ChatController controller,
  BuildContext context, {
  required MessageModel msg,
  required double fontScale,
}) {
  final theme = Theme.of(context);
  final viewportWidth = MediaQuery.sizeOf(context).width;
  final displayContent = controller.formatMessageContentForDisplay(msg.content);
  return Padding(
    key: ValueKey('sys:${msg.msgId}'),
    padding: const EdgeInsets.symmetric(vertical: 8),
    child: Center(
      child: Container(
        constraints: BoxConstraints(maxWidth: viewportWidth * 0.82),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        decoration: BoxDecoration(
          color: theme.colorScheme.outline.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(999),
        ),
        child: Text(
          displayContent,
          textAlign: TextAlign.center,
          style: TextStyle(
            fontSize: 12 * fontScale,
            color: theme.colorScheme.secondary.withValues(alpha: 0.9),
            fontWeight: FontWeight.w500,
          ),
        ),
      ),
    ),
  );
}

class _MessageSenderAvatar extends StatelessWidget {
  const _MessageSenderAvatar({
    required this.avatarUrl,
    required this.displayName,
    required this.avatarSeed,
    required this.size,
    this.onTap,
    this.onLongPress,
  });

  final String avatarUrl;
  final String displayName;
  final String avatarSeed;
  final double size;
  final VoidCallback? onTap;
  final VoidCallback? onLongPress;

  @override
  Widget build(BuildContext context) {
    Widget withHitArea(Widget child) {
      final tapTarget = SizedBox.square(
        dimension: size + 12,
        child: Align(alignment: Alignment.topCenter, child: child),
      );
      if (onTap == null && onLongPress == null) {
        return tapTarget;
      }
      return GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTap: onTap,
        onLongPress: onLongPress,
        child: tapTarget,
      );
    }

    final borderRadius = AppTheme.listAvatarCornerRadius(size);
    final fallback = SessionAvatar(
      isGroup: false,
      avatarTitle: displayName,
      avatarColor: AppTheme.getAvatarColor(avatarSeed),
      size: size,
      borderRadius: borderRadius,
    );

    final normalizedAvatarUrl = avatarUrl.trim();
    if (normalizedAvatarUrl.isEmpty) {
      return withHitArea(fallback);
    }

    final avatar = ClipRRect(
      borderRadius: BorderRadius.circular(borderRadius),
      child: SizedBox(
        width: size,
        height: size,
        child: AvatarNetworkImage(
          avatarUrl: normalizedAvatarUrl,
          fallback: fallback,
        ),
      ),
    );

    return withHitArea(avatar);
  }
}

class _StagedAttachmentPreviewStrip extends StatelessWidget {
  const _StagedAttachmentPreviewStrip({
    required this.attachments,
    required this.onRemove,
    this.onEditImage,
  });

  final List<PendingAttachmentUpload> attachments;
  final void Function(int index) onRemove;
  final Future<void> Function(int index)? onEditImage;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(maxHeight: 88),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          children: [
            for (var i = 0; i < attachments.length; i++)
              Padding(
                padding: const EdgeInsets.only(right: 8),
                child: _StagedAttachmentThumbnail(
                  attachment: attachments[i],
                  onRemove: () => onRemove(i),
                  onTap: () => _handleTap(context, i),
                ),
              ),
          ],
        ),
      ),
    );
  }

  void _handleTap(BuildContext context, int index) {
    final attachment = attachments[index];
    if (attachment.isImage) {
      _openImagePreview(context, index);
    }
  }

  void _openImagePreview(BuildContext context, int index) {
    final attachment = attachments[index];
    final bytes = attachment.bytes;
    if (bytes == null) {
      return;
    }
    showDialog<void>( // dialog-guard-allow: 图片预览（范围外）
      context: context,
      useSafeArea: false,
      barrierColor: Colors.black.withValues(alpha: 0.92),
      builder: (dialogContext) => SizedBox.expand(
        child: Material(
          color: Colors.black,
          child: SafeArea(
            child: Stack(
              children: [
                Positioned.fill(
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(12, 60, 12, 16),
                    child: InteractiveViewer(
                      minScale: 0.5,
                      maxScale: 4.0,
                      child: Center(
                        child: CheckerboardBackedImage(
                          image: MemoryImage(bytes),
                          errorBuilder: (_) => const Icon(
                            Icons.broken_image_outlined,
                            color: Colors.white54,
                            size: 48,
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
                Positioned(
                  top: 8,
                  left: 12,
                  child: _previewActionButton(
                    tooltip: '编辑图片',
                    onPressed: () async {
                      Navigator.of(dialogContext).pop();
                      await onEditImage?.call(index);
                    },
                    child: const Icon(Icons.edit_outlined),
                  ),
                ),
                Positioned(
                  top: 8,
                  right: 12,
                  child: _previewActionButton(
                    tooltip: '关闭预览',
                    onPressed: () => Navigator.of(dialogContext).pop(),
                    child: const Icon(Icons.close_rounded),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  static Widget _previewActionButton({
    required String tooltip,
    required VoidCallback onPressed,
    required Widget child,
  }) {
    return Container(
      decoration: BoxDecoration(
        color: Colors.black.withValues(alpha: 0.4),
        shape: BoxShape.circle,
      ),
      child: IconButton(
        tooltip: tooltip,
        onPressed: onPressed,
        color: Colors.white,
        iconSize: 22,
        icon: child,
      ),
    );
  }
}

class _StagedAttachmentThumbnail extends StatelessWidget {
  const _StagedAttachmentThumbnail({
    required this.attachment,
    required this.onRemove,
    this.onTap,
  });

  final PendingAttachmentUpload attachment;
  final VoidCallback onRemove;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    const size = 72.0;
    return Stack(
      clipBehavior: Clip.none,
      children: [
        GestureDetector(
          onTap: onTap,
          child: Container(
            width: size,
            height: size,
            decoration: BoxDecoration(
              borderRadius: BorderRadius.circular(8),
              color: const Color(0xFFF2F3F5),
              border: Border.all(color: const Color(0xFFE2E5E9)),
            ),
            clipBehavior: Clip.hardEdge,
            child: attachment.isImage && attachment.bytes != null
                ? Image.memory(
                    attachment.bytes!,
                    fit: BoxFit.cover,
                    errorBuilder: (_, __, ___) => const Center(
                      child: Icon(
                        Icons.broken_image_outlined,
                        color: Color(0xFFB0B8C4),
                      ),
                    ),
                  )
                : Center(
                    child: Column(
                      mainAxisAlignment: MainAxisAlignment.center,
                      children: [
                        Icon(
                          attachment.isVideo
                              ? Icons.videocam_outlined
                              : Icons.insert_drive_file_outlined,
                          size: 28,
                          color: const Color(0xFF5A6472),
                        ),
                        const SizedBox(height: 4),
                        Padding(
                          padding: const EdgeInsets.symmetric(horizontal: 4),
                          child: Text(
                            attachment.fileName,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: const TextStyle(
                              fontSize: 10,
                              color: Color(0xFF5A6472),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
          ),
        ),
        Positioned(
          top: -6,
          right: -6,
          child: GestureDetector(
            onTap: onRemove,
            child: Container(
              width: 20,
              height: 20,
              decoration: BoxDecoration(
                color: Colors.black.withValues(alpha: 0.5),
                shape: BoxShape.circle,
              ),
              alignment: Alignment.center,
              child: const Icon(Icons.close, size: 12, color: Colors.white),
            ),
          ),
        ),
      ],
    );
  }
}

class _InputFieldScrollBehavior extends MaterialScrollBehavior {
  const _InputFieldScrollBehavior();

  @override
  Set<PointerDeviceKind> get dragDevices => const <PointerDeviceKind>{
    PointerDeviceKind.touch,
    PointerDeviceKind.stylus,
    PointerDeviceKind.invertedStylus,
    PointerDeviceKind.trackpad,
  };
}
