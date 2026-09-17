import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/im_service.dart';
import '../../data/providers/session_service.dart';
import '../../shared/utils/toast_util.dart';
import '../../app/themes/app_theme.dart';
import '../../modules/home/widgets/session_avatar_view.dart';
import '../ai/widgets/contact_agent_picker_sheet.dart';
import '../chat/services/chat_route_navigator.dart';
import 'models/share_inbox_manifest.dart';
import 'services/share_ingest_native_bridge.dart';
import 'services/share_outbound_sender.dart';
import 'services/share_target_session_resolver.dart';

class ShareIngestPage extends StatefulWidget {
  const ShareIngestPage({super.key, required this.manifest});

  final ShareInboxManifest manifest;

  @override
  State<ShareIngestPage> createState() => _ShareIngestPageState();
}

class _ShareIngestPageState extends State<ShareIngestPage> {
  final ShareTargetSessionResolver _sessionResolver =
      ShareTargetSessionResolver();
  final ShareOutboundSender _sender = ShareOutboundSender();
  final TextEditingController _noteController = TextEditingController();

  bool _isSending = false;
  bool _sendInFlight = false;

  bool get _hasSendableItems => widget.manifest.items.isNotEmpty;

  @override
  void dispose() {
    _noteController.dispose();
    super.dispose();
  }

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      final skipped = widget.manifest.skippedCount;
      if (skipped > 0) {
        CustomToast.show(
          'share_ingest_skipped_files'.trParams({'count': '$skipped'}),
          isError: true,
        );
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final sessions = _sessionResolver.recentAgentSessions();
    final skipped = widget.manifest.skippedCount;
    return PopScope(
      onPopInvokedWithResult: (didPop, result) {
        if (!didPop) {
          return;
        }
        // Dismissing the page (back/close, not a successful send) must still
        // clear the inbox entry, or it reappears on every app launch. This
        // runs on every pop, including after a successful send, but
        // deleteEntry is idempotent there.
        unawaited(ShareIngestNativeBridge.deleteEntry(widget.manifest.id));
      },
      child: Scaffold(
      appBar: AppBar(
        title: Text('share_ingest_title'.tr),
      ),
      body: ListView(
        padding: EdgeInsets.fromLTRB(
          16,
          16,
          16,
          16 + MediaQuery.viewInsetsOf(context).bottom,
        ),
        keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
        children: [
          if (skipped > 0)
            Card(
              color: Theme.of(context).colorScheme.errorContainer,
              margin: const EdgeInsets.only(bottom: 12),
              child: Padding(
                padding: const EdgeInsets.all(12),
                child: Text(
                  'share_ingest_skipped_files'.trParams({
                    'count': '$skipped',
                  }),
                  style: TextStyle(
                    color: Theme.of(context).colorScheme.onErrorContainer,
                  ),
                ),
              ),
            ),
          Text(
            'share_ingest_preview_heading'.tr,
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 12),
          if (!_hasSendableItems) ...[
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Text('share_ingest_nothing_to_send'.tr),
            ),
            if (widget.manifest.offeredTypes.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: SelectableText(
                  widget.manifest.offeredTypes.join('\n'),
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
          ],
          ...widget.manifest.items.map(_buildPreviewTile),
          if (_hasSendableItems) ...[
            const SizedBox(height: 20),
            Text(
              'share_ingest_note_label'.tr,
              style: Theme.of(context).textTheme.titleSmall,
            ),
            const SizedBox(height: 8),
            TextField(
              controller: _noteController,
              enabled: !_isSending,
              maxLines: 4,
              minLines: 2,
              maxLength: ShareOutboundSender.maxShareTextCharacters,
              decoration: InputDecoration(
                hintText: 'share_ingest_note_hint'.tr,
                border: const OutlineInputBorder(),
                counterText: '',
              ),
            ),
            const SizedBox(height: 24),
            Text(
              'share_ingest_target_heading'.tr,
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const SizedBox(height: 8),
            _NewAgentSessionTile(
              enabled: !_isSending,
              onTap: _startNewAgentSession,
            ),
            if (sessions.isNotEmpty) ...[
              const SizedBox(height: 8),
              const Divider(height: 1),
              ...sessions.map((session) {
                final title = _sessionResolver.displayTitle(session);
                return Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    ListTile(
                      leading: SessionAvatarView(
                        session: session,
                        avatarTitle: title,
                        avatarColor: AppTheme.getAvatarColor(session.sessionId),
                        size: 44,
                        borderRadius: 10,
                      ),
                      title: Text(title),
                      subtitle: Text('share_ingest_recent_agent_session'.tr),
                      enabled: !_isSending,
                      onTap: () =>
                          _sendToExistingSession(session.sessionId, title),
                    ),
                    const Divider(height: 1),
                  ],
                );
              }),
            ],
          ],
        ],
      ),
    ),
    );
  }

