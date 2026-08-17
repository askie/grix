part of 'friend_service.dart';

class FriendServiceRequestApi {
  FriendServiceRequestApi(this._service);

  final FriendService _service;

  Future<FriendService> init() async {
    final authService = Get.find<AuthService>();
    _service._dio =
        _service._providedDio ??
        Dio(
          BaseOptions(
            baseUrl: AppRuntimeEndpoints.apiBaseUrl,
            connectTimeout: const Duration(seconds: 10),
            receiveTimeout: const Duration(seconds: 10),
          ),
        );
    authService.attachAuthInterceptor(_service._dio);
    return _service;
  }

  Future<List<UserSearchItem>> searchUsers(String keyword) async {
    try {
      final resp = await _service._dio.get(
        '/users/search',
        queryParameters: {'keyword': keyword},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final list = resp.data['data']['list'] as List;
        return list.map((e) => UserSearchItem.fromJson(e)).toList();
      }
    } on DioException catch (e) {
      final errMsg = _extractErrorMessage(e);
      debugPrint('Search users error: $errMsg');
    } catch (e) {
      debugPrint('Search users unexpected error: $e');
    }
    return [];
  }

  Future<FriendRequestSendResult> sendFriendRequest({
    String? toUserId,
    String? toUsername,
    String message = '',
  }) async {
    final normalizedUserId = (toUserId ?? '').trim();
    final payload = <String, dynamic>{'message': message};
    final normalizedName = (toUsername ?? '').trim();
    if (normalizedName.isNotEmpty) {
      payload['to_username'] = normalizedName;
    } else if (normalizedUserId.isNotEmpty) {
      payload['to_user_id'] = toUserId;
    } else {
      CustomToast.show('friend_invalid_target_user'.tr);
      return const FriendRequestSendResult.failed();
    }

    try {
      final resp = await _service._dio.post('/friends/request', data: payload);
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final autoApproved = await _detectAutoApproved(
          userId: normalizedUserId,
          username: normalizedName,
        );
        return FriendRequestSendResult(
          success: true,
          autoApproved: autoApproved,
        );
      }
      final msg = resp.data['msg'] ?? _service._unknownError;
      debugPrint('Send friend request failed: $msg');
      CustomToast.show(msg);
    } on DioException catch (e) {
      final errMsg = _extractErrorMessage(e);
      debugPrint('Send friend request error: $errMsg');
      CustomToast.show(errMsg);
    } catch (e) {
      debugPrint('Send friend request unexpected error: $e');
    }
    return const FriendRequestSendResult.failed();
  }

  Future<void> loadFriendRequests() async {
    try {
      final resp = await _service._dio.get('/friends/requests');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final list = resp.data['data']['list'] as List;
        _service.friendRequests.value = list
            .map((e) => FriendRequestItem.fromJson(e))
            .toList();
      }
    } on DioException catch (e) {
      final errMsg = _extractErrorMessage(e);
      debugPrint('Load friend requests error: $errMsg');
    } catch (e) {
      debugPrint('Load friend requests unexpected error: $e');
    }
  }

  Future<bool> handleFriendRequest(String requestId, bool accept) async {
    try {
      final resp = await _service._dio.post(
        '/friends/handle',
        data: {'request_id': requestId, 'accept': accept},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final idx = _service.friendRequests.indexWhere(
          (e) => e.id == requestId,
        );
        if (idx >= 0) {
          final req = _service.friendRequests[idx];
          _service.friendRequests[idx] = req.copyWith(status: accept ? 1 : 2);
          if (accept &&
              !_service.friendList.any((f) => f.userId == req.fromUserId)) {
            _service.friendList.insert(
              0,
              FriendItem(
                id: '',
                userId: req.fromUserId,
                username: req.username,
                nickname: req.nickname,
                remarkName: '',
                avatarUrl: req.avatarUrl,
              ),
            );
          }
        }
        return true;
      }
      final msg = resp.data['msg'] ?? _service._unknownError;
      debugPrint('Handle friend request failed: $msg');
      CustomToast.show(msg);
    } on DioException catch (e) {
      final errMsg = _extractErrorMessage(e);
      debugPrint('Handle friend request error: $errMsg');
      CustomToast.show(errMsg);
    } catch (e) {
      debugPrint('Handle friend request unexpected error: $e');
    }
    return false;
  }

  Future<void> loadFriendList() async {
    try {
      final resp = await _service._dio.get('/friends/list');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final list = resp.data['data']['list'] as List;
        _service.friendList.value = list
            .map((e) => FriendItem.fromJson(e))
            .toList();
      }
    } on DioException catch (e) {
      final errMsg = _extractErrorMessage(e);
      debugPrint('Load friend list error: $errMsg');
    } catch (e) {
      debugPrint('Load friend list unexpected error: $e');
    }
  }

  Future<bool> updateFriendRemark({
    required String friendUserId,
    required String remarkName,
  }) async {
    final normalizedUserId = friendUserId.trim();
    if (normalizedUserId.isEmpty) {
      CustomToast.show('friend_invalid_target_user'.tr);
      return false;
    }

    try {
      final resp = await _service._dio.post(
        '/friends/remark',
        data: {'friend_user_id': normalizedUserId, 'remark_name': remarkName},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final data = resp.data['data'];
        if (data is Map) {
          final friend = FriendItem.fromJson(Map<String, dynamic>.from(data));
          final idx = _service.friendList.indexWhere(
            (e) => e.userId == friend.userId,
          );
          if (idx >= 0) {
            _service.friendList[idx] = friend;
          } else {
            _service.friendList.insert(0, friend);
          }
        }
        return true;
      }
      final msg = resp.data['msg'] ?? _service._unknownError;
      debugPrint('Update friend remark failed: $msg');
      CustomToast.show(msg);
    } on DioException catch (e) {
      final errMsg = _extractErrorMessage(e);
      debugPrint('Update friend remark error: $errMsg');
      CustomToast.show(errMsg);
    } catch (e) {
      debugPrint('Update friend remark unexpected error: $e');
    }
    return false;
  }

  Future<bool> deleteFriend(String friendUserId) async {
    try {
      final resp = await _service._dio.delete('/friends/$friendUserId');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        _service.friendList.removeWhere((f) => f.userId == friendUserId);
        _service.friendRequests.removeWhere(
          (r) => r.fromUserId == friendUserId,
        );
        return true;
      }
      final msg = resp.data['msg'] ?? _service._unknownError;
      debugPrint('Delete friend failed: $msg');
      CustomToast.show(msg);
    } on DioException catch (e) {
      final errMsg = _extractErrorMessage(e);
      debugPrint('Delete friend error: $errMsg');
      CustomToast.show(errMsg);
    } catch (e) {
      debugPrint('Delete friend unexpected error: $e');
    }
    return false;
  }

  Future<bool> setFriendPinned({
    required String friendUserId,
    required bool isPinned,
  }) async {
    final normalizedUserId = friendUserId.trim();
    if (normalizedUserId.isEmpty) return false;

    try {
      final resp = await _service._dio.post(
        '/friends/pin',
        data: {'friend_user_id': normalizedUserId, 'is_pinned': isPinned},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return true;
      }
      final msg = resp.data['msg'] ?? _service._unknownError;
      debugPrint('Set friend pinned failed: $msg');
    } on DioException catch (e) {
      final errMsg = _extractErrorMessage(e);
      debugPrint('Set friend pinned error: $errMsg');
    } catch (e) {
      debugPrint('Set friend pinned unexpected error: $e');
    }
    return false;
  }

  Future<bool> setFriendMuted({
    required String friendUserId,
    required bool isMuted,
  }) async {
    final normalizedUserId = friendUserId.trim();
    if (normalizedUserId.isEmpty) return false;

    try {
      final resp = await _service._dio.post(
        '/friends/mute',
        data: {'friend_user_id': normalizedUserId, 'is_muted': isMuted},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return true;
      }
      final msg = resp.data['msg'] ?? _service._unknownError;
      debugPrint('Set friend muted failed: $msg');
    } on DioException catch (e) {
      final errMsg = _extractErrorMessage(e);
      debugPrint('Set friend muted error: $errMsg');
    } catch (e) {
      debugPrint('Set friend muted unexpected error: $e');
    }
    return false;
  }

  Future<bool> blockUser(String blockedUserId) async {
    final normalizedUserId = blockedUserId.trim();
    if (normalizedUserId.isEmpty) {
      CustomToast.show('friend_invalid_target_user'.tr);
      return false;
    }

    try {
      final resp = await _service._dio.post(
        '/friends/block',
        data: {'blocked_user_id': normalizedUserId},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        _service.friendList.removeWhere((f) => f.userId == normalizedUserId);
        _service.friendRequests.removeWhere(
          (r) => r.fromUserId == normalizedUserId,
        );
        return true;
      }
      final msg = resp.data['msg'] ?? _service._unknownError;
      debugPrint('Block user failed: $msg');
      CustomToast.show(msg);
    } on DioException catch (e) {
      final errMsg = _extractErrorMessage(e);
      debugPrint('Block user error: $errMsg');
      CustomToast.show(errMsg);
    } catch (e) {
      debugPrint('Block user unexpected error: $e');
    }
    return false;
  }

  Future<bool> _detectAutoApproved({
    required String userId,
    required String username,
  }) async {
    await _service.loadFriendList();
    if (userId.isNotEmpty) {
      return _service.friendList.any((f) => f.userId == userId);
    }
    if (username.isNotEmpty) {
      return _service.friendList.any((f) => f.username == username);
    }
    return false;
  }

  String _extractErrorMessage(DioException e, {String? fallbackMessage}) {
    var errMsg = e.message ?? fallbackMessage ?? _service._unknownError;
    if (e.response != null && e.response?.data is Map) {
      errMsg = e.response?.data['msg'] ?? errMsg;
    }
    return errMsg;
  }
}
