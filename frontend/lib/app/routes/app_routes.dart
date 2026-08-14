import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../modules/ai/agent_category_manage_view.dart';
import '../../modules/ai/agent_conn_security_view.dart';
import '../../modules/ai/agent_connection_setup_view.dart';
import '../../modules/ai/agent_create_view.dart';
import '../../modules/ai/agent_create_wizard_view.dart';
import '../../modules/ai/agent_quick_onboard_view.dart';
import '../../modules/ai/agent_scope_view.dart';
import '../../modules/ai/bindings/ai_binding.dart';
import '../../modules/ai/context_editor_view.dart';
import '../../modules/local_search/local_search_binding.dart';
import '../../modules/local_search/local_search_view.dart';
import '../../modules/account_info/account_info_view.dart';
import '../../modules/account_info/bindings/account_info_binding.dart';
import '../../modules/friend_requests/bindings/friend_requests_binding.dart';
import '../../modules/friend_requests/friend_requests_view.dart';
import '../../modules/group_info/group_info_view.dart';
import '../../modules/group_info/bindings/group_info_binding.dart';
import '../../modules/group_invite/group_invite_view.dart';
import '../../modules/group_invite/bindings/group_invite_binding.dart';
import '../../modules/auth/app_agreement_view.dart';
import '../../modules/auth/bindings/auth_binding.dart';
import '../../modules/auth/login_view.dart';
import '../../modules/auth/phone_login_view.dart';
import '../../modules/auth/register_view.dart';
import '../../modules/auth/reset_password_view.dart';
import '../../modules/auth/splash_view.dart';
import '../../modules/auth/user_agreement_view.dart';
import '../../modules/chat/bindings/chat_binding.dart';
import '../../modules/chat/chat_view.dart';
import '../../modules/chat/private_chat_creating_view.dart';
import '../../modules/home/bindings/home_binding.dart';
import '../../modules/home/home_view.dart';
import '../../modules/profile/about_view.dart';
import '../../modules/profile/account_switch_view.dart';
import '../../modules/profile/bindings/change_password_binding.dart';
import '../../modules/profile/controllers/account_switch_controller.dart';
import '../../modules/profile/change_password_view.dart';
import '../../modules/profile/device_management_view.dart';
import '../../modules/profile/help_view.dart';
import '../../modules/profile/agent_notification_prefs_view.dart';
import '../../modules/profile/notification_settings_view.dart';
import '../../modules/profile/privacy_view.dart';
import '../../modules/profile/settings_view.dart';
import '../../modules/profile/storage_view.dart';
import '../../modules/profile/webhook_integrations_view.dart';
import '../../modules/profile/widget_sites_view.dart';
import '../../modules/report/bindings/report_binding.dart';
import '../../modules/report/report_view.dart';
import '../../modules/home/bindings/favorites_binding.dart';
import '../../modules/home/favorites_view.dart';
import '../../modules/gateway/gateway_model_settings_view.dart';
import '../../platform/platform_capability.dart';

enum HomeTab { conversations, agents, eggsPond, contacts, settings, system }

extension HomeTabX on HomeTab {
  int get index {
    switch (this) {
      case HomeTab.conversations:
        return 0;
      case HomeTab.agents:
        return 1;
      case HomeTab.eggsPond:
        return 2;
      case HomeTab.contacts:
        return 3;
      case HomeTab.settings:
        return 4;
      case HomeTab.system:
        return 5;
    }
  }

  String get routePath {
    switch (this) {
      case HomeTab.conversations:
        return AppRoutes.home;
      case HomeTab.agents:
        return AppRoutes.homeAgents;
      case HomeTab.eggsPond:
        return AppRoutes.homeEggsPond;
      case HomeTab.contacts:
        return AppRoutes.homeContacts;
      case HomeTab.settings:
        return AppRoutes.homeSettings;
      case HomeTab.system:
        return AppRoutes.homeSystem;
    }
  }

  static HomeTab fromIndex(int index) {
    switch (index) {
      case 0:
        return HomeTab.conversations;
      case 1:
        return HomeTab.agents;
      case 2:
        return HomeTab.eggsPond;
      case 3:
        return HomeTab.contacts;
      case 4:
        return HomeTab.settings;
      case 5:
        return HomeTab.system;
      default:
        return HomeTab.conversations;
    }
  }
}

