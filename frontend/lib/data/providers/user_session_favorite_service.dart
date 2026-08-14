import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart' hide Response;

import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

class FavoriteSessionItem {
  const FavoriteSessionItem({
    required this.sessionId,
    required this.sessionType,
    required this.title,
    required this.lastMsg,
    required this.favoritedAt,
    this.peerNickname,
  });

  final String sessionId;
  final int sessionType;
  final String title;
  final String lastMsg;
  final int favoritedAt;
  final String? peerNickname;

  factory FavoriteSessionItem.fromJson(Map<String, dynamic> json) {
    final peer = json['peer'] as Map<String, dynamic>?;
    return FavoriteSessionItem(
      sessionId: json['session_id']?.toString() ?? '',
      sessionType: (json['session_type'] as num?)?.toInt() ?? 1,
      title: json['title']?.toString() ?? '',
      lastMsg: json['last_msg']?.toString() ?? '',
      favoritedAt: (json['favorited_at'] as num?)?.toInt() ?? 0,
      peerNickname: peer?['nickname']?.toString(),
    );
  }
}

class UserSessionFavoriteService {
  UserSessionFavoriteService({Dio? dio})
      : _dio = dio ??
            Dio(BaseOptions(
              baseUrl: AppRuntimeEndpoints.apiBaseUrl,
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 10),
            ));

  final Dio _dio;
  bool _authAttached = false;

  void _ensureAuth() {
    if (_authAttached) return;
    try {
      Get.find<AuthService>().attachAuthInterceptor(_dio);
      _authAttached = true;
    } catch (_) {}
  }

  Future<List<FavoriteSessionItem>> list({int limit = 200, int offset = 0}) async {
    _ensureAuth();
    try {
      final resp = await _dio.get(
        '/sessions/favorites',
        queryParameters: {'limit': limit, 'offset': offset},
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        final data = body['data'];
        if (data is Map && data['list'] is List) {
          return (data['list'] as List)
              .map((e) => FavoriteSessionItem.fromJson(e as Map<String, dynamic>))
              .toList();
        }
      }
      return [];
    } catch (e) {
      debugPrint('[UserSessionFavoriteService][list] $e');
      return [];
    }
  }

  Future<List<String>> listIds() async {
    _ensureAuth();
    try {
      final resp = await _dio.get('/sessions/favorites/ids');
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        final data = body['data'];
        if (data is Map && data['session_ids'] is List) {
          return (data['session_ids'] as List).map((e) => e.toString()).toList();
        }
      }
      return [];
    } catch (e) {
      debugPrint('[UserSessionFavoriteService][listIds] $e');
      return [];
    }
  }

  Future<bool> add(String sessionId) async {
    _ensureAuth();
    try {
      final resp = await _dio.post('/sessions/$sessionId/favorite');
      final body = resp.data;
      return resp.statusCode == 200 && body is Map && body['code'] == 0;
    } on DioException catch (e) {
      if (e.response?.statusCode == 409) return true; // already favorited
      debugPrint('[UserSessionFavoriteService][add] $e');
      return false;
    } catch (e) {
      debugPrint('[UserSessionFavoriteService][add] $e');
      return false;
    }
  }

  Future<bool> remove(String sessionId) async {
    _ensureAuth();
    try {
      final resp = await _dio.delete('/sessions/$sessionId/favorite');
      final body = resp.data;
      return resp.statusCode == 200 && body is Map && body['code'] == 0;
    } on DioException catch (e) {
      if (e.response?.statusCode == 404) return true; // already removed
      debugPrint('[UserSessionFavoriteService][remove] $e');
      return false;
    } catch (e) {
      debugPrint('[UserSessionFavoriteService][remove] $e');
      return false;
    }
  }
}
