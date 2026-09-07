import '../../data/models/local_search_result.dart';

/// 本地「联系人和 Agent」搜索：在已缓存的好友 / Agent 上按关键词匹配。
///
/// 与会话、聊天记录两段搜索保持同一口径：不区分大小写的子串匹配，命中全部
/// 关键词的排在前面，只命中部分关键词的降权排后面；同权次序保持候选原序。
class LocalContactMatcher {
  const LocalContactMatcher._();

  static const int defaultLimit = 30;

  static List<MatchedContact> match(
    List<MatchedContact> candidates,
    List<String> keywords, {
    int limit = defaultLimit,
  }) {
    final lowered = keywords
        .map((kw) => kw.trim().toLowerCase())
        .where((kw) => kw.isNotEmpty)
        .toList(growable: false);
    if (lowered.isEmpty || limit <= 0) {
      return const <MatchedContact>[];
    }

    final full = <MatchedContact>[];
    final partial = <MatchedContact>[];
    final seenPeers = <String>{};
    for (final candidate in candidates) {
      final peerKey = '${candidate.peerType}:${candidate.peerId.trim()}';
      if (candidate.peerId.trim().isEmpty || !seenPeers.add(peerKey)) {
        continue;
      }
      final haystack = candidate.searchableFields
          .map((field) => field.toLowerCase())
          .toList(growable: false);
      var hits = 0;
      for (final kw in lowered) {
        if (haystack.any((field) => field.contains(kw))) {
          hits++;
        }
      }
      if (hits == lowered.length) {
        full.add(candidate);
      } else if (hits > 0) {
        partial.add(candidate);
      }
    }

    final ranked = <MatchedContact>[...full, ...partial];
    return ranked.length <= limit
        ? List<MatchedContact>.unmodifiable(ranked)
        : List<MatchedContact>.unmodifiable(ranked.take(limit));
  }
}
