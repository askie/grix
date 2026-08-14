import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart' hide Response;
import 'package:grix/shared/utils/app_runtime_endpoints.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'auth_service.dart';

/// Service that manages feature flags with local cache + server refresh.
///
/// Startup flow:
/// 1. Read local SharedPreferences cache → UI immediately available
/// 2. Background: fetch from server → update memory + persist to cache
///
/// For unauthenticated users, fetches globally-enabled features from the
/// public API endpoint. For authenticated users, fetches user-specific
/// features (which may include whitelist-only features).
///
/// This avoids blocking startup on network and reduces unnecessary API calls.
class FeatureFlagService extends GetxService {
  FeatureFlagService({Dio? dio})
      : _dio = dio ??
            Dio(BaseOptions(
              baseUrl: AppRuntimeEndpoints.apiBaseUrl,
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 10),
            ));

  static const _prefsKey = 'feature_flags_cache';
  static const _maxRetries = 3;

  final Dio _dio;
  static bool _prefsUnavailableLogged = false;

  /// List of feature keys currently enabled for this user.
  final features = <String>[].obs;

  /// Whether the initial load (at least from cache) has completed.
  final hasLoaded = false.obs;

  /// Initializes the service: loads cache, then refreshes from server.
  Future<FeatureFlagService> init() async {
    final auth = Get.find<AuthService>();
    auth.attachAuthInterceptor(_dio);

    // Step 1: Load from local cache (instant, no network)
    await _loadFromCache();

    // Step 2: Refresh from server — use public or user API based on auth state
    _refresh();

    // Step 3: Listen for login state changes to trigger refresh
    ever(auth.isLoggedInRx, (_) => _refresh());

    return this;
  }

  /// Forces a re-fetch from the server (e.g. after the API region changes).
  void refresh() => _refresh();

  /// Refreshes features from the appropriate server endpoint.
  void _refresh() {
    final auth = Get.find<AuthService>();
    if (auth.isLoggedIn) {
      _refreshFromServer('/users/features');
    } else {
      _refreshFromServer('/features');
    }
  }

  /// Loads features from SharedPreferences cache.
  Future<void> _loadFromCache() async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;

    final raw = prefs.getString(_prefsKey);
    if (raw != null && raw.isNotEmpty) {
      try {
        final list = (jsonDecode(raw) as List).cast<String>();
        features.value = list;
      } catch (_) {
        // Corrupted cache, ignore
      }
    }
    hasLoaded.value = true;
  }

  /// Fetches features from the given server path, updates memory + persists to cache.
  /// Retries with exponential backoff on failure (handles iOS first-launch
  /// cellular permission dialog blocking network).
  Future<void> _refreshFromServer(String path, {int attempt = 0}) async {
    try {
      final response = await _dio.get(path);
      if (response.statusCode == 200 && response.data['code'] == 0) {
        final data = response.data['data'];
        if (data != null && data['features'] != null) {
          final list = (data['features'] as List).cast<String>();
          features.value = list;
          hasLoaded.value = true;
          await _saveToCache(list);
        }
      }
    } catch (_) {
      if (attempt < _maxRetries) {
        final delay = Duration(seconds: 2 << attempt);
        await Future<void>.delayed(delay);
        return _refreshFromServer(path, attempt: attempt + 1);
      }
    }
  }

  /// Persists features list to SharedPreferences.
  Future<void> _saveToCache(List<String> list) async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    await prefs.setString(_prefsKey, jsonEncode(list));
  }

  /// Returns true if the given feature key is enabled for this user.
  bool isEnabled(String key) => features.contains(key);

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
    debugPrint(
      'SharedPreferences unavailable, skip feature flags cache: $error',
    );
  }
}
