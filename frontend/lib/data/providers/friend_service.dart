import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';
import '../../shared/utils/app_runtime_endpoints.dart';
import '../../shared/utils/toast_util.dart';
import 'auth_service.dart';
import 'session_service.dart';

part 'friend_service_requests.dart';
part 'friend_service_cache.dart';
part 'friend_service_realtime.dart';

class FriendService extends GetxService {
  FriendService({Dio? dio}) : _providedDio = dio {
    _requestApi = FriendServiceRequestApi(this);
    _cacheApi = FriendServiceCacheApi(this);
    _realtimeApi = FriendServiceRealtimeApi(this);
  }

  final Dio? _providedDio;
  late final Dio _dio;
  late final FriendServiceRequestApi _requestApi;
  late final FriendServiceCacheApi _cacheApi;
  late final FriendServiceRealtimeApi _realtimeApi;

  final friendList = <FriendItem>[].obs;
  final friendRequests = <FriendRequestItem>[].obs;
  final profileCacheVersion = 0.obs;
  final _missingUserProfiles = <String>{};
  final _userProfileCache = <String, Map<String, String>>{};
  static final RegExp _numericUserIdPattern = RegExp(r'^\d+$');

  Future<FriendService> init() {
    return _requestApi.init();
  }

  String get _unknownError => 'common_unknown_error'.tr;

  Future<List<UserSearchItem>> searchUsers(String keyword) {
    return _requestApi.searchUsers(keyword);
  }

  Future<FriendRequestSendResult> sendFriendRequest({
    String? toUserId,
    String? toUsername,
    String message = '',
  }) {
    return _requestApi.sendFriendRequest(
      toUserId: toUserId,
      toUsername: toUsername,
      message: message,
    );
  }

  Future<void> loadFriendRequests() {
    return _requestApi.loadFriendRequests();
  }

  Future<bool> handleFriendRequest(String requestId, bool accept) {
    return _requestApi.handleFriendRequest(requestId, accept);
  }

  Future<void> loadFriendList() {
    return _requestApi.loadFriendList();
  }

  Future<bool> updateFriendRemark({
    required String friendUserId,
    required String remarkName,
  }) {
    return _requestApi.updateFriendRemark(
      friendUserId: friendUserId,
      remarkName: remarkName,
    );
  }

  void applyRealtimeEvent(Map<String, dynamic> payload) {
    _realtimeApi.applyRealtimeEvent(payload);
  }

  void resetForAccountSwitch() {
    _cacheApi.resetForAccountSwitch();
  }

  String? getUserNickname(String userId) {
    return _cacheApi.getUserNickname(userId);
  }

  FriendItem? getFriendItem(String userId) {
    return _cacheApi.getFriendItem(userId);
  }

  String? getFriendRemarkName(String userId) {
    return _cacheApi.getFriendRemarkName(userId);
  }

  String? getUserUsername(String userId) {
    return _cacheApi.getUserUsername(userId);
  }

  String? getUserAvatarUrl(String userId) {
    return _cacheApi.getUserAvatarUrl(userId);
  }

  String? getUserIntroduction(String userId) {
    return _cacheApi.getUserIntroduction(userId);
  }

  bool isFriend(String userId) {
    return _cacheApi.isFriend(userId);
  }

  Future<String?> fetchUserProfile(String userId) {
    return _cacheApi.fetchUserProfile(userId);
  }

  Future<void> ensureUserProfiles(List<String> userIds) {
    return _cacheApi.ensureUserProfiles(userIds);
  }

  Future<bool> deleteFriend(String friendUserId) {
    return _requestApi.deleteFriend(friendUserId);
  }

  Future<bool> blockUser(String blockedUserId) {
    return _requestApi.blockUser(blockedUserId);
  }

  Future<bool> setFriendPinned({
    required String friendUserId,
    required bool isPinned,
  }) async {
    final ok = await _requestApi.setFriendPinned(
      friendUserId: friendUserId,
      isPinned: isPinned,
    );
    if (ok && Get.isRegistered<SessionService>()) {
      // Friend-level pin drives the main conversation list; drop the 5s
      // first-page cache so stale pin state cannot be written back.
      Get.find<SessionService>().invalidateConversationFirstPageCache();
    }
    return ok;
  }
}

