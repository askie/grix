import 'dart:collection';

import 'package:get/get.dart';

import '../markdown/chat_markdown_code_language.dart';
import '../markdown/chat_markdown_normalizer.dart';
import 'chat_bind_directory_message.dart';
import 'chat_message_content.dart';

class ChatMessagePreview {
  static const ChatMarkdownNormalizer _normalizer = ChatMarkdownNormalizer();
  static final RegExp _jsonCandidatePattern = RegExp(r'^\s*[\[{]');
  static final RegExp _standaloneGrixCardPattern = RegExp(
    r'^\s*\[.*\]\(grix://card/[^)\s]+\)\s*$',
    dotAll: true,
  );
  static final RegExp _fencedCodePattern = RegExp(
    r'```([^\n`]*)\n?([\s\S]*?)```',
    multiLine: true,
  );
  static final RegExp _htmlImagePattern = RegExp(
    r'''<img\b[^>]*?(?:alt\s*=\s*["']?([^"'>]*)["']?)?[^>]*>''',
    caseSensitive: false,
  );
  static final RegExp _markdownImagePattern = RegExp(r'!\[([^\]]*)\]\([^)]+\)');
  static final RegExp _markdownLinkPattern = RegExp(r'\[([^\]]+)\]\([^)]+\)');
  static final RegExp _autoLinkPattern = RegExp(
    r'<((?:https?|mailto):[^>\s]+)>',
    caseSensitive: false,
  );
  static final RegExp _htmlTagPattern = RegExp(r'<[^>]+>');
  static final RegExp _tableSeparatorPattern = RegExp(
    r'^\s*\|?(?:\s*:?-{3,}:?\s*\|)+\s*:?-{3,}:?\s*\|?\s*$',
    multiLine: true,
  );
  static final RegExp _headingPrefixPattern = RegExp(
    r'^\s{0,3}#{1,6}\s+',
    multiLine: true,
  );
  static final RegExp _blockquotePrefixPattern = RegExp(
    r'^\s*>\s?',
    multiLine: true,
  );
  static final RegExp _listPrefixPattern = RegExp(
    r'^\s*(?:[-+*]|\d+\.)\s+',
    multiLine: true,
  );
  static final RegExp _taskListPrefixPattern = RegExp(
    r'^\s*[-+*]\s+\[[ xX]\]\s+',
    multiLine: true,
  );
  static final RegExp _inlineCodePattern = RegExp(r'`([^`]+)`');
  static final RegExp _formatMarkerPattern = RegExp(r'(\*\*|__|~~|\*|_)');
  // 标题专用：仅清掉成对强调标记 `**`/`~~`，保留全部下划线与单个 `*`，
  // 避免误删 snake_case / __dunder__ 之类合法标识符里的下划线。
  static final RegExp _titleFormatMarkerPattern = RegExp(r'(\*\*|~~)');

  // 会话列表每行预览都会调用本类，且一次重排/刷新会对所有会话重算，而 _summarize
  // 内部要跑整套 markdown 归一化（上千行）+ 十几个正则，是真实的主线程热点。
  // 两层保护，专治"返回会话列表卡死/ANR"：
  //  1) 有界 LRU 缓存：相同原文只算一次，避免每次列表重建重复重算。
  //  2) 超大输入截断（见 _summarize）：预览本就截断展示，只处理前若干字符，
  //     杜绝单条巨型消息在归一化/正则阶段触发灾难性回溯把主线程顶死。
  static const int _kMemoCapacity = 512;
  static final LinkedHashMap<String, String> _memo =
      LinkedHashMap<String, String>();

  static bool isStandaloneCardMessage(String content) {
    final trimmed = content.trim();
    if (trimmed.isEmpty) return false;
    return _standaloneGrixCardPattern.hasMatch(trimmed);
  }

  static String summarize(String raw, {bool unwrapStructuredText = true}) =>
      _memoized(
        raw,
        forTitle: false,
        unwrapStructuredText: unwrapStructuredText,
      );

