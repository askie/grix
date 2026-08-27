import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart' hide FormData, MultipartFile;
import '../../modules/ai/models/agent_conn_security_model.dart';
import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';
import 'im_service.dart';

class AgentProfileModel {
  const AgentProfileModel({this.avatarUrl = '', this.introduction = ''});

  final String avatarUrl;
  final String introduction;

  factory AgentProfileModel.fromJson(Map<String, dynamic> json) {
    final rawProfile = json['profile'];
    final profile = rawProfile is Map
        ? rawProfile.map((key, value) => MapEntry(key.toString(), value))
        : const <String, dynamic>{};

    return AgentProfileModel(
      avatarUrl:
          profile['avatar_url']?.toString().trim() ??
          json['avatar_url']?.toString().trim() ??
          '',
      introduction:
          profile['introduction']?.toString().trim() ??
          json['introduction']?.toString().trim() ??
          '',
    );
  }
}

class AgentModel {
  final String id;
  final String agentName;
  final String introduction;
  final String modelProvider;
  final String systemPrompt;
  final AgentProfileModel profile;
  final String ownerID;
  final int providerType; // 1=remote API, 2=local LLM, 3=agent API
  final String agentClientType;
  final String localEndpoint;
  final String localModelName;
  final String contextFile;
  final String apiEndpoint;
  final String apiKey;
  final String apiKeyHint;
  final bool online;
  final String categoryId;
  final int sortOrder;
  final int status;
  final String sessionId;
  final bool isMain;
  final int createdAt;
  final int updatedAt;
  // Phase 2: 语音媒体能力
  final String mediaCapability; // 'text' | 'voice' | 'multimodal'
  final String voiceProvider; // 'doubao_realtime' 等
  // 语音大模型 BYOK（provider_type=4）；api key 仅回 hint，永不回明文
  final String voiceModel;
  final String voiceEndpoint;
  final String voiceId;
  final String voiceApiKeyHint;
  final int voiceMaxCallSeconds;
  final int voiceDailyCallLimit;
  final int voiceMaxConcurrentCalls;
  final bool voiceAllowVisitor;

  /// 语音开场白（按语言存文案，key 为 locale 代码），通话建立后主动播报；
  /// 缺省不打招呼。与 [LocaleService.supportedLocales] 对齐。
  final Map<String, String> voiceWelcomeI18n;
  // 宿主机名称，由 connector auth 时 os.hostname() 上报
  final String hostname;
  // Tailscale IP 和文件服务端口，有值时支持手机直连上传
  final String tailnetIp;
  final int fileServerPort;
  // 文件服务的 HTTPS 端口，有值时上传/下载优先走 https（证书由宿主机自签 CA 现签）
  final int tailnetHttpsPort;

  String get avatarUrl => profile.avatarUrl;

  /// 是否支持语音托管
  bool get supportsVoice =>
      mediaCapability == 'voice' || mediaCapability == 'multimodal';

  AgentModel({
    required this.id,
    required this.agentName,
    this.introduction = '',
    AgentProfileModel? profile,
    String avatarUrl = '',
    this.modelProvider = '',
    this.systemPrompt = '',
    this.ownerID = '',
    this.providerType = 1,
    this.agentClientType = '',
    this.localEndpoint = '',
    this.localModelName = '',
    this.contextFile = '',
    this.apiEndpoint = '',
    this.apiKey = '',
    this.apiKeyHint = '',
    this.online = false,
    this.categoryId = '0',
    this.sortOrder = 0,
    this.status = 1,
    this.sessionId = '',
    this.isMain = false,
    this.createdAt = 0,
    this.updatedAt = 0,
    this.mediaCapability = 'text',
    this.voiceProvider = '',
    this.voiceModel = '',
    this.voiceEndpoint = '',
    this.voiceId = '',
    this.voiceApiKeyHint = '',
    this.voiceMaxCallSeconds = 0,
    this.voiceDailyCallLimit = 0,
    this.voiceMaxConcurrentCalls = 2,
    this.voiceAllowVisitor = false,
    this.voiceWelcomeI18n = const {},
    this.hostname = '',
    this.tailnetIp = '',
    this.fileServerPort = 0,
    this.tailnetHttpsPort = 0,
  }) : profile =
           profile ??
           AgentProfileModel(avatarUrl: avatarUrl, introduction: introduction);

