part of 'chat_controller.dart';

class _ChatMentionController {
  const _ChatMentionController(this.owner);

  final ChatController owner;

  void handleInputTextChanged() {
    syncPendingMentions(owner.inputController.text);

    if (!owner.isGroupChat) {
      clearSuggestionState();
      return;
    }

    final text = owner.inputController.text;
    final selection = owner._chatInputController
        .resolveInputSelectionWithinBounds(
          owner.inputController.selection,
          text.length,
        );
    if (selection == null) {
      clearSuggestionState();
      owner._mentionStartIndex = -1;
      return;
    }

    final cursorPosition = selection.extentOffset;
    final textBeforeCursor = text.substring(0, cursorPosition);
    final mentionStartIndex = resolveActiveMentionStart(textBeforeCursor);
    if (mentionStartIndex != -1) {
      owner._mentionStartIndex = mentionStartIndex;
      final query = textBeforeCursor.substring(mentionStartIndex + 1);
      owner.mentionSearchQuery.value = query;
      updateFilteredMentionList(query);
      owner.showMentionList.value = owner.filteredMentionList.isNotEmpty;
      return;
    }

    clearSuggestionState();
    owner._mentionStartIndex = -1;
  }

  void clearSuggestionState() {
    owner.showMentionList.value = false;
    owner.mentionSelectedIndex.value = 0;
  }

  void clearAfterMessageDispatched() {
    clearSuggestionState();
    owner._mentionStartIndex = -1;
    owner._pendingMentions.clear();
  }

  RxList<PinnedMention> get pinnedMentions => owner._pinnedMentions;

  bool isPinnedMention(String memberId) {
    final normalized = memberId.trim();
    if (normalized.isEmpty) {
      return false;
    }
    return owner._pinnedMentions.any((m) => m.memberId == normalized);
  }

  void togglePinnedMention(Map<String, dynamic> member) {
    if (!owner.isGroupChat) {
      return;
    }
    final memberId = (member['member_id'] ?? '').toString().trim();
    if (memberId.isEmpty) {
      return;
    }
    if (isPinnedMention(memberId)) {
      removePinnedMention(memberId);
      return;
    }
    final displayName = owner.resolveGroupMemberDisplayName(member).trim();
    if (displayName.isEmpty) {
      return;
    }
    owner._pinnedMentions.add(
      PinnedMention(memberId: memberId, displayName: displayName),
    );
    owner._chatInputController.persistPinnedMentionsDraft();
    owner._syncGroupToolbarTargetAgent();
  }

  void removePinnedMention(String memberId) {
    owner._pinnedMentions.removeWhere((m) => m.memberId == memberId.trim());
    owner._chatInputController.persistPinnedMentionsDraft();
    owner._syncGroupToolbarTargetAgent();
  }

  /// 发送时置于消息最前面的固定艾特前缀（纯成员 ID / @所有人 文案）。
  String buildPinnedMentionPrefix() {
    final parts = <String>[];
    for (final m in owner._pinnedMentions) {
      if (m.memberId == _mentionAllSyntheticMemberId) {
        parts.add('@${m.displayName}');
      } else if (m.memberId.trim().isNotEmpty) {
        parts.add('@${m.memberId}');
      }
    }
    return parts.join(' ');
  }

  /// 合并固定后消息整体：`[固定前缀] 用户文本`，再做过一次 @ 文本归一化。
  String buildDispatchContent(String rawText) {
    final prefix = buildPinnedMentionPrefix();
    final merged = prefix.isEmpty
        ? rawText
        : (rawText.trim().isEmpty ? prefix : '$prefix ${rawText.trim()}');
    return normalizeMentionContent(merged);
  }

  /// 在常规 mention extra 基础上并入固定成员的 mention_user_ids / mention_all。
  Map<String, dynamic>? buildMentionExtraWithPinned(String content) {
    final base = buildMentionExtra(content);
    final extra = base == null
        ? <String, dynamic>{}
        : Map<String, dynamic>.from(base);
    var mentionAll = extra[_mentionAllExtraKey] == true;
    final ids = <String>[];
    final existing = extra['mention_user_ids'];
    if (existing is List) {
      for (final item in existing) {
        final id = item.toString().trim();
        if (id.isNotEmpty && !ids.contains(id)) ids.add(id);
      }
    }
    for (final m in owner._pinnedMentions) {
      if (m.memberId == _mentionAllSyntheticMemberId) {
        mentionAll = true;
      } else if (m.memberId.trim().isNotEmpty &&
          !ids.contains(m.memberId.trim())) {
        ids.add(m.memberId.trim());
      }
    }
    if (mentionAll) extra[_mentionAllExtraKey] = true;
    if (ids.isNotEmpty) extra['mention_user_ids'] = ids;
    if (ids.isNotEmpty || mentionAll) return extra;
    return base;
  }

