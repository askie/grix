import 'dart:convert';

// ═══════════════════════════════════════════════════════════════════════════════
// CARD PROTOCOL — FRONTEND PARSING CONTRACT
// ═══════════════════════════════════════════════════════════════════════════════
//
// The card format is a backend-owned protocol. The backend generates:
//   content = "[fallback_text](grix://card/{type}?d={json_payload})"
//   extra   = { "biz_card": { "type": ..., "payload": ... }, ... }
//
// Frontend rules (DO NOT VIOLATE):
//
//   1. Parse strictly — only well-formed grix://card URIs produce cards.
//      If parsing fails, display raw text. Do NOT add fallback heuristics.
//
//   2. No compatibility layers — if the backend sends malformed card data,
//      fix the backend. Do NOT patch the parser to accommodate broken input.
//
//   3. No workarounds for markdown conflicts — the backend must ensure
//      fallback text is safe for the markdown pipeline (no bare $, no
//      unbalanced brackets). Fix belongs in:
//        backend/internal/agentadapter/approvalcards/normalize.go
//        backend/internal/ws/agentapi/local_action_handler.go
//
// If you are tempted to add a regex workaround, a try-catch that silently
// recovers, or a heuristic fallback — stop. Fix the backend instead.
// ═══════════════════════════════════════════════════════════════════════════════

import 'package:get/get.dart';

import '../models/chat_agent_pairing_card_data.dart';
import '../models/chat_agent_open_session_card_data.dart';
import '../models/chat_agent_question_card_data.dart';
import '../models/chat_agent_status_card_data.dart';
import '../models/chat_call_owner_card_data.dart';
import '../models/chat_exec_approval_card_data.dart';
import '../models/chat_egg_install_status_card_data.dart';
import '../models/chat_exec_status_card_data.dart';
import '../models/chat_conversation_card_data.dart';
import '../models/chat_message_card_data.dart';
import '../models/chat_message_card_type.dart';
import '../models/chat_progress_card_data.dart';
import '../models/chat_tool_execution_card_data.dart';
import '../models/chat_tool_execution_group_card_data.dart';
import '../models/chat_user_profile_card_data.dart';
import '../models/chat_thinking_card_data.dart';

class ChatMessageCardEnvelope {
  const ChatMessageCardEnvelope({
    required this.content,
    required this.extra,
    required this.card,
  });

  final String content;
  final Map<String, dynamic> extra;
  final ChatMessageCardData card;
}

class _ParsedGrixCardUri {
  const _ParsedGrixCardUri({required this.type, required this.params});

  final String type;
  final Map<String, String> params;
}

class ChatMessageCardCodec {
  const ChatMessageCardCodec._();

  static const String _grixCardUriPrefix = 'grix://card/';
  static int _debugDecodeFromMessageCount = 0;
  static final RegExp _standaloneGrixCardMarkdownLinkPattern = RegExp(
    r'^\s*\[(.*)\]\((grix://card/[^)\s]+)\)\s*$',
    dotAll: true,
  );
  static final RegExp _execApprovalResolutionDirectivePattern = RegExp(
    r'^\[\[exec-approval-resolution\|(.+)\]\]$',
    dotAll: true,
  );
  static ChatMessageCardEnvelope buildUserProfileCard({
    required String userId,
    required String nickname,
    required String avatarUrl,
    int peerType = 1,
  }) {
    final normalizedUserId = userId.trim();
    final normalizedNickname = nickname.trim();
    if (normalizedUserId.isEmpty) {
      throw ArgumentError.value(userId, 'userId', 'must not be empty');
    }
    if (normalizedNickname.isEmpty) {
      throw ArgumentError.value(nickname, 'nickname', 'must not be empty');
    }
    final normalizedPeerType = _normalizeUserProfilePeerType(peerType);
    if (normalizedPeerType == null) {
      throw ArgumentError.value(peerType, 'peerType', 'must be 1 or 2');
    }
    final card = ChatUserProfileCardData(
      userId: normalizedUserId,
      nickname: normalizedNickname,
      avatarUrl: avatarUrl.trim(),
      peerType: normalizedPeerType,
    );
    return encode(card);
  }

  static ChatMessageCardEnvelope buildConversationCard({
    required String sessionId,
    required String sessionType,
    required String title,
    String peerId = '',
    String peerNickname = '',
    String avatarUrl = '',
  }) {
    final normalizedSessionId = sessionId.trim();
    final normalizedSessionType = sessionType.trim();
    final normalizedTitle = title.trim();
    if (normalizedSessionId.isEmpty) {
      throw ArgumentError.value(sessionId, 'sessionId', 'must not be empty');
    }
    if (normalizedTitle.isEmpty) {
      throw ArgumentError.value(title, 'title', 'must not be empty');
    }
    if (normalizedSessionType != 'group' &&
        normalizedSessionType != 'private') {
      throw ArgumentError.value(
        sessionType,
        'sessionType',
        'must be group or private',
      );
    }
    final card = ChatConversationCardData(
      sessionId: normalizedSessionId,
      sessionType: normalizedSessionType,
      title: normalizedTitle,
      peerId: peerId.trim(),
      peerNickname: peerNickname.trim(),
      avatarUrl: avatarUrl.trim(),
    );
    return encode(card);
  }