  static ({String text, int updatedAt}) selectSessionPreview({
    required String lastMessage,
    required int lastMessageTime,
    String streamingText = '',
    int streamingUpdatedAt = 0,
  }) {
    if (streamingText.trim().isNotEmpty &&
        (lastMessage.trim().isEmpty ||
            streamingUpdatedAt >= lastMessageTime)) {
      return (text: streamingText, updatedAt: streamingUpdatedAt);
    }
    return (text: lastMessage, updatedAt: lastMessageTime);
  }

  /// 标题专用摘要：与 [summarize] 共用同一套结构清洗（图片/代码块/链接/表格/标题前缀等），
  /// 但保留全部下划线，仅清掉成对的 `**`/`~~` 强调标记。
  /// 用于会话标题显示，避免把 snake_case 之类的合法下划线误删。
  static String summarizeTitle(String raw) =>
      _memoized(raw, forTitle: true, unwrapStructuredText: true);

  static String _memoized(
    String raw, {
    required bool forTitle,
    required bool unwrapStructuredText,
  }) {
    // 占位符（[图片]/[代码] 等）走 i18n，随语言变化，故 locale 计入缓存键，
    // 避免切换语言后命中旧语言的占位符。
    final localeTag = Get.locale?.toString() ?? '';
    final key =
        '$localeTag\u0000${forTitle ? 'T' : 'S'}\u0000${unwrapStructuredText ? 'U' : 'R'}\u0000$raw';
    final cached = _memo.remove(key);
    if (cached != null) {
      _memo[key] = cached; // 命中后移到队尾，维持 LRU 顺序
      return cached;
    }
    final result = _summarize(
      raw,
      forTitle: forTitle,
      unwrapStructuredText: unwrapStructuredText,
    );
    _memo[key] = result;
    if (_memo.length > _kMemoCapacity) {
      _memo.remove(_memo.keys.first); // 超出容量淘汰最久未用项
    }
    return result;
  }

  // 预览只需开头一段即可得到可读摘要，超过此长度的原文在重型清洗前先截断，
  // 把最坏情况的正则回溯开销限定在有界范围内（可见摘要取自开头，不影响展示）。
  static const int _kMaxProcessChars = 4000;

  static String _summarize(
    String raw, {
    required bool forTitle,
    required bool unwrapStructuredText,
  }) {
    final trimmedFull = raw.trim();
    if (trimmedFull.isEmpty) return '';
    // 目录绑定指令消息：预览/标题都用友好短文案，不展示原始 grix:// 链接。
    final bindDirectory = ChatBindDirectoryMessage.friendlyShortText(
      trimmedFull,
    );
    if (bindDirectory.isNotEmpty) {
      return bindDirectory;
    }
    if (_standaloneGrixCardPattern.hasMatch(trimmedFull)) {
      return '';
    }
    final trimmed = trimmedFull.length > _kMaxProcessChars
        ? trimmedFull.substring(0, _kMaxProcessChars)
        : trimmedFull;

    var normalized = unwrapStructuredText
        ? ChatMessageContent.unwrapStructuredText(trimmed)
        : trimmed;
    normalized = _normalizer.normalizeForRendering(normalized).text;
    normalized = normalized.replaceAllMapped(
      _fencedCodePattern,
      (match) => _summarizeFencedCodeBlock(match),
    );
    normalized = normalized.replaceAllMapped(
      _htmlImagePattern,
      (match) => _resolveImageLabel(match.group(1)),
    );
    normalized = normalized.replaceAllMapped(
      _markdownImagePattern,
      (match) => _resolveImageLabel(match.group(1)),
    );
    normalized = normalized.replaceAllMapped(
      _markdownLinkPattern,
      (match) => match.group(1)?.trim() ?? '',
    );
    normalized = normalized.replaceAllMapped(
      _autoLinkPattern,
      (match) => match.group(1)?.trim() ?? '',
    );
    normalized = normalized.replaceAll(_tableSeparatorPattern, '');
    normalized = _normalizeMarkdownTableCells(normalized);
    normalized = normalized.replaceAll(_headingPrefixPattern, '');
    normalized = normalized.replaceAll(_blockquotePrefixPattern, '');
    normalized = normalized.replaceAll(_taskListPrefixPattern, '');
    normalized = normalized.replaceAll(_listPrefixPattern, '');
    normalized = normalized.replaceAllMapped(
      _inlineCodePattern,
      (match) => match.group(1)?.trim() ?? '',
    );
    normalized = normalized.replaceAll(
      forTitle ? _titleFormatMarkerPattern : _formatMarkerPattern,
      '',
    );
    normalized = normalized.replaceAll(_htmlTagPattern, ' ');
    normalized = _collapseWhitespace(normalized);

    if (normalized.isNotEmpty) {
      return normalized;
    }
    return _fallbackSummary(trimmed);
  }

