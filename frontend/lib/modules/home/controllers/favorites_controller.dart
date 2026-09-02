import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../data/providers/user_session_favorite_service.dart';
import '../../chat/services/chat_route_navigator.dart';
import 'conversations_controller.dart';

class FavoritesController extends GetxController {
  FavoritesController({UserSessionFavoriteService? service})
    : _service = service ?? UserSessionFavoriteService();

  final UserSessionFavoriteService _service;

  final _allItems = <FavoriteSessionItem>[].obs;
  final _isLoading = true.obs;
  final searchQuery = ''.obs;

  final searchController = TextEditingController();

  bool get isLoading => _isLoading.value;

  List<FavoriteSessionItem> get displayItems {
    final q = searchQuery.value.trim().toLowerCase();
    if (q.isEmpty) return _allItems;
    return _allItems.where((item) {
      final title = item.title.toLowerCase();
      final peer = item.peerNickname?.toLowerCase() ?? '';
      return title.contains(q) || peer.contains(q);
    }).toList();
  }

  @override
  void onInit() {
    super.onInit();
    _load();
  }

  @override
  void onClose() {
    searchController.dispose();
    super.onClose();
  }

  Future<void> _load() async {
    _isLoading.value = true;
    final items = await _service.list();
    _allItems.assignAll(items);
    _isLoading.value = false;
  }

  @override
  Future<void> refresh() => _load();

  void onSearchChanged(String query) {
    searchQuery.value = query;
  }

  /// The live conversations controller when registered. The favorites list
  /// reuses its avatar-resolution machinery (via [SessionAvatarView]) and its
  /// local session cache, so it renders the same avatars as the conversation
  /// list. Returns null when it is not registered (tiles then fall back to the
  /// first-letter placeholder).
  ConversationsController? get conversations =>
      Get.isRegistered<ConversationsController>()
      ? Get.find<ConversationsController>()
      : null;

  void openSession(FavoriteSessionItem item) {
    ChatRouteNavigator.toChat(
      sessionId: item.sessionId,
      title: item.title,
      type: item.sessionType == 2 ? 'group' : 'private',
    );
  }

  Future<void> removeFavorite(String sessionId) async {
    final ok = await _service.remove(sessionId);
    if (ok) {
      _allItems.removeWhere((item) => item.sessionId == sessionId);
      if (Get.isRegistered<ConversationsController>()) {
        Get.find<ConversationsController>().reloadFavoriteIds();
      }
    }
  }
}