  static ChatMessageCardEnvelope buildExecApprovalCard({
    required String approvalId,
    required String approvalSlug,
    String approvalCommandId = '',
    required String command,
    required String host,
    List<String> allowedDecisions = const <String>[
      'allow-once',
      'allow-always',
      'deny',
    ],
    String nodeId = '',
    String cwd = '',
    String warningText = '',
    int? expiresInSeconds,
    int? expiresAtMs,
  }) {
    final normalizedApprovalId = approvalId.trim();
    final normalizedApprovalSlug = approvalSlug.trim();
    final normalizedApprovalCommandId = approvalCommandId.trim();
    final normalizedCommand = command.trim();
    final normalizedHost = host.trim();
    if (normalizedApprovalId.isEmpty) {
      throw ArgumentError.value(approvalId, 'approvalId', 'must not be empty');
    }
    if (normalizedApprovalSlug.isEmpty) {
      throw ArgumentError.value(
        approvalSlug,
        'approvalSlug',
        'must not be empty',
      );
    }
    if (normalizedCommand.isEmpty) {
      throw ArgumentError.value(command, 'command', 'must not be empty');
    }
    if (normalizedHost.isEmpty) {
      throw ArgumentError.value(host, 'host', 'must not be empty');
    }
    final normalizedAllowedDecisions = allowedDecisions
        .map((value) => value.trim())
        .where((value) => _isExecApprovalDecision(value))
        .toSet()
        .toList();
    if (normalizedAllowedDecisions.isEmpty) {
      throw ArgumentError.value(
        allowedDecisions,
        'allowedDecisions',
        'must include at least one valid decision',
      );
    }
    final card = ChatExecApprovalCardData(
      approvalId: normalizedApprovalId,
      approvalSlug: normalizedApprovalSlug,
      approvalCommandId: normalizedApprovalCommandId,
      command: normalizedCommand,
      host: normalizedHost,
      nodeId: nodeId.trim(),
      cwd: cwd.trim(),
      warningText: warningText.trim(),
      expiresInSeconds: expiresInSeconds,
      expiresAtMs:
          expiresAtMs ??
          (expiresInSeconds != null
              ? DateTime.now().millisecondsSinceEpoch + expiresInSeconds * 1000
              : null),
      allowedDecisions: normalizedAllowedDecisions,
    );
    return encode(card);
  }

  static ChatMessageCardEnvelope buildExecStatusCard({
    required String status,
    required String summary,
    String detailText = '',
    String approvalId = '',
    String approvalCommandId = '',
    String host = '',
    String nodeId = '',
    String sessionId = '',
    String reason = '',
    String decision = '',
    String resolvedById = '',
    String command = '',
    String exitLabel = '',
    String channelLabel = '',
    String warningText = '',
  }) {
    final normalizedStatus = status.trim();
    final normalizedSummary = summary.trim();
    if (!_isExecStatusValue(normalizedStatus)) {
      throw ArgumentError.value(
        status,
        'status',
        'must be a supported exec status',
      );
    }
    if (normalizedSummary.isEmpty) {
      throw ArgumentError.value(summary, 'summary', 'must not be empty');
    }
    final card = ChatExecStatusCardData(
      status: normalizedStatus,
      summary: normalizedSummary,
      detailText: detailText.trim(),
      approvalId: approvalId.trim(),
      approvalCommandId: approvalCommandId.trim(),
      host: host.trim(),
      nodeId: nodeId.trim(),
      sessionId: sessionId.trim(),
      reason: reason.trim(),
      decision: decision.trim(),
      resolvedById: resolvedById.trim(),
      command: command.trim(),
      exitLabel: exitLabel.trim(),
      channelLabel: channelLabel.trim(),
      warningText: warningText.trim(),
    );
    return encode(card);
  }

  static ChatMessageCardEnvelope buildAgentOpenSessionCard({
    String cardInstanceId = '',
    String summaryText = '',
    String detailText = '',
    String initialCwd = '',
    String submittedPath = '',
  }) {
    final card = ChatAgentOpenSessionCardData(
      cardInstanceId: cardInstanceId.trim(),
      summaryText: summaryText.trim(),
      detailText: detailText.trim(),
      initialCwd: initialCwd.trim(),
      submittedPath: submittedPath.trim(),
    );
    return encode(card);
  }

  static ChatMessageCardEnvelope encode(ChatMessageCardData card) {
    final fallbackContent = _buildFallbackContent(card);
    final uri = _buildGrixCardURI(card.type, card.toPayload());
    return ChatMessageCardEnvelope(
      content: '[$fallbackContent]($uri)',
      extra: const <String, dynamic>{},
      card: card,
    );
  }

  /// [CARD PROTOCOL] Entry point 1 of 4 — detects a standalone card message.
  /// Parses strictly. Returns null for any non-compliant content → plain text.
  /// Do NOT add fallback logic here. If cards fail to parse, fix the backend.
  static ChatMessageCardData? decodeFromMessage({required String content}) {
    assert(() {
      _debugDecodeFromMessageCount++;
      return true;
    }());
    return _decodeStandaloneGrixCardMessage(content);
  }

  static void debugResetDecodeFromMessageCount() {
    _debugDecodeFromMessageCount = 0;
  }

  static int get debugDecodeFromMessageCount => _debugDecodeFromMessageCount;