  int resolveActiveMentionStart(String textBeforeCursor) {
    final lastAtSignIndex = textBeforeCursor.lastIndexOf('@');
    if (lastAtSignIndex == -1) {
      return -1;
    }
    if (!isMentionTriggerStart(textBeforeCursor, lastAtSignIndex)) {
      return -1;
    }

    final query = textBeforeCursor.substring(lastAtSignIndex + 1);
    if (query.contains(' ') || query.contains('\n')) {
      return -1;
    }
    return lastAtSignIndex;
  }

  bool isMentionTriggerStart(String content, int atIndex) {
    if (atIndex <= 0) {
      return true;
    }
    final previous = content.codeUnitAt(atIndex - 1);
    if (isAsciiWordCodeUnit(previous)) {
      return false;
    }
    switch (previous) {
      case 46:
      case 95:
      case 43:
      case 45:
        return false;
      default:
        return true;
    }
  }

  bool isAsciiWordCodeUnit(int codeUnit) {
    final isNumber = codeUnit >= 48 && codeUnit <= 57;
    final isUppercase = codeUnit >= 65 && codeUnit <= 90;
    final isLowercase = codeUnit >= 97 && codeUnit <= 122;
    return isNumber || isUppercase || isLowercase;
  }

  void rebuildMemberDisplayNameCache() {
    owner._memberDisplayNameCache.clear();
    for (final member in owner.groupMembers) {
      final id = (member['member_id'] ?? '').toString().trim();
      if (id.isNotEmpty) {
        owner._memberDisplayNameCache[id] = owner.resolveGroupMemberDisplayName(
          member,
        );
      }
    }
  }

  void refreshSuggestionState() {
    handleInputTextChanged();
  }

  void updateFilteredMentionList(String query) {
    final myId = owner.authService.userId?.trim() ?? '';
    final queryLower = query.toLowerCase();
    final filtered = owner.groupMembers
        .where((member) {
          final memberId = (member['member_id'] ?? '').toString().trim();
          if (memberId.isEmpty) {
            return false;
          }
          if (myId.isNotEmpty && memberId == myId) {
            return false;
          }

          final name = (owner._memberDisplayNameCache[memberId] ?? memberId)
              .toLowerCase();
          return name.contains(queryLower) ||
              memberId.toLowerCase().contains(queryLower);
        })
        .toList(growable: true);
    if (shouldShowMentionAllSuggestion(queryLower)) {
      filtered.insert(0, buildMentionAllSuggestion());
    }
    owner.filteredMentionList.assignAll(filtered);
    owner.mentionSelectedIndex.value = 0;
  }

  bool shouldShowMentionAllSuggestion(String queryLower) {
    if (!hasMentionAllTargets()) {
      return false;
    }
    if (queryLower.isEmpty) {
      return true;
    }
    return _mentionAllDisplayName.toLowerCase().contains(queryLower) ||
        '所有人'.contains(queryLower) ||
        'all'.contains(queryLower);
  }

  bool hasMentionAllTargets() {
    final totalMembers = owner.groupMemberCount > 0
        ? owner.groupMemberCount
        : owner.groupMembers.length;
    return totalMembers > 2;
  }

  Map<String, dynamic> buildMentionAllSuggestion() {
    return <String, dynamic>{
      'member_id': _mentionAllSyntheticMemberId,
      'member_type': 1,
      'nickname': _mentionAllDisplayName,
      _mentionBuiltinKindKey: _mentionBuiltinKindAll,
    };
  }

  void mentionMoveUp() {
    if (!owner.showMentionList.value || owner.filteredMentionList.isEmpty) {
      return;
    }
    if (owner.mentionSelectedIndex.value > 0) {
      owner.mentionSelectedIndex.value--;
    } else {
      owner.mentionSelectedIndex.value = owner.filteredMentionList.length - 1;
    }
  }

