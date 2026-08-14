import 'package:flutter/foundation.dart';

import 'app_runtime_endpoints.dart';
import 'app_storage_service.dart';

/// 编译时注入的两个区域端点（通过 --dart-define 覆盖）
const String kCnApiUrl = String.fromEnvironment(
  'CN_API_URL',
  defaultValue: 'https://grix.dhf.pub',
);
const String kCnWsUrl = String.fromEnvironment(
  'CN_WS_URL',
  defaultValue: 'wss://grix.dhf.pub/ws',
);
const String kGlobalApiUrl = String.fromEnvironment(
  'GLOBAL_API_URL',
  defaultValue: 'https://gb.grix.im',
);
const String kGlobalWsUrl = String.fromEnvironment(
  'GLOBAL_WS_URL',
  defaultValue: 'wss://ws.grix.im/ws',
);

enum AppRegion { cn, global }

/// 根据设备系统语言推断默认区域。
/// 简体中文（zh_CN / zh_Hans）→ cn，其他 → global。
AppRegion detectRegionFromLocale() {
  final locale = PlatformDispatcher.instance.locale;
  if (locale.languageCode == 'zh') {
    final script = locale.scriptCode ?? '';
    final country = locale.countryCode ?? '';
    // 仅中国大陆（zh_CN / zh_Hans）归 CN 区域；台湾、香港、新加坡走 Global
    if (script == 'Hans' || country == 'CN') {
      return AppRegion.cn;
    }
  }
  return AppRegion.global;
}

String apiUrlForRegion(AppRegion region) {
  final base = region == AppRegion.cn ? kCnApiUrl : kGlobalApiUrl;
  // 确保与 Dio baseUrl 格式一致（含 /v1），避免覆盖时丢失路径
  if (base.endsWith('/v1') || base.endsWith('/v1/')) return base;
  return '$base/v1';
}

String wsUrlForRegion(AppRegion region) =>
    region == AppRegion.cn ? kCnWsUrl : kGlobalWsUrl;

/// 目标区域网页的「域名根」URL（scheme://host[:port]/）。
///
/// 注入的区域地址是带 /v1 的接口地址（如 https://grix.dhf.pub/v1）。Web 端整页
/// 跳转分区时必须用域名根，否则带着 /v1 接口路径跳转会落到 404 白屏页。
String regionWebRootUrl(AppRegion region) =>
    webRootOfUrl(region == AppRegion.cn ? kCnApiUrl : kGlobalApiUrl);

/// 取 URL 的「域名根」（scheme://host[:port]/），丢弃 /v1 等接口路径。
/// 解析失败时原样返回，避免抛异常打断跳转。
String webRootOfUrl(String url) {
  final uri = Uri.tryParse(url.trim());
  if (uri == null || uri.host.isEmpty) return url.trim();
  final scheme = uri.scheme.isEmpty ? 'https' : uri.scheme;
  final port = uri.hasPort ? ':${uri.port}' : '';
  return '$scheme://${uri.host}$port/';
}

/// 解析实际使用的 API / WS 端点。
///
/// Web 端接口（REST）始终跟随当前打开页面的来源（同源），忽略区域选择与本地
/// 缓存的端点，避免「页面在 A 域名、请求发往 B 域名」导致的跨域 CORS 失败。
/// App 端（单一安装包）按选定区域返回固定端点。
String resolveRegionApiBaseUrl(AppRegion region, {bool isWeb = kIsWeb}) =>
    isWeb ? AppRuntimeEndpoints.apiBaseUrl : apiUrlForRegion(region);

/// 解析实际使用的 WS 端点。
///
/// App 端按选定区域返回固定端点。
/// Web 端 WS 不能简单跟随同源：全球区把接口与 WS 拆成两个域名
/// （gb.grix.im / ws.grix.im），页面开在接口域名 gb.grix.im 上，
/// 同源推导出的 wss://gb.grix.im/ws 并不存在 WS 服务。
/// 因此按当前页面域名匹配所属区域，返回该区域编译注入的 WS 地址（跨域连
/// ws.grix.im，由 WS 服务的 Origin 白名单放行）；CN 区接口与 WS 同域名，
/// 匹配结果与同源一致；未知域名（本地开发 / loopback）回退同源解析。
String resolveRegionWsUrl(
  AppRegion region, {
  bool isWeb = kIsWeb,
  Uri? baseUri,
}) => isWeb ? _resolveWebWsUrl(baseUri ?? Uri.base) : wsUrlForRegion(region);

