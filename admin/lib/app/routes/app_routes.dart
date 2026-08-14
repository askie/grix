import 'package:get/get.dart';

import '../../modules/admins/admins_binding.dart';
import '../../modules/admins/admins_view.dart';
import '../../modules/app_releases/app_rollout_view.dart';
import '../../modules/app_releases/binding.dart';
import '../../modules/app_releases/view.dart';
import '../../modules/auth/login_binding.dart';
import '../../modules/auth/login_view.dart';
import '../../modules/connector/connector_controller.dart';
import '../../modules/connector/connector_rollout_view.dart';
import '../../modules/connector/connector_view.dart';
import '../../modules/dashboard/dashboard_binding.dart';
import '../../modules/dashboard/dashboard_view.dart';
import '../../modules/eggs/egg_detail_view.dart';
import '../../modules/eggs/egg_form_view.dart';
import '../../modules/eggs/eggs_controller.dart';
import '../../modules/eggs/eggs_view.dart';
import '../../modules/feature_gates/feature_gates_binding.dart';
import '../../modules/feature_gates/feature_gates_view.dart';
import '../../modules/gateway/gateway_credentials_view.dart';
import '../../modules/gateway/gateway_pricing_view.dart';
import '../../modules/gateway/gateway_reconciliation_view.dart';
import '../../modules/gateway/gateway_wallet_detail_view.dart';
import '../../modules/gateway/gateway_wallets_binding.dart';
import '../../modules/gateway/gateway_wallets_view.dart';
import '../../modules/link_blocklist/link_blocklist_binding.dart';
import '../../modules/reach/reach_binding.dart';
import '../../modules/reach/reach_task_detail_view.dart';
import '../../modules/reach/reach_tasks_view.dart';
import '../../modules/reach/reach_templates_view.dart';
import '../../modules/link_blocklist/link_blocklist_settings_view.dart';
import '../../modules/link_blocklist/link_blocklist_view.dart';
import '../../modules/moderation/moderation_binding.dart';
import '../../modules/moderation/moderation_settings_view.dart';
import '../../modules/moderation/moderation_view.dart';
import '../../modules/reports/report_detail_view.dart';
import '../../modules/reports/reports_binding.dart';
import '../../modules/reports/reports_view.dart';
import '../../modules/settings/settings_binding.dart';
import '../../modules/settings/settings_view.dart';
import '../../modules/settings/pay_channel/pay_channel_settings_binding.dart';
import '../../modules/settings/pay_channel/pay_channel_settings_view.dart';
import '../../modules/settings/push/push_settings_binding.dart';
import '../../modules/settings/push/push_settings_view.dart';
import '../../modules/settings/sms/sms_settings_binding.dart';
import '../../modules/settings/sms/sms_settings_view.dart';
import '../../modules/users/users_binding.dart';
import '../../modules/users/users_view.dart';
import '../../modules/visitor_bans/visitor_bans_binding.dart';
import '../../modules/visitor_bans/visitor_bans_view.dart';
import '../../shared/navigation/auth_guard.dart';

/// 路由名常量。
class AppRoutes {
  AppRoutes._();

  static const String login = '/login';
  static const String dashboard = '/';
  static const String users = '/users';
  static const String onlineUsers = '/online-users';
  static const String reports = '/reports';
  static const String reportDetail = '/reports/:id';
  static const String moderation = '/moderation';
  static const String moderationSettings = '/moderation/settings';
  static const String visitorBans = '/visitor-bans';
  static const String linkBlocklist = '/link-blocklist';
  static const String linkBlocklistSettings = '/link-blocklist/settings';
  static const String admins = '/admins';
  static const String settings = '/settings';
  static const String smsSettings = '/settings/sms';
  static const String pushSettings = '/settings/push';
  static const String payChannelSettings = '/settings/pay-channel';
  static const String featureGates = '/feature-gates';
  static const String appReleases = '/app/releases';
  static const String appRollout = '/app/rollout';
  static const String connector = '/connector/releases';
  static const String connectorRollout = '/connector/rollout';
  static const String eggs = '/eggs';
  static const String eggNew = '/eggs/new';
  static const String eggEdit = '/eggs/:id/edit';
  static const String eggDetail = '/eggs/:id';
  static const String gatewayWallets = '/gateway/wallets';
  static const String gatewayWalletDetail = '/gateway/wallets/:id';
  static const String gatewayPricingRules = '/gateway/pricing-rules';
  static const String gatewayReconciliationReports =
      '/gateway/reconciliation-reports';
  static const String gatewayUpstreamCredentials =
      '/gateway/upstream-credentials';
  static const String reachTasks = '/reach/tasks';
  static const String reachTaskDetail = '/reach/tasks/:id';
  static const String reachTemplates = '/reach/templates';