  void mentionMoveDown() {
    if (!owner.showMentionList.value || owner.filteredMentionList.isEmpty) {
      return;
    }
    if (owner.mentionSelectedIndex.value <
        owner.filteredMentionList.length - 1) {
      owner.mentionSelectedIndex.value++;
    } else {
      owner.mentionSelectedIndex.value = 0;
    }
  }

  bool mentionSelectCurrent() {
    if (!owner.showMentionList.value || owner.filteredMentionList.isEmpty) {
      return false;
    }
    final idx = owner.mentionSelectedIndex.value;
    if (idx < 0 || idx >= owner.filteredMentionList.length) {
      return false;
    }
    owner.insertMention(owner.filteredMentionList[idx]);
    return true;
  }

  void mentionSenderFromMessage({
    required String senderId,
    required int senderType,
    required bool isMine,
    required String senderName,
  }) {
    unawaited(
      mentionSenderFromMessageInternal(
        senderId: senderId,
        senderType: senderType,
        isMine: isMine,
        senderName: senderName,
      ),
    );
  }

  Future<void> mentionSenderFromMessageInternal({
    required String senderId,
    required int senderType,
    required bool isMine,
    required String senderName,
  }) async {
    if (!owner.isGroupChat) {
      await owner.refreshSessionDetail(forceTypeProbe: true);
      if (!owner.isGroupChat) {
        return;
      }
    }

    final target = resolveMentionTargetFromMessage(
      senderId: senderId,
      senderType: senderType,
      isMine: isMine,
      senderName: senderName,
    );
    if (target == null) {
      return;
    }
    insertMentionTargetAtSelection(target);
  }

  _ResolvedMentionTarget? resolveMentionTargetFromMessage({
    required String senderId,
    required int senderType,
    required bool isMine,
    required String senderName,
  }) {
    if (senderType != 1 && senderType != 2) {
      return null;
    }

    final normalizedSenderName = senderName.trim();
    if (senderType == 2) {
      final agentId = senderId.trim();
      if (agentId.isEmpty) {
        return null;
      }
      final member = owner._findGroupMember(agentId, memberType: 2);
      final displayName = () {
        if (member != null) {
          final fromMember = owner.resolveGroupMemberDisplayName(member).trim();
          if (fromMember.isNotEmpty) {
            return fromMember;
          }
        }
        if (normalizedSenderName.isNotEmpty) {
          return normalizedSenderName;
        }
        final known = owner._resolveKnownAgentName(agentId).trim();
        if (known.isNotEmpty) {
          return known;
        }
        return 'Agent $agentId';
      }();
      return _ResolvedMentionTarget(
        memberId: agentId,
        displayName: displayName,
      );
    }

    final myId = owner.authService.userId?.trim() ?? '';
    final candidateMemberId = () {
      if (!isMine) {
        return senderId.trim();
      }
      if (myId.isNotEmpty) {
        return myId;
      }
      final rawSenderId = senderId.trim();
      if (rawSenderId.isNotEmpty && rawSenderId != 'me') {
        return rawSenderId;
      }
      return '';
    }();
    if (candidateMemberId.isEmpty) {
      return null;
    }
    if (myId.isNotEmpty && candidateMemberId == myId) {
      return null;
    }

    final member = owner._findGroupHumanMember(candidateMemberId);
    final displayName = () {
      if (member != null) {
        final fromMember = owner.resolveGroupMemberDisplayName(member).trim();
        if (fromMember.isNotEmpty) {
          return fromMember;
        }
      }
      if (normalizedSenderName.isNotEmpty) {
        return normalizedSenderName;
      }
      return candidateMemberId;
    }();
    if (displayName.isEmpty) {
      return null;
    }
    return _ResolvedMentionTarget(
      memberId: candidateMemberId,
      displayName: displayName,
    );
  }