  static String truncate(String raw, {required int maxRunes}) {
    final summary = summarize(raw);
    if (summary.isEmpty || maxRunes <= 0) {
      return summary;
    }
    final runes = summary.runes.toList(growable: false);
    if (runes.length <= maxRunes) {
      return summary;
    }
    return String.fromCharCodes(runes.take(maxRunes));
  }

  static String _summarizeFencedCodeBlock(Match match) {
    final language = normalizeCodeFenceLanguage(match.group(1));
    final content = _collapseWhitespace(match.group(2) ?? '');

    if (language == 'mermaid') {
      return ' ${_diagramPlaceholder()} ';
    }

    if (content.isEmpty) {
      return '';
    }

    if (_jsonCandidatePattern.hasMatch(content)) {
      final extracted = ChatMessageContent.unwrapStructuredText(content);
      if (extracted != content) {
        return ' $extracted ';
      }
    }

    if (_isPreviewFriendlyCodeLanguage(language)) {
      return ' $content ';
    }

    if (!content.contains('\n') && content.runes.length <= 48) {
      return ' $content ';
    }

    return ' ${_codePlaceholder()} ';
  }

  static bool _isPreviewFriendlyCodeLanguage(String language) {
    return language.isEmpty ||
        language == 'markdown' ||
        language == 'md' ||
        language == 'text' ||
        language == 'plain' ||
        language == 'plaintext';
  }

  static String _normalizeMarkdownTableCells(String input) {
    final lines = input.split('\n');
    for (var i = 0; i < lines.length; i += 1) {
      final line = lines[i];
      if (!_looksLikeMarkdownTableLine(line)) {
        continue;
      }
      lines[i] = line.replaceAll('|', ' ');
    }
    return lines.join('\n');
  }

  static bool _looksLikeMarkdownTableLine(String line) {
    final trimmed = line.trim();
    if (trimmed.isEmpty || !trimmed.contains('|')) {
      return false;
    }
    if (_tableSeparatorPattern.hasMatch(trimmed)) {
      return true;
    }
    if (trimmed.startsWith('|') || trimmed.endsWith('|')) {
      return true;
    }
    return '|'.allMatches(trimmed).length >= 2;
  }

  static String _resolveImageLabel(String? rawAlt) {
    final alt = (rawAlt ?? '').trim();
    if (alt.isEmpty) {
      return _imagePlaceholder();
    }
    final lower = alt.toLowerCase();
    if (lower == 'image' || lower == 'img' || lower == 'photo') {
      return _imagePlaceholder();
    }
    return alt;
  }

  static String _fallbackSummary(String raw) {
    if (_markdownImagePattern.hasMatch(raw) ||
        _htmlImagePattern.hasMatch(raw)) {
      return _imagePlaceholder();
    }
    if (raw.toLowerCase().contains('```mermaid')) {
      return _diagramPlaceholder();
    }
    if (raw.contains('```')) {
      return _codePlaceholder();
    }
    return _collapseWhitespace(raw);
  }

  static String _collapseWhitespace(String input) {
    return input
        .split(RegExp(r'\s+'))
        .where((part) => part.trim().isNotEmpty)
        .join(' ')
        .trim();
  }

  static String _imagePlaceholder() {
    final translated = 'chat_preview_image'.tr;
    if (translated.isEmpty || translated == 'chat_preview_image') {
      return '[image]';
    }
    return translated;
  }

  static String _diagramPlaceholder() {
    final translated = 'chat_preview_diagram'.tr;
    if (translated.isEmpty || translated == 'chat_preview_diagram') {
      return '[diagram]';
    }
    return translated;
  }

  static String _codePlaceholder() {
    final translated = 'chat_preview_code'.tr;
    if (translated.isEmpty || translated == 'chat_preview_code') {
      return '[code]';
    }
    return translated;
  }
}