  /// Decodes a grix://card/{type}?params URI into a card data model.
  /// Returns null if the URI is not a valid grix card URI.
  /// [CARD PROTOCOL] Entry point 2 of 4 — decodes a grix://card URI.
  /// Returns null for any malformed URI. No fallback, no retry.
  /// If the URI cannot be decoded, the backend sent bad data — fix it there.
  static ChatMessageCardData? decodeGrixUriCard(String href) {
    final trimmed = href.trim();
    final parsed = _parseGrixCardUri(trimmed);
    if (parsed == null) {
      return null;
    }
    Map<String, dynamic> payload;
    final cardType = _decodeType(parsed.type);
    if (cardType == null) {
      return null;
    }

    // If "d" parameter present, decode nested JSON.
    if (parsed.params.containsKey('d')) {
      final d = parsed.params['d']!;
      try {
        final decoded = jsonDecode(d);
        if (decoded is! Map) {
          return null;
        }
        payload = Map<String, dynamic>.from(decoded);
      } catch (_) {
        return null;
      }
    } else {
      // Flat query params become the payload.
      payload = Map<String, dynamic>.from(parsed.params);
      payload = _normalizeFlatUriPayload(cardType, payload);
    }

    return _decodeCardPayload(cardType, payload);
  }

  static ChatMessageCardData? _decodeCardPayload(
    ChatMessageCardType cardType,
    Map<String, dynamic> payload,
  ) {
    switch (cardType) {
      case ChatMessageCardType.userProfile:
        return _decodeUserProfileCard(payload);
      case ChatMessageCardType.conversation:
        return _decodeConversationCard(payload);
      case ChatMessageCardType.execApproval:
        return _decodeExecApprovalCard(payload);
      case ChatMessageCardType.execStatus:
        return _decodeExecStatusCard(payload);
      case ChatMessageCardType.toolExecution:
        return _decodeToolExecutionCard(payload);
      case ChatMessageCardType.toolExecutionGroup:
        return _decodeToolExecutionGroupCard(payload);
      case ChatMessageCardType.eggInstallStatus:
        return _decodeEggInstallStatusCard(payload);
      case ChatMessageCardType.agentStatus:
        return _decodeAgentStatusCard(payload);
      case ChatMessageCardType.agentQuestion:
        return _decodeAgentQuestionCard(payload);
      case ChatMessageCardType.agentPairing:
        return _decodeAgentPairingCard(payload);
      case ChatMessageCardType.agentOpenSession:
        return _decodeAgentOpenSessionCard(payload);
      case ChatMessageCardType.callOwner:
        return _decodeCallOwnerCard(payload);
      case ChatMessageCardType.thinking:
        return _decodeThinkingCard(payload);
      case ChatMessageCardType.progress:
        return _decodeProgressCard(payload);
    }
  }

  static ChatProgressCardData? _decodeProgressCard(
    Map<String, dynamic> payload,
  ) {
    final label = payload['label']?.toString().trim() ?? '';
    if (label.isEmpty) {
      return null;
    }
    final percent = payload.containsKey('percent')
        ? _readInt(payload['percent'])
        : null;
    return ChatProgressCardData(label: label, percent: percent);
  }

  static ChatCallOwnerCardData _decodeCallOwnerCard(
    Map<String, dynamic> payload,
  ) {
    final agentName = payload['agent_name']?.toString().trim() ?? '';
    final sessionId = payload['session_id']?.toString().trim() ?? '';
    final rawTs = payload['ts'];
    final ts = rawTs is int
        ? rawTs
        : int.tryParse(rawTs?.toString() ?? '') ?? 0;
    return ChatCallOwnerCardData(
      agentName: agentName,
      sessionId: sessionId,
      ts: ts,
    );
  }

  static _ParsedGrixCardUri? _parseGrixCardUri(String href) {
    if (href.isEmpty || href.length <= _grixCardUriPrefix.length) {
      return null;
    }
    if (!href.toLowerCase().startsWith(_grixCardUriPrefix)) {
      return null;
    }
    final bodyWithQuery = href
        .substring(_grixCardUriPrefix.length)
        .split('#')
        .first;
    if (bodyWithQuery.isEmpty) {
      return null;
    }
    final queryDelimiter = bodyWithQuery.indexOf('?');
    final rawType =
        (queryDelimiter >= 0
                ? bodyWithQuery.substring(0, queryDelimiter)
                : bodyWithQuery)
            .trim();
    if (rawType.isEmpty) {
      return null;
    }
    final type = _decodeQueryComponentLenient(rawType).trim();
    if (type.isEmpty) {
      return null;
    }
    final rawQuery = queryDelimiter >= 0
        ? bodyWithQuery.substring(queryDelimiter + 1)
        : '';
    return _ParsedGrixCardUri(
      type: type,
      params: _parseGrixCardQueryParameters(rawQuery),
    );
  }

  static Map<String, String> _parseGrixCardQueryParameters(String rawQuery) {
    if (rawQuery.isEmpty) {
      return const <String, String>{};
    }
    final params = <String, String>{};
    final normalizedQuery = rawQuery.replaceAll('&amp;', '&');
    for (final segment in normalizedQuery.split('&')) {
      if (segment.isEmpty) {
        continue;
      }
      final separator = segment.indexOf('=');
      final rawKey = separator >= 0 ? segment.substring(0, separator) : segment;
      if (rawKey.isEmpty) {
        continue;
      }
      final key = _decodeQueryComponentLenient(rawKey).trim();
      if (key.isEmpty || params.containsKey(key)) {
        continue;
      }
      final rawValue = separator >= 0 ? segment.substring(separator + 1) : '';
      params[key] = _decodeQueryComponentLenient(rawValue);
    }
    return params;
  }

  static String _decodeQueryComponentLenient(String raw) {
    if (raw.isEmpty) {
      return '';
    }
    try {
      return Uri.decodeQueryComponent(raw);
    } catch (_) {
      return _decodePercentEncodedLenient(raw);
    }
  }