  factory AgentModel.fromJson(Map<String, dynamic> json) {
    return AgentModel(
      id: _readId(json['id']),
      agentName: json['agent_name'] ?? '',
      introduction: json['introduction']?.toString().trim() ?? '',
      profile: AgentProfileModel.fromJson(json),
      modelProvider: json['model_provider'] ?? '',
      systemPrompt: json['system_prompt'] ?? '',
      ownerID: _readId(json['owner_id']),
      providerType: json['provider_type'] ?? 1,
      agentClientType: json['agent_client_type']?.toString().trim() ?? '',
      localEndpoint: json['local_endpoint'] ?? '',
      localModelName: json['local_model_name'] ?? '',
      contextFile: json['context_file'] ?? '',
      apiEndpoint: json['api_endpoint'] ?? '',
      apiKey: json['api_key'] ?? '',
      apiKeyHint: json['api_key_hint'] ?? '',
      online: json['online'] == true,
      categoryId: _readId(json['category_id']),
      sortOrder: json['sort_order'] ?? 0,
      status: json['status'] ?? 1,
      sessionId: json['session_id'] ?? '',
      isMain: json['is_main'] == true,
      createdAt: json['created_at'] ?? 0,
      updatedAt: json['updated_at'] ?? 0,
      mediaCapability: json['media_capability']?.toString() ?? 'text',
      voiceProvider: json['voice_provider']?.toString() ?? '',
      voiceModel: json['voice_model']?.toString() ?? '',
      voiceEndpoint: json['voice_endpoint']?.toString() ?? '',
      voiceId: json['voice_id']?.toString() ?? '',
      voiceApiKeyHint: json['voice_api_key_hint']?.toString() ?? '',
      voiceMaxCallSeconds:
          (json['voice_max_call_seconds'] as num?)?.toInt() ?? 0,
      voiceDailyCallLimit:
          (json['voice_daily_call_limit'] as num?)?.toInt() ?? 0,
      voiceMaxConcurrentCalls:
          (json['voice_max_concurrent_calls'] as num?)?.toInt() ?? 2,
      voiceAllowVisitor: json['voice_allow_visitor'] == true,
      voiceWelcomeI18n: json['voice_welcome_i18n'] is Map
          ? (json['voice_welcome_i18n'] as Map).map(
              (k, v) => MapEntry(k.toString(), v.toString()),
            )
          : const {},
      hostname: _readHostMeta(json['config'], 'hostname'),
      tailnetIp: _readHostMeta(json['config'], 'tailnet_ip'),
      fileServerPort: _readHostMetaInt(json['config'], 'file_server_port'),
      tailnetHttpsPort: _readHostMetaInt(
        json['config'],
        'file_server_https_port',
      ),
    );
  }

  static String _readHostMeta(dynamic config, String key) {
    if (config is Map) {
      final hostMeta = config['host_meta'];
      if (hostMeta is Map) {
        return hostMeta[key]?.toString().trim() ?? '';
      }
    }
    return '';
  }

  static int _readHostMetaInt(dynamic config, String key) {
    if (config is Map) {
      final hostMeta = config['host_meta'];
      if (hostMeta is Map) {
        return (hostMeta[key] as num?)?.toInt() ?? 0;
      }
    }
    return 0;
  }

  /// upload base URL for tailnet direct upload, empty when not available.
  /// 优先 https（宿主机自签 CA 现签，App 已通过 HttpOverrides 限定信任）；
  /// 仅在没有 https 端口时回退到旧的 http 直传，保证对旧 connector 兼容。
  String get tailnetUploadBaseUrl {
    if (tailnetIp.isEmpty) return '';
    if (tailnetHttpsPort > 0) return 'https://$tailnetIp:$tailnetHttpsPort';
    if (fileServerPort > 0) return 'http://$tailnetIp:$fileServerPort';
    return '';
  }
}

class AgentScopeConfig {
  const AgentScopeConfig({
    this.scopes = const [],
    this.availableScopes = const [],
    this.availableScopeItems = const [],
  });

  final List<String> scopes;
  final List<String> availableScopes;
  final List<AgentScopeItem> availableScopeItems;
}

class AgentScopeItem {
  const AgentScopeItem({
    required this.scope,
    required this.label,
    required this.description,
  });

  final String scope;
  final String label;
  final String description;
}

class AgentApiInstallGuide {
  const AgentApiInstallGuide({
    this.type = '',
    this.label = '',
    this.intro = '',
    this.contentMode = 'text',
    this.contentTemplate = '',
    this.linkLabel = '',
    this.linkUrl = '',
    this.copyTemplate = '',
  });

  final String type;
  final String label;
  final String intro;
  final String contentMode;
  final String contentTemplate;
  final String linkLabel;
  final String linkUrl;
  final String copyTemplate;

  bool get isLink => contentMode.trim().toLowerCase() == 'link';

