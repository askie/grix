import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../services/chat_recent_bind_directory_store.dart';

/// 空白 agent 聊天页的快捷绑定目录组件。
///
/// 展示最近绑定过的目录（MRU），点击直接发起绑定；
/// 底部提供"选择目录"按钮走远程目录选择器。
/// 绑定消息发出后会话不再是空态，组件随之消失。
class ChatQuickBindDirectoryPanel extends StatefulWidget {
  const ChatQuickBindDirectoryPanel({
    super.key,
    required this.entriesLoader,
    required this.onBindDirectory,
    required this.onPickDirectory,
    this.fontScale = 1.0,
    this.revealDelay = Duration.zero,
  });

  /// 加载最近绑定目录列表。
  final Future<List<RecentBindDirectoryEntry>> Function() entriesLoader;

  /// 发起绑定；返回是否成功（失败提示由调用方负责）。
  final Future<bool> Function(String path) onBindDirectory;

  /// 打开远程目录选择器；返回选中的目录路径，取消时返回 null。
  final Future<String?> Function() onPickDirectory;

  final double fontScale;

  /// 揭示延迟：进入会话后等这段时间再显示组件。
  /// 刚进会话时历史消息尚未加载，空态会被临时判为真；若消息在此延迟内到达，
  /// 空态整体消失、本组件随之被移除，从而避免"闪一下又不见"。默认零延迟。
  final Duration revealDelay;

  @override
  State<ChatQuickBindDirectoryPanel> createState() =>
      _ChatQuickBindDirectoryPanelState();
}

class _ChatQuickBindDirectoryPanelState
    extends State<ChatQuickBindDirectoryPanel> {
  List<RecentBindDirectoryEntry> _entries = const [];
  bool _loading = true;
  bool _submitting = false;
  bool _revealed = false;
  String _busyPath = '';
  Timer? _revealTimer;

  @override
  void initState() {
    super.initState();
    _loadEntries();
    if (widget.revealDelay <= Duration.zero) {
      _revealed = true;
    } else {
      _revealTimer = Timer(widget.revealDelay, () {
        if (!mounted) return;
        setState(() => _revealed = true);
      });
    }
  }

  @override
  void dispose() {
    _revealTimer?.cancel();
    super.dispose();
  }

  Future<void> _loadEntries() async {
    List<RecentBindDirectoryEntry> entries = const [];
    try {
      entries = await widget.entriesLoader();
    } catch (_) {
      // 历史目录加载失败按无历史处理，仍保留选择目录入口。
    }
    if (!mounted) return;
    setState(() {
      _entries = entries;
      _loading = false;
    });
  }

  Future<void> _bind(String path) async {
    if (_submitting) return;
    setState(() {
      _submitting = true;
      _busyPath = path;
    });
    try {
      await widget.onBindDirectory(path);
    } finally {
      if (mounted) {
        setState(() {
          _submitting = false;
          _busyPath = '';
        });
      }
    }
  }

  Future<void> _pickAndBind() async {
    if (_submitting) return;
    String? path;
    try {
      path = await widget.onPickDirectory();
    } catch (_) {
      // 选择器异常视同取消。
    }
    final normalized = path?.trim() ?? '';
    if (normalized.isEmpty) return;
    await _bind(normalized);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final fontScale = widget.fontScale;
    if (_loading || !_revealed) {
      return const SizedBox.shrink();
    }
    return Container(
      key: const Key('chat_quick_bind_directory_panel'),
      constraints: const BoxConstraints(maxWidth: 360),
      margin: const EdgeInsets.symmetric(horizontal: 24),
      padding: const EdgeInsets.fromLTRB(16, 14, 16, 12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(14),
        border: Border.all(
          color: theme.colorScheme.outlineVariant.withValues(alpha: 0.5),
        ),
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Icon(
                Icons.folder_open_rounded,
                size: 18 * fontScale,
                color: theme.primaryColor,
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  'chat_quick_bind_title'.tr,
                  style: TextStyle(
                    fontSize: 14 * fontScale,
                    fontWeight: FontWeight.w600,
                    color: theme.colorScheme.onSurface,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            'chat_quick_bind_subtitle'.tr,
            style: TextStyle(
              fontSize: 12 * fontScale,
              color: theme.colorScheme.onSurface.withValues(alpha: 0.55),
            ),
          ),
          if (_entries.isNotEmpty) ...[
            const SizedBox(height: 10),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 240),
              child: ListView.separated(
                shrinkWrap: true,
                padding: EdgeInsets.zero,
                itemCount: _entries.length,
                separatorBuilder: (_, _) => const SizedBox(height: 4),
                itemBuilder: (context, index) =>
                    _buildEntryTile(theme, _entries[index]),
              ),
            ),
          ],
          const SizedBox(height: 10),
          _buildPickButton(theme),
        ],
      ),
    );
  }

  Widget _buildEntryTile(ThemeData theme, RecentBindDirectoryEntry entry) {
    final fontScale = widget.fontScale;
    final busy = _submitting && _busyPath == entry.path;
    return Material(
      color: theme.colorScheme.surfaceContainerHighest.withValues(alpha: 0.4),
      borderRadius: BorderRadius.circular(10),
      child: InkWell(
        key: Key('chat_quick_bind_entry_${entry.path}'),
        borderRadius: BorderRadius.circular(10),
        onTap: _submitting ? null : () => _bind(entry.path),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          child: Row(
            children: [
              Icon(
                Icons.folder_rounded,
                size: 18 * fontScale,
                color: theme.primaryColor.withValues(alpha: 0.75),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      entry.displayName,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 13 * fontScale,
                        fontWeight: FontWeight.w500,
                        color: theme.colorScheme.onSurface,
                      ),
                    ),
                    Text(
                      entry.path,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 11 * fontScale,
                        color: theme.colorScheme.onSurface.withValues(
                          alpha: 0.45,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
              if (busy) ...[
                const SizedBox(width: 8),
                SizedBox(
                  width: 14,
                  height: 14,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: theme.primaryColor,
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildPickButton(ThemeData theme) {
    final fontScale = widget.fontScale;
    return OutlinedButton.icon(
      key: const Key('chat_quick_bind_pick_button'),
      onPressed: _submitting ? null : _pickAndBind,
      style: OutlinedButton.styleFrom(
        minimumSize: const Size(0, 40),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(10),
        ),
      ),
      icon: Icon(Icons.create_new_folder_outlined, size: 18 * fontScale),
      label: Text(
        'chat_message_card_agent_open_session_browse'.tr,
        style: TextStyle(fontSize: 13 * fontScale),
      ),
    );
  }
}