  static String _decodePercentEncodedLenient(String raw) {
    final normalized = raw.replaceAll('+', ' ');
    if (normalized.isEmpty || !normalized.contains('%')) {
      return normalized;
    }
    final output = StringBuffer();
    final bytes = <int>[];

    void flush() {
      if (bytes.isEmpty) {
        return;
      }
      output.write(utf8.decode(bytes, allowMalformed: true));
      bytes.clear();
    }

    var index = 0;
    while (index < normalized.length) {
      final codeUnit = normalized.codeUnitAt(index);
      if (codeUnit == 0x25 && index + 2 < normalized.length) {
        final high = _parseHexDigit(normalized.codeUnitAt(index + 1));
        final low = _parseHexDigit(normalized.codeUnitAt(index + 2));
        if (high >= 0 && low >= 0) {
          bytes.add((high << 4) + low);
          index += 3;
          continue;
        }
      }
      flush();
      output.write(normalized[index]);
      index += 1;
    }
    flush();
    return output.toString();
  }

  static int _parseHexDigit(int codeUnit) {
    if (codeUnit >= 0x30 && codeUnit <= 0x39) {
      return codeUnit - 0x30;
    }
    if (codeUnit >= 0x41 && codeUnit <= 0x46) {
      return codeUnit - 0x41 + 10;
    }
    if (codeUnit >= 0x61 && codeUnit <= 0x66) {
      return codeUnit - 0x61 + 10;
    }
    return -1;
  }

  static final RegExp _execApprovalSlashCommandPattern = RegExp(
    r'^/(approve|grix\s+approval)\s+\S+',
    caseSensitive: false,
  );

  static bool isInternalDirectiveMessage(String content) {
    final normalized = content.trim();
    if (normalized.isEmpty) {
      return false;
    }
    if (_execApprovalResolutionDirectivePattern.hasMatch(normalized)) {
      return true;
    }
    if (_execApprovalSlashCommandPattern.hasMatch(normalized)) {
      return true;
    }
    if (_isOpenSessionDirective(normalized)) {
      return true;
    }
    if (!normalized.startsWith('grix://card/')) {
      return false;
    }
    if (decodeGrixUriCard(normalized) != null) {
      return false;
    }
    return _parseGrixCardUri(normalized) != null;
  }

  static bool _isOpenSessionDirective(String content) {
    if (!content.startsWith('grix://open/')) {
      return false;
    }
    final uri = Uri.tryParse(content);
    if (uri == null || uri.scheme != 'grix' || uri.host != 'open') {
      return false;
    }
    final segments = uri.pathSegments
        .where((segment) => segment.isNotEmpty)
        .toList(growable: false);
    return segments.length == 1 && segments.first == 'session';
  }

  static ChatUserProfileCardData? _decodeUserProfileCard(
    Map<String, dynamic> payload,
  ) {
    final userId = payload['user_id']?.toString().trim() ?? '';
    final nickname = payload['nickname']?.toString().trim() ?? '';
    final avatarUrl = payload['avatar_url']?.toString().trim() ?? '';
    final peerType = _normalizeUserProfilePeerType(payload['peer_type']);
    if (payload.containsKey('peer_type') && peerType == null) {
      return null;
    }
    if (userId.isEmpty || nickname.isEmpty) {
      return null;
    }
    return ChatUserProfileCardData(
      userId: userId,
      nickname: nickname,
      avatarUrl: avatarUrl,
      peerType: peerType ?? 1,
    );
  }

  static ChatConversationCardData? _decodeConversationCard(
    Map<String, dynamic> payload,
  ) {
    final sessionId = payload['session_id']?.toString().trim() ?? '';
    final sessionType = payload['session_type']?.toString().trim() ?? '';
    final title = payload['title']?.toString().trim() ?? '';
    final peerId = payload['peer_id']?.toString().trim() ?? '';
    final peerNickname = payload['peer_nickname']?.toString().trim() ?? '';
    final avatarUrl = payload['avatar_url']?.toString().trim() ?? '';
    if (sessionId.isEmpty || title.isEmpty) {
      return null;
    }
    if (sessionType != 'group' && sessionType != 'private') {
      return null;
    }
    return ChatConversationCardData(
      sessionId: sessionId,
      sessionType: sessionType,
      title: title,
      peerId: peerId,
      peerNickname: peerNickname,
      avatarUrl: avatarUrl,
    );
  }

