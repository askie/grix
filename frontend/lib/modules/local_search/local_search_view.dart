import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/themes/app_theme.dart';
import '../../data/models/local_search_result.dart';
import '../../data/models/session_model.dart';
import '../../data/providers/im_service.dart';
import '../../shared/widgets/session_avatar.dart';
import 'local_search_controller.dart';

/// 本地搜索结果页：顶部输入框 + 平铺（不分组折叠）的会话与消息结果列表。
/// 每行左侧用统一头像组件（SessionAvatar）展示真实头像；点头像进用户资料页，
/// 点其余区域进会话。
class LocalSearchView extends GetView<LocalSearchController> {
  const LocalSearchView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        titleSpacing: 0,
        title: _buildSearchField(theme),
        actions: [
          TextButton(
            onPressed: controller.search,
            child: const Text('搜索'),
          ),
        ],
      ),
      body: Obx(() {
        if (controller.isSearching.value) {
          return const Center(child: CircularProgressIndicator());
        }
        final result = controller.result.value;
        if (result == null) {
          return _buildHint(theme, '输入关键词，搜索本地会话和消息');
        }
        if (result.isEmpty) {
          return _buildHint(theme, '未找到相关结果');
        }
        return _buildResultList(theme, result);
      }),
    );
  }

  Widget _buildSearchField(ThemeData theme) {
    return TextField(
      controller: controller.inputController,
      autofocus: false,
      textInputAction: TextInputAction.search,
      onSubmitted: (_) => controller.search(),
      decoration: const InputDecoration(
        hintText: '搜索会话、消息',
        prefixIcon: Icon(Icons.search, size: 20),
        border: InputBorder.none,
        isDense: true,
      ),
      style: theme.textTheme.bodyLarge,
    );
  }

  Widget _buildHint(ThemeData theme, String text) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Text(
          text,
          textAlign: TextAlign.center,
          style: TextStyle(color: theme.hintColor),
        ),
      ),
    );
  }

  Widget _buildResultList(ThemeData theme, LocalSearchResult result) {
    final children = <Widget>[];
    if (result.matchedSessions.isNotEmpty) {
      children.add(_buildSectionHeader(theme, '会话 (${result.matchedSessions.length})'));
      for (final s in result.matchedSessions) {
        children.add(_buildSessionTile(s));
      }
    }
    if (result.matchedMessages.isNotEmpty) {
      children.add(_buildSectionHeader(theme, '消息 (${result.matchedMessages.length})'));
      for (final m in result.matchedMessages) {
        children.add(_buildMessageTile(m));
      }
    }
    return ListView(children: children);
  }

  Widget _buildSectionHeader(ThemeData theme, String text) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
      child: Text(
        text,
        style: TextStyle(
          fontSize: 13,
          fontWeight: FontWeight.w600,
          color: theme.hintColor,
        ),
      ),
    );
  }

  /// 统一头像组件 + 点击进用户资料页。
  Widget _buildAvatar({
    required SessionModel? session,
    required String sessionId,
    required String displayTitle,
    required String type,
  }) {
    return GestureDetector(
      onTap: () => controller.openProfileOrSession(
        session,
        sessionId,
        displayTitle,
        type,
      ),
      child: SizedBox(
        width: 44,
        height: 44,
        child: SessionAvatar(
          isGroup: type.trim() == 'group',
          avatarTitle: displayTitle,
          avatarColor: AppTheme.getAvatarColor(sessionId),
          avatarUrl: controller.avatarUrlForSession(session),
          size: 44,
        ),
      ),
    );
  }

  Widget _buildSessionTile(MatchedSession s) {
    final session = Get.find<ImService>().findSessionById(s.sessionId);
    final displayTitle = s.title.trim().isNotEmpty
        ? s.title.trim()
        : (s.peerNickname.trim().isNotEmpty ? s.peerNickname.trim() : s.peerUsername.trim());
    final shownTitle = displayTitle.isEmpty ? s.sessionId : displayTitle;
    return ListTile(
      leading: _buildAvatar(
        session: session,
        sessionId: s.sessionId,
        displayTitle: shownTitle,
        type: s.type,
      ),
      title: Text(
        shownTitle,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      subtitle: s.lastMessage.trim().isEmpty
          ? null
          : Text(
              s.lastMessage.trim(),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
      onTap: () => controller.openSession(s.sessionId, shownTitle, s.type),
    );
  }

  Widget _buildMessageTile(MatchedMessage m) {
    // 用所属会话信息渲染标题与头像；内存无此会话时回退用 session_id。
    final session = Get.find<ImService>().findSessionById(m.sessionId);
    final sessionTitle = session?.title.trim().isNotEmpty == true
        ? session!.title.trim()
        : (session?.peerNickname.trim().isNotEmpty == true
            ? session!.peerNickname.trim()
            : m.sessionId);
    final sessionType = session?.type.trim() ?? 'private';
    return ListTile(
      leading: _buildAvatar(
        session: session,
        sessionId: m.sessionId,
        displayTitle: sessionTitle,
        type: sessionType,
      ),
      title: Text(
        sessionTitle,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      subtitle: Text(
        m.content.trim(),
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
      ),
      onTap: () => controller.openSession(m.sessionId, sessionTitle, sessionType),
    );
  }
}
