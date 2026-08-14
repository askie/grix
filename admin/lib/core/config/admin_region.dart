import 'package:flutter/foundation.dart' show kIsWeb, visibleForTesting;
import 'package:shared_preferences/shared_preferences.dart';

/// 管理后台支持的区域。
enum AdminRegion { cn, global }

/// 编译时可注入的区域 API 基地址（--dart-define=CN_ADMIN_API_BASE_URL=...）。
const String kCnAdminApiBase = String.fromEnvironment(
  'CN_ADMIN_API_BASE_URL',
  defaultValue: 'https://grix.dhf.pub',
);
const String kGlobalAdminApiBase = String.fromEnvironment(
  'GLOBAL_ADMIN_API_BASE_URL',
  defaultValue: 'https://gb.grix.im',
);

/// 管理后台区域选择的本地持久化。
class AdminRegionStore {
  AdminRegionStore._();

  static const _kKey = 'admin_region';

  /// 用户显式选择的区域；未选择过时为 null，用于和"默认 CN"区分开。
  static AdminRegion? _explicit;

  /// 供 UI 展示 / 凭据回填用的当前区域。
  ///
  /// 已显式选择过时返回选择值；未选择时，Web 端按当前访问域名推断——命中全球区
  /// 官方域名则展示"全球"，否则展示"中国大陆"（本地部署等非官方域名也归入此档，
  /// 因为登录请求本身走同源，与"中国大陆"这个展示无实际域名冲突）。原生端没有
  /// "当前域名"概念，始终展示"中国大陆"。
  static AdminRegion get current {
    final explicit = _explicit;
    if (explicit != null) return explicit;
    if (kIsWeb) return regionForHost(Uri.base.host);
    return AdminRegion.cn;
  }

  /// 是否已有用户显式选择（区分"从未选择"与"选择了 CN"）。
  static bool get hasExplicitChoice => _explicit != null;

  /// 是否应展示区域选择器：原生端始终展示；Web 端只在部署域名命中官方两个域名
  /// 之一时才展示，避免本地/自建域名部署时用户误触切换到生产绝对域名、且没有
  /// 回退入口（只能手动清本地存储）。
  static bool get shouldShowSelector {
    if (!kIsWeb) return true;
    return showSelectorForHost(Uri.base.host);
  }

  /// 按访问域名推断应展示的区域（不依赖 kIsWeb，方便单测直接喂 host 断言）。
  @visibleForTesting
  static AdminRegion regionForHost(String host) =>
      host == Uri.parse(kGlobalAdminApiBase).host
          ? AdminRegion.global
          : AdminRegion.cn;

  /// 按访问域名判断是否应展示区域选择器（不依赖 kIsWeb，方便单测直接喂 host 断言）。
  @visibleForTesting
  static bool showSelectorForHost(String host) =>
      host == Uri.parse(kCnAdminApiBase).host ||
      host == Uri.parse(kGlobalAdminApiBase).host;

  /// 启动时从本地加载区域到内存。
  static Future<void> load() async {
    final prefs = await SharedPreferences.getInstance();
    final v = prefs.getString(_kKey);
    if (v == 'global') {
      _explicit = AdminRegion.global;
    } else if (v == 'cn') {
      _explicit = AdminRegion.cn;
    } else {
      _explicit = null;
    }
  }

  /// 保存区域选择。
  static Future<void> save(AdminRegion region) async {
    _explicit = region;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_kKey, region == AdminRegion.global ? 'global' : 'cn');
  }

  /// 返回指定区域的后端基地址。
  static String baseUrlFor(AdminRegion region) =>
      region == AdminRegion.cn ? kCnAdminApiBase : kGlobalAdminApiBase;
}