// --- Models ---

class UserSearchItem {
  final String id;
  final String username;
  final String nickname;
  final String introduction;
  final String avatarUrl;

  UserSearchItem({
    required this.id,
    required this.username,
    required this.nickname,
    this.introduction = '',
    required this.avatarUrl,
  });

  factory UserSearchItem.fromJson(Map<String, dynamic> json) {
    return UserSearchItem(
      id: _readId(json['id']),
      username: json['username'] ?? '',
      nickname: json['nickname'] ?? json['username'] ?? '',
      introduction: json['introduction']?.toString().trim() ?? '',
      avatarUrl: json['avatar_url'] ?? '',
    );
  }
}

class FriendRequestItem {
  final String id;
  final String fromUserId;
  final String username;
  final String nickname;
  final String introduction;
  final String avatarUrl;
  final String message;
  final int status; // 0=pending, 1=accepted, 2=rejected
  final String createdAt;

  FriendRequestItem({
    required this.id,
    required this.fromUserId,
    required this.username,
    required this.nickname,
    this.introduction = '',
    required this.avatarUrl,
    required this.message,
    required this.status,
    required this.createdAt,
  });

  factory FriendRequestItem.fromJson(Map<String, dynamic> json) {
    return FriendRequestItem(
      id: _readId(json['id']),
      fromUserId: _readId(json['from_user_id']),
      username: json['username'] ?? '',
      nickname: json['nickname'] ?? json['username'] ?? '',
      introduction: json['introduction']?.toString().trim() ?? '',
      avatarUrl: json['avatar_url'] ?? '',
      message: json['message'] ?? '',
      status: json['status'] ?? 0,
      createdAt: (json['created_at'] ?? '').toString(),
    );
  }

  FriendRequestItem copyWith({int? status}) {
    return FriendRequestItem(
      id: id,
      fromUserId: fromUserId,
      username: username,
      nickname: nickname,
      introduction: introduction,
      avatarUrl: avatarUrl,
      message: message,
      status: status ?? this.status,
      createdAt: createdAt,
    );
  }
}

class FriendItem {
  final String id;
  final String userId;
  final String username;
  final String nickname;
  final String remarkName;
  final String introduction;
  final String avatarUrl;

  FriendItem({
    required this.id,
    required this.userId,
    required this.username,
    required this.nickname,
    required this.remarkName,
    this.introduction = '',
    required this.avatarUrl,
  });

  factory FriendItem.fromJson(Map<String, dynamic> json) {
    return FriendItem(
      id: _readId(json['id']),
      userId: _readId(json['user_id']),
      username: json['username'] ?? '',
      nickname: json['nickname'] ?? json['username'] ?? '',
      remarkName: json['remark_name'] ?? '',
      introduction: json['introduction']?.toString().trim() ?? '',
      avatarUrl: json['avatar_url'] ?? '',
    );
  }

  FriendItem copyWith({
    String? id,
    String? userId,
    String? username,
    String? nickname,
    String? remarkName,
    String? introduction,
    String? avatarUrl,
  }) {
    return FriendItem(
      id: id ?? this.id,
      userId: userId ?? this.userId,
      username: username ?? this.username,
      nickname: nickname ?? this.nickname,
      remarkName: remarkName ?? this.remarkName,
      introduction: introduction ?? this.introduction,
      avatarUrl: avatarUrl ?? this.avatarUrl,
    );
  }
}

String _readId(dynamic value) {
  final raw = value?.toString().trim() ?? '';
  return raw;
}

class FriendRequestSendResult {
  const FriendRequestSendResult({
    required this.success,
    required this.autoApproved,
  });

  const FriendRequestSendResult.failed()
    : success = false,
      autoApproved = false;

  final bool success;
  final bool autoApproved;
}