class AppRoutes {
  static const int defaultPageTransitionMilliseconds = 250;
  static const String splash = '/';
  static const String login = '/login';
  static const String register = '/register';
  static const String appAgreement = '/app-agreement';
  static const String userAgreement = '/user-agreement';
  static const String resetPassword = '/reset-password';
  static const String phoneLogin = '/phone-login';
  static const String home = '/home';
  static const String localSearch = '/local-search';
  static const String homeAgents = '/home/agents';
  static const String homeEggsPond = '/home/eggs-pond';
  static const String homeContacts = '/home/contacts';
  static const String homeSettings = '/home/settings';
  static const String homeSystem = '/home/system';
  static const String chat = '/chat';
  static const String privateChatCreating = '/private-chat-creating';
  static const String changePassword = '/change-password';
  static const String settings = '/settings';
  static const String notifications = '/settings/notifications';
  static const String agentNotificationPrefs = '/settings/agent-notifications';
  static const String privacy = '/settings/privacy';
  static const String storage = '/settings/storage';
  static const String deviceManagement = '/settings/devices';
  static const String accountSwitch = '/settings/accounts';
  static const String help = '/settings/help';
  static const String about = '/settings/about';
  static const String widgetSites = '/settings/widget-sites';
  static const String webhookIntegrations = '/settings/webhook-integrations';
  static const String agentCreate = '/agent/create';
  static const String agentEdit = '/agent/edit';
  static const String agentSetup = '/agent/setup';
  static const String agentQuickSetup = '/agent/quick-setup';
  static const String agentCategoryManage = '/agent/categories';
  static const String agentScopes = '/agent/scopes';
  static const String agentConnSecurity = '/agent/conn-security';
  static const String contextEditor = '/agent/context';
  static const String accountInfo = '/account-info';
  static const String groupInfo = '/group-info';
  static const String groupInvite = '/group-invite';
  static const String friendRequests = '/friend-requests';
  static const String report = '/report';

  // 模型设置（M4）：移动端默认模型 + Agent 模型设置，无平台门控
  static const String gatewayModelSettings = '/gateway/model-settings';

  static const String favorites = '/favorites';

  // 语音通话（Phase 1）- 来电/通话中改为 showDialog，无需路由
  static const String callHistory = '/call/history';

  static List<String> get _homeTabPaths => [
    home,
    homeAgents,
    homeEggsPond,
    homeContacts,
    homeSettings,
    if (PlatformCapability.isDesktop) homeSystem,
  ];

  static String pathOf(String routeName) {
    final normalizedRouteName = routeName.trim();
    if (normalizedRouteName.isEmpty) {
      return normalizedRouteName;
    }
    return Uri.tryParse(normalizedRouteName)?.path ?? normalizedRouteName;
  }

  static String get currentPath {
    if (kIsWeb) {
      return Uri.base.path;
    }
    return pathOf(Get.currentRoute);
  }

  static bool get isCurrentHomePath => isHomePath(currentPath);

  static bool isHomePath(String routeName) {
    return homeTabForPath(routeName) != null;
  }

  static HomeTab? homeTabForPath(String routeName) {
    switch (pathOf(routeName)) {
      case home:
        return HomeTab.conversations;
      case homeAgents:
        return HomeTab.agents;
      case homeEggsPond:
        return HomeTab.eggsPond;
      case homeContacts:
        return HomeTab.contacts;
      case homeSettings:
        return HomeTab.settings;
      case homeSystem:
        return HomeTab.system;
      default:
        return null;
    }
  }

  static GetPage<dynamic> _buildHomePage(String routeName) {
    return GetPage(
      name: routeName,
      page: () => const HomeView(),
      binding: HomeBinding(),
      transition: Transition.fadeIn,
      popGesture: false,
    );
  }