  static ChatExecApprovalCardData? _decodeExecApprovalCard(
    Map<String, dynamic> payload,
  ) {
    final approvalId = payload['approval_id']?.toString().trim() ?? '';
    final approvalSlug = payload['approval_slug']?.toString().trim() ?? '';
    final approvalCommandId =
        payload['approval_command_id']?.toString().trim() ?? '';
    final command = payload['command']?.toString().trim() ?? '';
    final host = payload['host']?.toString().trim() ?? '';
    final nodeId = payload['node_id']?.toString().trim() ?? '';
    final cwd = payload['cwd']?.toString().trim() ?? '';
    final warningText = payload['warning_text']?.toString().trim() ?? '';
    final expiresInSeconds = _readInt(payload['expires_in_seconds']);
    final expiresAtMs = _readInt(payload['expires_at_ms']);
    final rawAllowedDecisions = payload['allowed_decisions'];
    final rawDecisionCommands = payload['decision_commands'];
    if (approvalId.isEmpty ||
        approvalSlug.isEmpty ||
        command.isEmpty ||
        host.isEmpty) {
      return null;
    }
    if (rawAllowedDecisions is! List) {
      return null;
    }
    final allowedDecisions = rawAllowedDecisions
        .map((value) => value?.toString().trim() ?? '')
        .where(_isExecApprovalDecision)
        .toSet()
        .toList();
    if (allowedDecisions.isEmpty) {
      return null;
    }
    final decisionCommands = <String, String>{};
    if (rawDecisionCommands is Map) {
      for (final entry in rawDecisionCommands.entries) {
        final key = entry.key?.toString().trim() ?? '';
        final value = entry.value?.toString().trim() ?? '';
        if (!_isExecApprovalDecision(key) || value.isEmpty) {
          continue;
        }
        decisionCommands[key] = value;
      }
    }
    return ChatExecApprovalCardData(
      approvalId: approvalId,
      approvalSlug: approvalSlug,
      approvalCommandId: approvalCommandId,
      command: command,
      host: host,
      nodeId: nodeId,
      cwd: cwd,
      warningText: warningText,
      expiresInSeconds: expiresInSeconds,
      expiresAtMs: expiresAtMs,
      allowedDecisions: allowedDecisions,
      decisionCommands: decisionCommands,
    );
  }

  static ChatExecStatusCardData? _decodeExecStatusCard(
    Map<String, dynamic> payload,
  ) {
    final status = payload['status']?.toString().trim() ?? '';
    final summary = payload['summary']?.toString().trim() ?? '';
    final detailText = payload['detail_text']?.toString().trim() ?? '';
    final approvalId = payload['approval_id']?.toString().trim() ?? '';
    final approvalCommandId =
        payload['approval_command_id']?.toString().trim() ?? '';
    final host = payload['host']?.toString().trim() ?? '';
    final nodeId = payload['node_id']?.toString().trim() ?? '';
    final sessionId = payload['session_id']?.toString().trim() ?? '';
    final reason = payload['reason']?.toString().trim() ?? '';
    final decision = payload['decision']?.toString().trim() ?? '';
    final resolvedById = payload['resolved_by_id']?.toString().trim() ?? '';
    final command = payload['command']?.toString().trim() ?? '';
    final exitLabel = payload['exit_label']?.toString().trim() ?? '';
    final channelLabel = payload['channel_label']?.toString().trim() ?? '';
    final warningText = payload['warning_text']?.toString().trim() ?? '';
    if (!_isExecStatusValue(status) || summary.isEmpty) {
      return null;
    }
    return ChatExecStatusCardData(
      status: status,
      summary: summary,
      detailText: detailText,
      approvalId: approvalId,
      approvalCommandId: approvalCommandId,
      host: host,
      nodeId: nodeId,
      sessionId: sessionId,
      reason: reason,
      decision: decision,
      resolvedById: resolvedById,
      command: command,
      exitLabel: exitLabel,
      channelLabel: channelLabel,
      warningText: warningText,
    );
  }

  static ChatToolExecutionCardData? _decodeToolExecutionCard(
    Map<String, dynamic> payload,
  ) {
    final summaryText = payload['summary_text']?.toString().trim() ?? '';
    final detailText = payload['detail_text']?.toString().trim() ?? '';
    if (summaryText.isEmpty) {
      return null;
    }
    return ChatToolExecutionCardData(
      summaryText: summaryText,
      detailText: detailText,
    );
  }

  static ChatToolExecutionGroupCardData? _decodeToolExecutionGroupCard(
    Map<String, dynamic> payload,
  ) {
    final rawChildren = payload['children'];
    if (rawChildren is! List || rawChildren.isEmpty) return null;
    final children = <ChatToolExecutionCardData>[];
    for (final raw in rawChildren) {
      if (raw is! Map) continue;
      final child = _decodeToolExecutionCard(Map<String, dynamic>.from(raw));
      if (child != null) children.add(child);
    }
    if (children.isEmpty) return null;
    return ChatToolExecutionGroupCardData(
      children: children,
      displayCard: children.last,
    );
  }

  static ChatThinkingCardData? _decodeThinkingCard(
    Map<String, dynamic> payload,
  ) {
    final content = payload['content']?.toString().trim() ?? '';
    if (content.isEmpty) {
      return null;
    }
    return ChatThinkingCardData(content: content);
  }

  static ChatEggInstallStatusCardData? _decodeEggInstallStatusCard(
    Map<String, dynamic> payload,
  ) {
    final installId = payload['install_id']?.toString().trim() ?? '';
    final status = payload['status']?.toString().trim() ?? '';
    final summary = payload['summary']?.toString().trim() ?? '';
    final step = payload['step']?.toString().trim() ?? '';
    final detailText = payload['detail_text']?.toString().trim() ?? '';
    final targetAgentId = payload['target_agent_id']?.toString().trim() ?? '';
    final errorCode = payload['error_code']?.toString().trim() ?? '';
    final errorMsg = payload['error_msg']?.toString().trim() ?? '';
    if (installId.isEmpty ||
        !_isEggInstallStatusValue(status) ||
        summary.isEmpty) {
      return null;
    }
    return ChatEggInstallStatusCardData(
      installId: installId,
      status: status,
      summary: summary,
      step: step,
      detailText: detailText,
      targetAgentId: targetAgentId,
      errorCode: errorCode,
      errorMsg: errorMsg,
    );
  }

