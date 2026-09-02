import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../app/locale/locale_change_coordinator.dart';
import '../../app/locale/locale_service.dart';
import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

class UserSettingsService extends GetxService {
  static const int friendAddSettingNeedApproval = 1;
  static const int friendAddSettingAutoApprove = 2;
  static const int friendAddSettingForbidden = 3;

  UserSettingsService({Dio? dio})
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
  final RxString autoDelegateAgentId = ''.obs;
  final RxString voiceAutoDelegateAgentId = ''.obs;
  final RxString voiceBrainAgentId = ''.obs;
  // 语音大脑工作模式：true=豆包实时互动(端到端+502背景注入)，false=STT+TTS 念稿兜底。默认 true。
  final RxBool voiceBrainRealtime = true.obs;
  final RxString preferredLanguage = 'zh'.obs;
  final RxInt friendAddSetting = friendAddSettingNeedApproval.obs;
  final RxBool allowGroupInvite = true.obs;
  final RxBool isLoading = false.obs;
  final RxBool isSaving = false.obs;

  String _loadedUserId = '';
  Future<void>? _syncFuture;

  String get _unknownError => 'common_unknown_error'.tr;

  Future<UserSettingsService> init() async {
    Get.find<AuthService>().attachAuthInterceptor(_dio);
    return this;
  }

  void resetForAccountSwitch() {
    _loadedUserId = '';
    _syncFuture = null;
    autoDelegateAgentId.value = '';
    voiceAutoDelegateAgentId.value = '';
    voiceBrainAgentId.value = '';
    voiceBrainRealtime.value = true;
    preferredLanguage.value = 'zh';
    friendAddSetting.value = friendAddSettingNeedApproval;
    allowGroupInvite.value = true;
    isLoading.value = false;
    isSaving.value = false;
  }

  Future<void> ensureSyncedWithCurrentUser({bool force = false}) async {
    final userId = Get.find<AuthService>().userId?.trim() ?? '';
    if (userId.isEmpty) {
      resetForAccountSwitch();
      return;
    }

    if (!force && _loadedUserId == userId && !isLoading.value) {
      return;
    }
    if (!force && _loadedUserId == userId && _syncFuture != null) {
      await _syncFuture;
      return;
    }

    _loadedUserId = userId;
    final syncFuture = _loadSettings();
    _syncFuture = syncFuture;
    await syncFuture;
    if (identical(_syncFuture, syncFuture)) {
      _syncFuture = null;
    }
  }

