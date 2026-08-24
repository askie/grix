import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../bindings/chat_binding.dart';
import '../chat_view.dart';
import '../controllers/chat_controller.dart';

/// Desktop three-column mode: the home page mounts a nested [Navigator] on the
/// right and registers it here. While it is mounted, [ChatRouteNavigator.toChat]
/// opens chats inside that pane instead of pushing a full-screen route.
///
/// Controllers opened in the pane are created and deleted here explicitly,
/// because GetX route reporting only tracks the root navigator.
class ChatPaneHost {
  const ChatPaneHost._();

  static GlobalKey<NavigatorState>? _navigatorKey;
  static final RxnString _activeSessionId = RxnString();

  /// Session currently shown in the pane, or null when the placeholder is shown.
  static String? get activeSessionId => _activeSessionId.value;
  static RxnString get activeSessionIdRx => _activeSessionId;

  static bool get isAvailable =>
      _navigatorKey != null && _navigatorKey!.currentState != null;

  static void attach(GlobalKey<NavigatorState> key) {
    _navigatorKey = key;
  }

  /// Called when the pane unmounts (window narrowed or home disposed).
  static void detach(GlobalKey<NavigatorState> key) {
    if (!identical(_navigatorKey, key)) return;
    _navigatorKey = null;
    _disposeActiveController();
  }

  /// Show [sessionId] in the pane. Returns false when the pane is unavailable.
  static bool open({
    required String sessionId,
    required Map<String, dynamic> arguments,
  }) {
    final navigator = _navigatorKey?.currentState;
    if (navigator == null) return false;
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    if (_activeSessionId.value == sid) return true;

    final previousTag = _activeTag;
    if (previousTag != null &&
        Get.isRegistered<ChatController>(tag: previousTag)) {
      final previous = Get.find<ChatController>(tag: previousTag);
      previous.persistDraftImmediately();
      previous.deactivateVoiceCommandForRouteChange();
      previous.dismissInputInteraction();
    }

    final tag = ChatBinding.controllerTagForSession(sid);
    if (!Get.isRegistered<ChatController>(tag: tag)) {
      Get.put<ChatController>(
        ChatController(routeArguments: arguments),
        tag: tag,
      );
    }
    _activeSessionId.value = sid;
    navigator.pushReplacement<void, void>(
      _paneRoute(ChatView(controllerTag: tag, embedded: true)),
    );
    _deleteControllerAfterFrame(previousTag);
    return true;
  }

  /// Back to the placeholder if [sessionId] is the one shown in the pane.
  static bool closeIfActive(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty || _activeSessionId.value != sid) return false;
    final navigator = _navigatorKey?.currentState;
    final tag = _activeTag;
    _activeSessionId.value = null;
    navigator?.pushReplacement<void, void>(
      _paneRoute(const ChatPanePlaceholder()),
    );
    _deleteControllerAfterFrame(tag);
    return true;
  }

  static String? get _activeTag {
    final sid = _activeSessionId.value;
    if (sid == null || sid.isEmpty) return null;
    return ChatBinding.controllerTagForSession(sid);
  }

  static void _disposeActiveController() {
    final tag = _activeTag;
    _activeSessionId.value = null;
    if (tag != null && Get.isRegistered<ChatController>(tag: tag)) {
      Get.delete<ChatController>(tag: tag);
    }
  }

  // Delete after the replaced page has been unmounted so its widgets never
  // observe a closed controller during the swap frame.
  static void _deleteControllerAfterFrame(String? tag) {
    if (tag == null) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (tag == _activeTag) return;
      if (Get.isRegistered<ChatController>(tag: tag)) {
        Get.delete<ChatController>(tag: tag);
      }
    });
  }

  static Route<void> _paneRoute(Widget child) {
    return PageRouteBuilder<void>(
      pageBuilder: (_, __, ___) => child,
      transitionDuration: Duration.zero,
      reverseTransitionDuration: Duration.zero,
    );
  }
}

/// Right pane content before any chat is selected.
class ChatPanePlaceholder extends StatelessWidget {
  const ChatPanePlaceholder({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.chat_bubble_outline_rounded,
              size: 48,
              color: theme.colorScheme.outline,
            ),
            const SizedBox(height: 12),
            Text(
              'chat_pane_select_hint'.tr,
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.outline,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Nested navigator hosting the desktop chat pane; registers itself with
/// [ChatPaneHost] for its lifetime.
class ChatPaneNavigator extends StatefulWidget {
  const ChatPaneNavigator({super.key});

  @override
  State<ChatPaneNavigator> createState() => _ChatPaneNavigatorState();
}

class _ChatPaneNavigatorState extends State<ChatPaneNavigator> {
  final GlobalKey<NavigatorState> _key = GlobalKey<NavigatorState>();

  @override
  void initState() {
    super.initState();
    ChatPaneHost.attach(_key);
  }

  @override
  void dispose() {
    ChatPaneHost.detach(_key);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Navigator(
      key: _key,
      onGenerateRoute: (_) =>
          ChatPaneHost._paneRoute(const ChatPanePlaceholder()),
    );
  }
}