  factory AgentApiInstallGuide.fromJson(Map<String, dynamic> json) {
    return AgentApiInstallGuide(
      type: json['type']?.toString().trim() ?? '',
      label: json['label']?.toString().trim() ?? '',
      intro: json['intro']?.toString().trim() ?? '',
      contentMode: json['content_mode']?.toString().trim() ?? 'text',
      contentTemplate: json['content_template']?.toString() ?? '',
      linkLabel: json['link_label']?.toString().trim() ?? '',
      linkUrl: json['link_url']?.toString().trim() ?? '',
      copyTemplate: json['copy_template']?.toString() ?? '',
    );
  }
}

class AgentApiInstallGuideCatalog {
  const AgentApiInstallGuideCatalog({
    this.defaultType = '',
    this.list = const [],
  });

  final String defaultType;
  final List<AgentApiInstallGuide> list;

  factory AgentApiInstallGuideCatalog.fromJson(Map<String, dynamic> json) {
    final rawList = json['list'];
    return AgentApiInstallGuideCatalog(
      defaultType: json['default_type']?.toString().trim() ?? '',
      list:
          (rawList as List?)
              ?.map(
                (item) => AgentApiInstallGuide.fromJson(
                  Map<String, dynamic>.from(item as Map),
                ),
              )
              .toList() ??
          const [],
    );
  }
}

/// 语音模型选项：C 端创建语音(type=4) agent 时下拉可选的一条。
/// 由塘主后台统一维护，用户只按 label 选择，provider/model/endpoint 随选项带回。
class VoicePresetOption {
  const VoicePresetOption({this.id = '', this.label = ''});

  final String id;
  final String label;

  factory VoicePresetOption.fromJson(Map<String, dynamic> json) {
    return VoicePresetOption(
      id: json['id']?.toString().trim() ?? '',
      label: json['label']?.toString().trim() ?? '',
    );
  }
}

class VoiceModelOption {
  const VoiceModelOption({
    this.id = '',
    this.label = '',
    this.provider = '',
    this.model = '',
    this.endpoint = '',
    this.voices = const [],
  });

  final String id;
  final String label;
  final String provider;
  final String model;
  final String endpoint;
  final List<VoicePresetOption> voices;

  factory VoiceModelOption.fromJson(Map<String, dynamic> json) {
    return VoiceModelOption(
      id: json['id']?.toString().trim() ?? '',
      label: json['label']?.toString().trim() ?? '',
      provider: json['provider']?.toString().trim() ?? '',
      model: json['model']?.toString().trim() ?? '',
      endpoint: json['endpoint']?.toString().trim() ?? '',
      voices: ((json['voices'] as List?) ?? const [])
          .map(
            (e) =>
                VoicePresetOption.fromJson((e as Map).cast<String, dynamic>()),
          )
          .toList(),
    );
  }
}

class AgentService extends GetxService {
  AgentService({Dio? dio})
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
  String _lastOperationError = '';
  int _lastOperationCode = 0;
  Future<void>? _loadAgentsTask;
  String? _loadAgentsTaskCategoryId;
  int _loadAgentsRequestSerial = 0;

  final agents = <AgentModel>[].obs;
  // 「分享给我的 agent」：别人共享给当前账户、可像自己的 agent 一样使用（但不能管理）。
  final sharedAgents = <AgentModel>[].obs;
  final hasLoaded = false.obs;

  String get lastOperationError => _lastOperationError;
  int get lastOperationCode => _lastOperationCode;

  /// 当前账户「可用」的全部 agent：自己持有的 + 别人共享给我的，按 id 去重。
  /// 用于托管 agent 选择器、消息气泡昵称解析等需要把共享 agent 视为可用的场景。
  List<AgentModel> get allAccessibleAgents {
    if (sharedAgents.isEmpty) {
      return agents.toList(growable: false);
    }
    final merged = <AgentModel>[...agents];
    final seen = <String>{for (final a in agents) a.id.trim()};
    for (final shared in sharedAgents) {
      final id = shared.id.trim();
      if (id.isEmpty || !seen.add(id)) {
        continue;
      }
      merged.add(shared);
    }
    return merged;
  }

  void resetForAccountSwitch() {
    agents.clear();
    sharedAgents.clear();
    hasLoaded.value = false;
    _loadAgentsTask = null;
    _loadAgentsTaskCategoryId = null;
    _loadAgentsRequestSerial++;
  }