  static ChatAgentQuestionCardData? _decodeAgentQuestionCard(
    Map<String, dynamic> payload,
  ) {
    final requestId = payload['request_id']?.toString().trim() ?? '';
    final mode = payload['mode']?.toString().trim().toLowerCase() ?? 'form';
    final message = payload['message']?.toString().trim() ?? '';
    final url = payload['url']?.toString().trim() ?? '';
    final openUrlLabel = payload['open_url_label']?.toString().trim() ?? '';
    final footerText = payload['footer_text']?.toString().trim() ?? '';
    final submittedAnswer =
        payload['submitted_answer']?.toString().trim() ?? '';
    final submittedAcceptText =
        payload['submitted_accept_text']?.toString().trim() ?? '';
    final submittedCancelText =
        payload['submitted_cancel_text']?.toString().trim() ?? '';
    final expiresAtMs = _readInt(payload['expires_at']) ?? 0;
    final rawQuestions = payload['questions'];
    if (requestId.isEmpty || rawQuestions is! List) {
      return null;
    }
    final questions = <ChatAgentQuestionPrompt>[];
    for (final rawQuestion in rawQuestions) {
      if (rawQuestion is! Map) {
        continue;
      }
      final question = Map<String, dynamic>.from(rawQuestion);
      final index = _readInt(question['index']) ?? (questions.length + 1);
      final header = question['header']?.toString().trim() ?? '';
      final fieldKey = question['field_key']?.toString().trim() ?? '';
      final prompt = question['prompt']?.toString().trim() ?? '';
      final rawOptions = question['options'];
      final options = rawOptions is List
          ? rawOptions
                .map((value) => value?.toString().trim() ?? '')
                .where((value) => value.isNotEmpty)
                .toList(growable: false)
          : const <String>[];
      final multiSelect = question['multi_select'] == true;
      if (header.isEmpty || prompt.isEmpty) {
        continue;
      }
      questions.add(
        ChatAgentQuestionPrompt(
          index: index,
          header: header,
          prompt: prompt,
          fieldKey: fieldKey,
          options: options,
          multiSelect: multiSelect,
        ),
      );
    }
    if (mode == 'url') {
      if (url.isEmpty) {
        return null;
      }
      return ChatAgentQuestionCardData(
        requestId: requestId,
        questions: const <ChatAgentQuestionPrompt>[],
        mode: mode,
        message: message,
        url: url,
        openUrlLabel: openUrlLabel,
        footerText: footerText,
        submittedAnswer: submittedAnswer,
        submittedAcceptText: submittedAcceptText,
        submittedCancelText: submittedCancelText,
        expiresAtMs: expiresAtMs,
      );
    }
    if (questions.isEmpty) {
      return null;
    }
    return ChatAgentQuestionCardData(
      requestId: requestId,
      questions: questions,
      mode: mode,
      message: message,
      url: url,
      openUrlLabel: openUrlLabel,
      footerText: footerText,
      submittedAnswer: submittedAnswer,
      submittedAcceptText: submittedAcceptText,
      submittedCancelText: submittedCancelText,
      expiresAtMs: expiresAtMs,
    );
  }

  static ChatAgentPairingCardData? _decodeAgentPairingCard(
    Map<String, dynamic> payload,
  ) {
    final pairingCode = payload['pairing_code']?.toString().trim() ?? '';
    final instructionText =
        payload['instruction_text']?.toString().trim() ?? '';
    final commandHint = payload['command_hint']?.toString().trim() ?? '';
    if (pairingCode.isEmpty) {
      return null;
    }
    return ChatAgentPairingCardData(
      pairingCode: pairingCode,
      instructionText: instructionText,
      commandHint: commandHint.isEmpty
          ? '/grix access pair <code>'
          : commandHint,
    );
  }

  static ChatMessageCardData? _decodeStandaloneGrixCardMessage(
    String rawContent,
  ) {
    final normalized = rawContent.trim();
    if (normalized.isEmpty) {
      return null;
    }

    if (normalized.startsWith('grix://card/')) {
      return decodeGrixUriCard(normalized);
    }

    final match = _standaloneGrixCardMarkdownLinkPattern.firstMatch(normalized);
    if (match == null) {
      return null;
    }

    final href = match.group(2)?.trim() ?? '';
    if (href.isEmpty) {
      return null;
    }
    return decodeGrixUriCard(href);
  }

  static Map<String, dynamic> _normalizeFlatUriPayload(
    ChatMessageCardType type,
    Map<String, dynamic> payload,
  ) {
    switch (type) {
      case ChatMessageCardType.execApproval:
        final rawAllowedDecisions =
            payload['allowed_decisions']?.toString().trim() ?? '';
        if (rawAllowedDecisions.isNotEmpty) {
          payload['allowed_decisions'] = rawAllowedDecisions
              .split(',')
              .map((value) => value.trim())
              .where((value) => value.isNotEmpty)
              .toList();
        }
        return payload;
      case ChatMessageCardType.userProfile:
      case ChatMessageCardType.conversation:
      case ChatMessageCardType.execStatus:
      case ChatMessageCardType.toolExecution:
      case ChatMessageCardType.toolExecutionGroup:
      case ChatMessageCardType.eggInstallStatus:
      case ChatMessageCardType.agentStatus:
      case ChatMessageCardType.agentQuestion:
      case ChatMessageCardType.agentPairing:
      case ChatMessageCardType.agentOpenSession:
      case ChatMessageCardType.callOwner:
      case ChatMessageCardType.thinking:
      case ChatMessageCardType.progress:
        return payload;
    }
  }

