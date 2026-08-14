import 'package:get/get.dart';
import 'package:sentry_flutter/sentry_flutter.dart';

import '../controllers/chat_controller.dart';

class ChatBinding extends Bindings {
  static String? currentControllerTag() {
    final rawArgs = Get.arguments;
    final args = rawArgs is Map<String, dynamic>
        ? rawArgs
        : const <String, dynamic>{};
    final params = Get.parameters;
    final sessionId = (args['session_id'] ?? params['session_id'] ?? '')
        .toString()
        .trim();

    Sentry.addBreadcrumb(
      Breadcrumb(
        category: 'chat_binding',
        message: 'currentControllerTag',
        data: {
          'rawArgs_type': rawArgs.runtimeType.toString(),
          'session_id': sessionId,
          'route': Get.currentRoute,
        },
        level: SentryLevel.info,
      ),
    );

    if (sessionId.isEmpty) {
      // Only report error on chat route — returning null on non-chat routes
      // (e.g. ChatRouteNavigator checking for stale controllers from /home)
      // is expected behavior, not an error.
      if (Get.currentRoute.startsWith('/chat')) {
        Sentry.captureMessage(
          'ChatBinding: sessionId is EMPTY, '
          'route=${Get.currentRoute} args=$rawArgs params=$params',
          level: SentryLevel.error,
        );
      }
      return null;
    }
    return controllerTagForSession(sessionId);
  }

  static String controllerTagForSession(String sessionId) {
    return 'chat:${sessionId.trim()}';
  }

  @override
  void dependencies() {
    final tag = currentControllerTag();
    Get.lazyPut<ChatController>(() => ChatController(), tag: tag);
  }
}