  Future<AgentService> init() async {
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

  AgentModel? _upsertAgent(AgentModel agent) {
    final idx = agents.indexWhere((current) => current.id == agent.id);
    if (idx == -1) {
      agents.add(agent);
      return null;
    }
    final previous = agents[idx];
    agents[idx] = agent;
    return previous;
  }

  Future<void> _syncPrivateAgentSessions(
    Map<String, String> currentNames, {
    Map<String, String> previousNames = const <String, String>{},
  }) async {
    if (currentNames.isEmpty || !Get.isRegistered<ImService>()) {
      return;
    }
    await Get.find<ImService>().syncPrivateAgentSessions(
      currentNames,
      previousAgentNames: previousNames,
    );
  }

  Map<String, String> _buildPreviousAgentNameMap(List<AgentModel> nextAgents) {
    final previousById = <String, String>{};
    for (final agent in nextAgents) {
      final agentId = agent.id.trim();
      if (agentId.isEmpty) {
        continue;
      }
      final idx = agents.indexWhere((current) => current.id == agentId);
      if (idx == -1) {
        continue;
      }
      final previousName = agents[idx].agentName.trim();
      if (previousName.isEmpty) {
        continue;
      }
      previousById[agentId] = previousName;
    }
    return previousById;
  }

  String _responseMessage(dynamic body) {
    if (body is Map) {
      final code = _responseCode(body);
      if (code == 10005) {
        return 'auth_error_rate_limit'.tr;
      }
      if (body['msg'] != null) {
        return body['msg'].toString();
      }
    }
    return _unknownError;
  }

  int _responseCode(dynamic body) {
    if (body is! Map) {
      return 50001;
    }
    final value = body['code'];
    if (value is int) {
      return value;
    }
    if (value is num) {
      return value.toInt();
    }
    if (value is String) {
      return int.tryParse(value.trim()) ?? 50001;
    }
    return 50001;
  }

  bool _isUnauthorizedBody(dynamic body) {
    if (body is! Map) return false;
    return body['code'] == 10001;
  }

  bool _isUnauthorizedError(Object error) {
    if (error is! DioException) return false;
    if (error.response?.statusCode == 401) return true;
    return _isUnauthorizedBody(error.response?.data);
  }

  void _reportErrorIfNeeded(
    String operation,
    String message, {
    dynamic body,
    Object? error,
  }) {
    final shouldSuppress = error != null
        ? _isUnauthorizedError(error)
        : _isUnauthorizedBody(body);
    final normalizedMessage = message.trim().isEmpty ? _unknownError : message;
    if (shouldSuppress) {
      debugPrint(
        '[AgentService][$operation] Suppressed unauthorized error: $normalizedMessage',
      );
      return;
    }
    debugPrint('[AgentService][$operation] $normalizedMessage');
  }

  /// Extract readable error message from DioException or other errors.
  String _extractError(Object e) {
    if (e is DioException) {
      final data = e.response?.data;
      if (data is Map) {
        return _responseMessage(data);
      }
      if (e.response?.statusCode == 429) {
        return 'auth_error_rate_limit'.tr;
      }
      if (e.response != null) {
        return 'HTTP ${e.response!.statusCode}: ${e.response!.statusMessage}';
      }
      if (e.type == DioExceptionType.connectionTimeout ||
          e.type == DioExceptionType.receiveTimeout) {
        return 'agent_error_timeout'.tr;
      }
      if (e.type == DioExceptionType.connectionError) {
        return 'agent_error_connection_failed'.trParams({
          'url': _dio.options.baseUrl,
        });
      }
      return e.message ?? e.toString();
    }
    return e.toString();
  }

  String _normalizeCategoryId(String? categoryId) {
    final normalized = categoryId?.trim() ?? '';
    if (normalized.isEmpty || normalized == '0') {
      return '';
    }
    return normalized;
  }

  Future<void> loadAgents({String? categoryId}) {
    final normalizedCategoryId = _normalizeCategoryId(categoryId);
    final inflightTask = _loadAgentsTask;
    if (inflightTask != null &&
        _loadAgentsTaskCategoryId == normalizedCategoryId) {
      return inflightTask;
    }

    final requestSerial = ++_loadAgentsRequestSerial;
    final future = _loadAgentsInternal(
      categoryId: normalizedCategoryId,
      requestSerial: requestSerial,
    );
    _loadAgentsTask = future;
    _loadAgentsTaskCategoryId = normalizedCategoryId;
    future.whenComplete(() {
      if (identical(_loadAgentsTask, future)) {
        _loadAgentsTask = null;
        _loadAgentsTaskCategoryId = null;
      }
    });
    return future;
  }

  Future<void> _loadAgentsInternal({
    required String categoryId,
    required int requestSerial,
  }) async {
    try {
      final query = categoryId.isNotEmpty ? '?category_id=$categoryId' : '';
      final resp = await _dio.get('/agents/list$query');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final rawData = resp.data['data'];
        final rawList = rawData is List
            ? rawData
            : (rawData is Map ? rawData['list'] : null);
        final list =
            (rawList as List?)
                ?.map((e) => AgentModel.fromJson(e as Map<String, dynamic>))
                .toList() ??
            [];
        if (requestSerial != _loadAgentsRequestSerial) {
          return;
        }
        final previousNames = _buildPreviousAgentNameMap(list);
        agents.value = list;
        hasLoaded.value = true;
        // 顺带刷新「分享给我的」列表（内部吞错，不影响主列表）。
        await loadSharedWithMe();
        await _syncPrivateAgentSessions({
          for (final agent in list)
            if (agent.id.trim().isNotEmpty && agent.agentName.trim().isNotEmpty)
              agent.id.trim(): agent.agentName.trim(),
        }, previousNames: previousNames);
      } else {
        final msg = _responseMessage(resp.data);
        _reportErrorIfNeeded('loadAgents', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('loadAgents', msg, error: e);
    }
  }

  /// 当前账户是否为该 agent 的主人。非主人=别人共享给我的，只能使用不能管理。
  bool isOwnedByMe(AgentModel agent) {
    final myId = Get.find<AuthService>().userId?.trim() ?? '';
    return myId.isNotEmpty && agent.ownerID.trim() == myId;
  }

  /// 加载「分享给我的 agent」列表（内部吞错，不影响主流程）。
  Future<void> loadSharedWithMe() async {
    try {
      final resp = await _dio.get('/agents/shared-with-me');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final rawData = resp.data['data'];
        final rawList = rawData is List
            ? rawData
            : (rawData is Map ? rawData['list'] : null);
        sharedAgents.value =
            (rawList as List?)
                ?.map((e) => AgentModel.fromJson(e as Map<String, dynamic>))
                .toList() ??
            [];
      }
    } catch (_) {
      // 静默
    }
  }

  /// 把 agent 共享给某账户。
  Future<bool> shareAgentTo(String agentId, String sharedToUserId) async {
    try {
      final resp = await _dio.post(
        '/agents/$agentId/shares',
        data: {'shared_to': sharedToUserId},
      );
      return resp.statusCode == 200 && resp.data['code'] == 0;
    } catch (e) {
      _reportErrorIfNeeded('shareAgentTo', _extractError(e), error: e);
      return false;
    }
  }

  /// 撤销对某账户的共享。
  Future<bool> revokeAgentShare(String agentId, String sharedToUserId) async {
    try {
      final resp = await _dio.delete('/agents/$agentId/shares/$sharedToUserId');
      return resp.statusCode == 200 && resp.data['code'] == 0;
    } catch (e) {
      _reportErrorIfNeeded('revokeAgentShare', _extractError(e), error: e);
      return false;
    }
  }

  /// 列出某 agent 当前共享给的账户 user_id 列表。
  Future<List<String>> listAgentShares(String agentId) async {
    try {
      final resp = await _dio.get('/agents/$agentId/shares');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final rawData = resp.data['data'];
        final rawList = rawData is Map ? rawData['list'] : null;
        return (rawList as List?)
                ?.map((e) => (e as Map)['shared_to']?.toString() ?? '')
                .where((s) => s.isNotEmpty)
                .toList() ??
            [];
      }
    } catch (e) {
      _reportErrorIfNeeded('listAgentShares', _extractError(e), error: e);
    }
    return [];
  }

