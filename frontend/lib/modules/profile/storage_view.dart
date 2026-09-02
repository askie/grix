import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/agent_service.dart';
import '../../data/providers/friend_service.dart';
import '../../data/providers/im_service.dart';
import '../../data/providers/local_db.dart';
import '../chat/controllers/chat_controller.dart';
import '../chat/services/conversation_audit_preference_service.dart';
import '../../shared/utils/app_storage_service.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_dialog_style.dart';

class StorageView extends StatefulWidget {
  const StorageView({super.key});

  @override
  State<StorageView> createState() => _StorageViewState();
}

class _StorageViewState extends State<StorageView> {
  LocalStorageSummary _summary = const LocalStorageSummary.empty();
  bool _loading = true;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _reloadSummary();
  }

  Future<void> _reloadSummary() async {
    setState(() => _loading = true);
    final summary = await AppStorageService.getActiveUserSummary();
    if (!mounted) return;
    setState(() {
      _summary = summary;
      _loading = false;
    });
  }

  Future<void> _clearImageCache() async {
    setState(() => _busy = true);
    await AppStorageService.clearActiveUserImageCache();
    if (!mounted) return;
    setState(() => _busy = false);
    CustomToast.show('storage_clear_images_success'.tr, isError: false);
  }

  Future<void> _clearLocalChatData() async {
    setState(() => _busy = true);
    final userId = LocalDb.activeUserId;

    await AppStorageService.clearActiveUserLocalData();

    if (Get.isRegistered<ImService>()) {
      await Get.find<ImService>().resetForAccountSwitch();
      if (userId != null) {
        await LocalDb.setActiveUser(userId);
        await Get.find<ImService>().loadSessionsForCurrentUser();
        // The reset cleared the shared message window; a chat still on screen
        // (desktop pane) must reload instead of staying empty.
        ChatController.restoreSharedMessageWindow();
      }
    }
    if (Get.isRegistered<FriendService>()) {
      Get.find<FriendService>().resetForAccountSwitch();
      await Get.find<FriendService>().loadFriendList();
      await Get.find<FriendService>().loadFriendRequests();
    }
    if (Get.isRegistered<AgentService>()) {
      Get.find<AgentService>().resetForAccountSwitch();
      await Get.find<AgentService>().loadAgents();
    }
    if (Get.isRegistered<ConversationAuditPreferenceService>()) {
      Get.find<ConversationAuditPreferenceService>().resetForAccountSwitch();
    }

    if (!mounted) return;
    setState(() => _busy = false);
    await _reloadSummary();
    CustomToast.show('storage_clear_local_success'.tr, isError: false);
  }

  Future<void> _confirmClear({
    required String title,
    required String content,
    required Future<void> Function() action,
  }) async {
    await showAppGetDialog(
      AlertDialog(
        title: Text(title),
        content: Text(content),
        actions: [
          TextButton(
            onPressed: () => Get.back(),
            child: Text('common_cancel'.tr),
          ),
          ElevatedButton(
            onPressed: () async {
              Get.back();
              await action();
            },
            child: Text('common_confirm'.tr),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(
        leading: IconButton(
          icon: const Icon(Icons.arrow_back_ios_rounded, size: 20),
          onPressed: () => Get.back(),
        ),
        title: Text('me_storage'.tr),
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : ListView(
              padding: const EdgeInsets.all(16),
              children: [
                _StorageCard(
                  title: 'storage_summary_title'.tr,
                  children: [
                    _StorageMetric(
                      label: 'storage_summary_sessions'.tr,
                      value: _summary.sessionCount.toString(),
                    ),
                    const SizedBox(height: 12),
                    _StorageMetric(
                      label: 'storage_summary_messages'.tr,
                      value: _summary.messageCount.toString(),
                    ),
                    const SizedBox(height: 12),
                    _StorageMetric(
                      label: 'storage_summary_profiles'.tr,
                      value: _summary.userCount.toString(),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                _StorageAction(
                  title: 'storage_clear_images_title'.tr,
                  subtitle: 'storage_clear_images_subtitle'.tr,
                  busy: _busy,
                  onTap: () => _confirmClear(
                    title: 'storage_clear_images_title'.tr,
                    content: 'storage_clear_images_confirm'.tr,
                    action: _clearImageCache,
                  ),
                ),
                const SizedBox(height: 12),
                _StorageAction(
                  title: 'storage_clear_local_title'.tr,
                  subtitle: 'storage_clear_local_subtitle'.tr,
                  busy: _busy,
                  onTap: () => _confirmClear(
                    title: 'storage_clear_local_title'.tr,
                    content: 'storage_clear_local_confirm'.tr,
                    action: _clearLocalChatData,
                  ),
                ),
                const SizedBox(height: 12),
                Text(
                  'storage_local_only_hint'.tr,
                  style: TextStyle(
                    fontSize: 13,
                    color: theme.colorScheme.secondary,
                  ),
                ),
              ],
            ),
    );
  }
}

class _StorageCard extends StatelessWidget {
  const _StorageCard({required this.title, required this.children});

  final String title;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 12),
          ...children,
        ],
      ),
    );
  }
}

class _StorageMetric extends StatelessWidget {
  const _StorageMetric({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      children: [
        Expanded(
          child: Text(
            label,
            style: TextStyle(fontSize: 13, color: theme.colorScheme.secondary),
          ),
        ),
        Text(
          value,
          style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 15),
        ),
      ],
    );
  }
}

class _StorageAction extends StatelessWidget {
  const _StorageAction({
    required this.title,
    required this.subtitle,
    required this.onTap,
    required this.busy,
  });

  final String title;
  final String subtitle;
  final VoidCallback onTap;
  final bool busy;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      color: theme.colorScheme.surface,
      borderRadius: BorderRadius.circular(16),
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: busy ? null : onTap,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      title,
                      style: const TextStyle(
                        fontWeight: FontWeight.w700,
                        fontSize: 15,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      subtitle,
                      style: TextStyle(
                        fontSize: 13,
                        color: theme.colorScheme.secondary,
                      ),
                    ),
                  ],
                ),
              ),
              busy
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Icon(
                      Icons.chevron_right_rounded,
                      color: theme.colorScheme.secondary.withValues(alpha: 0.5),
                    ),
            ],
          ),
        ),
      ),
    );
  }
}
