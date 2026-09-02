import 'package:flutter/foundation.dart' show kIsWeb;

import 'admin_region.dart';

/// 全局应用配置。
///
/// baseUrl 指向后端服务地址，adminApiPrefix 为管理后台 JSON 接口前缀。
/// 可在编译时通过 --dart-define=ADMIN_API_BASE_URL=... 强制覆盖所有区域。
class AppConfig {
  AppConfig._();

  /// 编译时强制注入的后端地址（--dart-define=ADMIN_API_BASE_URL=...）。
  /// 非空时优先于区域选择，用于 CI / 特定部署环境。
  static const String _envBaseUrl = String.fromEnvironment(
    'ADMIN_API_BASE_URL',
  );

  /// 管理后台 JSON 接口前缀。
  static const String adminApiPrefix = '/admin/api';

  /// 当前选定区域的完整 API 根地址。
  ///
  /// Web 端在未显式选择区域时走同源相对路径，部署到哪个域名就打到哪个域名；
  /// 一旦用户在登录页显式切换过区域，才按选择打对应区域的绝对域名（跨域）。
  /// 原生端（macOS/iOS/Windows 客户端）没有"同源"概念，始终使用绝对域名。
  static String get apiRoot {
    if (_envBaseUrl.isNotEmpty) return '$_envBaseUrl$adminApiPrefix';
    if (kIsWeb && !AdminRegionStore.hasExplicitChoice) return adminApiPrefix;
    return apiRootForRegion(AdminRegionStore.current);
  }

  /// 返回指定区域的完整 API 根地址（绝对域名，不含"未选择走相对路径"的分支）。
  static String apiRootForRegion(AdminRegion region) {
    if (_envBaseUrl.isNotEmpty) return '$_envBaseUrl$adminApiPrefix';
    return '${AdminRegionStore.baseUrlFor(region)}$adminApiPrefix';
  }

  /// 当前管理后台对应的公网根地址（不含路径），用于拼网关 Base URL 展示给运维。
  ///
  /// 与 [apiRoot] 同源选择规则一致：编译时强制注入优先；Web 未显式选区走当前访问域名；
  /// 否则按区域绝对域名。网关与 API 共用公网域名，路径前缀不同（`/anthropic`、`/openai`）。
  static String get publicOrigin {
    if (_envBaseUrl.isNotEmpty) {
      return _envBaseUrl.replaceAll(RegExp(r'/+$'), '');
    }
    if (kIsWeb && !AdminRegionStore.hasExplicitChoice) {
      return Uri.base.origin;
    }
    return AdminRegionStore.baseUrlFor(AdminRegionStore.current);
  }
}