  static final routes = [
    GetPage(
      name: splash,
      page: () => const SplashView(),
      binding: SplashBinding(),
      popGesture: false,
    ),
    GetPage(
      name: login,
      page: () => const LoginView(),
      binding: LoginBinding(),
      popGesture: false,
    ),
    GetPage(
      name: register,
      page: () => const RegisterView(),
      binding: RegisterBinding(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: appAgreement,
      page: () => const AppAgreementView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: userAgreement,
      page: () => const UserAgreementView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: localSearch,
      page: () => const LocalSearchView(),
      binding: LocalSearchBinding(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: resetPassword,
      page: () => const ResetPasswordView(),
      binding: ResetPasswordBinding(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: phoneLogin,
      page: () => const PhoneLoginView(),
      binding: PhoneLoginBinding(),
      transition: Transition.rightToLeft,
    ),
    for (final routeName in _homeTabPaths) _buildHomePage(routeName),
    GetPage(
      name: privateChatCreating,
      page: () => const PrivateChatCreatingView(),
      // Fallback for direct toNamed; production create flow uses
      // PrivateChatCreatingRoute (allowSnapshotting:false). Keep this aligned:
      // no snapshot window, no parallax handoff, opaque shell.
      transition: Transition.noTransition,
      transitionDuration: Duration.zero,
      opaque: true,
      showCupertinoParallax: false,
    ),
    GetPage(
      name: chat,
      page: () => ChatView(controllerTag: ChatBinding.currentControllerTag()),
      binding: ChatBinding(),
      transition: Transition.rightToLeft,
      transitionDuration: const Duration(
        milliseconds: defaultPageTransitionMilliseconds,
      ),
      // opaque:false 让聊天页打开后不遮挡剔除下层会话列表,
      // 右滑返回时下层始终保持已绘制,消除重新光栅化导致的白屏。
      // 聊天页 Scaffold 自身背景不透明,视觉上不会透出下层。
      opaque: false,
    ),
    GetPage(
      name: changePassword,
      page: () => const ChangePasswordView(),
      binding: ChangePasswordBinding(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: accountInfo,
      page: () => const AccountInfoView(),
      binding: AccountInfoBinding(),
      transition: Transition.rightToLeft,
      transitionDuration: const Duration(
        milliseconds: defaultPageTransitionMilliseconds,
      ),
    ),
    GetPage(
      name: groupInfo,
      page: () => const GroupInfoView(),
      binding: GroupInfoBinding(),
      transition: Transition.rightToLeft,
      transitionDuration: const Duration(
        milliseconds: defaultPageTransitionMilliseconds,
      ),
    ),
    GetPage(
      name: groupInvite,
      page: () => const GroupInviteView(),
      binding: GroupInviteBinding(),
      transition: Transition.rightToLeft,
      transitionDuration: const Duration(
        milliseconds: defaultPageTransitionMilliseconds,
      ),
    ),
    GetPage(
      name: report,
      page: () => const ReportView(),
      binding: ReportBinding(),
      transition: Transition.rightToLeft,
      transitionDuration: const Duration(
        milliseconds: defaultPageTransitionMilliseconds,
      ),
    ),
    GetPage(
      name: gatewayModelSettings,
      page: () => const GatewayModelSettingsView(),
      transition: Transition.rightToLeft,
      transitionDuration: const Duration(
        milliseconds: defaultPageTransitionMilliseconds,
      ),
    ),
    GetPage(
      name: friendRequests,
      page: () => const FriendRequestsView(),
      binding: FriendRequestsBinding(),
      transition: Transition.rightToLeft,
      transitionDuration: const Duration(
        milliseconds: defaultPageTransitionMilliseconds,
      ),
    ),
    GetPage(
      name: favorites,
      page: () => const FavoritesView(),
      binding: FavoritesBinding(),
      transition: Transition.rightToLeft,
      transitionDuration: const Duration(
        milliseconds: defaultPageTransitionMilliseconds,
      ),
    ),
    GetPage(
      name: settings,
      page: () => const SettingsView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: notifications,
      page: () => const NotificationSettingsView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: agentNotificationPrefs,
      page: () => const AgentNotificationPrefsView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: privacy,
      page: () => const PrivacyView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: storage,
      page: () => const StorageView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: deviceManagement,
      page: () => const DeviceManagementView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: accountSwitch,
      page: () => const AccountSwitchView(),
      binding: BindingsBuilder(() {
        Get.lazyPut(() => AccountSwitchController());
      }),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: help,
      page: () => const HelpView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: about,
      page: () => const AboutView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: widgetSites,
      page: () => const WidgetSitesView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: webhookIntegrations,
      page: () => const WebhookIntegrationsView(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: agentCreate,
      page: () => const AgentCreateWizardView(),
      binding: AiBinding(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: agentEdit,
      page: () => const AgentCreateView(),
      binding: AiBinding(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: agentSetup,
      page: () => const AgentConnectionSetupView(),
      binding: AiBinding(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: agentQuickSetup,
      page: () => const AgentQuickOnboardView(),
      binding: AiBinding(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: agentCategoryManage,
      page: () => const AgentCategoryManageView(),
      binding: AiBinding(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: agentScopes,
      page: () => const AgentScopeView(),
      binding: AiBinding(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: agentConnSecurity,
      page: () => const AgentConnSecurityView(),
      binding: AiBinding(),
      transition: Transition.rightToLeft,
    ),
    GetPage(
      name: contextEditor,
      page: () => const ContextEditorView(),
      binding: AiBinding(),
      transition: Transition.rightToLeft,
    ),
    // 语音通话（Phase 1）
  ];
}
