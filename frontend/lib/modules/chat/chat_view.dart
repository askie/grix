import 'dart:async';
import 'dart:io';

import 'package:desktop_drop/desktop_drop.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'controllers/chat_controller.dart';
import '../../app/scroll/horizontal_drag_scroll_behavior.dart';
import '../../app/settings/chat_background_service.dart';
import '../../app/settings/chat_font_size_service.dart';
import '../../app/settings/chat_skill_usage_service.dart';
import '../../data/models/message_model.dart';
import '../../data/models/agent_toolbar_model.dart';
import '../../data/providers/im_service.dart';
import '../../data/providers/feature_flag_service.dart';
import '../../modules/call/call_controller.dart';
import 'message_cards/models/chat_message_card_data.dart';
import 'message_cards/services/chat_message_card_codec.dart';
import 'models/chat_forward_dispatch_mode.dart';
import 'models/chat_forward_target_option.dart';
import 'models/chat_message_identity.dart';
import 'models/chat_message_list_snapshot.dart';
import '../../shared/widgets/message_bubble.dart';
import '../../shared/widgets/avatar_network_image.dart';
import '../../shared/models/session_avatar_member.dart';
import '../../shared/widgets/session_avatar.dart';
import '../../shared/widgets/session_activity_indicator.dart';
import '../../app/themes/app_theme.dart';
import '../../shared/utils/chat_message_preview.dart';
import '../../shared/utils/sheet_guard.dart';
import '../../shared/utils/time_formatter.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/services/native_clipboard_service.dart';
import '../../shared/widgets/app_dialog_style.dart';
import '../../shared/widgets/circular_progress_button.dart';
import '../../shared/widgets/transparency_checkerboard.dart';
import '../../shared/widgets/progress_detail_bottom_sheet.dart';
import 'rate_limit_ring_math.dart';
import 'widgets/chat_attachment_menu.dart';
import 'widgets/chat_quick_bind_directory_panel.dart';
import 'widgets/chat_attachment_source_sheet.dart';
import 'widgets/chat_forward_selection_action_bar.dart';
import 'widgets/chat_forward_target_picker_sheet.dart';
import 'widgets/chat_message_action_sheet.dart';
import 'widgets/chat_retry_action_button.dart';
import 'widgets/chat_selectable_message_bubble.dart';
import 'widgets/chat_voice_command_button.dart';
import 'widgets/conversation_audit_detail_page.dart';
import 'widgets/group_chat_qr_view.dart';
import 'widgets/send_message_to_agent_dialog.dart';
import 'widgets/webhook_manager_dialog.dart';
import 'services/chat_agent_path_opener.dart';
import 'services/chat_input_bottom_inset_resolver.dart';
import 'services/chat_message_owner_classifier.dart';
import 'services/agent_remote_file_node_mapper.dart';
import '../../shared/widgets/remote_file_picker/remote_file_picker.dart';
import '../../shared/widgets/agent_session_list/agent_session_list.dart';
import '../../data/providers/user_favorite_path_service.dart';
import '../../data/providers/user_session_favorite_service.dart';
import '../../platform/platform_capability.dart';
import '../home/controllers/conversations_controller.dart';

part 'chat_view_message_sections.dart';
part 'chat_view_action_sheets.dart';
part 'chat_view_expanded_input.dart';

class _ChatViewDebugBuildCounter {
  const _ChatViewDebugBuildCounter._();

  static final Map<String, int> _counts = <String, int>{};

  static void hit(String key) {
    assert(() {
      _counts[key] = (_counts[key] ?? 0) + 1;
      return true;
    }());
  }

  static int read(String key) => _counts[key] ?? 0;

  static void reset() {
    _counts.clear();
  }
}

class _ChatInitialMessageRenderWarmupScheduler {
  static const Duration _warmupDelay = Duration(milliseconds: 96);

  static Timer? _timer;
  static String _scheduledSignature = '';
  static String _scheduledSessionId = '';

  static void schedule({
    required ChatController controller,
    required List<MessageModel> messages,
    required int maxEntries,
  }) {
    if (maxEntries <= 0) {
      return;
    }

    final candidates = <String>[];
    final candidateSignatures = <String>[];
    for (var i = messages.length - 1; i >= 0; i--) {
      if (candidates.length >= maxEntries) {
        break;
      }
      final message = messages[i];
      if (message.msgType == 3) {
        continue;
      }
      if (controller.imService.isMessageStreaming(message.msgId)) {
        continue;
      }
      if (!MessageBubble.isFinalRenderPrecacheEligible(message.content)) {
        continue;
      }
      final displayContent = controller.formatMessageContentForDisplay(
        message.content,
      );
      if (displayContent.trim().isEmpty) {
        continue;
      }
      if (MessageBubble.hasCachedFinalRenderState(displayContent)) {
        continue;
      }
      candidates.add(displayContent);
      candidateSignatures.add(
        '${message.msgId}:${identityHashCode(message)}:${displayContent.length}',
      );
    }

    if (candidates.isEmpty) {
      _clear();
      return;
    }

    final signature =
        '${controller.sessionId}\u0000${candidateSignatures.join('\u0001')}';
    if (signature == _scheduledSignature) {
      return;
    }

    _timer?.cancel();
    _scheduledSignature = signature;
    _scheduledSessionId = controller.sessionId;
    _timer = Timer(_warmupDelay, () {
      _timer = null;
      final activeSignature = _scheduledSignature;
      _clear();
      if (activeSignature != signature) {
        return;
      }
      MessageBubble.precacheFinalRenderStates(
        candidates,
        maxEntries: maxEntries,
      );
    });
  }

  static void cancelForSession(String sessionId) {
    if (_scheduledSessionId != sessionId) {
      return;
    }
    _clear();
  }

  @visibleForTesting
  static void resetForTest() {
    _clear();
  }

