import 'package:flutter/widgets.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../data/models/local_search_result.dart';
import '../../data/models/session_model.dart';
import '../../data/providers/friend_service.dart';
import '../../data/providers/local_db.dart';
import '../chat/services/chat_route_navigator.dart';

/// 本地搜索页控制器。
///
/// 单一职责：维护输入关键词、执行本地 LIKE 搜索、暴露结果给视图。
/// 进入页面时若带了初始关键词（AI 调 grix_local_search 透传），自动执行一次搜索；
/// 用户也可在顶部输入框继续输入、重新搜索。
class LocalSearchController extends GetxController {
  final TextEditingController inputController = TextEditingController();

  /// 最近一次搜索结果。null 表示尚未搜索。
  final Rxn<LocalSearchResult> result = Rxn<LocalSearchResult>();
  final RxBool isSearching = false.obs;

  @override
  void onInit() {
    super.onInit();
    // AI 调用时通过 arguments 传入初始关键词，进入页面即自动搜索。
    final args = Get.arguments;
    if (args is Map && args['keywords'] is List) {
      final initial = (args['keywords'] as List)
          .map((e) => '$e'.trim())
          .where((s) => s.isNotEmpty)
          .join(' ');
      if (initial.isNotEmpty) {
        inputController.text = initial;
        search();
      }
    }
  }

  @override
  void onClose() {
    inputController.dispose();
    super.onClose();
  }

  /// 按输入框当前文本执行本地搜索。空文本清空结果。
  Future<void> search() async {
    final raw = inputController.text.trim();
    if (raw.isEmpty) {
      result.value = null;
      return;
    }
    final keywords = raw
        .split(RegExp(r'\s+'))
        .where((s) => s.isNotEmpty)
        .toList(growable: false);
    isSearching.value = true;
    try {
      result.value = await LocalDbSearchRepository.search(keywords);
    } finally {
      isSearching.value = false;
    }
  }

  /// 点击列表项主体：进入对应会话。
  void openSession(String sessionId, String title, String type) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return;
    ChatRouteNavigator.toChat(
      sessionId: sid,
      title: title,
      type: type.trim().isEmpty ? 'private' : type.trim(),
    );
  }

  /// 私聊对端用户的头像 URL（从好友服务取真实头像）；群聊 / agent / 无 peer
  /// 返回空字符串，由统一头像组件回退到首字母头像。
  String avatarUrlForSession(SessionModel? session) {
    if (session == null || session.type == 'group') return '';
    final peerId = session.peerId.trim();
    if (peerId.isEmpty || session.peerType == 2) return '';
    if (!Get.isRegistered<FriendService>()) return '';
    return Get.find<FriendService>().getUserAvatarUrl(peerId)?.trim() ?? '';
  }

  /// 点击头像：私聊对端进入用户资料页；群聊 / agent / 无 peer 回退进会话。
  void openProfileOrSession(
    SessionModel? session,
    String sessionId,
    String displayTitle,
    String type,
  ) {
    if (session != null &&
        session.type != 'group' &&
        session.peerType != 2 &&
        session.peerId.trim().isNotEmpty) {
      final peerId = session.peerId.trim();
      Get.toNamed(
        AppRoutes.accountInfo,
        arguments: {
          'session_id': session.sessionId,
          'peer_id': peerId,
          'peer_type': session.peerType.toString(),
          'nickname': session.peerNickname.trim(),
          'username': session.peerUsername.trim(),
          'avatar_url': avatarUrlForSession(session),
          'title': displayTitle,
        },
        parameters: {
          'session_id': session.sessionId,
          'peer_id': peerId,
          'peer_type': session.peerType.toString(),
        },
      );
      return;
    }
    openSession(sessionId, displayTitle, type);
  }
}
