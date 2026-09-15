import 'package:get/get.dart';

import '../../../data/models/session_model.dart';
import '../../../data/providers/im_service.dart';

class ShareTargetSessionResolver {
  ShareTargetSessionResolver({ImService? imService})
    : _imService = imService ?? Get.find<ImService>();

  final ImService _imService;

  /// Recent private agent sessions (peer_type == 2), newest activity first.
  List<SessionModel> recentAgentSessions({int limit = 30}) {
    final sessions = _imService.sessions
        .where(
          (session) =>
              session.type == 'private' &&
              session.peerType == 2 &&
              session.sessionId.trim().isNotEmpty,
        )
        .toList(growable: false);
    sessions.sort((a, b) => b.activityAt.compareTo(a.activityAt));
    if (sessions.length <= limit) {
      return sessions;
    }
    return sessions.sublist(0, limit);
  }

  String displayTitle(SessionModel session) {
    final title = session.title.trim();
    if (title.isNotEmpty) return title;
    final nickname = session.peerNickname.trim();
    if (nickname.isNotEmpty) return nickname;
    return session.peerId.trim();
  }
}
