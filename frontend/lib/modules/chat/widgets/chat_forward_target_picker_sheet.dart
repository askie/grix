import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../app/themes/app_theme.dart';
import '../../../shared/widgets/session_avatar.dart';
import '../models/chat_forward_target_option.dart';

class ChatForwardTargetPickerSheet extends StatefulWidget {
  const ChatForwardTargetPickerSheet({
    super.key,
    required this.options,
    this.onSendToAgent,
  });

  final List<ChatForwardTargetOption> options;

  /// 点右上角 "+" 时触发：先关闭本 sheet（结果为 null），再由调用方
  /// 打开"选择 Agent 并发送一段话"的对话框。
  final VoidCallback? onSendToAgent;

  static Future<ChatForwardTargetOption?> show(
    BuildContext context, {
    required List<ChatForwardTargetOption> options,
    VoidCallback? onSendToAgent,
  }) {
    return showModalBottomSheet<ChatForwardTargetOption>(
      context: context,
      isScrollControlled: true,
      constraints: const BoxConstraints(maxWidth: 420),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => ChatForwardTargetPickerSheet(
        options: options,
        onSendToAgent: onSendToAgent,
      ),
    );
  }

  @override
  State<ChatForwardTargetPickerSheet> createState() =>
      _ChatForwardTargetPickerSheetState();
}

class _ChatForwardTargetPickerSheetState
    extends State<ChatForwardTargetPickerSheet> {
  final TextEditingController _searchController = TextEditingController();
  String _searchQuery = '';
  String _selectedSessionId = '';

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final filteredOptions = _resolveFilteredOptionsByQuery(_searchQuery);
    final hasVisibleSelection = filteredOptions.any(
      (option) => option.sessionId == _selectedSessionId,
    );

    return SizedBox(
      height: MediaQuery.of(context).size.height * 0.78,
      child: Column(
        children: [
          Container(
            width: 36,
            height: 4,
            margin: const EdgeInsets.only(top: 10, bottom: 12),
            decoration: BoxDecoration(
              color: theme.colorScheme.outline.withValues(alpha: 0.3),
              borderRadius: BorderRadius.circular(2),
            ),
          ),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
            child: Row(
              children: [
                const Icon(Icons.forward_to_inbox_rounded, size: 18),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    'chat_forward_pick_target'.tr,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
                if (widget.onSendToAgent != null)
                  IconButton(
                    visualDensity: VisualDensity.compact,
                    tooltip: 'chat_forward_send_to_agent'.tr,
                    icon: const Icon(Icons.add_rounded),
                    onPressed: () {
                      final callback = widget.onSendToAgent!;
                      Navigator.of(context).pop();
                      callback();
                    },
                  ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 8, 12, 8),
            child: TextField(
              controller: _searchController,
              onChanged: (value) {
                setState(() {
                  _searchQuery = value;
                  final nextFiltered = _resolveFilteredOptionsByQuery(
                    _searchQuery,
                  );
                  if (!nextFiltered.any(
                    (option) => option.sessionId == _selectedSessionId,
                  )) {
                    _selectedSessionId = '';
                  }
                });
              },
              decoration: InputDecoration(
                isDense: true,
                prefixIcon: const Icon(Icons.search_rounded),
                hintText: 'conversations_search'.tr,
              ),
            ),
          ),
          Expanded(
            child: filteredOptions.isEmpty
                ? Center(
                    child: Text(
                      'conversations_no_match'.tr,
                      style: TextStyle(
                        color: theme.colorScheme.secondary.withValues(
                          alpha: 0.7,
                        ),
                      ),
                    ),
                  )
                : ListView.builder(
                    itemCount: filteredOptions.length,
                    itemBuilder: (context, index) {
                      final option = filteredOptions[index];
                      final selected = option.sessionId == _selectedSessionId;
                      final subtitle = option.subtitle.trim();
                      final avatarColor = AppTheme.getAvatarColor(
                        option.avatarColorSeed,
                      );
                      return ListTile(
                        leading: SessionAvatar(
                          isGroup: option.isGroup,
                          avatarTitle: option.title,
                          avatarColor: avatarColor,
                          avatarUrl: option.avatarUrl,
                          members: option.members,
                          size: 42,
                          borderRadius: 8,
                        ),
                        title: Text(
                          option.title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                        subtitle: subtitle.isEmpty
                            ? null
                            : Text(
                                subtitle,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                              ),
                        trailing: Icon(
                          selected
                              ? Icons.check_circle_rounded
                              : Icons.radio_button_unchecked_rounded,
                          color: selected
                              ? theme.primaryColor
                              : theme.colorScheme.outline.withValues(
                                  alpha: 0.6,
                                ),
                        ),
                        selected: selected,
                        onTap: () {
                          setState(() {
                            _selectedSessionId = option.sessionId;
                          });
                        },
                      );
                    },
                  ),
          ),
          Padding(
            padding: EdgeInsets.fromLTRB(
              12,
              8,
              12,
              10 + MediaQuery.of(context).padding.bottom,
            ),
            child: SizedBox(
              width: double.infinity,
              child: FilledButton(
                onPressed: !hasVisibleSelection
                    ? null
                    : () {
                        final option = filteredOptions.firstWhere(
                          (item) => item.sessionId == _selectedSessionId,
                        );
                        Navigator.of(context).pop(option);
                      },
                child: Text('common_confirm'.tr),
              ),
            ),
          ),
        ],
      ),
    );
  }

  List<ChatForwardTargetOption> _resolveFilteredOptionsByQuery(
    String rawQuery,
  ) {
    final query = rawQuery.trim().toLowerCase();
    if (query.isEmpty) {
      return widget.options;
    }

    return widget.options
        .where((option) {
          final title = option.title.toLowerCase();
          final subtitle = option.subtitle.toLowerCase();
          return title.contains(query) || subtitle.contains(query);
        })
        .toList(growable: false);
  }
}
