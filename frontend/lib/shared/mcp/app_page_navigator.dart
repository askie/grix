/// APP 内置 MCP 工具的页面导航封装。
///
/// 单一职责：把「打开聊天会话 / 打开某个功能页」翻译成具体的 GetX 导航动作。
/// 工具注册表只调用这里，不关心 tab 切换与路由跳转的差异。新增可导航页面
/// 只需在 [_pages] 映射表追加一行。
library;

import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../modules/chat/services/chat_route_navigator.dart';
import '../../modules/home/controllers/home_controller.dart';

/// 单个可导航页面的描述：中文名 + 目标（底部 tab 或独立路由，二选一）。
class _PageTarget {
  const _PageTarget(this.label, {this.tab, this.route})
    : assert((tab == null) != (route == null), 'tab 与 route 必须二选一');

  /// 给用户/AI 回执用的中文名。
  final String label;

  /// 底部主页 tab（与 route 互斥）。
  final HomeTab? tab;

  /// 独立路由名（与 tab 互斥）。
  final String? route;
}

class AppPageNavigator {
  AppPageNavigator._();

  /// 打开指定会话的聊天页。title/type 走默认，进入后聊天页会按 session_id
  /// 自行加载会话详情并纠正标题与会话类型。
  static bool openChat(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) return false;
    ChatRouteNavigator.toChat(sessionId: sid, title: '', type: 'private');
    return true;
  }

  /// 打开本地搜索页并带入初始关键词（AI 调 grix_local_search 用）。
  static bool openLocalSearch(List<String> keywords) {
    Get.toNamed(AppRoutes.localSearch, arguments: {'keywords': keywords});
    return true;
  }

  /// 打开指定页面。返回页面中文名（成功）或 null（未知页面名）。
  static String? openPage(String page) {
    final target = _pages[page.trim()];
    if (target == null) return null;
    if (target.tab != null) {
      _openTab(target.tab!);
    } else if (target.route != null) {
      Get.toNamed(target.route!);
    }
    return target.label;
  }

  /// 切到底部主页 tab：先回到主页层（弹掉压在其上的子页），再切换 tab，
  /// 避免 tab 切了但用户仍停留在上层子页而看不到效果。主页未初始化时
  /// （如尚未登录）直接按路由进入对应 tab。
  static void _openTab(HomeTab tab) {
    if (Get.isRegistered<HomeController>()) {
      Get.until(
        (route) =>
            AppRoutes.isHomePath(AppRoutes.pathOf(route.settings.name ?? '')) ||
            route.isFirst,
      );
      Get.find<HomeController>().changePage(tab.index);
    } else {
      Get.toNamed(tab.routePath);
    }
  }

  /// 页面名 → 目标映射。新增可导航页面只加一行。
  static const Map<String, _PageTarget> _pages = {
    // 底部主页 tab
    'messages': _PageTarget('消息', tab: HomeTab.conversations),
    'ai': _PageTarget('AI 列表', tab: HomeTab.agents),
    'eggs_pond': _PageTarget('虾蛋池', tab: HomeTab.eggsPond),
    'contacts': _PageTarget('联系人', tab: HomeTab.contacts),
    'me': _PageTarget('我的', tab: HomeTab.settings),
    // 独立页面
    'ai_create': _PageTarget('创建 AI', route: AppRoutes.agentCreate),
    'ai_categories': _PageTarget(
      'AI 分类管理',
      route: AppRoutes.agentCategoryManage,
    ),
    'friend_requests': _PageTarget('好友请求', route: AppRoutes.friendRequests),
    'settings': _PageTarget('账号设置', route: AppRoutes.settings),
    'notifications': _PageTarget('通知设置', route: AppRoutes.notifications),
    'agent_notifications': _PageTarget(
      'AI 通知订阅',
      route: AppRoutes.agentNotificationPrefs,
    ),
    'privacy': _PageTarget('隐私设置', route: AppRoutes.privacy),
    'storage': _PageTarget('存储管理', route: AppRoutes.storage),
    'devices': _PageTarget('设备管理', route: AppRoutes.deviceManagement),
    'account': _PageTarget('账号信息', route: AppRoutes.accountInfo),
    'change_password': _PageTarget('修改密码', route: AppRoutes.changePassword),
    'widget_sites': _PageTarget('Widget 站点', route: AppRoutes.widgetSites),
    'webhook': _PageTarget('Webhook 集成', route: AppRoutes.webhookIntegrations),
    'help': _PageTarget('帮助', route: AppRoutes.help),
    'about': _PageTarget('关于', route: AppRoutes.about),
  };

  /// 所有支持的页面名（供工具 inputSchema 的 enum 使用）。
  static List<String> get supportedPages => _pages.keys.toList(growable: false);

  /// 给工具描述用的页面名提示，形如 "messages(消息) ai(AI 列表) ..."，
  /// 让 AI 知道每个页面名对应什么，避免描述与映射表两处维护。
  static String get pagesHint =>
      _pages.entries.map((e) => '${e.key}(${e.value.label})').join(' ');
}
