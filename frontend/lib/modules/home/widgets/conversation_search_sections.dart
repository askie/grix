import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../app/themes/app_theme.dart';
import '../../../data/models/local_search_result.dart';
import '../../../data/models/session_model.dart';
import '../../../shared/widgets/session_avatar.dart';
import '../controllers/conversations_controller.dart';

/// 顶部搜索框的分组结果：会话 / 联系人和 Agent / 聊天记录三段。
///
/// 全 APP 只有这一套搜索结果 UI —— AI 调 `grix_local_search` 也是把关键词写进
/// 顶部搜索框，落到同一份结果上。空的段不出标题；三段全空由调用方走 no_match 空态。
List<Widget> buildConversationSearchSlivers({
  required ThemeData theme,
  required ConversationsController controller,
  required Widget Function(ConversationListItem item) sessionTileBuilder,
}) {
  final sessions = controller.groupedSessions;
  final contacts = controller.searchContacts;
  final messages = controller.searchMessages;
  final slivers = <Widget>[];

  void addSection(String labelKey, int count, Widget list) {
    slivers.add(
      SliverToBoxAdapter(
        child: _sectionHeader(
          theme,
          labelKey.trParams({'count': '$count'}),
        ),
      ),
    );
    slivers.add(list);
  }

  if (sessions.isNotEmpty) {
    addSection(
      'local_search_section_sessions',
      sessions.length,
      SliverList(
        delegate: SliverChildBuilderDelegate(
          (context, index) => sessionTileBuilder(sessions[index]),
          childCount: sessions.length,
        ),
      ),
    );
  }
  if (contacts.isNotEmpty) {
    addSection(
      'local_search_section_contacts',
      contacts.length,
      SliverList(
        delegate: SliverChildBuilderDelegate(
          (context, index) =>
              _contactTile(controller: controller, contact: contacts[index]),
          childCount: contacts.length,
        ),
      ),
    );
  }
  if (messages.isNotEmpty) {
    addSection(
      'local_search_section_messages',
      messages.length,
      SliverList(
        delegate: SliverChildBuilderDelegate(
          (context, index) =>
              _messageTile(controller: controller, message: messages[index]),
          childCount: messages.length,
        ),
      ),
    );
  }
  return slivers;
}

Widget _sectionHeader(ThemeData theme, String text) {
  return Padding(
    padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
    child: Text(
      text,
      style: TextStyle(
        fontSize: 13,
        fontWeight: FontWeight.w600,
        color: theme.colorScheme.secondary.withValues(alpha: 0.6),
      ),
    ),
  );
}

Widget _contactTile({
  required ConversationsController controller,
  required MatchedContact contact,
}) {
  final title = contact.displayName.trim().isNotEmpty
      ? contact.displayName.trim()
      : contact.username.trim();
  final subtitle = contact.username.trim().isNotEmpty
      ? contact.username.trim()
      : contact.introduction.trim();
  return ListTile(
    leading: SizedBox(
      width: 44,
      height: 44,
      child: SessionAvatar(
        isGroup: false,
        avatarTitle: title,
        avatarColor: AppTheme.getAvatarColor(contact.peerId),
        avatarUrl: contact.avatarUrl,
        size: 44,
      ),
    ),
    title: Text(title, maxLines: 1, overflow: TextOverflow.ellipsis),
    subtitle: subtitle.isEmpty
        ? null
        : Text(subtitle, maxLines: 1, overflow: TextOverflow.ellipsis),
    onTap: () => controller.openSearchedContact(contact),
  );
}

Widget _messageTile({
  required ConversationsController controller,
  required MatchedMessage message,
}) {
  final SessionModel? session = controller.imService.findSessionById(
    message.sessionId,
  );
  final title = session == null
      ? message.sessionId
      : controller.getDisplayTitle(session);
  return ListTile(
    leading: SizedBox(
      width: 44,
      height: 44,
      child: SessionAvatar(
        isGroup: session?.type.trim() == 'group',
        avatarTitle: title,
        avatarColor: AppTheme.getAvatarColor(message.sessionId),
        avatarUrl: controller.searchResultAvatarUrl(session),
        size: 44,
      ),
    ),
    title: Text(title, maxLines: 1, overflow: TextOverflow.ellipsis),
    subtitle: Text(
      message.content.trim(),
      maxLines: 2,
      overflow: TextOverflow.ellipsis,
    ),
    onTap: () => controller.openSearchedMessage(message),
  );
}
