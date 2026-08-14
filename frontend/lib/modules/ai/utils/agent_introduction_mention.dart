/// Agent 介绍里的「艾特联系人」工具：编辑态显示昵称，保存时写成 id。
///
/// 与聊天输入框艾特逻辑对齐：
/// - 插入：`@displayName`
/// - 保存：把 `@displayName` 归一成 `@id`
/// - 回显：把已知 `@id` 还原成 `@displayName`
class AgentIntroductionMention {
  const AgentIntroductionMention({
    required this.id,
    required this.displayName,
  });

  final String id;
  final String displayName;
}

bool isIntroductionMentionBoundary(int codeUnit) {
  switch (codeUnit) {
    case 9:
    case 10:
    case 13:
    case 32:
    case 33:
    case 34:
    case 39:
    case 41:
    case 44:
    case 46:
    case 58:
    case 59:
    case 63:
    case 12289:
    case 65281:
    case 65292:
    case 65306:
    case 65311:
      return true;
    default:
      return false;
  }
}

String buildIntroductionMentionInsertText({
  required String prefix,
  required String suffix,
  required String displayName,
}) {
  final mentionToken = '@${displayName.trim()}';
  final needsLeadingSpace =
      prefix.isNotEmpty &&
      !isIntroductionMentionBoundary(prefix.codeUnitAt(prefix.length - 1));
  final needsTrailingSpace =
      suffix.isEmpty || !isIntroductionMentionBoundary(suffix.codeUnitAt(0));
  final builder = StringBuffer();
  if (needsLeadingSpace) {
    builder.write(' ');
  }
  builder.write(mentionToken);
  if (needsTrailingSpace) {
    builder.write(' ');
  }
  return builder.toString();
}

bool containsIntroductionMentionToken(String content, String displayName) {
  final normalizedContent = content;
  final needle = '@${displayName.trim()}';
  if (normalizedContent.isEmpty || needle == '@') {
    return false;
  }

  var searchFrom = 0;
  while (true) {
    final index = normalizedContent.indexOf(needle, searchFrom);
    if (index == -1) {
      return false;
    }
    final end = index + needle.length;
    if (end == normalizedContent.length ||
        isIntroductionMentionBoundary(normalizedContent.codeUnitAt(end))) {
      return true;
    }
    searchFrom = index + needle.length;
  }
}

String normalizeIntroductionMentions(
  String content,
  Iterable<AgentIntroductionMention> mentions,
) {
  if (content.isEmpty || !content.contains('@')) {
    return content;
  }
  final sorted = List<AgentIntroductionMention>.from(mentions)
    ..sort((a, b) => b.displayName.length.compareTo(a.displayName.length));
  var result = content;
  for (final mention in sorted) {
    final id = mention.id.trim();
    final displayName = mention.displayName.trim();
    if (id.isEmpty || displayName.isEmpty) {
      continue;
    }
    result = _replaceMentionToken(result, displayName, id);
  }
  return result;
}

/// 把介绍里已知的 `@id` 还原成 `@displayName`，并返回编辑态文本与 pending 列表。
({String text, List<AgentIntroductionMention> mentions})
hydrateIntroductionMentions(
  String content,
  Map<String, String> idToDisplayName,
) {
  if (content.isEmpty || idToDisplayName.isEmpty || !content.contains('@')) {
    return (text: content, mentions: const <AgentIntroductionMention>[]);
  }

  // 按 id 长度倒序，避免短 id 误伤长 id 前缀。
  final entries = idToDisplayName.entries
      .where((e) => e.key.trim().isNotEmpty && e.value.trim().isNotEmpty)
      .toList(growable: false)
    ..sort((a, b) => b.key.length.compareTo(a.key.length));

  var result = content;
  final mentions = <AgentIntroductionMention>[];
  final seen = <String>{};

  for (final entry in entries) {
    final id = entry.key.trim();
    final displayName = entry.value.trim();
    if (displayName == id) {
      continue;
    }
    final before = result;
    result = _replaceMentionToken(result, id, displayName);
    if (result != before && seen.add('$id::$displayName')) {
      mentions.add(
        AgentIntroductionMention(id: id, displayName: displayName),
      );
    }
  }

  return (text: result, mentions: mentions);
}

String _replaceMentionToken(
  String content,
  String fromDisplay,
  String toToken,
) {
  final needle = '@$fromDisplay';
  final result = StringBuffer();
  var i = 0;
  while (i < content.length) {
    final index = content.indexOf(needle, i);
    if (index == -1) {
      result.write(content.substring(i));
      break;
    }
    final end = index + needle.length;
    if (end == content.length ||
        isIntroductionMentionBoundary(content.codeUnitAt(end))) {
      result.write(content.substring(i, index));
      result.write('@$toToken');
      i = end;
    } else {
      result.write(content.substring(i, index + 1));
      i = index + 1;
    }
  }
  return result.toString();
}
