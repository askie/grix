import 'package:dio/dio.dart';
import 'package:get/get.dart' hide FormData, MultipartFile;

import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

class EggMarketEggModel {
  EggMarketEggModel({
    required this.id,
    required this.name,
    required this.description,
    required this.color,
    required this.emoji,
    required this.vibe,
    required this.canCreateAgent,
    required this.existingAgentClientTypes,
    required this.status,
    required this.version,
    required this.versionDesc,
    required this.installCount,
  });

  final String id;
  final String name;
  final String description;
  final String color;
  final String emoji;
  final String vibe;
  final bool canCreateAgent;
  final List<String> existingAgentClientTypes;
  final String status;
  final int version;
  final String versionDesc;
  final int installCount;

  bool get supportsOpenClawTarget =>
      canCreateAgent ||
      existingAgentClientTypes.contains(EggInstallTargetType.openclaw);

  bool get supportsClaudeTarget =>
      existingAgentClientTypes.contains(EggInstallTargetType.claude);

  bool get supportsExistingOpenClaw =>
      existingAgentClientTypes.contains(EggInstallTargetType.openclaw);

  bool get supportsExistingClaude =>
      existingAgentClientTypes.contains(EggInstallTargetType.claude);

  factory EggMarketEggModel.fromJson(Map<String, dynamic> json) {
    return EggMarketEggModel(
      id: json['id']?.toString().trim() ?? '',
      name: json['name']?.toString().trim() ?? '',
      description: json['description']?.toString().trim() ?? '',
      color: json['color']?.toString().trim() ?? '#D97706',
      emoji: json['emoji']?.toString().trim() ?? '🌍',
      vibe: json['vibe']?.toString().trim() ?? '',
      canCreateAgent: json['can_create_agent'] == true,
      existingAgentClientTypes: _readStringList(
        json['existing_agent_client_types'],
      ),
      status: json['status']?.toString().trim() ?? '',
      version: _readInt(json['version']),
      versionDesc: json['version_desc']?.toString().trim() ?? '',
      installCount: _readInt(json['install_count']),
    );
  }
}

class EggSearchResult {
  EggSearchResult({
    required this.localeUsed,
    required this.page,
    required this.pageSize,
    required this.hasMore,
    required this.list,
  });

  final String localeUsed;
  final int page;
  final int pageSize;
  final bool hasMore;
  final List<EggMarketEggModel> list;
}

class EggInstallCandidateModel {
  EggInstallCandidateModel({
    required this.agentID,
    required this.agentName,
    required this.agentClientType,
  });

  final String agentID;
  final String agentName;
  final String agentClientType;

  factory EggInstallCandidateModel.fromJson(Map<String, dynamic> json) {
    return EggInstallCandidateModel(
      agentID: json['agent_id']?.toString().trim() ?? '',
      agentName: json['agent_name']?.toString().trim() ?? '',
      agentClientType: json['agent_client_type']?.toString().trim() ?? '',
    );
  }
}

class EggInstallAcceptModel {
  EggInstallAcceptModel({
    required this.installID,
    required this.status,
    required this.sessionID,
    required this.executorAgentID,
    required this.candidates,
  });

  final String installID;
  final String status;
  final String sessionID;
  final String executorAgentID;
  final List<EggInstallCandidateModel> candidates;

  bool get requiresExecutorSelection =>
      status.trim().toLowerCase() == 'choose_executor';

  factory EggInstallAcceptModel.fromJson(Map<String, dynamic> json) {
    final rawCandidates = json['candidates'];
    return EggInstallAcceptModel(
      installID: json['install_id']?.toString().trim() ?? '',
      status: json['status']?.toString().trim() ?? '',
      sessionID: json['session_id']?.toString().trim() ?? '',
      executorAgentID: json['executor_agent_id']?.toString().trim() ?? '',
      candidates: rawCandidates is List
          ? rawCandidates
                .whereType<Map>()
                .map(
                  (item) => EggInstallCandidateModel.fromJson(
                    item.map((key, value) => MapEntry(key.toString(), value)),
                  ),
                )
                .toList()
          : const <EggInstallCandidateModel>[],
    );
  }
}

class EggInstallStatusModel {
  EggInstallStatusModel({
    required this.installID,
    required this.status,
    required this.step,
    required this.sessionID,
    required this.executorAgentID,
    required this.targetAgentID,
    required this.errorCode,
    required this.errorMsg,
  });

  final String installID;
  final String status;
  final String step;
  final String sessionID;
  final String executorAgentID;
  final String targetAgentID;
  final String errorCode;
  final String errorMsg;

  bool get isSuccess => status.toLowerCase() == 'success';
  bool get isFailed => status.toLowerCase() == 'failed';

