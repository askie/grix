import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../../app/themes/app_theme.dart';
import '../../../data/providers/agent_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../data/providers/session_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/widgets/session_avatar.dart';
import '../../../shared/widgets/app_dialog_style.dart';
import '../../ai/widgets/contact_agent_picker_sheet.dart';
import '../services/chat_route_navigator.dart';

class _AgentMessageRecipientStore {
  static const String _agentIdKey =
      'send_message_to_agent.last_selected_agent_id';

  Future<void> _pendingWrite = Future<void>.value();

  Future<String?> loadAgentId() async {
    try {
      final preferences = await SharedPreferences.getInstance();
      final agentId = preferences.getString(_agentIdKey)?.trim();
      return agentId == null || agentId.isEmpty ? null : agentId;
    } catch (error) {
      debugPrint('Failed to load cached message recipient: $error');
      return null;
    }
  }

  Future<void> saveAgentId(String agentId) async {
    final normalizedAgentId = agentId.trim();
    if (normalizedAgentId.isEmpty) {
      return;
    }
    final write = _pendingWrite.then((_) async {
      try {
        final preferences = await SharedPreferences.getInstance();
        await preferences.setString(_agentIdKey, normalizedAgentId);
      } catch (error) {
        debugPrint('Failed to cache message recipient: $error');
      }
    });
    _pendingWrite = write;
    await write;
  }

  Future<void> clearIfMatches(String agentId) async {
    final normalizedAgentId = agentId.trim();
    final write = _pendingWrite.then((_) async {
      try {
        final preferences = await SharedPreferences.getInstance();
        if (preferences.getString(_agentIdKey)?.trim() == normalizedAgentId) {
          await preferences.remove(_agentIdKey);
        }
      } catch (error) {
        debugPrint('Failed to clear cached message recipient: $error');
      }
    });
    _pendingWrite = write;
    await write;
  }
}

/// Opens a reusable flow for selecting one Agent, editing a message, sending it
/// in a new private session, and navigating to that conversation.
Future<String?> showSendMessageToAgentDialog(
  BuildContext context, {
  required String initialMessage,
  String title = 'send_message_to_agent_title',
  String messageLabel = 'send_message_to_agent_message_label',
  bool barrierDismissible = true,
  Widget? header,
}) {
  if (!Get.isRegistered<AgentService>() ||
      !Get.isRegistered<ImService>() ||
      !Get.isRegistered<SessionService>()) {
    CustomToast.show('send_message_to_agent_unavailable'.tr, isError: true);
    return Future<String?>.value();
  }

  return showAppDialog<String>(
    context: context,
    barrierDismissible: barrierDismissible,
    builder: (_) => _SendMessageToAgentDialog(
      initialMessage: initialMessage,
      title: title,
      messageLabel: messageLabel,
      header: header,
    ),
  );
}

class _SendMessageToAgentDialog extends StatefulWidget {
  const _SendMessageToAgentDialog({
    required this.initialMessage,
    this.title = 'send_message_to_agent_title',
    this.messageLabel = 'send_message_to_agent_message_label',
    this.header,
  });

  final String initialMessage;
  final String title;
  final String messageLabel;
  final Widget? header;

  @override
  State<_SendMessageToAgentDialog> createState() =>
      _SendMessageToAgentDialogState();
}

class _SendMessageToAgentDialogState extends State<_SendMessageToAgentDialog> {
  final AgentService _agentService = Get.find<AgentService>();
  final ImService _imService = Get.find<ImService>();
  final SessionService _sessionService = Get.find<SessionService>();
  final _AgentMessageRecipientStore _recipientStore =
      _AgentMessageRecipientStore();

  late final TextEditingController _messageController;
  ContactAgentPickResult? _selectedAgent;
  int _selectionRevision = 0;
  bool _isSubmitting = false;

  @override
  void initState() {
    super.initState();
    _messageController = TextEditingController(text: widget.initialMessage);
    unawaited(_restoreCachedAgent());
  }

  @override
  void dispose() {
    _messageController.dispose();
    super.dispose();
  }

  String get _selectedAgentTitle {
    final selectedAgent = _selectedAgent;
    if (selectedAgent == null) {
      return 'send_message_to_agent_select_agent'.tr;
    }
    final displayName = selectedAgent.displayName.trim();
    return displayName.isNotEmpty ? displayName : selectedAgent.id;
  }

  bool get _canSubmit =>
      _selectedAgent != null &&
      _messageController.text.trim().isNotEmpty &&
      !_isSubmitting;