  Widget _buildPreviewTile(ShareInboxItem item) {
    if (item.isText || item.isUrl) {
      final text = item.text ?? '';
      final preview = text.length > 240 ? '${text.substring(0, 240)}…' : text;
      return Card(
        margin: const EdgeInsets.only(bottom: 8),
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Text(preview, style: const TextStyle(fontSize: 14)),
        ),
      );
    }
    final name = item.fileName ?? item.path ?? 'file';
    final sizeLabel = item.size != null ? _formatSize(item.size!) : '';
    Widget leading = const Icon(Icons.insert_drive_file_outlined);
    final path = item.absolutePath;
    if (!kIsWeb && path != null && _isImageItem(item)) {
      final file = File(path);
      if (file.existsSync()) {
        leading = ClipRRect(
          borderRadius: BorderRadius.circular(6),
          child: Image.file(file, width: 48, height: 48, fit: BoxFit.cover),
        );
      }
    }
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        leading: leading,
        title: Text(name),
        subtitle: sizeLabel.isEmpty ? null : Text(sizeLabel),
      ),
    );
  }

  bool _isImageItem(ShareInboxItem item) {
    final mime = item.mime?.toLowerCase() ?? '';
    if (mime.startsWith('image/')) return true;
    final name = (item.fileName ?? '').toLowerCase();
    return name.endsWith('.png') ||
        name.endsWith('.jpg') ||
        name.endsWith('.jpeg') ||
        name.endsWith('.gif') ||
        name.endsWith('.webp');
  }

  String _formatSize(int bytes) {
    if (bytes < 1024) return '$bytes B';
    if (bytes < 1024 * 1024) {
      return '${(bytes / 1024).toStringAsFixed(1)} KB';
    }
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  }

  Future<void> _startNewAgentSession() async {
    if (_isSending || _sendInFlight) return;
    final picked = await showContactAgentPickerSheet(context, agentsOnly: true);
    if (!mounted || picked == null) return;

    _sendInFlight = true;
    setState(() => _isSending = true);
    try {
      final sessionService = Get.find<SessionService>();
      final imService = Get.find<ImService>();
      final agentId = picked.id.trim();
      final agentTitle = picked.displayName.trim().isNotEmpty
          ? picked.displayName.trim()
          : agentId;
      final sessionId = (await sessionService.createSession(agentId, 2))?.trim();
      if (sessionId == null || sessionId.isEmpty) {
        throw StateError('empty session');
      }
      await imService.bindSessionDisplayTitle(
        sessionId,
        agentTitle,
        type: 'private',
        peerId: agentId,
        peerType: 2,
      );
      await _sender.sendManifestToSession(
        manifest: widget.manifest,
        sessionId: sessionId,
        caption: _noteController.text,
      );
      await ShareIngestNativeBridge.deleteEntry(widget.manifest.id);
      if (!mounted) return;
      Navigator.of(context).pop();
      await ChatRouteNavigator.toChat(
        sessionId: sessionId,
        title: agentTitle,
        type: 'private',
      );
      unawaited(imService.refreshSessionsWindowNow());
    } on ShareOutboundSendFailure catch (error) {
      if (mounted) {
        CustomToast.show(error.messageKey.tr, isError: true);
      }
    } catch (error, stackTrace) {
      debugPrint('share ingest new session failed: $error\n$stackTrace');
      if (mounted) {
        CustomToast.show('share_ingest_send_failed'.tr, isError: true);
      }
    } finally {
      _sendInFlight = false;
      if (mounted) {
        setState(() => _isSending = false);
      }
    }
  }

  Future<void> _sendToExistingSession(String sessionId, String title) async {
    if (_isSending || _sendInFlight) return;
    _sendInFlight = true;
    setState(() => _isSending = true);
    try {
      await _sender.sendManifestToSession(
        manifest: widget.manifest,
        sessionId: sessionId,
        caption: _noteController.text,
      );
      await ShareIngestNativeBridge.deleteEntry(widget.manifest.id);
      if (!mounted) return;
      Navigator.of(context).pop();
      await ChatRouteNavigator.toChat(
        sessionId: sessionId,
        title: title,
        type: 'private',
      );
    } on ShareOutboundSendFailure catch (error) {
      if (mounted) {
        CustomToast.show(error.messageKey.tr, isError: true);
      }
    } catch (error, stackTrace) {
      debugPrint('share ingest send failed: $error\n$stackTrace');
      if (mounted) {
        CustomToast.show('share_ingest_send_failed'.tr, isError: true);
      }
    } finally {
      _sendInFlight = false;
      if (mounted) {
        setState(() => _isSending = false);
      }
    }
  }
}

class _NewAgentSessionTile extends StatelessWidget {
  const _NewAgentSessionTile({required this.enabled, required this.onTap});

  final bool enabled;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Card(
      color: Theme.of(
        context,
      ).colorScheme.primaryContainer.withValues(alpha: 0.35),
      child: ListTile(
        leading: const Icon(Icons.add_comment_outlined),
        title: Text('share_ingest_new_agent_session'.tr),
        enabled: enabled,
        onTap: enabled ? onTap : null,
      ),
    );
  }
}