  static void _clear() {
    _timer?.cancel();
    _timer = null;
    _scheduledSignature = '';
    _scheduledSessionId = '';
  }
}

@visibleForTesting
void resetChatInitialMessageRenderWarmupSchedulerForTest() {
  _ChatInitialMessageRenderWarmupScheduler.resetForTest();
}

@visibleForTesting
void resetChatViewDebugBuildCounterForTest() {
  _ChatViewDebugBuildCounter.reset();
}

@visibleForTesting
int chatViewDebugBuildCountForTest(String key) {
  return _ChatViewDebugBuildCounter.read(key);
}

// ignore: must_be_immutable
class ChatView extends GetView<ChatController> {
  ChatView({super.key, this.controllerTag, this.embedded = false});

  final String? controllerTag;

  /// True when hosted in the desktop chat pane: no back button, the
  /// conversation list stays visible on the left.
  final bool embedded;

  ChatController? _cachedController;

  @override
  String? get tag => controllerTag;

  @override
  ChatController get controller =>
      _cachedController ??= GetInstance().find<ChatController>(tag: tag);

  static const double _messageAvatarSize = 32;
  static const double _messageAvatarGap = 10;
  static const double _messageAvatarHitAreaInset = 6;
  static const double _messageAvatarContentInset =
      _messageAvatarSize + _messageAvatarGap + _messageAvatarHitAreaInset;
  static const double _messageAvatarEdgeInset = 4;
  static const double _messageContentHorizontalInset = 4;
  static const double _messageSenderMetaBottomSpacing = 1;
  static const double _messageBubbleTopMargin = 4;
  static const double _messageBubbleTopMarginWithSender = 1;
  static const double _messageBubbleVerticalMargin = 4;
  static const double _messageBubbleCornerRadius = 12;

  void _dismissKeyboard() {
    controller.dismissInputInteraction();
  }

  double _scaleFont(double base, double fontScale) {
    return base * fontScale;
  }

