part of 'friend_service.dart';

class FriendServiceRealtimeApi {
  FriendServiceRealtimeApi(this._service);

  final FriendService _service;

  void applyRealtimeEvent(Map<String, dynamic> payload) {
    final event = payload['event']?.toString() ?? '';
    switch (event) {
      case 'friend_request_received':
        final reqRaw = payload['request'];
        if (reqRaw is Map) {
          final req = FriendRequestItem.fromJson(
            Map<String, dynamic>.from(reqRaw),
          );
          final idx = _service.friendRequests.indexWhere((e) => e.id == req.id);
          if (idx >= 0) {
            _service.friendRequests[idx] = req;
          } else {
            _service.friendRequests.insert(0, req);
          }
        }
        break;
      case 'friend_request_handled':
        final requestId = _toId(payload['request_id']);
        final status = _toInt(payload['status']);
        if (requestId.isEmpty || status < 0) return;
        final idx = _service.friendRequests.indexWhere(
          (e) => e.id == requestId,
        );
        if (idx >= 0) {
          _service.friendRequests[idx] = _service.friendRequests[idx].copyWith(
            status: status,
          );
        }
        break;
      case 'friend_added':
        final friendRaw = payload['friend'];
        if (friendRaw is Map) {
          final friend = FriendItem.fromJson(
            Map<String, dynamic>.from(friendRaw),
          );
          final idx = _service.friendList.indexWhere(
            (e) => e.userId == friend.userId,
          );
          if (idx >= 0) {
            _service.friendList[idx] = friend;
          } else {
            _service.friendList.insert(0, friend);
          }
        }
        break;
      case 'friend_remark_updated':
        final friendRaw = payload['friend'];
        if (friendRaw is Map) {
          final friend = FriendItem.fromJson(
            Map<String, dynamic>.from(friendRaw),
          );
          final idx = _service.friendList.indexWhere(
            (e) => e.userId == friend.userId,
          );
          if (idx >= 0) {
            _service.friendList[idx] = friend;
          } else {
            _service.friendList.insert(0, friend);
          }
        }
        break;
      case 'friend_deleted':
        final friendUserId = _toId(payload['friend_user_id']);
        if (friendUserId.isNotEmpty) {
          _service.friendList.removeWhere((e) => e.userId == friendUserId);
        }
        break;
    }
  }

  int _toInt(dynamic v) {
    if (v is int) return v;
    if (v is num) return v.toInt();
    return int.tryParse(v?.toString() ?? '') ?? -1;
  }

  String _toId(dynamic v) => _readId(v);
}