  Future<AgentModel?> createAgent(Map<String, dynamic> data) async {
    _clearOperationError();
    try {
      final resp = await _dio.post('/agents/create', data: data);
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final agent = AgentModel.fromJson(resp.data['data']);
        _upsertAgent(agent);
        return agent;
      } else {
        final code = _responseCode(resp.data);
        final msg = _responseMessage(resp.data);
        _setOperationError(msg, code: code);
        _reportErrorIfNeeded('createAgent', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _setOperationError(msg);
      _reportErrorIfNeeded('createAgent', msg, error: e);
    }
    return null;
  }

  Future<AgentModel?> getAgent(String agentId) async {
    try {
      final resp = await _dio.get('/agents/$agentId');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return AgentModel.fromJson(resp.data['data']);
      } else {
        final msg = _responseMessage(resp.data);
        _reportErrorIfNeeded('getAgent', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('getAgent', msg, error: e);
    }
    return null;
  }

  /// 语音托管实时状态：通话中/排队人数与配置上限。
  Future<AgentVoiceStats?> getAgentVoiceStats(String agentId) async {
    try {
      final resp = await _dio.get('/agents/$agentId/voice-stats');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return AgentVoiceStats.fromJson(
          Map<String, dynamic>.from(resp.data['data'] as Map),
        );
      }
    } catch (e) {
      debugPrint('getAgentVoiceStats error: $e');
    }
    return null;
  }

  Future<AgentApiInstallGuideCatalog?> getAgentApiInstallGuides() async {
    try {
      final resp = await _dio.get('/agents/agent-api/install-guides');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return AgentApiInstallGuideCatalog.fromJson(
          Map<String, dynamic>.from(resp.data['data'] as Map),
        );
      }
      final msg = _responseMessage(resp.data);
      _reportErrorIfNeeded('getAgentApiInstallGuides', msg, body: resp.data);
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('getAgentApiInstallGuides', msg, error: e);
    }
    return null;
  }

