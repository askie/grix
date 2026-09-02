import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

class FavoritePathItem {
  const FavoritePathItem({
    required this.id,
    required this.path,
    required this.name,
    required this.isDirectory,
    required this.machineName,
    required this.createdAt,
  });

  final String id;
  final String path;
  final String name;
  final bool isDirectory;

  /// 该收藏所属机器名。老数据可能为空（归"未知机器"分组）。
  final String machineName;
  final String createdAt;

  @override
  bool operator ==(Object other) =>
      identical(this, other) || other is FavoritePathItem && id == other.id;

  @override
  int get hashCode => id.hashCode;

  factory FavoritePathItem.fromJson(Map<String, dynamic> json) {
    return FavoritePathItem(
      id: json['id']?.toString() ?? '',
      path: json['path']?.toString() ?? '',
      name: json['name']?.toString() ?? '',
      isDirectory: json['is_directory'] == true,
      machineName: json['machine_name']?.toString() ?? '',
      createdAt: json['created_at']?.toString() ?? '',
    );
  }
}

class UserFavoritePathService {
  UserFavoritePathService({Dio? dio})
    : _dio =
          dio ??
          Dio(
            BaseOptions(
              baseUrl: AppRuntimeEndpoints.apiBaseUrl,
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 10),
            ),
          );

  final Dio _dio;
  bool _authAttached = false;

  void _ensureAuth() {
    if (_authAttached) return;
    _authAttached = true;
    try {
      Get.find<AuthService>().attachAuthInterceptor(_dio);
    } catch (_) {}
  }

  Future<List<FavoritePathItem>> list() async {
    _ensureAuth();
    try {
      final resp = await _dio.get('/users/favorites/paths/list');
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        final data = body['data'];
        if (data is List) {
          return data
              .map((e) => FavoritePathItem.fromJson(e as Map<String, dynamic>))
              .toList();
        }
        return [];
      }
      return [];
    } catch (e) {
      debugPrint('[UserFavoritePathService][list] $e');
      return [];
    }
  }

  Future<FavoritePathItem?> add(
    String path,
    String name,
    bool isDirectory, {
    String machineName = '',
  }) async {
    _ensureAuth();
    try {
      final resp = await _dio.post(
        '/users/favorites/paths/add',
        data: {
          'path': path,
          'name': name,
          'is_directory': isDirectory,
          'machine_name': machineName,
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        final data = body['data'];
        if (data is Map<String, dynamic>) {
          return FavoritePathItem.fromJson(data);
        }
      }
      return null;
    } on DioException catch (e) {
      if (e.response?.statusCode == 409) {
        return null;
      }
      debugPrint('[UserFavoritePathService][add] $e');
      return null;
    } catch (e) {
      debugPrint('[UserFavoritePathService][add] $e');
      return null;
    }
  }

  Future<bool> delete(String id) async {
    _ensureAuth();
    try {
      final resp = await _dio.delete('/users/favorites/paths/$id');
      final body = resp.data;
      return resp.statusCode == 200 && body is Map && body['code'] == 0;
    } catch (e) {
      debugPrint('[UserFavoritePathService][delete] $e');
      return false;
    }
  }
}