  Future<bool> updateAutoDelegateAgentId(String? agentId) async {
    final userId = Get.find<AuthService>().userId?.trim() ?? '';
    if (userId.isEmpty) {
      return false;
    }

    if (_loadedUserId != userId) {
      await ensureSyncedWithCurrentUser(force: true);
    }

    final normalized = agentId?.trim() ?? '';
    isSaving.value = true;
    try {
      final resp = await _dio.put(
        '/users/settings',
        data: {
          'chat': {
            'auto_delegate_agent_id': normalized.isEmpty ? null : normalized,
          },
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        _applySettingsFromBody(body);
        return true;
      }
      final message = body is Map
          ? body['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('[UserSettingsService][updateAutoDelegateAgentId] $message');
      return false;
    } on DioException catch (e) {
      final data = e.response?.data;
      final message = data is Map
          ? data['msg']?.toString() ?? _unknownError
          : (e.message ?? _unknownError);
      debugPrint('[UserSettingsService][updateAutoDelegateAgentId] $message');
      return false;
    } catch (e) {
      debugPrint('[UserSettingsService][updateAutoDelegateAgentId] $e');
      return false;
    } finally {
      isSaving.value = false;
    }
  }

  Future<bool> updateVoiceAutoDelegateAgentId(String? agentId) async {
    final userId = Get.find<AuthService>().userId?.trim() ?? '';
    if (userId.isEmpty) {
      return false;
    }
    if (_loadedUserId != userId) {
      await ensureSyncedWithCurrentUser(force: true);
    }
    final normalized = agentId?.trim() ?? '';
    isSaving.value = true;
    try {
      final resp = await _dio.put(
        '/users/settings',
        data: {
          'chat': {
            'voice_auto_delegate_agent_id': normalized.isEmpty
                ? null
                : normalized,
          },
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        _applySettingsFromBody(body);
        return true;
      }
      return false;
    } on DioException catch (e) {
      debugPrint(
        '[UserSettingsService][updateVoiceAutoDelegateAgentId] ${e.message}',
      );
      return false;
    } catch (e) {
      debugPrint('[UserSettingsService][updateVoiceAutoDelegateAgentId] $e');
      return false;
    } finally {
      isSaving.value = false;
    }
  }

  Future<bool> updateVoiceBrainAgentId(String? agentId) async {
    final userId = Get.find<AuthService>().userId?.trim() ?? '';
    if (userId.isEmpty) {
      return false;
    }
    if (_loadedUserId != userId) {
      await ensureSyncedWithCurrentUser(force: true);
    }
    final normalized = agentId?.trim() ?? '';
    isSaving.value = true;
    try {
      final resp = await _dio.put(
        '/users/settings',
        data: {
          'chat': {
            'voice_brain_agent_id': normalized.isEmpty ? null : normalized,
          },
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        _applySettingsFromBody(body);
        return true;
      }
      return false;
    } on DioException catch (e) {
      debugPrint('[UserSettingsService][updateVoiceBrainAgentId] ${e.message}');
      return false;
    } catch (e) {
      debugPrint('[UserSettingsService][updateVoiceBrainAgentId] $e');
      return false;
    } finally {
      isSaving.value = false;
    }
  }

  Future<bool> updatePreferredLanguage(String languageTag) async {
    final userId = Get.find<AuthService>().userId?.trim() ?? '';
    if (userId.isEmpty) {
      return false;
    }

    if (_loadedUserId != userId) {
      await ensureSyncedWithCurrentUser(force: true);
    }

    final normalized = _normalizePreferredLanguage(languageTag);
    isSaving.value = true;
    try {
      final resp = await _dio.put(
        '/users/settings',
        data: {'preferred_language': normalized},
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        _applySettingsFromBody(body);
        return true;
      }
      final message = body is Map
          ? body['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('[UserSettingsService][updatePreferredLanguage] $message');
      return false;
    } on DioException catch (e) {
      final data = e.response?.data;
      final message = data is Map
          ? data['msg']?.toString() ?? _unknownError
          : (e.message ?? _unknownError);
      debugPrint('[UserSettingsService][updatePreferredLanguage] $message');
      return false;
    } catch (e) {
      debugPrint('[UserSettingsService][updatePreferredLanguage] $e');
      return false;
    } finally {
      isSaving.value = false;
    }
  }

  Future<bool> updateFriendAddSetting(int nextSetting) async {
    if (!_isValidFriendAddSetting(nextSetting)) {
      return false;
    }

    final userId = Get.find<AuthService>().userId?.trim() ?? '';
    if (userId.isEmpty) {
      return false;
    }

    if (_loadedUserId != userId) {
      await ensureSyncedWithCurrentUser(force: true);
    }

    isSaving.value = true;
    try {
      final resp = await _dio.put(
        '/users/settings',
        data: {
          'chat': {'friend_add_setting': nextSetting},
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        _applySettingsFromBody(body);
        return true;
      }
      final message = body is Map
          ? body['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('[UserSettingsService][updateFriendAddSetting] $message');
      return false;
    } on DioException catch (e) {
      final data = e.response?.data;
      final message = data is Map
          ? data['msg']?.toString() ?? _unknownError
          : (e.message ?? _unknownError);
      debugPrint('[UserSettingsService][updateFriendAddSetting] $message');
      return false;
    } catch (e) {
      debugPrint('[UserSettingsService][updateFriendAddSetting] $e');
      return false;
    } finally {
      isSaving.value = false;
    }
  }

  Future<bool> updateAllowGroupInvite(bool nextValue) async {
    final userId = Get.find<AuthService>().userId?.trim() ?? '';
    if (userId.isEmpty) {
      return false;
    }

    if (_loadedUserId != userId) {
      await ensureSyncedWithCurrentUser(force: true);
    }

    isSaving.value = true;
    try {
      final resp = await _dio.put(
        '/users/settings',
        data: {
          'chat': {'allow_group_invite': nextValue},
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        _applySettingsFromBody(body);
        return true;
      }
      final message = body is Map
          ? body['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('[UserSettingsService][updateAllowGroupInvite] $message');
      return false;
    } on DioException catch (e) {
      final data = e.response?.data;
      final message = data is Map
          ? data['msg']?.toString() ?? _unknownError
          : (e.message ?? _unknownError);
      debugPrint('[UserSettingsService][updateAllowGroupInvite] $message');
      return false;
    } catch (e) {
      debugPrint('[UserSettingsService][updateAllowGroupInvite] $e');
      return false;
    } finally {
      isSaving.value = false;
    }
  }

  Future<bool> updateVoiceBrainRealtime(bool nextValue) async {
    final userId = Get.find<AuthService>().userId?.trim() ?? '';
    if (userId.isEmpty) {
      return false;
    }

    if (_loadedUserId != userId) {
      await ensureSyncedWithCurrentUser(force: true);
    }

    isSaving.value = true;
    try {
      final resp = await _dio.put(
        '/users/settings',
        data: {
          'chat': {'voice_brain_realtime': nextValue},
        },
      );
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        _applySettingsFromBody(body);
        return true;
      }
      final message = body is Map
          ? body['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('[UserSettingsService][updateVoiceBrainRealtime] $message');
      return false;
    } on DioException catch (e) {
      final data = e.response?.data;
      final message = data is Map
          ? data['msg']?.toString() ?? _unknownError
          : (e.message ?? _unknownError);
      debugPrint('[UserSettingsService][updateVoiceBrainRealtime] $message');
      return false;
    } catch (e) {
      debugPrint('[UserSettingsService][updateVoiceBrainRealtime] $e');
      return false;
    } finally {
      isSaving.value = false;
    }
  }

  Future<void> _loadSettings() async {
    isLoading.value = true;
    try {
      final resp = await _dio.get('/users/settings');
      final body = resp.data;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        _applySettingsFromBody(body);
        return;
      }
      final message = body is Map
          ? body['msg']?.toString() ?? _unknownError
          : _unknownError;
      debugPrint('[UserSettingsService][load] $message');
      _resetSettingsValues();
    } on DioException catch (e) {
      final data = e.response?.data;
      final message = data is Map
          ? data['msg']?.toString() ?? _unknownError
          : (e.message ?? _unknownError);
      debugPrint('[UserSettingsService][load] $message');
      _resetSettingsValues();
    } catch (e) {
      debugPrint('[UserSettingsService][load] $e');
      _resetSettingsValues();
    } finally {
      isLoading.value = false;
    }
  }

  void _applySettingsFromBody(Map body) {
    autoDelegateAgentId.value = _parseAutoDelegateAgentId(body);
    voiceAutoDelegateAgentId.value = _parseChatStringField(
      body,
      'voice_auto_delegate_agent_id',
    );
    voiceBrainAgentId.value = _parseChatStringField(
      body,
      'voice_brain_agent_id',
    );
    voiceBrainRealtime.value = _parseVoiceBrainRealtime(body);
    preferredLanguage.value = _parsePreferredLanguage(body);
    friendAddSetting.value = _parseFriendAddSetting(body);
    allowGroupInvite.value = _parseAllowGroupInvite(body);
    _syncLocaleFromPreference(preferredLanguage.value);
  }

  /// 将服务端返回的语言偏好同步到 UI locale（仅当本地无已保存偏好时才覆盖）。
  /// 本地已有明确保存的偏好且与服务端不一致时（典型路径：登录前在登录页选过
  /// 语言，当时未登录只保存到本地，服务端 preferred_language 仍是默认 zh），
  /// 反向把本地选择推送到服务端，避免 agent 工具栏等按服务端
  /// preferred_language 渲染的文案与 UI 语言不一致。
  void _syncLocaleFromPreference(String lang) {
    final serverLocale = LocaleService.supportedLocales
        .where((e) => e.locale.languageCode == lang)
        .map((e) => e.locale)
        .firstOrNull;
    if (serverLocale == null) return;
    final current = Get.locale;
    if (current?.languageCode == serverLocale.languageCode) return;
    // 只在本地没有明确保存过偏好时才跟随服务端（避免覆盖用户本地选择）
    LocaleService.loadSavedLocale().then((saved) {
      if (saved == null) {
        LocaleChangeCoordinator.changeLocale(serverLocale);
        return;
      }
      if (saved.languageCode != serverLocale.languageCode) {
        updatePreferredLanguage(saved.languageCode);
      }
    });
  }

  void _resetSettingsValues() {
    autoDelegateAgentId.value = '';
    voiceAutoDelegateAgentId.value = '';
    voiceBrainAgentId.value = '';
    voiceBrainRealtime.value = true;
    preferredLanguage.value = 'zh';
    friendAddSetting.value = friendAddSettingNeedApproval;
    allowGroupInvite.value = true;
  }

  String _parsePreferredLanguage(Map body) {
    final data = body['data'];
    if (data is! Map) {
      return 'zh';
    }
    return _normalizePreferredLanguage(data['preferred_language']?.toString());
  }

  String _parseAutoDelegateAgentId(Map body) {
    final data = body['data'];
    if (data is! Map) {
      return '';
    }
    final chat = data['chat'];
    if (chat is! Map) {
      return '';
    }
    return chat['auto_delegate_agent_id']?.toString().trim() ?? '';
  }

  String _parseChatStringField(Map body, String key) {
    final data = body['data'];
    if (data is! Map) return '';
    final chat = data['chat'];
    if (chat is! Map) return '';
    return chat[key]?.toString().trim() ?? '';
  }

  int _parseFriendAddSetting(Map body) {
    final data = body['data'];
    if (data is! Map) {
      return friendAddSettingNeedApproval;
    }
    final chat = data['chat'];
    if (chat is! Map) {
      return friendAddSettingNeedApproval;
    }
    final parsed = int.tryParse(chat['friend_add_setting']?.toString() ?? '');
    if (parsed == null || !_isValidFriendAddSetting(parsed)) {
      return friendAddSettingNeedApproval;
    }
    return parsed;
  }

  bool _parseAllowGroupInvite(Map body) {
    final data = body['data'];
    if (data is! Map) {
      return true;
    }
    final chat = data['chat'];
    if (chat is! Map) {
      return true;
    }
    final raw = chat['allow_group_invite'];
    if (raw is bool) {
      return raw;
    }
    final normalized = raw?.toString().trim().toLowerCase() ?? '';
    if (normalized == 'false' || normalized == '0') {
      return false;
    }
    if (normalized == 'true' || normalized == '1') {
      return true;
    }
    return true;
  }

  bool _parseVoiceBrainRealtime(Map body) {
    final data = body['data'];
    if (data is! Map) {
      return true;
    }
    final chat = data['chat'];
    if (chat is! Map) {
      return true;
    }
    final raw = chat['voice_brain_realtime'];
    if (raw is bool) {
      return raw;
    }
    final normalized = raw?.toString().trim().toLowerCase() ?? '';
    if (normalized == 'false' || normalized == '0') {
      return false;
    }
    return true;
  }

  bool _isValidFriendAddSetting(int value) {
    return value == friendAddSettingNeedApproval ||
        value == friendAddSettingAutoApprove ||
        value == friendAddSettingForbidden;
  }

  String _normalizePreferredLanguage(String? raw) {
    final normalized = (raw?.trim().toLowerCase() ?? '').replaceAll('-', '_');
    const supported = [
      'zh',
      'en',
      'ja',
      'ko',
      'de',
      'fr',
      'es',
      'pt',
      'ru',
      'ar',
      'hi',
    ];
    for (final lang in supported) {
      if (normalized == lang || normalized.startsWith('${lang}_')) {
        return lang;
      }
    }
    return 'zh';
  }
}