  Future<List<VoiceModelOption>?> getVoiceModels() async {
    try {
      final resp = await _dio.get('/agents/voice-models');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final rawList = resp.data['data']?['list'];
        return (rawList as List?)
                ?.map(
                  (item) => VoiceModelOption.fromJson(
                    Map<String, dynamic>.from(item as Map),
                  ),
                )
                .toList() ??
            const [];
      }
      final msg = _responseMessage(resp.data);
      _reportErrorIfNeeded('getVoiceModels', msg, body: resp.data);
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('getVoiceModels', msg, error: e);
    }
    return null;
  }

  Future<AgentModel?> updateAgent(
    String agentId,
    Map<String, dynamic> data,
  ) async {
    _clearOperationError();
    try {
      final resp = await _dio.put('/agents/$agentId', data: data);
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final updated = AgentModel.fromJson(resp.data['data']);
        final previous = _upsertAgent(updated);
        await _syncPrivateAgentSessions(
          {updated.id.trim(): updated.agentName.trim()},
          previousNames: {
            if (previous != null && previous.agentName.trim().isNotEmpty)
              updated.id.trim(): previous.agentName.trim(),
          },
        );
        return updated;
      } else {
        final code = _responseCode(resp.data);
        final msg = _responseMessage(resp.data);
        _setOperationError(msg, code: code);
        _reportErrorIfNeeded('updateAgent', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _setOperationError(msg);
      _reportErrorIfNeeded('updateAgent', msg, error: e);
    }
    return null;
  }

  Future<bool> updateContext(String agentId, String contextFile) async {
    try {
      final resp = await _dio.put(
        '/agents/$agentId/context',
        data: {'context_file': contextFile},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return true;
      } else {
        final msg = _responseMessage(resp.data);
        _reportErrorIfNeeded('updateContext', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('updateContext', msg, error: e);
    }
    return false;
  }

  Future<bool> deleteAgent(String agentId) async {
    try {
      final resp = await _dio.delete('/agents/$agentId');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        agents.removeWhere((a) => a.id == agentId);
        return true;
      } else {
        final msg = _responseMessage(resp.data);
        _reportErrorIfNeeded('deleteAgent', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('deleteAgent', msg, error: e);
    }
    return false;
  }

  Future<bool> batchSortAgents(List<Map<String, dynamic>> items) async {
    try {
      final resp = await _dio.put('/agents/batch-sort', data: {'items': items});
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return true;
      } else {
        final msg = _responseMessage(resp.data);
        _reportErrorIfNeeded('batchSortAgents', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('batchSortAgents', msg, error: e);
    }
    return false;
  }

  Future<AgentModel?> rotateAgentApiKey(String agentId) async {
    try {
      final resp = await _dio.post('/agents/$agentId/api/key/rotate');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final updated = AgentModel.fromJson(resp.data['data']);
        _upsertAgent(updated);
        return updated;
      } else {
        final msg = _responseMessage(resp.data);
        _reportErrorIfNeeded('rotateAgentApiKey', msg, body: resp.data);
      }
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('rotateAgentApiKey', msg, error: e);
    }
    return null;
  }

  Future<ServiceResult<AgentScopeConfig>> getAgentScopes(String agentId) async {
    final normalizedAgentId = agentId.trim();
    if (normalizedAgentId.isEmpty) {
      return ServiceResult<AgentScopeConfig>.failure(
        message: 'ai_agent_scope_target_invalid'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final resp = await _dio.get('/agents/$normalizedAgentId/scopes');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return ServiceResult<AgentScopeConfig>.success(
          data: _parseScopeConfig(resp.data['data']),
          httpStatus: resp.statusCode ?? 200,
        );
      }
      return ServiceResult<AgentScopeConfig>.failure(
        message: _responseMessage(resp.data),
        code: _responseCode(resp.data),
        httpStatus: resp.statusCode ?? 0,
      );
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('getAgentScopes', msg, error: e);
      return ServiceResult<AgentScopeConfig>.failure(message: msg);
    }
  }

  Future<ServiceResult<AgentScopeConfig>> replaceAgentScopes(
    String agentId,
    List<String> scopes,
  ) async {
    final normalizedAgentId = agentId.trim();
    if (normalizedAgentId.isEmpty) {
      return ServiceResult<AgentScopeConfig>.failure(
        message: 'ai_agent_scope_target_invalid'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final resp = await _dio.put(
        '/agents/$normalizedAgentId/scopes',
        data: {'scopes': scopes},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return ServiceResult<AgentScopeConfig>.success(
          data: _parseScopeConfig(resp.data['data']),
          httpStatus: resp.statusCode ?? 200,
        );
      }
      return ServiceResult<AgentScopeConfig>.failure(
        message: _responseMessage(resp.data),
        code: _responseCode(resp.data),
        httpStatus: resp.statusCode ?? 0,
      );
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('replaceAgentScopes', msg, error: e);
      return ServiceResult<AgentScopeConfig>.failure(message: msg);
    }
  }

  /// 拉取某 agent 的连接（登录）历史，按连接时间倒序。
  Future<ServiceResult<List<AgentConnectionLogEntry>>> getAgentConnectionLogs(
    String agentId, {
    int page = 1,
    int pageSize = 30,
  }) async {
    final normalizedAgentId = agentId.trim();
    if (normalizedAgentId.isEmpty) {
      return ServiceResult<List<AgentConnectionLogEntry>>.failure(
        message: 'ai_agent_conn_target_invalid'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final resp = await _dio.get(
        '/agents/$normalizedAgentId/connection-logs',
        queryParameters: {'page': page, 'page_size': pageSize},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final items = (resp.data['data']?['items'] as List?) ?? const [];
        final logs = items
            .whereType<Map>()
            .map(
              (e) =>
                  AgentConnectionLogEntry.fromJson(e.cast<String, dynamic>()),
            )
            .toList(growable: false);
        return ServiceResult<List<AgentConnectionLogEntry>>.success(
          data: logs,
          httpStatus: resp.statusCode ?? 200,
        );
      }
      return ServiceResult<List<AgentConnectionLogEntry>>.failure(
        message: _responseMessage(resp.data),
        code: _responseCode(resp.data),
        httpStatus: resp.statusCode ?? 0,
      );
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('getAgentConnectionLogs', msg, error: e);
      return ServiceResult<List<AgentConnectionLogEntry>>.failure(message: msg);
    }
  }

  /// 拉取某 agent 的 IP 规则列表（黑/白名单，含各自类型）。
  Future<ServiceResult<List<AgentIPRuleEntry>>> getAgentIPRules(
    String agentId,
  ) async {
    final normalizedAgentId = agentId.trim();
    if (normalizedAgentId.isEmpty) {
      return ServiceResult<List<AgentIPRuleEntry>>.failure(
        message: 'ai_agent_conn_target_invalid'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final resp = await _dio.get('/agents/$normalizedAgentId/ip-rules');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final items = (resp.data['data']?['items'] as List?) ?? const [];
        final rules = items
            .whereType<Map>()
            .map((e) => AgentIPRuleEntry.fromJson(e.cast<String, dynamic>()))
            .toList(growable: false);
        return ServiceResult<List<AgentIPRuleEntry>>.success(
          data: rules,
          httpStatus: resp.statusCode ?? 200,
        );
      }
      return ServiceResult<List<AgentIPRuleEntry>>.failure(
        message: _responseMessage(resp.data),
        code: _responseCode(resp.data),
        httpStatus: resp.statusCode ?? 0,
      );
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('getAgentIPRules', msg, error: e);
      return ServiceResult<List<AgentIPRuleEntry>>.failure(message: msg);
    }
  }

  /// 为某 agent 新增一条 IP 规则（当前前端只用 ruleType='ban'）。
  Future<ServiceResult<AgentIPRuleEntry>> createAgentIPRule(
    String agentId, {
    required String ruleType,
    required String ipCidr,
    String remark = '',
  }) async {
    final normalizedAgentId = agentId.trim();
    final normalizedIP = ipCidr.trim();
    if (normalizedAgentId.isEmpty || normalizedIP.isEmpty) {
      return ServiceResult<AgentIPRuleEntry>.failure(
        message: 'ai_agent_conn_target_invalid'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final resp = await _dio.post(
        '/agents/$normalizedAgentId/ip-rules',
        data: {
          'rule_type': ruleType,
          'ip_cidr': normalizedIP,
          'remark': remark.trim(),
        },
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final data = resp.data['data'];
        return ServiceResult<AgentIPRuleEntry>.success(
          data: data is Map
              ? AgentIPRuleEntry.fromJson(data.cast<String, dynamic>())
              : null,
          httpStatus: resp.statusCode ?? 200,
        );
      }
      return ServiceResult<AgentIPRuleEntry>.failure(
        message: _responseMessage(resp.data),
        code: _responseCode(resp.data),
        httpStatus: resp.statusCode ?? 0,
      );
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('createAgentIPRule', msg, error: e);
      return ServiceResult<AgentIPRuleEntry>.failure(message: msg);
    }
  }

  /// 删除某 agent 的一条 IP 规则。
  Future<ServiceResult<void>> deleteAgentIPRule(
    String agentId,
    String ruleId,
  ) async {
    final normalizedAgentId = agentId.trim();
    final normalizedRuleId = ruleId.trim();
    if (normalizedAgentId.isEmpty || normalizedRuleId.isEmpty) {
      return ServiceResult<void>.failure(
        message: 'ai_agent_conn_target_invalid'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final resp = await _dio.delete(
        '/agents/$normalizedAgentId/ip-rules/$normalizedRuleId',
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        return ServiceResult<void>.success(httpStatus: resp.statusCode ?? 200);
      }
      return ServiceResult<void>.failure(
        message: _responseMessage(resp.data),
        code: _responseCode(resp.data),
        httpStatus: resp.statusCode ?? 0,
      );
    } catch (e) {
      final msg = _extractError(e);
      _reportErrorIfNeeded('deleteAgentIPRule', msg, error: e);
      return ServiceResult<void>.failure(message: msg);
    }
  }

  Future<ServiceResult<AgentModel>> uploadAgentAvatar({
    required String agentId,
    required Uint8List bytes,
    required String filename,
  }) async {
    if (bytes.isEmpty) {
      return ServiceResult<AgentModel>.failure(
        message: 'ai_agent_avatar_upload_failed'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    final normalizedAgentId = agentId.trim();
    if (normalizedAgentId.isEmpty) {
      return ServiceResult<AgentModel>.failure(
        message: 'ai_agent_avatar_upload_failed'.tr,
        code: 10003,
        httpStatus: 400,
      );
    }

    try {
      final normalizedFilename = filename.trim().isEmpty
          ? 'agent_avatar.jpg'
          : filename.trim();
      final form = FormData.fromMap({
        'file': MultipartFile.fromBytes(bytes, filename: normalizedFilename),
      });
      final resp = await _dio.post(
        '/agents/$normalizedAgentId/avatar',
        data: form,
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final updated = AgentModel.fromJson(resp.data['data']);
        _upsertAgent(updated);
        return ServiceResult<AgentModel>.success(
          data: updated,
          httpStatus: resp.statusCode ?? 200,
        );
      }

      return ServiceResult<AgentModel>.failure(
        message: _responseMessage(resp.data),
        code: _responseCode(resp.data),
        httpStatus: resp.statusCode ?? 0,
      );
    } catch (e) {
      final message = _extractError(e);
      _reportErrorIfNeeded('uploadAgentAvatar', message, error: e);
      return ServiceResult<AgentModel>.failure(
        message: message.trim().isEmpty
            ? 'ai_agent_avatar_upload_failed'.tr
            : message,
      );
    }
  }
}

AgentScopeConfig _parseScopeConfig(dynamic rawData) {
  if (rawData is! Map) {
    return const AgentScopeConfig();
  }
  final items = _parseScopeItems(rawData['available_scope_items']);
  final availableScopes = _parseScopeList(rawData['available_scopes']);
  return AgentScopeConfig(
    scopes: _parseScopeList(rawData['scopes']),
    availableScopes: availableScopes.isEmpty
        ? items.map((item) => item.scope).toList(growable: false)
        : availableScopes,
    availableScopeItems: items,
  );
}

List<AgentScopeItem> _parseScopeItems(dynamic rawItems) {
  if (rawItems is! List) {
    return const [];
  }
  final result = <AgentScopeItem>[];
  final seen = <String>{};
  for (final item in rawItems) {
    if (item is! Map) {
      continue;
    }
    final scope = item['scope']?.toString().trim() ?? '';
    if (scope.isEmpty || seen.contains(scope)) {
      continue;
    }
    seen.add(scope);
    result.add(
      AgentScopeItem(
        scope: scope,
        label: item['label']?.toString().trim() ?? '',
        description: item['description']?.toString().trim() ?? '',
      ),
    );
  }
  return result;
}

List<String> _parseScopeList(dynamic rawScopes) {
  if (rawScopes is! List) {
    return const [];
  }
  return rawScopes
      .map((item) => item?.toString().trim() ?? '')
      .where((item) => item.isNotEmpty)
      .toList();
}

String _readId(dynamic value) {
  return value?.toString().trim() ?? '';
}

/// 语音托管实时状态。
class AgentVoiceStats {
  final int active;
  final int queued;
  final int maxConcurrent;

  const AgentVoiceStats({
    required this.active,
    required this.queued,
    required this.maxConcurrent,
  });

  factory AgentVoiceStats.fromJson(Map<String, dynamic> json) {
    return AgentVoiceStats(
      active: (json['active'] as num?)?.toInt() ?? 0,
      queued: (json['queued'] as num?)?.toInt() ?? 0,
      maxConcurrent: (json['max_concurrent'] as num?)?.toInt() ?? 0,
    );
  }
}
