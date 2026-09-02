import 'package:dio/dio.dart';
import 'package:get/get.dart' hide Response;

import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

/// 自定义技能库中的一条技能。
/// 列表接口只返回摘要（无正文）；详情/新建/更新接口带 content。
class UserSkillModel {
  const UserSkillModel({
    required this.id,
    required this.name,
    this.ownerId = '',
    this.content = '',
    this.version = 1,
    this.digest = '',
    this.updatedAt = 0,
  });

  final String id;
  final String name;
  final String ownerId;
  final String content;
  final int version;
  final String digest;
  final int updatedAt;

  /// 系统内置技能（owner_id=0）：平台下发、对用户只读。
  bool get isSystem => ownerId == '0';

  factory UserSkillModel.fromJson(Map<String, dynamic> json) {
    return UserSkillModel(
      id: json['id']?.toString() ?? '',
      name: json['name']?.toString() ?? '',
      ownerId: json['owner_id']?.toString() ?? '',
      content: json['content']?.toString() ?? '',
      version: int.tryParse(json['version']?.toString() ?? '') ?? 1,
      digest: json['digest']?.toString() ?? '',
      updatedAt: json['updated_at'] is int
          ? json['updated_at'] as int
          : int.tryParse(json['updated_at']?.toString() ?? '') ?? 0,
    );
  }
}

/// 自定义技能库服务：直接读写平台技能库 REST（/v1/skills，用户 JWT 鉴权）。
/// 平台落库后由各机器 connector 同步到本地 grix/skills，多机一致。
class SkillLibraryService extends GetxService {
  SkillLibraryService({Dio? dio})
    : _dio =
          dio ??
          Dio(
            BaseOptions(
              baseUrl: AppRuntimeEndpoints.apiBaseUrl,
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 15),
              // 后端在 4xx 也返回结构化 {code,msg}，交由 _ok 按 body 判定，
              // 这样能读到具体错误消息而非被 Dio 当异常吞掉。
              validateStatus: (_) => true,
            ),
          ) {
    if (Get.isRegistered<AuthService>()) {
      Get.find<AuthService>().attachAuthInterceptor(_dio);
    }
  }

  final Dio _dio;
  final skills = <UserSkillModel>[].obs;
  final loading = false.obs;

  String lastError = '';

  bool _ok(Response<dynamic> resp) {
    final data = resp.data;
    return data is Map && (data['code'] == 0 || data['code'] == null);
  }

  /// 拉取技能库列表（摘要）。
  Future<bool> refresh() async {
    loading.value = true;
    try {
      final resp = await _dio.get('/skills');
      if (!_ok(resp)) {
        lastError = resp.data is Map ? '${resp.data['msg'] ?? ''}' : '';
        return false;
      }
      final items = (resp.data['data']?['items'] as List?) ?? const [];
      skills.assignAll(
        items.whereType<Map<String, dynamic>>().map(UserSkillModel.fromJson),
      );
      return true;
    } catch (e) {
      lastError = '$e';
      return false;
    } finally {
      loading.value = false;
    }
  }

  /// 读取单条技能全文。
  Future<UserSkillModel?> fetchContent(String id) async {
    try {
      final resp = await _dio.get('/skills/$id/content');
      if (!_ok(resp)) return null;
      final data = resp.data['data'];
      if (data is Map<String, dynamic>) return UserSkillModel.fromJson(data);
      return null;
    } catch (e) {
      lastError = '$e';
      return null;
    }
  }

  /// 新建技能。
  Future<bool> create(String name, String content) => _write(
    () => _dio.post('/skills', data: {'name': name, 'content': content}),
  );

  /// 更新技能。
  Future<bool> update(String id, String name, String content) => _write(
    () => _dio.put('/skills/$id', data: {'name': name, 'content': content}),
  );

  /// 删除技能。
  Future<bool> remove(String id) => _write(() => _dio.delete('/skills/$id'));

  Future<bool> _write(Future<Response<dynamic>> Function() call) async {
    try {
      final resp = await call();
      if (!_ok(resp)) {
        lastError = resp.data is Map ? '${resp.data['msg'] ?? ''}' : '';
        return false;
      }
      await refresh();
      return true;
    } catch (e) {
      lastError = '$e';
      return false;
    }
  }
}