  @override
  Widget build(BuildContext context) {
    controller.bindFlutterView(View.of(context));
    final chatFontSizeService = Get.isRegistered<ChatFontSizeService>()
        ? Get.find<ChatFontSizeService>()
        : null;

    return _ChatWarmupLifecycleScope(
      sessionId: controller.sessionId,
      child: _ChatBackNavigationScope(
        controller: controller,
        child: Scaffold(
          // 路由 opaque:false 阻止下层会话列表被遮挡剔除以消除返回白屏,
          // 这里显式锁定 Scaffold 背景为主题不透明色,确保聊天页视觉上仍完全遮盖下层。
          backgroundColor: Theme.of(context).scaffoldBackgroundColor,
          resizeToAvoidBottomInset: false,
          appBar: PreferredSize(
            preferredSize: Size.fromHeight(
              Theme.of(context).appBarTheme.toolbarHeight ?? 44,
            ),
            child: Obx(() {
              _ChatViewDebugBuildCounter.hit('app_bar_scope_obx');
              final chatFontScale = chatFontSizeService?.scale ?? 1.0;
              if (controller.isForwardSelectionMode) {
                return _buildForwardSelectionAppBar(context);
              }
              return _buildDefaultChatAppBar(
                context: context,
                isGroup: controller.isGroupChat,
                fontScale: chatFontScale,
              );
            }),
          ),
          body: DropTarget(
            // Web 端覆盖层显隐改由 ChatFileInterceptor 按"是否真的拖入文件"驱动，
            // 这里不再直接置位，避免拖链接/选中文字误触且松手卡住；桌面端仍用
            // desktop_drop 的原生回调。
            onDragEntered: (_) {
              if (!kIsWeb) controller.isFileDragOver.value = true;
            },
            onDragExited: (_) {
              if (!kIsWeb) controller.isFileDragOver.value = false;
            },
            onDragDone: (details) {
              if (!kIsWeb) controller.isFileDragOver.value = false;
              _handleDroppedFiles(controller, details.files);
            },
            child: Stack(
              children: [
                const _ChatBackgroundDecoration(),
                Column(
                  children: [
                    _ChatDelegateBanner(controller: controller),
                    // TODO: 语音托管横幅暂时隐藏，代码保留
                    // _ChatVoiceDelegateBanner(controller: controller),
                    _ChatVisitorInfoBanner(controller: controller),
                    Expanded(
                      child: Stack(
                        children: [
                          _ChatMessageListSection(controller: controller),
                          Obx(() {
                            if (!controller.isAttachmentMenuOpen.value) {
                              return const SizedBox.shrink();
                            }
                            return GestureDetector(
                              behavior: HitTestBehavior.opaque,
                              onTap: _dismissKeyboard,
                            );
                          }),
                        ],
                      ),
                    ),
                    _ChatBottomDockSection(
                      controller: controller,
                      onDismissKeyboard: _dismissKeyboard,
                    ),
                  ],
                ),
                _ChatDragOverlay(controller: controller),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _handleDroppedFiles(
    ChatController controller,
    List<DropItem> files,
  ) async {
    for (final file in files) {
      if (file is DropItemDirectory) continue;
      try {
        final bytes = await file.readAsBytes();
        if (bytes.isEmpty) continue;
        final fileName = file.name;
        final contentType = file.mimeType ?? 'application/octet-stream';
        await controller.stageFileFromBytes(
          bytes: bytes,
          fileName: fileName,
          contentType: contentType,
        );
      } catch (e) {
        debugPrint('handleDroppedFile error: $e');
      }
    }
  }

  AppBar _buildForwardSelectionAppBar(BuildContext context) {
    return AppBar(
      leading: IconButton(
        icon: const Icon(Icons.close_rounded),
        onPressed: controller.exitForwardSelectionMode,
      ),
      title: Obx(() {
        _ChatViewDebugBuildCounter.hit('forward_appbar_title_obx');
        return Text(
          'chat_forward_selected_count'.trParams({
            'count': controller.selectedForwardMessageCount.toString(),
          }),
        );
      }),
      actions: [
        TextButton(
          onPressed: controller.exitForwardSelectionMode,
          child: Text('common_cancel'.tr),
        ),
      ],
    );
  }

  AppBar _buildDefaultChatAppBar({
    required BuildContext context,
    required bool isGroup,
    required double fontScale,
  }) {
    final theme = Theme.of(context);
    final avatarColor = AppTheme.getAvatarColor(
      controller.headerAvatarColorSeed,
    );
    return AppBar(
      automaticallyImplyLeading: false,
      leading: embedded
          ? null
          : IconButton(
              icon: const Icon(Icons.arrow_back_ios_rounded, size: 20),
              onPressed: controller.closeChatRoute,
            ),
      titleSpacing: embedded ? 16 : 0,
      title: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTap: () {
          _dismissKeyboard();
          controller.scrollToLoadedTop(animated: true);
        },
        child: Row(
          children: [
            Obx(() {
              if (!controller.shouldShowHeaderAvatar) {
                return const SizedBox.shrink();
              }
              return Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  GestureDetector(
                    behavior: HitTestBehavior.opaque,
                    onTap: controller.onHeaderAvatarTap,
                    child: SessionAvatar(
                      isGroup: isGroup,
                      avatarTitle: controller.headerAvatarTitle,
                      avatarColor: avatarColor,
                      avatarUrl: isGroup ? '' : controller.privatePeerAvatarUrl,
                      members: controller.groupAvatarMembers,
                      size: 36,
                      borderRadius: 5,
                    ),
                  ),
                  const SizedBox(width: 10),
                ],
              );
            }),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Obx(
                    () => Text(
                      controller.displayChatTitle,
                      style: TextStyle(
                        fontSize: _scaleFont(15, fontScale),
                        fontWeight: FontWeight.w600,
                      ),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  if (isGroup)
                    Obx(() {
                      final subtitle = controller.chatSubtitle.trim();
                      if (subtitle.isEmpty) {
                        return const SizedBox.shrink();
                      }
                      return Text(
                        subtitle,
                        style: TextStyle(
                          fontSize: _scaleFont(11, fontScale),
                          color: controller.isChatSubtitleOnline
                              ? AppTheme.successColor
                              : controller.isChatSubtitleOffline
                              ? AppTheme.errorColor
                              : Theme.of(context).textTheme.bodySmall?.color ??
                                    AppTheme.lightTextSecondary,
                          fontWeight: FontWeight.w400,
                        ),
                      );
                    }),
                ],
              ),
            ),
          ],
        ),
      ),
      actions: [
        // 会话有 AI 代接中的通话时显示"通话"入口（点击进入旁听）
        if (Get.isRegistered<CallController>() &&
            Get.find<FeatureFlagService>().isEnabled('voice_call'))
          Obx(() {
            final sessionId = controller.sessionId;
            final hasVoice = Get.find<CallController>().hasVoiceCallForSession(
              sessionId,
            );
            if (!hasVoice) return const SizedBox.shrink();
            return IconButton(
              icon: const Icon(Icons.mic, size: 22, color: Colors.blueAccent),
              tooltip: 'call_listen_entry'.tr,
              onPressed: () {
                final im = Get.find<ImService>();
                // 打开通话窗口并停在「待命」档，由用户在四档选择器里选择参与方式
                Get.find<CallController>().openStandby(
                  sessionId,
                  im.sendCallPacket,
                );
              },
            );
          }),
        Obx(() {
          final state =
              controller.imService.delegateStates[controller.sessionId];
          if (state == null) return const SizedBox.shrink();
          final open = controller.delegatePanelOpen.value;
          return IconButton(
            icon: Icon(
              Icons.smart_toy_rounded,
              size: 22,
              color: open
                  ? theme.primaryColor
                  : theme.primaryColor.withValues(alpha: 0.55),
            ),
            onPressed: () => controller.delegatePanelOpen.value =
                !controller.delegatePanelOpen.value,
          );
        }),
        IconButton(
          icon: const Icon(Icons.more_vert_rounded, size: 22),
          onPressed: () =>
              showChatMenu(controller, context, fontScale: fontScale),
        ),
      ],
    );
  }
}

class _ChatBackNavigationScope extends StatelessWidget {
  const _ChatBackNavigationScope({
    required this.controller,
    required this.child,
  });

  final ChatController controller;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      _ChatViewDebugBuildCounter.hit('root_obx');
      final isForwardSelectionMode = controller.isForwardSelectionMode;
      final interceptSystemBack = kIsWeb && !isForwardSelectionMode;
      return PopScope<Object?>(
        canPop: !isForwardSelectionMode && !interceptSystemBack,
        onPopInvokedWithResult: (didPop, _) {
          if (didPop) {
            controller.persistDraftImmediately();
            return;
          }
          if (isForwardSelectionMode) {
            controller.exitForwardSelectionMode();
            return;
          }
          if (interceptSystemBack) {
            controller.closeChatRoute();
          }
        },
        child: child,
      );
    });
  }
}

// ---------------------------------------------------------------------------
// 渲染隔离 Widget：背景装饰
// ---------------------------------------------------------------------------

class _ChatBackgroundDecoration extends StatelessWidget {
  const _ChatBackgroundDecoration();

  @override
  Widget build(BuildContext context) {
    final brightness = Theme.of(context).brightness;
    final chatBackgroundService = Get.isRegistered<ChatBackgroundService>()
        ? Get.find<ChatBackgroundService>()
        : null;
    // 如果服务未注册，直接使用默认背景，无需响应式监听
    if (chatBackgroundService == null) {
      return Positioned.fill(
        child: Container(
          decoration: _buildDecoration(
            ChatBackgroundStyle.defaultStyle,
            brightness,
          ),
        ),
      );
    }
    return Obx(() {
      _ChatViewDebugBuildCounter.hit('background_obx');
      final style = chatBackgroundService.style;
      return Positioned.fill(
        child: Container(decoration: _buildDecoration(style, brightness)),
      );
    });
  }

