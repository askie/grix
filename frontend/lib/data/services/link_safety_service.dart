import 'dart:collection';

import 'package:dio/dio.dart';
import 'package:get/get.dart' hide Response;

import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/shared/utils/app_runtime_endpoints.dart';

/// LinkSafetyService 链接安全校验：点击外链时同步调用 `/v1/link/check`，
/// 本地会话内存 LRU 防重复请求。失败保守按"可疑"提示。
/// 详见 docs/architecture/35_link_safety_protection_design.md。
class LinkSafetyService extends GetxService {
  LinkSafetyService({Dio? dio})
      : _dio = dio ??
            Dio(BaseOptions(
              baseUrl: AppRuntimeEndpoints.apiBaseUrl,
              connectTimeout: const Duration(seconds: 5),
              receiveTimeout: const Duration(seconds: 5),
            ));

  final Dio _dio;

  // 会话内存 LRU，本次 App 启动期间已查过的 URL（不落地）。
  final LinkedHashMap<String, LinkVerdict> _lru =
      LinkedHashMap<String, LinkVerdict>();
  static const int _lruCap = 512;

  Future<LinkSafetyService> init() async {
    if (Get.isRegistered<AuthService>()) {
      Get.find<AuthService>().attachAuthInterceptor(_dio);
    }
    return this;
  }

  /// 校验单个 URL。
  /// 干净 -> 直接放行；可疑 -> 中间页确认；恶意 -> 中间页拦死。
  Future<LinkVerdict> check(String rawUrl) async {
    final url = rawUrl.trim();
    if (url.isEmpty) return LinkVerdict.clean();

    final cached = _lru.remove(url);
    if (cached != null) {
      _lru[url] = cached; // 移到末尾，标记最近使用
      return cached;
    }

    try {
      final resp = await _dio.post(
        '/link/check',
        data: {
          'urls': [url],
        },
      );
      final data = resp.data;
      if (data is Map &&
          data['data'] is Map &&
          data['data']['results'] is List &&
          (data['data']['results'] as List).isNotEmpty) {
        final v = LinkVerdict.fromJson(
          (data['data']['results'] as List).first as Map<String, dynamic>,
        );
        _putLru(url, v);
        return v;
      }
      return LinkVerdict.clean();
    } on DioException catch (e) {
      // 区分 401 与真正的网络失败：
      // - 401 = token 过期 / 未登录，链接拦截不该顺手把用户挡在外面，按 clean 放行；
      //   登录态相关的兜底由全局拦截器接管（跳登录页）。
      // - 其他（超时 / 5xx / 连接失败）= 真正的"可疑兜底"。
      if (e.response?.statusCode == 401) {
        return LinkVerdict.clean();
      }
      return LinkVerdict.suspicious(reason: 'network');
    } catch (_) {
      return LinkVerdict.suspicious(reason: 'network');
    }
  }

  void _putLru(String url, LinkVerdict v) {
    if (_lru.length >= _lruCap) {
      _lru.remove(_lru.keys.first);
    }
    _lru[url] = v;
  }
}

/// 链接判定等级。
enum LinkVerdictLevel { clean, suspicious, malicious }

class LinkVerdict {
  const LinkVerdict({
    required this.level,
    this.canonicalHost = '',
    this.reason = '',
    this.ruleSource = '',
  });

  factory LinkVerdict.clean() => const LinkVerdict(level: LinkVerdictLevel.clean);
  factory LinkVerdict.suspicious({String reason = ''}) => LinkVerdict(
        level: LinkVerdictLevel.suspicious,
        reason: reason,
      );
  factory LinkVerdict.malicious({String reason = ''}) => LinkVerdict(
        level: LinkVerdictLevel.malicious,
        reason: reason,
      );

  factory LinkVerdict.fromJson(Map<String, dynamic> json) {
    final raw = (json['verdict'] as String? ?? 'clean').toLowerCase();
    LinkVerdictLevel level;
    switch (raw) {
      case 'malicious':
        level = LinkVerdictLevel.malicious;
        break;
      case 'suspicious':
        level = LinkVerdictLevel.suspicious;
        break;
      default:
        level = LinkVerdictLevel.clean;
    }
    return LinkVerdict(
      level: level,
      canonicalHost: (json['canonical_host'] as String?) ?? '',
      reason: (json['reason'] as String?) ?? '',
      ruleSource: (json['rule_source'] as String?) ?? '',
    );
  }

  final LinkVerdictLevel level;
  final String canonicalHost;
  final String reason;
  final String ruleSource;
}