/// 无明确区域上下文时使用的默认 WS 端点（启动引导 / 重连 / 推送跳转等）。
///
/// Web 端按当前页面域名解析所属区域的 WS（全球区跨域连 ws.grix.im）；
/// App 端回退编译注入的默认 / 同源 WS（具体区域连接由 [resolveRegionWsUrl] 决定）。
String resolveDefaultWsUrl({bool isWeb = kIsWeb, Uri? baseUri}) =>
    isWeb ? _resolveWebWsUrl(baseUri ?? Uri.base) : AppRuntimeEndpoints.wsUrl;

String _resolveWebWsUrl(Uri baseUri) {
  final pageHost = baseUri.host.trim().toLowerCase();
  if (pageHost.isNotEmpty) {
    if (pageHost == _hostOfUrl(kGlobalApiUrl)) return kGlobalWsUrl;
    if (pageHost == _hostOfUrl(kCnApiUrl)) return kCnWsUrl;
  }
  // 未知域名（本地开发 / loopback）：回退同源解析（含 WS_URL 编译注入与
  // loopback 兜底逻辑）。生产 web 端 baseUri 恒为 Uri.base，与此处一致。
  return AppRuntimeEndpoints.wsUrl;
}

String _hostOfUrl(String url) {
  final uri = Uri.tryParse(url.trim());
  return (uri?.host ?? '').toLowerCase();
}

/// 按 API 地址的域名识别所属区域；未知域名（本地开发 / 自建环境）返回 null。
AppRegion? regionOfApiBaseUrl(String url) {
  final host = _hostOfUrl(url);
  if (host.isEmpty) return null;
  if (host == _hostOfUrl(kCnApiUrl)) return AppRegion.cn;
  if (host == _hostOfUrl(kGlobalApiUrl)) return AppRegion.global;
  return null;
}

/// 将字符串解析为 AppRegion，未知值默认 global。
AppRegion regionFromString(String? value) {
  if (value == 'cn') return AppRegion.cn;
  return AppRegion.global;
}

/// 根据当前网页 URL 识别所属区域。
///
/// 仅用于 Web 端：登录页高亮当前域名对应分区，避免"页面在 gb.grix.im 上却
/// 显示 CN 被选中"的视觉错乱。未知域名（本地开发）回退按系统语言推断。
AppRegion detectRegionFromWebBaseUri(Uri baseUri) {
  final host = baseUri.host.trim().toLowerCase();
  if (host == _hostOfUrl(kCnApiUrl)) return AppRegion.cn;
  if (host == _hostOfUrl(kGlobalApiUrl)) return AppRegion.global;
  return detectRegionFromLocale();
}

/// 解析进入登录 / 注册 / 找回密码 / 启动页时的初始区域。
///
/// Web 端：按当前页面域名识别分区（grix.dhf.pub → CN，gb.grix.im → Global），
/// 保证显示的分区与实际所在域名一致；未知域名（本地开发）回退按语言推断。
/// App 端：优先沿用用户上次手动选择并记住的区域；没有记录时按系统语言推断
/// （简体中文大陆 → CN，其余 → Global），让海外用户首次打开即落在全球区。
Future<AppRegion> resolveInitialRegion({
  bool isWeb = kIsWeb,
  Uri? baseUri,
}) async {
  if (isWeb) {
    return detectRegionFromWebBaseUri(baseUri ?? Uri.base);
  }
  final saved = await AppStorageService.loadRegion();
  if (saved != null && saved.isNotEmpty) {
    return regionFromString(saved);
  }
  return detectRegionFromLocale();
}