  static String _buildGrixCardURI(
    ChatMessageCardType type,
    Map<String, dynamic> payload,
  ) {
    final typeStr = _encodeType(type);
    // For simple payloads, use flat query params.
    // For complex payloads with nested objects/lists, use d= JSON.
    final hasComplex = payload.values.any((v) => v is Map || v is List);
    if (hasComplex) {
      final d = Uri.encodeComponent(jsonEncode(payload));
      return 'grix://card/$typeStr?d=$d';
    }
    final params = <String, String>{};
    for (final entry in payload.entries) {
      final v = entry.value;
      if (v == null || (v is String && v.isEmpty)) continue;
      if (v is List) {
        params[entry.key] = v.join(',');
      } else {
        params[entry.key] = v.toString();
      }
    }
    return Uri(
      scheme: 'grix',
      host: 'card',
      pathSegments: <String>[typeStr],
      queryParameters: params.isEmpty ? null : params,
    ).toString();
  }

  static String _buildFallbackContent(ChatMessageCardData card) {
    switch (card.type) {
      case ChatMessageCardType.userProfile:
        final userProfile = card as ChatUserProfileCardData;
        return '[${'chat_message_card_user_profile_label'.tr}] ${userProfile.displayName}';
      case ChatMessageCardType.conversation:
        final conversation = card as ChatConversationCardData;
        return '[${'chat_message_card_conversation_label'.tr}] ${conversation.displayTitle}';
      case ChatMessageCardType.execApproval:
        final execApproval = card as ChatExecApprovalCardData;
        return '[${'chat_message_card_exec_approval_label'.tr}] ${execApproval.displayCommand}';
      case ChatMessageCardType.execStatus:
        final execStatus = card as ChatExecStatusCardData;
        return '[${'chat_message_card_exec_status_label'.tr}] ${execStatus.displaySummary}';
      case ChatMessageCardType.toolExecution:
        final toolExecution = card as ChatToolExecutionCardData;
        return '[${'chat_message_card_tool_execution_label'.tr}] ${toolExecution.displaySummaryText}';
      case ChatMessageCardType.toolExecutionGroup:
        return '[Tools]';
      case ChatMessageCardType.eggInstallStatus:
        final eggInstallStatus = card as ChatEggInstallStatusCardData;
        return '[${'chat_message_card_egg_install_status_label'.tr}] ${eggInstallStatus.displaySummary}';
      case ChatMessageCardType.agentStatus:
        final status = card as ChatAgentStatusCardData;
        return '[${'chat_message_card_agent_status_label'.tr}] ${status.displaySummary}';
      case ChatMessageCardType.agentQuestion:
        final question = card as ChatAgentQuestionCardData;
        return '[${'chat_message_card_agent_question_label'.tr}] ${question.displayRequestId}';
      case ChatMessageCardType.agentPairing:
        final pairing = card as ChatAgentPairingCardData;
        return '[${'chat_message_card_agent_pairing_label'.tr}] ${pairing.displayPairingCode}';
      case ChatMessageCardType.agentOpenSession:
        final openSession = card as ChatAgentOpenSessionCardData;
        final label = openSession.displaySummaryText.isNotEmpty
            ? openSession.displaySummaryText
            : openSession.displayDetailText.isNotEmpty
            ? openSession.displayDetailText
            : openSession.displayInitialCwd.isNotEmpty
            ? openSession.displayInitialCwd
            : 'workspace';
        return '[${'chat_message_card_agent_open_session_label'.tr}] $label';
      case ChatMessageCardType.callOwner:
        final callOwner = card as ChatCallOwnerCardData;
        final name = callOwner.displayAgentName;
        return name.isEmpty
            ? '[📞 ${'chat_call_owner_copy'.tr}]'
            : '[📞 ${'chat_call_owner_copy_from'.trParams({'name': name})}]';
      case ChatMessageCardType.thinking:
        return '[${'chat_message_card_thinking_label'.tr}]';
      case ChatMessageCardType.progress:
        final progress = card as ChatProgressCardData;
        final percent = progress.clampedPercent;
        final suffix = percent == null ? '' : ' ($percent%)';
        return '[${'chat_message_card_progress_label'.tr}] ${progress.displayLabel}$suffix';
    }
  }

  /// 提取 card 消息的可读文本，用于复制到剪贴板。
  /// 如果 [content] 不是 card 消息，返回 null。
  static String? buildCopyableText(String content) {
    final card = _decodeStandaloneGrixCardMessage(content);
    if (card == null) return null;
    return _buildCopyableTextFromCard(card);
  }

  static String _buildCopyableTextFromCard(ChatMessageCardData card) {
    final parts = <String>[];
    switch (card.type) {
      case ChatMessageCardType.execStatus:
        final c = card as ChatExecStatusCardData;
        if (c.displaySummary.isNotEmpty) parts.add(c.displaySummary);
        if (c.displayCommand.isNotEmpty) parts.add(c.displayCommand);
        if (c.displayWarningText.isNotEmpty) parts.add(c.displayWarningText);
        if (c.displayDetailText.isNotEmpty) parts.add(c.displayDetailText);
      case ChatMessageCardType.agentStatus:
        final c = card as ChatAgentStatusCardData;
        if (c.displaySummary.isNotEmpty) parts.add(c.displaySummary);
        if (c.displayDetailText.isNotEmpty) parts.add(c.displayDetailText);
      case ChatMessageCardType.eggInstallStatus:
        final c = card as ChatEggInstallStatusCardData;
        if (c.displaySummary.isNotEmpty) parts.add(c.displaySummary);
        if (c.displayErrorMsg.isNotEmpty) parts.add(c.displayErrorMsg);
        if (c.displayDetailText.isNotEmpty) parts.add(c.displayDetailText);
      case ChatMessageCardType.thinking:
        final c = card as ChatThinkingCardData;
        if (c.content.trim().isNotEmpty) parts.add(c.content.trim());
      case ChatMessageCardType.toolExecution:
        final c = card as ChatToolExecutionCardData;
        if (c.displaySummaryText.isNotEmpty) parts.add(c.displaySummaryText);
        if (c.displayDetailText.isNotEmpty) parts.add(c.displayDetailText);
      default:
        parts.add(_buildFallbackContent(card));
    }
    return parts.join('\n');
  }

