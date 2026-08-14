import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../data/models/session_model.dart';
import '../../../shared/widgets/session_avatar.dart';
import '../controllers/conversations_controller.dart';

/// Self-resolving session avatar.
///
/// Drop-in widget that renders the correct avatar for a [SessionModel] — agent
/// avatar, friend avatar, or group nine-grid — by delegating to the single
/// avatar-resolution implementation in [ConversationsController]. Any page that
/// has a session (conversation list, favorites, …) uses this instead of wiring
/// up avatar URLs / members by hand.
///
/// When the conversations controller is not registered it degrades to the
/// first-letter placeholder, and it refreshes reactively as the avatar resolves.
class SessionAvatarView extends StatelessWidget {
  const SessionAvatarView({
    super.key,
    required this.session,
    required this.avatarTitle,
    required this.avatarColor,
    this.size = 50,
    this.borderRadius = 0,
  });

  final SessionModel session;
  final String avatarTitle;
  final Color avatarColor;
  final double size;
  final double borderRadius;

  @override
  Widget build(BuildContext context) {
    final conv = Get.isRegistered<ConversationsController>()
        ? Get.find<ConversationsController>()
        : null;
    if (conv == null) {
      return SessionAvatar(
        isGroup: session.type == 'group',
        avatarTitle: avatarTitle,
        avatarColor: avatarColor,
        size: size,
        borderRadius: borderRadius,
      );
    }

    return Obx(() {
      conv.watchSessionAvatar(session);
      return SessionAvatar(
        isGroup: conv.isGroupSession(session),
        avatarTitle: avatarTitle,
        avatarColor: avatarColor,
        avatarUrl: conv.avatarUrlForSession(session),
        members: conv.avatarMembersForSession(session),
        size: size,
        borderRadius: borderRadius,
      );
    });
  }
}
