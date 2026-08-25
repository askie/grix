import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';
import '../../../data/providers/friend_service.dart';
import '../../../data/providers/session_service.dart';
import '../../../data/providers/im_service.dart';
import '../../chat/services/chat_route_navigator.dart';
import '../../account_info/services/account_info_navigator.dart';
import '../../../shared/utils/toast_util.dart';
import '../services/friend_qr_flow_service.dart';

class ContactsController extends GetxController {
  final FriendService friendService = Get.find<FriendService>();
  final ImService imService = Get.find<ImService>();
  final FriendQrFlowService _friendQrFlowService =
      Get.find<FriendQrFlowService>();

  final searchController = TextEditingController();
  final searchResults = <UserSearchItem>[].obs;
  final sentUsernames = <String>{}.obs;
  final isSearching = false.obs;
  final hasSearchQuery = false.obs;

  @override
  void onReady() {
    super.onReady();
    unawaited(_loadInitialContacts());
  }

  Future<void> _loadInitialContacts() async {
    if (friendService.friendList.isNotEmpty &&
        friendService.friendRequests.isNotEmpty) {
      return;
    }
    await refreshContacts();
  }

  Future<void> refreshContacts() async {
    await friendService.loadFriendList();
    await friendService.loadFriendRequests();
  }

  Future<void> searchUsers(String keyword) async {
    final k = keyword.trim();
    if (k.isNotEmpty) {
      hasSearchQuery.value = true;
      isSearching.value = true;
      searchResults.value = await friendService.searchUsers(k);
      isSearching.value = false;
    }
  }

  void resetSearch() {
    searchController.clear();
    searchResults.clear();
    sentUsernames.clear();
    isSearching.value = false;
    hasSearchQuery.value = false;
  }

  Future<bool> sendFriendRequest(UserSearchItem user) async {
    final result = await friendService.sendFriendRequest(toUserId: user.id);
    if (!result.success) {
      return false;
    }

    if (result.autoApproved) {
      if (Get.isDialogOpen == true) {
        Get.back();
      }
      await createChat(
        FriendItem(
          id: '',
          userId: user.id,
          username: user.username,
          nickname: user.nickname,
          remarkName: '',
          avatarUrl: user.avatarUrl,
        ),
      );
      return true;
    }

    sentUsernames.add(user.username);
    CustomToast.show(
      '${'friend_request_sent'.tr} @${user.username}',
      isError: false,
    );
    return true;
  }

  void navigateToAccountInfo(FriendItem friend) {
    final displayName = friend.nickname.isNotEmpty ? friend.nickname : friend.username;
    AccountInfoNavigator.open(
      arguments: {
        'peer_id': friend.userId,
        'peer_type': '1',
        'nickname': friend.nickname,
        'username': friend.username,
        'avatar_url': friend.avatarUrl,
        'title': displayName,
      },
      parameters: {
        'peer_id': friend.userId,
        'peer_type': '1',
      },
    );
  }

  Future<void> createChat(FriendItem friend) async {
    final sessionService = Get.find<SessionService>();
    final realSessionId = await sessionService.openLatestSession(
      friend.userId,
      1,
    );
    await _openFriendChat(
      friend,
      realSessionId,
      errorText: 'contacts_open_session_failed'.tr,
    );
  }

  Future<void> createNewChat(FriendItem friend) async {
    final displayName = friend.nickname.isNotEmpty
        ? friend.nickname
        : friend.username;
    final sid = await ChatRouteNavigator.createAndOpenPrivateChat(
      peerId: friend.userId,
      peerType: 1,
      fallbackTitle: displayName,
    );
    if (sid == null) {
      CustomToast.show('contacts_create_session_failed'.tr);
    }
  }

  Future<void> _openFriendChat(
    FriendItem friend,
    String? sessionId, {
    required String errorText,
  }) async {
    if (sessionId == null) {
      CustomToast.show(errorText);
      return;
    }

    final displayName = friend.nickname.isNotEmpty
        ? friend.nickname
        : friend.username;
    final routeTitle = imService.resolveSessionDisplayTitleById(
      sessionId,
      fallbackTitle: displayName,
      type: 'private',
    );
    if (!imService.hasSessionDisplayTitleById(sessionId)) {
      await imService.bindSessionDisplayTitle(
        sessionId,
        displayName,
        type: 'private',
      );
    }
    ChatRouteNavigator.toChat(
      sessionId: sessionId,
      title: routeTitle,
      type: 'private',
    );
  }

  Future<void> createGroup(String name) async {
    final trimName = name.trim();
    if (trimName.isEmpty) return;

    final sessionService = Get.find<SessionService>();
    final realSessionId = await sessionService.createGroupSession(
      name: trimName,
      memberIds: const [],
      memberTypes: const [],
    );
    if (realSessionId == null) {
      CustomToast.show('contacts_create_group_failed'.tr);
      return;
    }

    await imService.bindSessionDisplayTitle(
      realSessionId,
      trimName,
      type: 'group',
    );

    if (Get.isDialogOpen == true) {
      Get.back();
    }
    ChatRouteNavigator.toChat(
      sessionId: realSessionId,
      title: trimName,
      type: 'group',
    );
  }

  Future<void> openUserQrScanner() async {
    await _friendQrFlowService.openUserQrScanner();
  }

  Future<bool> deleteFriend(FriendItem friend) async {
    final success = await friendService.deleteFriend(friend.userId);
    if (!success) {
      return false;
    }
    await imService.refreshSessionsNow();
    CustomToast.show('account_info_delete_friend_success'.tr, isError: false);
    return true;
  }

  Future<bool> blockUser(FriendItem friend) async {
    final success = await friendService.blockUser(friend.userId);
    if (!success) {
      return false;
    }
    await imService.refreshSessionsNow();
    CustomToast.show('contacts_block_friend_success'.tr, isError: false);
    return true;
  }
}