  void insertMentionTargetAtSelection(_ResolvedMentionTarget target) {
    final memberId = target.memberId.trim();
    final displayName = target.displayName.trim();
    if (memberId.isEmpty || displayName.isEmpty) {
      return;
    }
    owner._chatInputController.updateInputValue((currentValue) {
      final currentSelection = owner._chatInputController.normalizeSelection(
        currentValue.selection,
        currentValue.text.length,
      );
      final currentPrefix = currentValue.text.substring(
        0,
        currentSelection.start,
      );
      final currentSuffix = currentValue.text.substring(currentSelection.end);
      final currentInsertText = buildMentionInsertText(
        prefix: currentPrefix,
        suffix: currentSuffix,
        displayName: displayName,
      );
      return TextEditingValue(
        text: '$currentPrefix$currentInsertText$currentSuffix',
        selection: TextSelection.collapsed(
          offset: currentPrefix.length + currentInsertText.length,
        ),
        composing: TextRange.empty,
      );
    }, requestFocus: true);

    upsertPendingMention(memberId, displayName);
    clearSuggestionState();
    owner._mentionStartIndex = -1;
  }

  String buildMentionInsertText({
    required String prefix,
    required String suffix,
    required String displayName,
  }) {
    final mentionToken = '@$displayName';
    final needsLeadingSpace =
        prefix.isNotEmpty &&
        !isMentionBoundary(prefix.codeUnitAt(prefix.length - 1));
    final needsTrailingSpace =
        suffix.isEmpty || !isMentionBoundary(suffix.codeUnitAt(0));
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

  void insertMention(Map<String, dynamic> member) {
    if (owner._mentionStartIndex == -1) {
      return;
    }

    final displayName = owner.resolveGroupMemberDisplayName(member);
    final memberId = (member['member_id'] ?? '').toString().trim();
    final text = owner.inputController.text;
    final selection = owner._chatInputController
        .resolveInputSelectionWithinBounds(
          owner.inputController.selection,
          text.length,
        );
    if (selection == null ||
        owner._mentionStartIndex > selection.extentOffset) {
      clearSuggestionState();
      owner._mentionStartIndex = -1;
      return;
    }

    final insertText = '@$displayName ';
    final mentionStartIndex = owner._mentionStartIndex;
    owner._chatInputController.updateInputValue((currentValue) {
      final currentSelection = owner._chatInputController
          .resolveInputSelectionWithinBounds(
            currentValue.selection,
            currentValue.text.length,
          );
      if (currentSelection == null ||
          mentionStartIndex > currentSelection.extentOffset) {
        return currentValue;
      }
      final prefix = currentValue.text.substring(0, mentionStartIndex);
      final suffix = currentValue.text.substring(currentSelection.extentOffset);
      return TextEditingValue(
        text: prefix + insertText + suffix,
        selection: TextSelection.collapsed(
          offset: prefix.length + insertText.length,
        ),
        composing: TextRange.empty,
      );
    }, requestFocus: true);

    clearSuggestionState();
    owner._mentionStartIndex = -1;
    if (memberId.isNotEmpty) {
      upsertPendingMention(memberId, displayName);
    }
  }

  String normalizeMentionContent(String content) {
    if (owner._pendingMentions.isEmpty || !content.contains('@')) {
      return content;
    }
    final sorted = List<_PendingMention>.from(owner._pendingMentions)
      ..sort((a, b) => b.displayName.length.compareTo(a.displayName.length));
    var result = content;
    for (final mention in sorted) {
      if (mention.memberId == _mentionAllSyntheticMemberId) {
        continue;
      }
      result = _replaceMentionToken(
        result,
        mention.displayName,
        mention.memberId,
      );
    }
    return result;
  }

  String _replaceMentionToken(
    String content,
    String displayName,
    String memberId,
  ) {
    final needle = '@$displayName';
    final result = StringBuffer();
    var i = 0;
    while (i < content.length) {
      final index = content.indexOf(needle, i);
      if (index == -1) {
        result.write(content.substring(i));
        break;
      }
      final end = index + needle.length;
      if (end == content.length || isMentionBoundary(content.codeUnitAt(end))) {
        result.write(content.substring(i, index));
        result.write('@$memberId');
        i = end;
      } else {
        result.write(content.substring(i, index + 1));
        i = index + 1;
      }
    }
    return result.toString();
  }

  Map<String, dynamic>? buildMentionExtra(String content) {
    if (!owner.isGroupChat) {
      return null;
    }
    final mentionAll = hasSelectedMentionAll(content);
    final mentionUserIds = resolveExplicitMentionUserIds(content);
    if (!mentionAll && mentionUserIds.isEmpty) {
      return null;
    }
    final extra = <String, dynamic>{};
    if (mentionAll) {
      extra[_mentionAllExtraKey] = true;
    }
    if (mentionUserIds.isNotEmpty) {
      extra['mention_user_ids'] = mentionUserIds;
    }
    return extra;
  }

  List<String> resolveExplicitMentionUserIds(String content) {
    final uniq = <String>[];
    final seen = <String>{};
    final specialDisplayNames = <String>{};
    for (final mention in owner._pendingMentions) {
      if (mention.memberId.isEmpty) {
        continue;
      }
      if (!containsMentionToken(content, mention.displayName)) {
        continue;
      }
      if (mention.memberId == _mentionAllSyntheticMemberId) {
        specialDisplayNames.add(mention.displayName);
        continue;
      }
      if (seen.contains(mention.memberId)) {
        continue;
      }
      seen.add(mention.memberId);
      uniq.add(mention.memberId);
    }
    for (final memberId in resolveDisplayNameMentionUserIds(
      content,
      excludedDisplayNames: specialDisplayNames,
    )) {
      if (memberId.isEmpty || seen.contains(memberId)) {
        continue;
      }
      seen.add(memberId);
      uniq.add(memberId);
    }
    return uniq;
  }

  bool hasSelectedMentionAll(String content) {
    return owner._pendingMentions.any(
      (mention) =>
          mention.memberId == _mentionAllSyntheticMemberId &&
          containsMentionToken(content, mention.displayName),
    );
  }

  List<String> resolveDisplayNameMentionUserIds(
    String content, {
    Set<String> excludedDisplayNames = const <String>{},
  }) {
    if (owner._groupMembers.isEmpty) {
      return const <String>[];
    }

    final displayToMember = <String, String>{};
    final ambiguousDisplays = <String>{};
    for (final member in owner._groupMembers) {
      final memberId = (member['member_id'] ?? '').toString().trim();
      if (memberId.isEmpty) {
        continue;
      }
      final displayName = owner.resolveGroupMemberDisplayName(member).trim();
      if (displayName.isEmpty) {
        continue;
      }
      final existing = displayToMember[displayName];
      if (existing == null) {
        displayToMember[displayName] = memberId;
        continue;
      }
      if (existing != memberId) {
        ambiguousDisplays.add(displayName);
      }
    }

    final uniq = <String>[];
    final seen = <String>{};
    for (final entry in displayToMember.entries) {
      if (ambiguousDisplays.contains(entry.key) ||
          excludedDisplayNames.contains(entry.key)) {
        continue;
      }
      if (!containsMentionToken(content, entry.key) ||
          seen.contains(entry.value)) {
        continue;
      }
      seen.add(entry.value);
      uniq.add(entry.value);
    }
    return uniq;
  }

  void syncPendingMentions(String content) {
    if (owner._pendingMentions.isEmpty) {
      return;
    }
    owner._pendingMentions.removeWhere(
      (mention) => !containsMentionToken(content, mention.displayName),
    );
  }

  void upsertPendingMention(String memberId, String displayName) {
    final normalizedMemberId = memberId.trim();
    final normalizedDisplayName = displayName.trim();
    if (normalizedMemberId.isEmpty || normalizedDisplayName.isEmpty) {
      return;
    }

    final existingIndex = owner._pendingMentions.indexWhere(
      (mention) =>
          mention.memberId == normalizedMemberId &&
          mention.displayName == normalizedDisplayName,
    );
    if (existingIndex != -1) {
      return;
    }
    owner._pendingMentions.add(
      _PendingMention(
        memberId: normalizedMemberId,
        displayName: normalizedDisplayName,
      ),
    );
  }

  bool containsMentionToken(String content, String displayName) {
    final normalizedContent = content.trim();
    final normalizedDisplayName = displayName.trim();
    if (normalizedContent.isEmpty || normalizedDisplayName.isEmpty) {
      return false;
    }

    final needle = '@$normalizedDisplayName';
    var searchFrom = 0;
    while (true) {
      final index = normalizedContent.indexOf(needle, searchFrom);
      if (index == -1) {
        return false;
      }
      final end = index + needle.length;
      if (end == normalizedContent.length ||
          isMentionBoundary(normalizedContent.codeUnitAt(end))) {
        return true;
      }
      searchFrom = index + needle.length;
    }
  }

  bool isMentionBoundary(int codeUnit) {
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
}