  Future<void> _restoreCachedAgent() async {
    final revisionAtStart = _selectionRevision;
    final cachedAgentId = await _recipientStore.loadAgentId();
    if (cachedAgentId != null) {
      await _agentService.loadAgents();
    }
    if (!mounted || _selectionRevision != revisionAtStart) {
      return;
    }

    AgentModel? cachedAgent;
    if (cachedAgentId != null) {
      for (final agent in _agentService.allAccessibleAgents) {
        if (agent.id.trim() == cachedAgentId) {
          cachedAgent = agent;
          break;
        }
      }
    }

    final resolvedAgent = cachedAgent;
    if (resolvedAgent != null) {
      final displayName = resolvedAgent.agentName.trim();
      setState(() {
        _selectedAgent = ContactAgentPickResult(
          id: resolvedAgent.id,
          displayName: displayName.isNotEmpty ? displayName : resolvedAgent.id,
          avatarUrl: resolvedAgent.avatarUrl,
        );
      });
    } else if (cachedAgentId != null && _agentService.hasLoaded.value) {
      unawaited(_recipientStore.clearIfMatches(cachedAgentId));
    }
  }

  Future<void> _pickAgent() async {
    final picked = await showContactAgentPickerSheet(context, agentsOnly: true);
    if (!mounted || picked == null) {
      return;
    }
    setState(() {
      _selectedAgent = picked;
      _selectionRevision += 1;
    });
    unawaited(_recipientStore.saveAgentId(picked.id));
  }

  Future<void> _submit() async {
    final selectedAgent = _selectedAgent;
    final message = _messageController.text.trim();
    if (selectedAgent == null || message.isEmpty || _isSubmitting) {
      return;
    }

    setState(() {
      _isSubmitting = true;
    });

    final agentId = selectedAgent.id.trim();
    final agentTitle = _selectedAgentTitle;
    late final String sessionId;
    try {
      final createdSessionId = (await _sessionService.createSession(
        agentId,
        2,
      ))?.trim();
      if (createdSessionId == null || createdSessionId.isEmpty) {
        throw StateError('Agent session creation returned an empty ID');
      }
      sessionId = createdSessionId;
      await _imService.bindSessionDisplayTitle(
        sessionId,
        agentTitle,
        type: 'private',
        peerId: agentId,
        peerType: 2,
      );
      await _imService.sendMessage(message, sessionId);
    } catch (error, stackTrace) {
      debugPrint('Failed to send message to agent: $error\n$stackTrace');
      if (!mounted) {
        return;
      }
      setState(() {
        _isSubmitting = false;
      });
      CustomToast.show('send_message_to_agent_send_failed'.tr, isError: true);
      return;
    }

    if (!mounted) {
      return;
    }
    Navigator.of(context).pop(sessionId);
    unawaited(
      ChatRouteNavigator.toChat(
        sessionId: sessionId,
        title: agentTitle,
        type: 'private',
      ),
    );
    unawaited(_imService.refreshSessionsWindowNow());
  }

  void _cancel() {
    if (_isSubmitting) {
      return;
    }
    Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      titlePadding: const EdgeInsets.fromLTRB(24, 20, 8, 0),
      title: Row(
        children: [
          Expanded(child: Text(widget.title.tr)),
          IconButton(
            key: const Key('send_message_to_agent_cancel_button'),
            tooltip: 'common_cancel'.tr,
            visualDensity: VisualDensity.compact,
            onPressed: _isSubmitting ? null : _cancel,
            icon: const Icon(Icons.close),
          ),
        ],
      ),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 500),
        child: SingleChildScrollView(
          padding: const EdgeInsets.only(bottom: 2),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (widget.header != null) ...[
                widget.header!,
                const SizedBox(height: 12),
              ],
              OutlinedButton.icon(
                key: const Key('send_message_to_agent_picker_button'),
                style: OutlinedButton.styleFrom(
                  backgroundColor: theme.colorScheme.surface,
                  foregroundColor: theme.colorScheme.onSurface,
                ),
                onPressed: _isSubmitting ? null : _pickAgent,
                icon: _selectedAgent == null
                    ? const Icon(Icons.smart_toy_outlined, size: 18)
                    : SessionAvatar(
                        isGroup: false,
                        avatarTitle: _selectedAgentTitle,
                        avatarColor: AppTheme.getAvatarColor(
                          _selectedAgent!.id,
                        ),
                        avatarUrl: _selectedAgent!.avatarUrl,
                        size: 28,
                        borderRadius: AppTheme.listAvatarCornerRadius(28),
                      ),
                label: Text(_selectedAgentTitle),
              ),
              const SizedBox(height: 12),
              TextField(
                key: const Key('send_message_to_agent_text_field'),
                controller: _messageController,
                maxLines: 12,
                minLines: 7,
                onChanged: (_) => setState(() {}),
                decoration: const InputDecoration(
                  border: OutlineInputBorder(),
                ).copyWith(labelText: widget.messageLabel.tr),
              ),
            ],
          ),
        ),
      ),
      actions: [
        ElevatedButton(
          key: const Key('send_message_to_agent_send_button'),
          onPressed: _canSubmit ? _submit : null,
          child: _isSubmitting
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : Text('send_message_to_agent_send'.tr),
        ),
      ],
    );
  }
}
