import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

class AgentCategoryModel {
  const AgentCategoryModel({
    required this.id,
    required this.parentId,
    required this.name,
    this.sortOrder = 0,
    this.createdAt = 0,
    this.updatedAt = 0,
  });

  final String id;
  final String parentId;
  final String name;
  final int sortOrder;
  final int createdAt;
  final int updatedAt;

  factory AgentCategoryModel.fromJson(Map<String, dynamic> json) {
    return AgentCategoryModel(
      id: json['id']?.toString() ?? '',
      parentId: json['parent_id']?.toString() ?? '0',
      name: json['name']?.toString() ?? '',
      sortOrder: json['sort_order'] as int? ?? 0,
      createdAt: json['created_at'] as int? ?? 0,
      updatedAt: json['updated_at'] as int? ?? 0,
    );
  }

  Map<String, dynamic> toJson() => {
    'id': id,
    'parent_id': parentId,
    'name': name,
    'sort_order': sortOrder,
    'created_at': createdAt,
    'updated_at': updatedAt,
  };
}

class AgentCategoryService extends GetxService {
  AgentCategoryService({Dio? dio})
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
  final categories = <AgentCategoryModel>[].obs;
  final hasLoaded = false.obs;
  Future<void>? _restoreCachedCategoriesTask;
  Future<void>? _syncCategoriesFromRemoteTask;
  bool _hasRestoredCache = false;

  String _lastOperationError = '';
  int _lastOperationCode = 0;

  String get lastOperationError => _lastOperationError;
  int get lastOperationCode => _lastOperationCode;

  bool _prefsUnavailableLogged = false;

  String get _cacheKey {
    final uid = Get.find<AuthService>().userId?.trim() ?? '';
    return 'agent_categories_$uid';
  }

  Future<SharedPreferences?> _safeGetPrefs() async {
    try {
      return await SharedPreferences.getInstance();
    } on MissingPluginException catch (e) {
      _logPrefsUnavailable(e);
      return null;
    } on PlatformException catch (e) {
      _logPrefsUnavailable(e);
      return null;
    }
  }

  void _logPrefsUnavailable(Object error) {
    if (_prefsUnavailableLogged) return;
    _prefsUnavailableLogged = true;
    debugPrint('SharedPreferences unavailable, skip category cache: $error');
  }