  /// 登录后的默认落地页。
  static const String home = dashboard;
}

/// GetX 页面表。
class AppPages {
  AppPages._();

  static final List<GetPage> pages = <GetPage>[
    GetPage(
      name: AppRoutes.login,
      page: () => const LoginView(),
      binding: LoginBinding(),
    ),
    GetPage(
      name: AppRoutes.dashboard,
      page: () => const DashboardView(),
      binding: DashboardBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.users,
      page: () => const UsersView(),
      binding: UsersBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.onlineUsers,
      page: () => const UsersView(onlineOnly: true),
      binding: UsersBinding(onlineOnly: true),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.reports,
      page: () => const ReportsView(),
      binding: ReportsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.reportDetail,
      page: () => const ReportDetailView(),
      binding: ReportDetailBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.moderation,
      page: () => const ModerationView(),
      binding: ModerationBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.moderationSettings,
      page: () => const ModerationSettingsView(),
      binding: ModerationSettingsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.visitorBans,
      page: () => const VisitorBansView(),
      binding: VisitorBansBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.linkBlocklist,
      page: () => const LinkBlocklistView(),
      binding: LinkBlocklistBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.linkBlocklistSettings,
      page: () => const LinkBlocklistSettingsView(),
      binding: LinkBlocklistSettingsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.admins,
      page: () => const AdminsView(),
      binding: AdminsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.settings,
      page: () => const SettingsView(),
      binding: SettingsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.smsSettings,
      page: () => const SmsSettingsView(),
      binding: SmsSettingsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.pushSettings,
      page: () => const PushSettingsView(),
      binding: PushSettingsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.payChannelSettings,
      page: () => const PayChannelSettingsView(),
      binding: PayChannelSettingsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.featureGates,
      page: () => const FeatureGatesView(),
      binding: FeatureGatesBinding(),
      middlewares: [AuthGuard()],
    ),
    // App releases + sub-pages
    GetPage(
      name: AppRoutes.appReleases,
      page: () => const AppReleasesView(),
      binding: AppReleasesBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.appRollout,
      page: () => const AppRolloutView(),
      middlewares: [AuthGuard()],
    ),
    // Connector + sub-pages
    GetPage(
      name: AppRoutes.connector,
      page: () => const ConnectorView(),
      binding: ConnectorBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.connectorRollout,
      page: () => const ConnectorRolloutView(),
      middlewares: [AuthGuard()],
    ),
    // Eggs + sub-pages（注意：/eggs/new 需在 /eggs/:id 之前注册）
    GetPage(
      name: AppRoutes.eggs,
      page: () => const EggsView(),
      binding: EggsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.eggNew,
      page: () => const EggFormView(),
      binding: EggsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.eggEdit,
      page: () => const EggFormView(),
      binding: EggsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.eggDetail,
      page: () => const EggDetailView(),
      binding: EggsBinding(),
      middlewares: [AuthGuard()],
    ),
    // 大模型计费网关 + 子页面
    GetPage(
      name: AppRoutes.gatewayWallets,
      page: () => const GatewayWalletsView(),
      binding: GatewayWalletsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.gatewayWalletDetail,
      page: () => const GatewayWalletDetailView(),
      binding: GatewayWalletDetailBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.gatewayPricingRules,
      page: () => const GatewayPricingView(),
      binding: GatewayPricingRulesBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.gatewayReconciliationReports,
      page: () => const GatewayReconciliationView(),
      binding: GatewayReconciliationReportsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.gatewayUpstreamCredentials,
      page: () => const GatewayCredentialsView(),
      binding: GatewayUpstreamCredentialsBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.reachTasks,
      page: () => const ReachTasksView(),
      binding: ReachTasksBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.reachTemplates,
      page: () => const ReachTemplatesView(),
      binding: ReachTemplatesBinding(),
      middlewares: [AuthGuard()],
    ),
    GetPage(
      name: AppRoutes.reachTaskDetail,
      page: () => const ReachTaskDetailView(),
      binding: ReachTaskDetailBinding(),
      middlewares: [AuthGuard()],
    ),
  ];
}