  factory EggInstallStatusModel.fromJson(Map<String, dynamic> json) {
    return EggInstallStatusModel(
      installID: json['install_id']?.toString().trim() ?? '',
      status: json['status']?.toString().trim() ?? '',
      step: json['step']?.toString().trim() ?? '',
      sessionID: json['session_id']?.toString().trim() ?? '',
      executorAgentID: json['executor_agent_id']?.toString().trim() ?? '',
      targetAgentID: json['target_agent_id']?.toString().trim() ?? '',
      errorCode: json['error_code']?.toString().trim() ?? '',
      errorMsg: json['error_msg']?.toString().trim() ?? '',
    );
  }
}

class EggInstallMode {
  static const String createNew = 'create_new';
  static const String existingAgent = 'existing_agent';
}

class EggHatchType {
  static const String agent = 'agent';
  static const String skill = 'skill';
}

class EggInstallTargetType {
  static const String openclaw = 'openclaw';
  static const String hermes = 'hermes';
  static const String claude = 'claude';
  static const String codex = 'codex';
  static const String gemini = 'gemini';
  static const String qwen = 'qwen';
  static const String reasonix = 'reasonix';
  static const String deepseek = 'deepseek';
  static const String opencode = 'opencode';
  static const String kiro = 'kiro';
  static const String copilot = 'copilot';
  static const String kimi = 'kimi';

  static const Set<String> proprietary = {
    claude,
    codex,
    gemini,
    qwen,
    reasonix,
    deepseek,
    opencode,
    kiro,
    copilot,
    kimi,
  };

  static bool isProprietary(String type) => proprietary.contains(type);
}

class EggMarketService extends GetxService {
  EggMarketService({Dio? dio})
    : _dio =
          dio ??
          Dio(
            BaseOptions(
              baseUrl: AppRuntimeEndpoints.apiBaseUrl,
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 15),
            ),
          );

  final Dio _dio;

  Future<EggMarketService> init() async {
    Get.find<AuthService>().attachAuthInterceptor(_dio);
    return this;
  }

  Future<EggSearchResult> searchEggs({
    required String keyword,
    int page = 1,
    int pageSize = 20,
    String locale = '',
  }) async {
    final response = await _dio.get(
      '/eggs/search',
      queryParameters: {
        if (keyword.trim().isNotEmpty) 'keyword': keyword.trim(),
        'page': page,
        'page_size': pageSize,
        if (locale.trim().isNotEmpty) 'locale': locale.trim(),
      },
    );
    final data = _requireResponseData(response.data);
    final rawList = data['list'];
    final list = rawList is List
        ? rawList
              .whereType<Map>()
              .map(
                (item) => EggMarketEggModel.fromJson(
                  item.map((key, value) => MapEntry(key.toString(), value)),
                ),
              )
              .toList()
        : const <EggMarketEggModel>[];

    return EggSearchResult(
      localeUsed: data['locale_used']?.toString().trim() ?? '',
      page: _readInt(data['page']),
      pageSize: _readInt(data['page_size']),
      hasMore: data['has_more'] == true,
      list: list,
    );
  }

  Future<EggInstallAcceptModel> installEgg({
    required String eggID,
    required int version,
    required String idempotencyKey,
    required String installMode,
    String? targetAgentID,
    String? executorAgentID,
  }) async {
    final body = <String, dynamic>{
      'egg_id': eggID.trim(),
      'version': version,
      'idempotency_key': idempotencyKey.trim(),
      'install_mode': installMode.trim(),
      if (targetAgentID != null && targetAgentID.trim().isNotEmpty)
        'target_agent_id': targetAgentID.trim(),
      if (executorAgentID != null && executorAgentID.trim().isNotEmpty)
        'executor_agent_id': executorAgentID.trim(),
    };

    final response = await _dio.post('/eggs/install', data: body);
    final data = _requireResponseData(response.data);
    return EggInstallAcceptModel.fromJson(
      data.map((key, value) => MapEntry(key.toString(), value)),
    );
  }

  Future<EggInstallStatusModel> fetchInstallStatus(String installID) async {
    final response = await _dio.get('/eggs/install/${installID.trim()}');
    final data = _requireResponseData(response.data);
    return EggInstallStatusModel.fromJson(
      data.map((key, value) => MapEntry(key.toString(), value)),
    );
  }

  Map<String, dynamic> _requireResponseData(dynamic body) {
    if (body is! Map) {
      throw Exception('eggs_pond_invalid_response'.tr);
    }
    final code = _readInt(body['code']);
    if (code != 0) {
      final message = body['msg']?.toString().trim();
      if (message != null && message.isNotEmpty) {
        throw Exception(message);
      }
      throw Exception('eggs_pond_request_failed'.tr);
    }
    final data = body['data'];
    if (data is Map) {
      return data.map((key, value) => MapEntry(key.toString(), value));
    }
    return <String, dynamic>{};
  }
}

int _readInt(dynamic value) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  if (value is String) return int.tryParse(value.trim()) ?? 0;
  return 0;
}

List<String> _readStringList(dynamic value) {
  if (value is! List) {
    return const <String>[];
  }
  return value
      .map((item) => item?.toString().trim() ?? '')
      .where((item) => item.isNotEmpty)
      .toList();
}