  Future<void> _saveToCache() async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    final json = jsonEncode(categories.map((c) => c.toJson()).toList());
    await prefs.setString(_cacheKey, json);
  }

  Future<void> _loadFromCache() async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    final raw = prefs.getString(_cacheKey);
    if (raw == null || raw.isEmpty) return;
    try {
      final list = (jsonDecode(raw) as List)
          .map((e) => AgentCategoryModel.fromJson(e as Map<String, dynamic>))
          .toList();
      if (list.isNotEmpty) {
        categories.value = list;
        hasLoaded.value = true;
      }
    } catch (_) {}
  }

  void resetForAccountSwitch() {
    categories.clear();
    hasLoaded.value = false;
    _restoreCachedCategoriesTask = null;
    _syncCategoriesFromRemoteTask = null;
    _hasRestoredCache = false;
  }

  Future<AgentCategoryService> init() async {
    Get.find<AuthService>().attachAuthInterceptor(_dio);
    return this;
  }

  String get _unknownError => 'common_unknown_error'.tr;

  void _clearOperationError() {
    _lastOperationError = '';
    _lastOperationCode = 0;
  }

  void _setOperationError(String message, {int code = 50001}) {
    final normalized = message.trim().isEmpty ? _unknownError : message.trim();
    _lastOperationError = normalized;
    _lastOperationCode = code;
  }

  String _responseMessage(dynamic body) {
    if (body is Map) {
      if (body['msg'] != null) {
        return body['msg'].toString();
      }
    }
    return _unknownError;
  }

  int _responseCode(dynamic body) {
    if (body is! Map) return 50001;
    final value = body['code'];
    if (value is int) return value;
    if (value is String) return int.tryParse(value.trim()) ?? 50001;
    return 50001;
  }

  bool _isUnauthorizedError(Object error) {
    if (error is! DioException) return false;
    if (error.response?.statusCode == 401) return true;
    final body = error.response?.data;
    if (body is! Map) return false;
    return body['code'] == 10001;
  }

  String _extractError(Object e) {
    if (e is DioException) {
      final data = e.response?.data;
      if (data is Map) return _responseMessage(data);
      if (e.response != null)
        return 'HTTP ${e.response!.statusCode}: ${e.response!.statusMessage}';
      return e.message ?? e.toString();
    }
    return e.toString();
  }

  void _reportErrorIfNeeded(
    String operation,
    String message, {
    dynamic body,
    Object? error,
  }) {
    if (error != null && _isUnauthorizedError(error)) return;
    if (body is Map && body['code'] == 10001) return;
    debugPrint('[AgentCategoryService][$operation] $message');
  }

  Future<void> restoreCachedCategories() {
    if (_hasRestoredCache) {
      return Future<void>.value();
    }
    final inflightTask = _restoreCachedCategoriesTask;
    if (inflightTask != null) {
      return inflightTask;
    }
    final future = _restoreCachedCategoriesInternal();
    _restoreCachedCategoriesTask = future;
    future.whenComplete(() {
      if (identical(_restoreCachedCategoriesTask, future)) {
        _restoreCachedCategoriesTask = null;
      }
    });
    return future;
  }

  Future<void> _restoreCachedCategoriesInternal() async {
    await _loadFromCache();
    _hasRestoredCache = true;
  }

  Future<void> syncCategoriesFromRemote() {
    final inflightTask = _syncCategoriesFromRemoteTask;
    if (inflightTask != null) {
      return inflightTask;
    }
    final future = _syncCategoriesFromRemoteInternal();
    _syncCategoriesFromRemoteTask = future;
    future.whenComplete(() {
      if (identical(_syncCategoriesFromRemoteTask, future)) {
        _syncCategoriesFromRemoteTask = null;
      }
    });
    return future;
  }

  Future<void> _syncCategoriesFromRemoteInternal() async {
    try {
      final resp = await _dio.get('/agents/categories/list');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final rawData = resp.data['data'];
        final rawList = rawData is List
            ? rawData
            : (rawData is Map ? rawData['list'] : null);
        final list =
            (rawList as List?)
                ?.map(
                  (e) => AgentCategoryModel.fromJson(e as Map<String, dynamic>),
                )
                .toList() ??
            [];
        categories.value = list;
        hasLoaded.value = true;
        await _saveToCache();
      } else {
        final msg = _responseMessage(resp.data);
        _reportErrorIfNeeded('loadCategories', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('loadCategories', msg, error: e);
    }
  }

  Future<void> loadCategories() async {
    await restoreCachedCategories();
    await syncCategoriesFromRemote();
  }

  Future<AgentCategoryModel?> createCategory({
    required String name,
    required String parentId,
  }) async {
    _clearOperationError();
    try {
      final resp = await _dio.post(
        '/agents/categories/create',
        data: {'name': name, 'parent_id': parentId},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final updated = AgentCategoryModel.fromJson(resp.data['data']);
        categories.add(updated);
        await _saveToCache();
        return updated;
      } else {
        final code = _responseCode(resp.data);
        final msg = _responseMessage(resp.data);
        _setOperationError(msg, code: code);
        _reportErrorIfNeeded('createCategory', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _setOperationError(msg);
      _reportErrorIfNeeded('createCategory', msg, error: e);
    }
    return null;
  }

  Future<AgentCategoryModel?> updateCategory(
    String id, {
    required String name,
    required String parentId,
    int? sortOrder,
  }) async {
    _clearOperationError();
    try {
      final data = <String, dynamic>{'name': name, 'parent_id': parentId};
      if (sortOrder != null) data['sort_order'] = sortOrder;
      final resp = await _dio.put('/agents/categories/$id', data: data);
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final updated = AgentCategoryModel.fromJson(resp.data['data']);
        final idx = categories.indexWhere((c) => c.id == id);
        if (idx != -1) {
          categories[idx] = updated;
        } else {
          categories.add(updated);
        }
        await _saveToCache();
        return updated;
      } else {
        final code = _responseCode(resp.data);
        final msg = _responseMessage(resp.data);
        _setOperationError(msg, code: code);
        _reportErrorIfNeeded('updateCategory', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _setOperationError(msg);
      _reportErrorIfNeeded('updateCategory', msg, error: e);
    }
    return null;
  }

  Future<bool> deleteCategory(String id) async {
    _clearOperationError();
    try {
      final resp = await _dio.delete('/agents/categories/$id');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        categories.removeWhere((c) => c.id == id);
        await _saveToCache();
        return true;
      } else {
        final code = _responseCode(resp.data);
        final msg = _responseMessage(resp.data);
        _setOperationError(msg, code: code);
        _reportErrorIfNeeded('deleteCategory', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _setOperationError(msg);
      _reportErrorIfNeeded('deleteCategory', msg, error: e);
    }
    return false;
  }
}
