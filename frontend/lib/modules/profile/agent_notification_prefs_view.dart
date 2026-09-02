import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/notification_prefs_service.dart';
import '../../shared/utils/toast_util.dart';

/// Per-event Agent notification preferences. Each event has a master on/off
/// switch (push is the default and currently only channel — TTS broadcast is
/// deferred). approval_requested is force-enabled and its switch is locked on.
class AgentNotificationPrefsView extends StatefulWidget {
  const AgentNotificationPrefsView({super.key});

  @override
  State<AgentNotificationPrefsView> createState() =>
      _AgentNotificationPrefsViewState();
}

class _AgentNotificationPrefsViewState
    extends State<AgentNotificationPrefsView> {
  late final NotificationPrefsService _service;

  // Display metadata per event key → (titleKey, descKey). i18n keys resolved at
  // build time via .tr. Order follows the backend canonical list.
  static const Map<String, (String, String)> _meta = {
    'approval_requested': (
      'agent_notif_ev_approval_title',
      'agent_notif_ev_approval_desc',
    ),
    'agent_question': (
      'agent_notif_ev_question_title',
      'agent_notif_ev_question_desc',
    ),
    'task_completed': (
      'agent_notif_ev_completed_title',
      'agent_notif_ev_completed_desc',
    ),
    'task_failed': (
      'agent_notif_ev_failed_title',
      'agent_notif_ev_failed_desc',
    ),
    'task_stopped_unexpected': (
      'agent_notif_ev_stopped_title',
      'agent_notif_ev_stopped_desc',
    ),
    'agent_online': (
      'agent_notif_ev_online_title',
      'agent_notif_ev_online_desc',
    ),
    'agent_offline': (
      'agent_notif_ev_offline_title',
      'agent_notif_ev_offline_desc',
    ),
  };

  @override
  void initState() {
    super.initState();
    _service = Get.find<NotificationPrefsService>();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _service.ensureSyncedWithCurrentUser(force: true);
    });
  }

  Future<void> _toggle(NotificationPref pref, bool value) async {
    final ok = await _service.updatePref(pref.eventKey, enabled: value);
    if (!ok) {
      CustomToast.show('agent_notif_save_failed'.tr);
    }
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
        title: Text('agent_notif_title'.tr),
      ),
      body: Obx(() {
        if (_service.isLoading.value && _service.prefs.isEmpty) {
          return const Center(child: CircularProgressIndicator());
        }
        final ordered = <NotificationPref>[];
        for (final key in _meta.keys) {
          final found = _service.prefs.firstWhereOrNull(
            (p) => p.eventKey == key,
          );
          if (found != null) ordered.add(found);
        }
        if (ordered.isEmpty) {
          return Center(
            child: Text(
              'agent_notif_empty'.tr,
              style: TextStyle(color: theme.colorScheme.secondary),
            ),
          );
        }
        return ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Padding(
              padding: const EdgeInsets.only(bottom: 8, left: 4),
              child: Text(
                'agent_notif_hint'.tr,
                style: TextStyle(
                  fontSize: 13,
                  color: theme.colorScheme.secondary,
                ),
              ),
            ),
            ...ordered.map(_buildRow),
          ],
        );
      }),
    );
  }

  Widget _buildRow(NotificationPref pref) {
    final theme = Theme.of(context);
    final meta = _meta[pref.eventKey];
    final title = meta != null ? meta.$1.tr : pref.eventKey;
    final subtitle = meta != null ? meta.$2.tr : '';
    final locked =
        pref.eventKey == NotificationPrefsService.eventApprovalRequested;

    return Container(
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(16),
      ),
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
                if (subtitle.isNotEmpty) ...[
                  const SizedBox(height: 4),
                  Text(
                    subtitle,
                    style: TextStyle(
                      fontSize: 13,
                      color: theme.colorScheme.secondary,
                    ),
                  ),
                ],
              ],
            ),
          ),
          const SizedBox(width: 12),
          Switch(
            value: locked ? true : pref.enabled,
            onChanged: locked ? null : (v) => _toggle(pref, v),
          ),
        ],
      ),
    );
  }
}