  static BoxDecoration _buildDecoration(
    ChatBackgroundStyle style,
    Brightness brightness,
  ) {
    final resolvedColor = style.resolveColor(brightness);
    if (!style.hasImage) {
      return BoxDecoration(color: resolvedColor);
    }
    return BoxDecoration(
      color: resolvedColor,
      image: DecorationImage(
        image: NetworkImage(style.imageUrl),
        fit: BoxFit.cover,
        onError: (_, __) {},
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// 渲染隔离 Widget：Delegate 状态横幅
// ---------------------------------------------------------------------------

class _ChatDelegateBanner extends StatelessWidget {
  const _ChatDelegateBanner({required this.controller});

  final ChatController controller;

  double _scaleFont(double base, double fontScale) => base * fontScale;

  @override
  Widget build(BuildContext context) {
    final chatFontSizeService = Get.isRegistered<ChatFontSizeService>()
        ? Get.find<ChatFontSizeService>()
        : null;
    final theme = Theme.of(context);

    return Obx(() {
      _ChatViewDebugBuildCounter.hit('delegate_banner_obx');
      final fontScale = chatFontSizeService?.scaleRx.value ?? 1.0;
      final state = controller.imService.delegateStates[controller.sessionId];
      if (state == null || !controller.delegatePanelOpen.value) {
        return const SizedBox.shrink();
      }
      final channelUnavailable = state['channel_unavailable'] == true;
      return Container(
        width: double.infinity,
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
        color: theme.primaryColor.withValues(alpha: 0.1),
        child: Column(
          children: [
            if (channelUnavailable) ...[
              Row(
                children: [
                  const Icon(
                    Icons.error_outline_rounded,
                    size: 14,
                    color: AppTheme.errorColor,
                  ),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      'ai_delegate_channel_unavailable'.tr,
                      style: TextStyle(
                        fontSize: _scaleFont(11, fontScale),
                        color: AppTheme.errorColor,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
            ],
            Row(
              children: [
                Text(
                  'ai_delegate_rounds'.tr,
                  style: TextStyle(
                    fontSize: _scaleFont(11, fontScale),
                    color: theme.colorScheme.secondary.withValues(alpha: 0.75),
                    fontWeight: FontWeight.w500,
                  ),
                ),
                const SizedBox(width: 8),
                buildChatRoundControlButton(
                  icon: Icons.remove_rounded,
                  onTap: controller.decreaseDelegateRounds,
                ),
                Container(
                  width: 42,
                  height: 28,
                  margin: const EdgeInsets.symmetric(horizontal: 6),
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    color: theme.colorScheme.surface,
                    borderRadius: BorderRadius.circular(6),
                  ),
                  child: Text(
                    '${controller.delegateRoundsDraft}',
                    style: TextStyle(
                      fontSize: _scaleFont(12, fontScale),
                      color: theme.colorScheme.onSurface,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
                buildChatRoundControlButton(
                  icon: Icons.add_rounded,
                  onTap: controller.increaseDelegateRounds,
                ),
                const Spacer(),
                TextButton(
                  onPressed: controller.delegateRoundsDirty
                      ? controller.saveDelegateRounds
                      : null,
                  style: TextButton.styleFrom(
                    foregroundColor: theme.primaryColor,
                    disabledForegroundColor: theme.colorScheme.secondary
                        .withValues(alpha: 0.35),
                    minimumSize: const Size(56, 30),
                    padding: const EdgeInsets.symmetric(horizontal: 10),
                  ),
                  child: Text('common_save'.tr),
                ),
                const SizedBox(width: 4),
                GestureDetector(
                  onTap: controller.stopDelegate,
                  child: Text(
                    'ai_delegate_stop'.tr,
                    style: TextStyle(
                      fontSize: _scaleFont(12, fontScale),
                      color: AppTheme.errorColor,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
              ],
            ),
          ],
        ),
      );
    });
  }
}

// ---------------------------------------------------------------------------
// 渲染隔离 Widget：消息列表区域
// ---------------------------------------------------------------------------

class _ChatMessageListSection extends StatefulWidget {
  const _ChatMessageListSection({required this.controller});

  final ChatController controller;

  @override
  State<_ChatMessageListSection> createState() =>
      _ChatMessageListSectionState();
}

class _ChatVisitorInfoBanner extends StatelessWidget {
  const _ChatVisitorInfoBanner({required this.controller});

  final ChatController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Obx(() {
      if (!controller.isVisitorSession) {
        return const SizedBox.shrink();
      }
      final siteName = controller.visitorSiteName;
      final visitorName = controller.visitorName;
      final visitorEmail = controller.visitorEmail;
      final lastPage = controller.visitorLastPageUrl;
      final parts = <String>[];
      if (siteName.isNotEmpty) {
        parts.add('chat_visitor_banner_site'.trParams({'value': siteName}));
      }
      if (visitorName.isNotEmpty) {
        parts.add(
          'chat_visitor_banner_visitor'.trParams({'value': visitorName}),
        );
      } else if (visitorEmail.isNotEmpty) {
        parts.add(
          'chat_visitor_banner_visitor'.trParams({'value': visitorEmail}),
        );
      }
      if (lastPage.isNotEmpty) {
        parts.add('chat_visitor_banner_page'.trParams({'value': lastPage}));
      }
      if (parts.isEmpty) {
        return const SizedBox.shrink();
      }
      return Container(
        width: double.infinity,
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        color: theme.primaryColor.withValues(alpha: 0.08),
        child: Text(
          parts.join('  ·  '),
          style: TextStyle(
            fontSize: 12,
            color: theme.colorScheme.onSurface.withValues(alpha: 0.78),
            fontWeight: FontWeight.w500,
          ),
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
        ),
      );
    });
  }
}

class _ChatMessageListSectionState extends State<_ChatMessageListSection> {
  static const double _messageListHorizontalPadding = 4;
  static const double _messageAvatarSize = 32;
  static const double _messageAvatarGap = 10;
  static const double _messageAvatarHitAreaInset = 6;
  static const double _messageAvatarContentInset =
      _messageAvatarSize + _messageAvatarGap + _messageAvatarHitAreaInset;
  static const double _messageListCacheExtent = 600;
  static const double _historyTopStatusSlotHeight = 28;
  static const int _initialMessageRenderCacheCount = 10;

  int _cachedRevision = -1;
  bool _cachedIsGroup = false;
  double _cachedFontScale = -1;
  SliverChildBuilderDelegate? _cachedDelegate;

  ChatController get controller => widget.controller;

  double _scaleFont(double base, double fontScale) => base * fontScale;

  void _scheduleInitialMessageRenderWarmup(List<MessageModel> messages) {
    _ChatInitialMessageRenderWarmupScheduler.schedule(
      controller: controller,
      messages: messages,
      maxEntries: _initialMessageRenderCacheCount,
    );
  }

  @override
  Widget build(BuildContext context) {
    final chatFontSizeService = Get.isRegistered<ChatFontSizeService>()
        ? Get.find<ChatFontSizeService>()
        : null;
    final theme = Theme.of(context);

    return Obx(() {
      _ChatViewDebugBuildCounter.hit('message_list_obx');
      final fontScale = chatFontSizeService?.scaleRx.value ?? 1.0;
      final revision = controller.messageListSnapshotRevision;
      final isGroup = controller.isGroupChat;
      final snapshot = controller.messageListSnapshot;
      final msgs = snapshot.messages;

      if (msgs.isEmpty) {
        _cachedDelegate = null;
        _cachedRevision = -1;
        return _buildEmptyOrSessionActivityState(context, fontScale: fontScale);
      }

      // 仅在 revision、isGroup 或 fontScale 变化时重建 delegate
      if (_cachedDelegate == null ||
          revision != _cachedRevision ||
          isGroup != _cachedIsGroup ||
          fontScale != _cachedFontScale) {
        _cachedRevision = revision;
        _cachedIsGroup = isGroup;
        _cachedFontScale = fontScale;
        _cachedDelegate = _buildDelegate(
          snapshot: snapshot,
          isGroup: isGroup,
          fontScale: fontScale,
          theme: theme,
          context: context,
        );
      }

      _scheduleInitialMessageRenderWarmup(msgs);

      return Listener(
        behavior: HitTestBehavior.translucent,
        onPointerSignal: (event) {
          if (event is! PointerScrollEvent) return;
          controller.onPointerSignalScroll();
        },
        child: NotificationListener<UserScrollNotification>(
          onNotification: (notification) {
            if (!controller.scrollController.hasClients ||
                notification.depth != 0) {
              return false;
            }
            if (notification.direction == ScrollDirection.idle) {
              controller.onUserScrollInteractionReset();
            }
            return false;
          },
          child: NotificationListener<ScrollStartNotification>(
            onNotification: (notification) {
              if (!controller.scrollController.hasClients ||
                  notification.depth != 0) {
                return false;
              }
              if (notification.dragDetails == null) return false;
              controller.onUserScrollStart(notification.metrics);
              return false;
            },
            child: NotificationListener<ScrollUpdateNotification>(
              onNotification: (notification) {
                if (!controller.scrollController.hasClients ||
                    notification.depth != 0) {
                  return false;
                }
                if (notification.dragDetails == null) return false;
                controller.onUserScrollActive(notification.metrics);
                return false;
              },
              child: NotificationListener<ScrollEndNotification>(
                onNotification: (notification) {
                  if (!controller.scrollController.hasClients ||
                      notification.depth != 0) {
                    return false;
                  }
                  if (notification.dragDetails == null) return false;
                  controller.onUserScrollEnd(notification.metrics);
                  return false;
                },
                child: NotificationListener<ScrollMetricsNotification>(
                  onNotification: (notification) {
                    if (!controller.scrollController.hasClients) return false;
                    if (notification.depth != 0) return false;
                    controller.onScrollMetricsChanged(notification.metrics);
                    return false;
                  },
                  child: _KeyboardAwareMessageList(
                    controller: controller,
                    scrollController: controller.scrollController,
                    cacheExtent: _messageListCacheExtent,
                    horizontalPadding: _messageListHorizontalPadding,
                    delegate: _cachedDelegate!,
                  ),
                ),
              ),
            ),
          ),
        ),
      );
    });
  }

  SliverChildBuilderDelegate _buildDelegate({
    required ChatMessageListSnapshot snapshot,
    required bool isGroup,
    required double fontScale,
    required ThemeData theme,
    required BuildContext context,
  }) {
    final msgs = snapshot.messages;
    final currentUserId = controller.authService.userId?.toString();
    final cardProjection = snapshot.cardProjection;
    final previousVisibleBubbleIndexes = snapshot.previousVisibleBubbleIndexes;
    final messageIndexByKey = snapshot.messageIndexByKey;
    final messageByLookupId = snapshot.messageByLookupId;
    final peerReplyAfterFlags = snapshot.peerReplyAfterFlags;

    return SliverChildBuilderDelegate(
      (context, index) {
        if (index == 0) {
          return _buildHistoryTopStatusSlot(context, fontScale: fontScale);
        }

        final msgIndex = index - 1;
        if (msgIndex >= msgs.length) {
          return _buildSessionActivityFooter();
        }
        final msg = msgs[msgIndex];
        if (controller.isInternalDirectiveMessage(msg.content)) {
          return const SizedBox.shrink();
        }
        final isSystemMessage = msg.msgType == 3;
        if (isSystemMessage) {
          return buildChatSystemMessageItem(
            controller,
            context,
            msg: msg,
            fontScale: fontScale,
          );
        }

        final isStreaming = controller.imService.isMessageStreaming(msg.msgId);
        final isMine = ChatMessageOwnerClassifier.isMineMessage(
          msg,
          currentUserId: currentUserId,
        );
        final status = msg.status ?? '';
        final isSending = status.startsWith('sending');
        final isFailed = status.startsWith('failed');
        final isDelegateStatus = status.contains('delegate');
        final hasPeerReplyAfter = peerReplyAfterFlags[msgIndex];
        final agentDeliveryLabel = controller.agentDeliveryLabelForMessage(
          msg,
          hasPeerReplyAfter: hasPeerReplyAfter,
        );
        final isAgentDeliveryError = controller.isAgentDeliveryErrorForMessage(
          msg,
          hasPeerReplyAfter: hasPeerReplyAfter,
        );
        final showRetry = isFailed || isAgentDeliveryError;
        final previousBubbleIndex = previousVisibleBubbleIndexes[msgIndex];
        final prevMsg = previousBubbleIndex >= 0
            ? msgs[previousBubbleIndex]
            : null;
        final sameSenderAsPrev =
            prevMsg != null &&
            ChatMessageOwnerClassifier.isSameOwner(
              prevMsg,
              msg,
              currentUserId: currentUserId,
            );
        final showAvatar = !sameSenderAsPrev;
        final itemKey = ChatMessageIdentity.selectionKey(msg);
        if (cardProjection.hiddenIndexes.contains(msgIndex)) {
          return const SizedBox.shrink();
        }
        final messageCardDataOverride =
            cardProjection.overridesByIndex[msgIndex];

        return Obx(() {
          // call_segment(msgType=6) 可能用 effectiveSenderId（extra.speaker_user_id）覆盖展示发言者，
          // 例如语音大脑中豆包实际发声但权限属于文字 agent 的场景。
          final displaySenderId = msg.effectiveSenderId;
          controller.senderProfileVersionFor(
            senderId: displaySenderId,
            senderType: msg.senderType,
            isMine: isMine,
          );
          final senderName = controller.resolveSenderName(
            senderId: displaySenderId,
            isMine: isMine,
            isGroup: isGroup,
            senderType: msg.senderType,
          );
          final senderAvatarUrl = controller.resolveSenderAvatarUrl(
            senderId: displaySenderId,
            isMine: isMine,
            isGroup: isGroup,
            senderType: msg.senderType,
          );
          final senderVisualSeed = ChatMessageOwnerClassifier.visualSeed(
            senderId: displaySenderId,
            senderType: msg.senderType,
            isMine: isMine,
            currentUserId: currentUserId,
          );
          // 连续同发送者：人类气泡隐藏整行 meta；agent 连续气泡保留时间、隐藏昵称。
          final showSenderName = !sameSenderAsPrev && senderName.isNotEmpty;
          final showSender =
              showSenderName || (sameSenderAsPrev && msg.senderType == 2);
          final canOpenSenderProfile =
              !isMine && (msg.senderType == 1 || msg.senderType == 2);
          final onSenderTap = canOpenSenderProfile
              ? () => controller.onMessageAvatarTap(
                  senderId: displaySenderId,
                  senderType: msg.senderType,
                  isMine: isMine,
                  senderName: senderName,
                  senderAvatarUrl: senderAvatarUrl,
                )
              : null;
          final onSenderLongPress = (msg.senderType == 1 || msg.senderType == 2)
              ? () => controller.mentionSenderFromMessage(
                  senderId: displaySenderId,
                  senderType: msg.senderType,
                  isMine: isMine,
                  senderName: senderName,
                )
              : null;
          final hasVisibleTo =
              controller.isGroupChat &&
              msg.visibleTo != null &&
              msg.visibleTo!.isNotEmpty;
          final visibleToSummary = hasVisibleTo
              ? controller.visibleToSummaryForMessage(
                  msg.visibleTo,
                  ownerId: msg.senderId,
                )
              : '';
          final visibleToTip = visibleToSummary.isNotEmpty
              ? 'chat_visible_to_title_with_names'.trParams({
                  'names': visibleToSummary,
                })
              : 'chat_visible_to_title'.tr;
          final senderMeta = showSender
              ? buildChatMessageSenderMeta(
                  senderName: senderName,
                  createdAt: msg.createdAt,
                  senderVisualSeed: senderVisualSeed,
                  isMine: isMine,
                  fontScale: fontScale,
                  showSenderName: showSenderName,
                  onSenderTap: onSenderTap,
                  onSenderLongPress: onSenderLongPress,
                )
              : null;
          final bubbleContent = buildChatMessageBubbleWithAvatar(
            bubble: buildChatMessageBubbleWithMenu(
              controller: controller,
              context: context,
              msg: msg,
              showVisibleToLock: hasVisibleTo,
              visibleToTip: visibleToTip,
              fontScale: fontScale,
              messageCardDataOverride: messageCardDataOverride,
              hasSenderMeta: senderMeta != null,
              showAvatar: showAvatar,
              isMine: isMine,
              isStreaming: isStreaming,
              itemKey: itemKey,
              messageByLookupId: messageByLookupId,
            ),
            senderMeta: senderMeta,
            isMine: isMine,
            senderType: msg.senderType,
            senderId: displaySenderId,
            senderName: senderName,
            senderAvatarUrl: senderAvatarUrl,
            senderVisualSeed: senderVisualSeed,
            showAvatar: showAvatar,
            onSenderTap: onSenderTap,
            onSenderLongPress: onSenderLongPress,
          );
          Widget? statusWidget;
          if (isSending) {
            statusWidget = Text(
              isDelegateStatus ? 'chat_sending_delegate'.tr : 'chat_sending'.tr,
              style: TextStyle(
                fontSize: _scaleFont(11, fontScale),
                color: theme.colorScheme.secondary.withValues(alpha: 0.6),
              ),
            );
          } else if (showRetry) {
            statusWidget = Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Flexible(
                  child: Text(
                    isFailed
                        ? (isDelegateStatus
                              ? 'chat_send_failed_delegate'.tr
                              : 'chat_send_failed'.tr)
                        : agentDeliveryLabel!,
                    style: TextStyle(
                      fontSize: _scaleFont(11, fontScale),
                      color: AppTheme.errorColor,
                    ),
                  ),
                ),
                const SizedBox(width: 6),
                ChatRetryActionButton(
                  label: 'common_retry'.tr,
                  onTap: () => controller.retryMessage(
                    msg.clientMsgId,
                    msgId: msg.msgId,
                  ),
                  color: AppTheme.errorColor,
                  fontSize: _scaleFont(11, fontScale),
                ),
              ],
            );
          } else if (agentDeliveryLabel != null) {
            statusWidget = Text(
              agentDeliveryLabel,
              style: TextStyle(
                fontSize: _scaleFont(11, fontScale),
                color: isAgentDeliveryError
                    ? AppTheme.errorColor
                    : theme.colorScheme.secondary.withValues(alpha: 0.6),
              ),
            );
          }

          final messageColumn =
              NotificationListener<SizeChangedLayoutNotification>(
                onNotification: (_) {
                  controller.onMessageViewportLayoutChanged();
                  return false;
                },
                child: SizeChangedLayoutNotifier(
                  child: KeyedSubtree(
                    key: ValueKey(itemKey),
                    child: Column(
                      key: controller.messageViewportItemGlobalKey(itemKey),
                      crossAxisAlignment: isMine
                          ? CrossAxisAlignment.end
                          : CrossAxisAlignment.start,
                      children: [
                        bubbleContent,
                        if (isMine && statusWidget != null)
                          Padding(
                            padding: const EdgeInsets.only(
                              right: 14 + _messageAvatarContentInset,
                              top: 2,
                              bottom: 2,
                            ),
                            child: statusWidget,
                          ),
                      ],
                    ),
                  ),
                ),
              );
          return messageColumn;
        });
      },
      childCount: msgs.length + 2,
      addAutomaticKeepAlives: true,
      addRepaintBoundaries: true,
      findChildIndexCallback: (key) {
        if (key is ValueKey<String>) {
          final msgIndex = messageIndexByKey[key.value];
          if (msgIndex == null) return null;
          return msgIndex + 1;
        }
        return null;
      },
    );
  }

  Widget _buildHistoryTopStatusSlot(
    BuildContext context, {
    required double fontScale,
  }) {
    return Obx(
      () => _buildHistoryTopStatus(
        context,
        isLoadingOlderHistory: controller.isLoadingOlderHistory,
        hasOlderHistory: controller.hasOlderHistory,
        fontScale: fontScale,
      ),
    );
  }

  Widget _buildHistoryTopStatus(
    BuildContext context, {
    required bool isLoadingOlderHistory,
    required bool hasOlderHistory,
    required double fontScale,
  }) {
    final theme = Theme.of(context);
    final shouldShow = isLoadingOlderHistory || !hasOlderHistory;
    final label = isLoadingOlderHistory
        ? 'chat_loading_older'.tr
        : 'chat_loaded_top_reached'.tr;

    return SizedBox(
      height: _historyTopStatusSlotHeight,
      child: AnimatedOpacity(
        opacity: shouldShow ? 1 : 0,
        duration: const Duration(milliseconds: 120),
        child: IgnorePointer(
          ignoring: !shouldShow,
          child: Center(
            child: Container(
              key: ValueKey(
                isLoadingOlderHistory
                    ? 'history_top_loading'
                    : 'history_top_reached',
              ),
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
              decoration: BoxDecoration(
                color: theme.colorScheme.surface.withValues(alpha: 0.88),
                borderRadius: BorderRadius.circular(999),
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (isLoadingOlderHistory) ...[
                    SizedBox(
                      width: 12,
                      height: 12,
                      child: CircularProgressIndicator(
                        strokeWidth: 1.8,
                        valueColor: AlwaysStoppedAnimation<Color>(
                          theme.colorScheme.secondary,
                        ),
                      ),
                    ),
                    const SizedBox(width: 6),
                  ],
                  Flexible(
                    child: Text(
                      label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: _scaleFont(11, fontScale),
                        color: theme.colorScheme.secondary.withValues(
                          alpha: 0.85,
                        ),
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildSessionActivityFooter() {
    return Obx(() {
      if (!controller.hasChatStatusIndicator) {
        return const SizedBox.shrink();
      }
      return Padding(
        key: const ValueKey('session_activity_indicator'),
        padding: const EdgeInsets.only(top: 4),
        child: SessionActivityIndicator(label: controller.chatStatusLabel),
      );
    });
  }

  Widget _buildEmptyOrSessionActivityState(
    BuildContext context, {
    required double fontScale,
  }) {
    return Obx(() {
      if (!controller.hasChatStatusIndicator) {
        return buildChatEmptyState(controller, context, fontScale: fontScale);
      }
      return Center(
        child: SessionActivityIndicator(label: controller.chatStatusLabel),
      );
    });
  }
}

// ---------------------------------------------------------------------------
// 渲染隔离 Widget：底部 Dock（输入区 + 转发选择栏）
// ---------------------------------------------------------------------------

class _ChatBottomDockSection extends StatelessWidget {
  const _ChatBottomDockSection({
    required this.controller,
    required this.onDismissKeyboard,
  });

  final ChatController controller;
  final VoidCallback onDismissKeyboard;

  @override
  Widget build(BuildContext context) {
    final chatFontSizeService = Get.isRegistered<ChatFontSizeService>()
        ? Get.find<ChatFontSizeService>()
        : null;

    return Obx(() {
      _ChatViewDebugBuildCounter.hit('bottom_dock_obx');
      final fontScale = chatFontSizeService?.scaleRx.value ?? 1.0;
      if (controller.isForwardSelectionMode) {
        return ChatForwardSelectionActionBar(
          selectedCount: controller.selectedForwardMessageCount,
          onCancel: controller.exitForwardSelectionMode,
          onMergeForward: () => forwardChatSelectedMessages(
            controller: controller,
            context: context,
            mode: ChatForwardDispatchMode.merged,
          ),
          onSeparateForward: () => forwardChatSelectedMessages(
            controller: controller,
            context: context,
            mode: ChatForwardDispatchMode.separate,
          ),
        );
      }
      return NotificationListener<SizeChangedLayoutNotification>(
        onNotification: (_) {
          controller.onBottomDockLayoutChanged();
          return false;
        },
        child: SizeChangedLayoutNotifier(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              buildChatMentionList(controller, context, fontScale: fontScale),
              buildPinnedMentionBar(controller, context, fontScale: fontScale),
              buildVisibleToPickerList(
                controller,
                context,
                fontScale: fontScale,
              ),
              buildVisibleToIndicatorBar(
                controller,
                context,
                fontScale: fontScale,
              ),
              buildChatQueueEditingBanner(
                controller,
                context,
                fontScale: fontScale,
              ),
              buildChatRevokedMessageBanner(
                controller,
                context,
                fontScale: fontScale,
              ),
              buildChatReplyPreviewBlock(
                controller,
                context,
                fontScale: fontScale,
              ),
              buildChatInputArea(
                controller,
                context,
                fontScale: fontScale,
                onDismissKeyboard: onDismissKeyboard,
              ),
            ],
          ),
        ),
      );
    });
  }
}

// ---------------------------------------------------------------------------
// 渲染隔离 Widget：文件拖拽覆盖层
// ---------------------------------------------------------------------------

class _ChatDragOverlay extends StatelessWidget {
  const _ChatDragOverlay({required this.controller});

  final ChatController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Obx(() {
      if (!controller.isFileDragOver.value) {
        return const SizedBox.shrink();
      }
      return Positioned.fill(
        child: IgnorePointer(
          child: Container(
            margin: const EdgeInsets.all(16),
            decoration: BoxDecoration(
              color: theme.primaryColor.withValues(alpha: 0.08),
              borderRadius: BorderRadius.circular(16),
              border: Border.all(
                color: theme.primaryColor.withValues(alpha: 0.4),
                width: 2,
                strokeAlign: BorderSide.strokeAlignInside,
              ),
            ),
            child: Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(
                    Icons.cloud_upload_rounded,
                    size: 48,
                    color: theme.primaryColor.withValues(alpha: 0.6),
                  ),
                  const SizedBox(height: 12),
                  Text(
                    'chat_drop_files_hint'.tr,
                    style: TextStyle(
                      fontSize: 16,
                      fontWeight: FontWeight.w500,
                      color: theme.primaryColor.withValues(alpha: 0.7),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      );
    });
  }
}

class _ChatWarmupLifecycleScope extends StatefulWidget {
  const _ChatWarmupLifecycleScope({
    required this.sessionId,
    required this.child,
  });

  final String sessionId;
  final Widget child;

  @override
  State<_ChatWarmupLifecycleScope> createState() =>
      _ChatWarmupLifecycleScopeState();
}

class _ChatWarmupLifecycleScopeState extends State<_ChatWarmupLifecycleScope> {
  @override
  void didUpdateWidget(covariant _ChatWarmupLifecycleScope oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.sessionId == widget.sessionId) {
      return;
    }
    _ChatInitialMessageRenderWarmupScheduler.cancelForSession(
      oldWidget.sessionId,
    );
  }

  @override
  void dispose() {
    _ChatInitialMessageRenderWarmupScheduler.cancelForSession(widget.sessionId);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return widget.child;
  }
}

class _KeyboardAwareMessageList extends StatefulWidget {
  const _KeyboardAwareMessageList({
    required this.controller,
    required this.scrollController,
    required this.cacheExtent,
    required this.horizontalPadding,
    required this.delegate,
  });

  final ChatController controller;
  final ScrollController scrollController;
  final double cacheExtent;
  final double horizontalPadding;
  final SliverChildBuilderDelegate delegate;

  @override
  State<_KeyboardAwareMessageList> createState() =>
      _KeyboardAwareMessageListState();
}

class _KeyboardAwareMessageListState extends State<_KeyboardAwareMessageList> {
  Widget _buildListView(double bottomPadding) {
    return ListView.custom(
      controller: widget.scrollController,
      primary: false,
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      cacheExtent: widget.cacheExtent,
      padding: EdgeInsets.fromLTRB(
        widget.horizontalPadding,
        12,
        widget.horizontalPadding,
        12 + bottomPadding,
      ),
      childrenDelegate: widget.delegate,
    );
  }

  @override
  Widget build(BuildContext context) {
    return Obx(() {
      final shouldFollow = widget.controller.shouldFollowKeyboardForInputDock;
      final obstructionBottom =
          widget.controller.messageListViewportObstructionBottom;
      final liveKeyboardBottom = MediaQuery.viewInsetsOf(context).bottom;
      final bottomPadding = shouldFollow
          ? 0.0
          : (liveKeyboardBottom > obstructionBottom
                ? liveKeyboardBottom
                : obstructionBottom);
      return _buildListView(bottomPadding);
    });
  }
}
