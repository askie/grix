import 'package:flutter/material.dart';

import '../../../../app/themes/app_theme.dart';
import '../models/chat_call_owner_card_data.dart';

/// 呼叫主人卡片视图。
/// - 主人打开会话且卡片新鲜（90 秒内）时，自动拉起语音大脑通话一次。
/// - 同时提供"接听"按钮作为手动兜底。
class ChatCallOwnerCardView extends StatefulWidget {
  const ChatCallOwnerCardView({
    super.key,
    required this.card,
    required this.isMine,
    required this.fontScale,
    this.onAccept,
  });

  final ChatCallOwnerCardData card;
  final bool isMine;
  final double fontScale;
  final VoidCallback? onAccept;

  @override
  State<ChatCallOwnerCardView> createState() => _ChatCallOwnerCardViewState();
}

class _ChatCallOwnerCardViewState extends State<ChatCallOwnerCardView> {
  /// 自动拉起的新鲜度窗口。
  static const Duration _freshWindow = Duration(seconds: 90);

  /// 跨重建/滚动去重：同一次呼叫（session+ts）只自动拉起一次。
  static final Set<String> _autoStarted = <String>{};

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _maybeAutoStart());
  }

  void _maybeAutoStart() {
    if (!mounted) return;
    if (widget.isMine || widget.onAccept == null) return;
    final ts = widget.card.ts;
    if (ts <= 0) return;
    final elapsed = DateTime.now().millisecondsSinceEpoch - ts;
    if (elapsed < 0 || elapsed >= _freshWindow.inMilliseconds) return;
    final key = '${widget.card.sessionId}:$ts';
    if (_autoStarted.contains(key)) return;
    _autoStarted.add(key);
    widget.onAccept!.call();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final accent = theme.colorScheme.primary;
    final agentName = widget.card.displayAgentName;

    final titleStyle = AppTheme.applyTextFont(
      theme.textTheme.bodyMedium?.copyWith(
            fontSize: 13 * widget.fontScale,
            fontWeight: FontWeight.w600,
            color: theme.colorScheme.onSurface,
            height: 1.4,
          ) ??
          TextStyle(
            fontSize: 13 * widget.fontScale,
            fontWeight: FontWeight.w600,
            color: theme.colorScheme.onSurface,
          ),
    );

    return Container(
      key: const Key('chat_message_card_call_owner'),
      constraints: const BoxConstraints(minWidth: 220, maxWidth: 360),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: accent.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: accent.withValues(alpha: 0.18)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          Container(
            width: 34,
            height: 34,
            decoration: BoxDecoration(
              color: accent.withValues(alpha: 0.14),
              borderRadius: BorderRadius.circular(10),
            ),
            child: Icon(Icons.call_rounded, size: 18, color: accent),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              agentName.isEmpty ? '请求与你语音通话' : '$agentName 请求与你语音通话',
              style: titleStyle,
            ),
          ),
          const SizedBox(width: 10),
          if (widget.onAccept != null)
            FilledButton(
              key: const Key('chat_message_card_call_owner_accept'),
              onPressed: widget.onAccept,
              style: FilledButton.styleFrom(
                backgroundColor: accent,
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                minimumSize: const Size(0, 0),
                tapTargetSize: MaterialTapTargetSize.shrinkWrap,
              ),
              child: Text('接听', style: TextStyle(fontSize: 13 * widget.fontScale)),
            ),
        ],
      ),
    );
  }
}
