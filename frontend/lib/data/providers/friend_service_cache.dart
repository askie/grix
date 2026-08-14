part of 'friend_service.dart';

class FriendServiceCacheApi {
  FriendServiceCacheApi(this._service);

  final FriendService _service;

  void resetForAccountSwitch() {
    _service.friendList.clear();
    _service.friendRequests.clear();
    _service._userProfileCache.clear();
    _service._missingUserProfiles.clear();
    _service.profileCacheVersion.value = 0;
  }

  String? getUserNickname(String userId) {
    final friend = _service.friendList.firstWhereOrNull(
      (f) => f.userId == userId,
    );
    if (friend != null) {
      return friend.nickname.isNotEmpty ? friend.nickname : friend.username;
    }

    final nickname =
        _service._userProfileCache[userId]?['nickname']?.trim() ?? '';
    if (nickname.isNotEmpty) return nickname;
    final username =
        _service._userProfileCache[userId]?['username']?.trim() ?? '';
    if (username.isNotEmpty) return username;
    return null;
  }

  FriendItem? getFriendItem(String userId) {
    final normalized = userId.trim();
    if (normalized.isEmpty) return null;
    return _service.friendList.firstWhereOrNull((f) => f.userId == normalized);
  }

  String? getFriendRemarkName(String userId) {
    final friend = getFriendItem(userId);
    if (friend == null) return null;
    final remarkName = friend.remarkName.trim();
    if (remarkName.isEmpty) return null;
    return remarkName;
  }

  String? getUserUsername(String userId) {
    final friend = _service.friendList.firstWhereOrNull(
      (f) => f.userId == userId,
    );
    if (friend != null) {
      final username = friend.username.trim();
      if (username.isNotEmpty) return username;
    }

    final username =
        _service._userProfileCache[userId]?['username']?.trim() ?? '';
    if (username.isNotEmpty) return username;
    return null;
  }

  String? getUserAvatarUrl(String userId) {
    final friend = _service.friendList.firstWhereOrNull(
      (f) => f.userId == userId,
    );
    if (friend != null) {
      final avatarUrl = friend.avatarUrl.trim();
      if (avatarUrl.isNotEmpty) return avatarUrl;
    }

    final avatarUrl =
        _service._userProfileCache[userId]?['avatar_url']?.trim() ?? '';
    if (avatarUrl.isNotEmpty) return avatarUrl;
    return null;
  }

  String? getUserIntroduction(String userId) {
    final friend = _service.friendList.firstWhereOrNull(
      (f) => f.userId == userId,
    );
    if (friend != null) {
      final introduction = friend.introduction.trim();
      if (introduction.isNotEmpty) return introduction;
    }

    final introduction =
        _service._userProfileCache[userId]?['introduction']?.trim() ?? '';
    if (introduction.isNotEmpty) return introduction;
    return null;
  }

  bool isFriend(String userId) {
    final normalized = userId.trim();
    if (normalized.isEmpty) return false;
    return _service.friendList.any((f) => f.userId == normalized);
  }

  Future<String?> fetchUserProfile(String userId) async {
    final normalizedUserId = userId.trim();
    if (normalizedUserId.isEmpty ||
        !FriendService._numericUserIdPattern.hasMatch(normalizedUserId)) {
      return null;
    }
    if (_service._missingUserProfiles.contains(normalizedUserId)) {
      return null;
    }

    if (_service._userProfileCache.containsKey(normalizedUserId)) {
      final cached =
          _service._userProfileCache[normalizedUserId] ??
          const <String, String>{};
      final nickname = cached['nickname']?.trim() ?? '';
      if (nickname.isNotEmpty) return nickname;
      final username = cached['username']?.trim() ?? '';
      if (username.isNotEmpty) return username;
      if (cached['is_visitor'] == 'true') return 'common_visitor'.tr;
      return null;
    }

    try {
      final resp = await _service._dio.get('/users/$normalizedUserId/profile');
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        final data = body['data'];
        if (data is! Map) {
          // 后端对“查无此人”（已注销用户、非本人访客等）返回 200 空数据，
          // 与 404 同样记入缺失缓存，避免刷新/重进会话时反复请求。
          _service._missingUserProfiles.add(normalizedUserId);
          return null;
        }
        final nickname = data['nickname']?.toString() ?? '';
        final username = data['username']?.toString() ?? '';
        final introduction = data['introduction']?.toString() ?? '';
        final avatarUrl = data['avatar_url']?.toString() ?? '';
        final isVisitor = data['is_visitor'] == true;
        final trimmedNickname = nickname.trim();
        final trimmedUsername = username.trim();
        final displayName = trimmedNickname.isNotEmpty
            ? trimmedNickname
            : (trimmedUsername.isNotEmpty
                  ? trimmedUsername
                  : (isVisitor ? 'common_visitor'.tr : null));
        _service._userProfileCache[normalizedUserId] = {
          'nickname': nickname,
          'username': username,
          'introduction': introduction,
          'avatar_url': avatarUrl,
          'is_visitor': isVisitor ? 'true' : 'false',
        };
        _service.profileCacheVersion.value++;
        return displayName;
      }
    } on DioException catch (e) {
      if (e.response?.statusCode == 404) {
        _service._missingUserProfiles.add(normalizedUserId);
        return null;
      }
      final errMsg = _extractErrorMessage(e);
      debugPrint('Fetch user profile error: $errMsg');
    } catch (e) {
      debugPrint('Fetch user profile unexpected error: $e');
    }
    return null;
  }

  Future<void> ensureUserProfiles(List<String> userIds) async {
    final unknown = userIds
        .where(
          (id) =>
              !_service.friendList.any((f) => f.userId == id) &&
              !_service._userProfileCache.containsKey(id) &&
              !_service._missingUserProfiles.contains(id),
        )
        .toList();
    if (unknown.isEmpty) return;
    await Future.wait(unknown.map((id) => fetchUserProfile(id)));
  }

  String _extractErrorMessage(DioException e) {
    var errMsg = e.message ?? _service._unknownError;
    if (e.response != null && e.response?.data is Map) {
      errMsg = e.response?.data['msg'] ?? errMsg;
    }
    return errMsg;
  }
}
