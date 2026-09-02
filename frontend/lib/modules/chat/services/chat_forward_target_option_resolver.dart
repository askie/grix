import 'package:get/get.dart';

import '../../../data/models/session_model.dart';
import '../../../data/providers/agent_service.dart';
import '../../../data/providers/friend_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../shared/models/session_avatar_member.dart';
import '../../../shared/utils/chat_message_preview.dart';
import '../models/chat_forward_target_option.dart';

class ChatForwardTargetOptionResolver {
  const ChatForwardTargetOptionResolver({
    required ImService imService,
    FriendService? friendService,
    AgentService? agentService,
  }) : _imService = imService,
       _friendService = friendService,
       _agentService = agentService;

  final ImService _imService;
  final FriendService? _friendService;
  final AgentService? _agentService;

  List<ChatForwardTargetOption> resolveAll() {
    final options =
        _imService.sessions
            .where((session) => session.sessionId.trim().isNotEmpty)
            .map(resolveFromSession)
            .toList(growable: false)
          ..sort((a, b) => b.activityAt.compareTo(a.activityAt));
    return options;
  }

  ChatForwardTargetOption resolveFromSession(SessionModel session) {
    final isGroup = session.type == 'group';
    return ChatForwardTargetOption(
      sessionId: session.sessionId,
      avatarColorSeed: _resolveAvatarColorSeed(session),
      title: _resolveTargetTitle(session),
      // 仅携带原文，预览清洗推迟到副标题被实际访问时（懒计算，见 ChatForwardTargetOption.subtitle），
      // 避免点开转发列表时对全部会话同步跑预览正则导致卡顿。
      previewSource: session.lastMessage,
      isGroup: isGroup,
      activityAt: session.activityAt,
      avatarUrl: isGroup ? '' : _resolveAvatarUrl(session),
      members: const <SessionAvatarMember>[],
    );
  }

  String _resolveAvatarColorSeed(SessionModel session) {
    if (session.type != 'private') {
      final sid = session.sessionId.trim();
      if (sid.isNotEmpty) return sid;
      return _resolveTargetTitle(session);
    }

    final peerId = session.peerId.trim();
    if (peerId.isEmpty) {
      return session.sessionId.trim();
    }
    if (session.peerType == 2) {
      return 'agent:$peerId';
    }
    return peerId;
  }

  String _resolveAvatarUrl(SessionModel session) {
    if (session.peerType == 2) {
      return _resolveAgentAvatarUrl(session.peerId.trim());
    }
    final peerId = _resolvePrivatePeerId(session);
    if (peerId.isEmpty) return '';
    return _friendService?.getUserAvatarUrl(peerId) ?? '';
  }

  String _resolveAgentAvatarUrl(String agentId) {
    if (agentId.isEmpty) return '';
    final service = _agentService;
    if (service == null) return '';
    final agent = service.agents.firstWhereOrNull((a) => a.id == agentId);
    return agent?.avatarUrl ?? '';
  }

  String _resolvePrivatePeerId(SessionModel session) {
    final peerId = session.peerId.trim();
    if (peerId.isNotEmpty) return peerId;
    return session.sessionId.trim();
  }

  String _resolveTargetTitle(SessionModel session) {
    if (session.type == 'private') {
      return _resolvePrivateTargetTitle(session);
    }
    final title = _imService.resolveSessionDisplayTitle(session).trim();
    if (title.isNotEmpty) {
      return title;
    }
    return session.sessionId.trim();
  }

  String _resolvePrivateTargetTitle(SessionModel session) {
    final fromSession = _resolvePrivatePeerDisplayName(session);
    if (fromSession.isNotEmpty) {
      return fromSession;
    }

    final peerId = session.peerId.trim();
    if (peerId.isNotEmpty && session.peerType != 2) {
      final nickname = _friendService?.getUserNickname(peerId)?.trim() ?? '';
      if (nickname.isNotEmpty) {
        return nickname;
      }
    }

    if (session.peerType == 2) {
      final threadTitle = _resolvePrivateThreadTitle(session);
      if (threadTitle.isNotEmpty) {
        return threadTitle;
      }
    }

    return 'conversations_thread_untitled'.tr;
  }

  String _resolvePrivatePeerDisplayName(SessionModel session) {
    final nickname = session.peerNickname.trim();
    if (nickname.isNotEmpty) {
      return nickname;
    }
    final username = session.peerUsername.trim();
    if (username.isNotEmpty) {
      return username;
    }
    return '';
  }

  String _resolvePrivateThreadTitle(SessionModel session) {
    final sid = session.sessionId.trim();
    final explicitTitle = ChatMessagePreview.summarizeTitle(
      session.title,
    ).trim();
    if (explicitTitle.isNotEmpty && explicitTitle != sid) {
      return explicitTitle;
    }
    return '';
  }
}
