import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

/// One configurable notification event row, mirroring the backend
/// notification_prefs model (event_key + enabled + channels).
class NotificationPref {
  NotificationPref({
    required this.eventKey,
    required this.enabled,
    required this.channels,
  });

  final String eventKey;
  bool enabled;
  List<String> channels;

  factory NotificationPref.fromJson(Map data) {
    final rawChannels = data['channels'];
    final channels = <String>[];
    if (rawChannels is List) {
      for (final c in rawChannels) {
        final s = c?.toString().trim() ?? '';
        if (s.isNotEmpty) channels.add(s);
      }
    }
    return NotificationPref(
      eventKey: data['event_key']?.toString() ?? '',
      enabled: data['enabled'] == true,
      channels: channels.isEmpty ? ['push'] : channels,
    );
  }

  Map<String, dynamic> toJson() => {
    'event_key': eventKey,
    'enabled': enabled,
    'channels': channels,
  };

  bool hasChannel(String ch) => channels.contains(ch);
}

/// Loads and updates the user's Agent-notification preferences, and caches the
/// offline-callback URL to the native side so push action buttons can reach the
/// backend even when the app has been killed.
class NotificationPrefsService extends GetxService {
  static const String eventApprovalRequested = 'approval_requested';
  static const String channelPush = 'push';
  static const String channelTts = 'tts';

  static const MethodChannel _notifyConfigChannel = MethodChannel(
    'pub.dhf.grix/notify_action',
  );

  NotificationPrefsService({Dio? dio})
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
  final RxList<NotificationPref> prefs = <NotificationPref>[].obs;
  final RxBool isLoading = false.obs;
  final RxBool isSaving = false.obs;

  String _loadedUserId = '';

  Future<NotificationPrefsService> init() async {
    Get.find<AuthService>().attachAuthInterceptor(_dio);
    await cacheCallbackConfig();
    return this;
  }

  /// Tells the native layer where to POST offline action callbacks. The URL is
  /// the same host as the API base, path /notify-callback (ingress routes it to
  /// the ws service). Stored natively so it survives app termination.
  Future<void> cacheCallbackConfig() async {
    if (kIsWeb) return;
    try {
      final base = AppRuntimeEndpoints.apiBaseUrl;
      final url = '${base.replaceFirst(RegExp(r'/+$'), '')}/notify-callback';
      await _notifyConfigChannel.invokeMethod('setCallbackUrl', {'url': url});
    } catch (e) {
      debugPrint('[NotificationPrefsService][cacheCallbackConfig] $e');
    }
  }

  void resetForAccountSwitch() {
    _loadedUserId = '';
    prefs.clear();
    isLoading.value = false;
    isSaving.value = false;
  }

  Future<void> ensureSyncedWithCurrentUser({bool force = false}) async {
    final userId = Get.find<AuthService>().userId?.trim() ?? '';
    if (userId.isEmpty) {
      resetForAccountSwitch();
      return;
    }
    if (!force && _loadedUserId == userId && prefs.isNotEmpty) {
      return;
    }
    _loadedUserId = userId;
    await _load();
  }

  Future<void> _load() async {
    isLoading.value = true;
    try {
      final resp = await _dio.get('/notification-prefs');
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        final data = body['data'];
        final list = <NotificationPref>[];
        if (data is List) {
          for (final item in data) {
            if (item is Map) list.add(NotificationPref.fromJson(item));
          }
        }
        prefs.assignAll(list);
        return;
      }
      debugPrint('[NotificationPrefsService][load] unexpected: $body');
    } on DioException catch (e) {
      debugPrint('[NotificationPrefsService][load] ${e.message}');
    } catch (e) {
      debugPrint('[NotificationPrefsService][load] $e');
    } finally {
      isLoading.value = false;
    }
  }

  /// Updates a single preference and persists the full set. approval_requested
  /// cannot be disabled (server also enforces this); the UI keeps its switch on.
  Future<bool> updatePref(
    String eventKey, {
    bool? enabled,
    List<String>? channels,
  }) async {
    final idx = prefs.indexWhere((p) => p.eventKey == eventKey);
    if (idx < 0) return false;
    final current = prefs[idx];
    final nextEnabled = eventKey == eventApprovalRequested
        ? true
        : (enabled ?? current.enabled);
    final next = NotificationPref(
      eventKey: eventKey,
      enabled: nextEnabled,
      channels: channels ?? current.channels,
    );

    isSaving.value = true;
    try {
      final resp = await _dio.put(
        '/notification-prefs',
        data: {
          'prefs': [next.toJson()],
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        prefs[idx] = next;
        prefs.refresh();
        return true;
      }
      debugPrint('[NotificationPrefsService][updatePref] unexpected: $body');
      return false;
    } on DioException catch (e) {
      debugPrint('[NotificationPrefsService][updatePref] ${e.message}');
      return false;
    } catch (e) {
      debugPrint('[NotificationPrefsService][updatePref] $e');
      return false;
    } finally {
      isSaving.value = false;
    }
  }
}