  static String _encodeType(ChatMessageCardType type) {
    return switch (type) {
      ChatMessageCardType.userProfile => 'user_profile',
      ChatMessageCardType.conversation => 'conversation',
      ChatMessageCardType.execApproval => 'exec_approval',
      ChatMessageCardType.execStatus => 'exec_status',
      ChatMessageCardType.toolExecution => 'tool_execution',
      ChatMessageCardType.toolExecutionGroup => 'tool_execution_group',
      ChatMessageCardType.eggInstallStatus => 'egg_install_status',
      ChatMessageCardType.agentStatus => 'agent_status',
      ChatMessageCardType.agentQuestion => 'agent_question',
      ChatMessageCardType.agentPairing => 'agent_pairing',
      ChatMessageCardType.agentOpenSession => 'agent_open_session',
      ChatMessageCardType.callOwner => 'call_owner',
      ChatMessageCardType.thinking => 'thinking',
      ChatMessageCardType.progress => 'progress',
    };
  }

  static ChatMessageCardType? _decodeType(String rawType) {
    return switch (rawType) {
      'user_profile' => ChatMessageCardType.userProfile,
      'conversation' => ChatMessageCardType.conversation,
      'exec_approval' => ChatMessageCardType.execApproval,
      'exec_status' => ChatMessageCardType.execStatus,
      'tool_execution' => ChatMessageCardType.toolExecution,
      'tool_execution_group' => ChatMessageCardType.toolExecutionGroup,
      'egg_install_status' => ChatMessageCardType.eggInstallStatus,
      'agent_status' => ChatMessageCardType.agentStatus,
      'agent_question' => ChatMessageCardType.agentQuestion,
      'agent_pairing' => ChatMessageCardType.agentPairing,
      'agent_open_session' => ChatMessageCardType.agentOpenSession,
      'call_owner' => ChatMessageCardType.callOwner,
      'thinking' => ChatMessageCardType.thinking,
      'progress' => ChatMessageCardType.progress,
      _ => null,
    };
  }

  static ChatAgentOpenSessionCardData? _decodeAgentOpenSessionCard(
    Map<String, dynamic> payload,
  ) {
    final cardInstanceId = payload['card_instance_id']?.toString().trim() ?? '';
    final summaryText = payload['summary_text']?.toString().trim() ?? '';
    final detailText = payload['detail_text']?.toString().trim() ?? '';
    final initialCwd = payload['initial_cwd']?.toString().trim() ?? '';
    final submittedPath = payload['submitted_path']?.toString().trim() ?? '';
    if (summaryText.isEmpty &&
        detailText.isEmpty &&
        initialCwd.isEmpty &&
        submittedPath.isEmpty) {
      return null;
    }
    return ChatAgentOpenSessionCardData(
      cardInstanceId: cardInstanceId,
      summaryText: summaryText,
      detailText: detailText,
      initialCwd: initialCwd,
      submittedPath: submittedPath,
    );
  }

  static bool _isExecApprovalDecision(String value) {
    return value == 'allow' ||
        value == 'allow-once' ||
        value == 'allow-always' ||
        value == 'deny' ||
        RegExp(r'^allow-rule:\d+$').hasMatch(value);
  }

  static bool _isExecStatusValue(String value) {
    return value == 'approval-expired' ||
        value == 'approval-forwarded' ||
        value == 'approval-unavailable' ||
        value == 'resolved-allow-once' ||
        value == 'resolved-allow-always' ||
        value == 'resolved-allow-rule' ||
        value == 'resolved-deny' ||
        value == 'running' ||
        value == 'finished' ||
        value == 'denied';
  }

  static bool _isEggInstallStatusValue(String value) {
    return value == 'running' || value == 'success' || value == 'failed';
  }

  static ChatAgentStatusCardData? _decodeAgentStatusCard(
    Map<String, dynamic> payload,
  ) {
    final category = payload['category']?.toString().trim() ?? '';
    final status = payload['status']?.toString().trim() ?? '';
    final summary = payload['summary']?.toString().trim() ?? '';
    if (category.isEmpty || status.isEmpty || summary.isEmpty) {
      return null;
    }
    return ChatAgentStatusCardData(
      category: category,
      status: status,
      summary: summary,
      detailText: payload['detail_text']?.toString().trim() ?? '',
      referenceId: payload['reference_id']?.toString().trim() ?? '',
      cardInstanceId: payload['card_instance_id']?.toString().trim() ?? '',
    );
  }

  static int? _readInt(dynamic value) {
    if (value is int) {
      return value;
    }
    if (value is num) {
      return value.toInt();
    }
    if (value is String) {
      return int.tryParse(value.trim());
    }
    return null;
  }

  static int? _normalizeUserProfilePeerType(dynamic value) {
    final peerType = _readInt(value);
    if (peerType == 1 || peerType == 2) {
      return peerType;
    }
    return null;
  }
}
