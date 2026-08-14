import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:path/path.dart';
import 'package:sqflite/sqflite.dart';

import '../../shared/models/session_avatar_member.dart';
import '../../shared/utils/strict_int_parser.dart';
import '../models/local_search_result.dart';
import 'local_db_factory_initializer.dart';

part 'local_db_lifecycle.dart';
part 'local_db_message_repository.dart';
part 'local_db_search_repository.dart';
part 'local_db_session_repository.dart';
part 'local_db_markdown_render_cache_repository.dart';

class LocalDb {
  static Database? _db;
  static DatabaseFactory? _databaseFactory;
  static String? _activeUserId;
  static Future<void> _dbQueue = Future<void>.value();
  static bool _databaseFactoryInitialized = false;
  static bool _markdownRenderCacheSchemaEnsured = false;
  static const String _markdownRenderCacheTable = 'markdown_render_cache';

  /// 仅测试环境置 true（见 test/flutter_test_config.dart）：打开 DB 时关闭
  /// fsync 落盘（synchronous=OFF + journal_mode=MEMORY），消除每事务磁盘提交开销。
  /// 自建/慢盘（如 WSL2）上真实 fsync 会让 DB 密集型用例慢到超时；生产保持默认持久化。
  static bool useTestFastPragmas = false;

  static String? get activeUserId => _activeUserId;
  static bool get hasActiveUser => _activeUserId != null;

  static Future<T> _runSerialized<T>(Future<T> Function() action) {
    final previous = _dbQueue;
    final release = Completer<void>();
    _dbQueue = release.future;
    final queuedAtMs = DateTime.now().millisecondsSinceEpoch;

    return (() async {
      var runStartedAtMs = 0;
      try {
        await previous;
      } catch (_) {}
      try {
        runStartedAtMs = DateTime.now().millisecondsSinceEpoch;
        return await action();
      } finally {
        final finishedAtMs = DateTime.now().millisecondsSinceEpoch;
        final waitMs = runStartedAtMs > 0 ? runStartedAtMs - queuedAtMs : 0;
        final runMs = runStartedAtMs > 0 ? finishedAtMs - runStartedAtMs : 0;
        if (waitMs >= 1500 || runMs >= 1500) {
          debugPrint('⚠️ LocalDb slow queue wait=${waitMs}ms run=${runMs}ms');
        }
        if (!release.isCompleted) {
          release.complete();
        }
      }
    })();
  }

  static Future<T?> _withDatabase<T>(Future<T> Function(Database db) action) {
    return _runSerialized(() async {
      final db = await _databaseOrNull();
      if (db == null) {
        return null;
      }
      return action(db);
    });
  }

  static Future<T> _withDatabaseOr<T>(
    T fallback,
    Future<T> Function(Database db) action,
  ) async {
    final result = await _withDatabase(action);
    return result ?? fallback;
  }

  static Future<Database?> _databaseOrNull() async {
    if (_activeUserId == null) {
      return null;
    }
    return database;
  }

  static Future<void> _closeCurrentDb() async {
    if (_db != null) {
      await _db!.close();
      _db = null;
    }
    _markdownRenderCacheSchemaEnsured = false;
  }

  static Future<void> initDatabaseFactory() =>
      LocalDbLifecycle.initDatabaseFactory();

  static DatabaseFactory get databaseFactory {
    final factory = _databaseFactory;
    if (factory == null) {
      throw StateError('LocalDb database factory is not initialized.');
    }
    return factory;
  }

  static Future<void> setActiveUser(String? userId) =>
      LocalDbLifecycle.setActiveUser(userId);

  static Future<Database> get database => LocalDbLifecycle.database;

  static Future<LocalStorageSummary> getStorageSummary() =>
      LocalDbLifecycle.getStorageSummary();

  static Future<void> clearActiveUserData() =>
      LocalDbLifecycle.clearActiveUserData();

  static Future<void> batchInsertMessages(List<Map<String, dynamic>> msgs) =>
      LocalDbMessageRepository.batchInsertMessages(msgs);

  static Future<void> batchUpsertMessages(List<Map<String, dynamic>> msgs) =>
      LocalDbMessageRepository.batchUpsertMessages(msgs);

  static Future<void> upsertMessage(Map<String, dynamic> msg) =>
      LocalDbMessageRepository.upsertMessage(msg);

  static Future<int> getMaxInboxSeq() =>
      LocalDbMessageRepository.getMaxInboxSeq();

  static Future<void> insertLocalStub(Map<String, dynamic> msg) =>
      LocalDbMessageRepository.insertLocalStub(msg);

  static Future<void> deleteMessageByLocalSeq(String localSeq) =>
      LocalDbMessageRepository.deleteMessageByLocalSeq(localSeq);

  static Future<List<Map<String, dynamic>>> getSessions() =>
      LocalDbSessionRepository.getSessions();

  static Future<Map<String, dynamic>?> getSessionRecord(String sessionId) =>
      LocalDbSessionRepository.getSessionRecord(sessionId);

  static Future<List<Map<String, dynamic>>> getLatestMessages(
    String sessionId, {
    int limit = 60,
  }) => LocalDbMessageRepository.getLatestMessages(sessionId, limit: limit);

  static Future<Map<String, dynamic>?> getLatestPreviewableMessage(
    String sessionId,
  ) => LocalDbMessageRepository.getLatestPreviewableMessage(sessionId);

  static Future<List<Map<String, dynamic>>> getMessagesBefore(
    String sessionId, {
    required int beforeCreatedAt,
    required String beforeMsgId,
    int limit = 20,
  }) => LocalDbMessageRepository.getMessagesBefore(
    sessionId,
    beforeCreatedAt: beforeCreatedAt,
    beforeMsgId: beforeMsgId,
    limit: limit,
  );

  static Future<List<Map<String, dynamic>>> getMessagesAfter(
    String sessionId, {
    required int afterCreatedAt,
    required String afterMsgId,
    int limit = 20,
  }) => LocalDbMessageRepository.getMessagesAfter(
    sessionId,
    afterCreatedAt: afterCreatedAt,
    afterMsgId: afterMsgId,
    limit: limit,
  );

  static Future<List<Map<String, dynamic>>> getRecentMessagesForSession(
    String sessionId, {
    int limit = 30,
  }) => LocalDbMessageRepository.getRecentMessagesForSession(
    sessionId,
    limit: limit,
  );

  static Future<String> getLatestServerMessageId(String sessionId) =>
      LocalDbMessageRepository.getLatestServerMessageId(sessionId);

  static Future<String> getFirstMessageContentBySession(String sessionId) =>
      LocalDbMessageRepository.getFirstMessageContentBySession(sessionId);

  static Future<List<MarkdownRenderCacheRecord>> getMarkdownRenderCachesByKeys(
    List<String> cacheKeys,
  ) => LocalDbMarkdownRenderCacheRepository.getMarkdownRenderCachesByKeys(
    cacheKeys,
  );

  static Future<void> upsertMarkdownRenderCaches(
    List<MarkdownRenderCacheRecord> records, {
    int maxEntries = 1024,
  }) => LocalDbMarkdownRenderCacheRepository.upsertMarkdownRenderCaches(
    records,
    maxEntries: maxEntries,
  );

  static Future<void> upsertSession(Map<String, dynamic> session) =>
      LocalDbSessionRepository.upsertSession(session);

  static Future<void> upsertSessionGroupAvatarMembers(
    String sessionId,
    List<SessionAvatarMember> members,
  ) => LocalDbSessionRepository.upsertSessionGroupAvatarMembers(
    sessionId,
    members,
  );

  static Future<void> deleteSessionRecord(String sessionId) =>
      LocalDbSessionRepository.deleteSessionRecord(sessionId);

  static Future<void> deleteConversation(String sessionId) =>
      LocalDbSessionRepository.deleteConversation(sessionId);

  static Future<void> deleteMessage(String msgId) =>
      LocalDbMessageRepository.deleteMessage(msgId);

  static Future<void> updateSessionLastMsg(
    String sessionId,
    String lastMsg,
    int updatedAt, {
    String type = 'private',
    String peerId = '',
    int peerType = 0,
  }) => LocalDbSessionRepository.updateSessionLastMsg(
    sessionId,
    lastMsg,
    updatedAt,
    type: type,
    peerId: peerId,
    peerType: peerType,
  );

  static Future<void> bumpSessionActivity(String sessionId, int updatedAt) =>
      LocalDbSessionRepository.bumpSessionActivity(sessionId, updatedAt);

  static Future<void> updateSessionPeerIdentity(
    String sessionId, {
    required String peerId,
    required int peerType,
    String peerNickname = '',
  }) => LocalDbSessionRepository.updateSessionPeerIdentity(
    sessionId,
    peerId: peerId,
    peerType: peerType,
    peerNickname: peerNickname,
  );

  static Future<void> incrementUnread(String sessionId) =>
      LocalDbSessionRepository.incrementUnread(sessionId);

  static Future<void> incrementUnreadBy(String sessionId, int delta) =>
      LocalDbSessionRepository.incrementUnreadBy(sessionId, delta);

  static Future<void> batchApplySessionDeltas(
    Map<String, Map<String, dynamic>> deltas,
    Map<String, String> typeHints,
  ) => LocalDbSessionRepository.batchApplySessionDeltas(deltas, typeHints);

  static Future<void> replaceUnreadCounts(Map<String, int> snapshot) =>
      LocalDbSessionRepository.replaceUnreadCounts(snapshot);

  static Future<void> clearUnread(String sessionId) =>
      LocalDbSessionRepository.clearUnread(sessionId);

  static Future<void> setUnreadCount(String sessionId, int unreadCount) =>
      LocalDbSessionRepository.setUnreadCount(sessionId, unreadCount);

  static Future<void> setUnreadCounts(Map<String, int> unreadBySession) =>
      LocalDbSessionRepository.setUnreadCounts(unreadBySession);

  static Future<void> setSessionPinned(
    String sessionId, {
    required bool isPinned,
    required int pinnedAt,
  }) => LocalDbSessionRepository.setSessionPinned(
    sessionId,
    isPinned: isPinned,
    pinnedAt: pinnedAt,
  );

  static Future<void> setFriendPinned(
    String sessionId, {
    required bool isPinned,
    required int pinnedAt,
  }) => LocalDbSessionRepository.setFriendPinned(
    sessionId,
    isPinned: isPinned,
    pinnedAt: pinnedAt,
  );

  static Future<void> setSessionMuted(
    String sessionId, {
    required bool isMuted,
  }) => LocalDbSessionRepository.setSessionMuted(sessionId, isMuted: isMuted);

  static Future<Map<String, Map<String, dynamic>>> getLastMessages() =>
      LocalDbSessionRepository.getLastMessages();

  static Future<void> upsertSessionTitle(
    String sessionId,
    String title, {
    String type = 'private',
  }) =>
      LocalDbSessionRepository.upsertSessionTitle(sessionId, title, type: type);

  static Future<void> upsertSessionTypes(Map<String, String> typeMap) =>
      LocalDbSessionRepository.upsertSessionTypes(typeMap);

  static Future<Map<String, String>> getLatestPeerSenderIds({
    required String myUserId,
  }) => LocalDbMessageRepository.getLatestPeerSenderIds(myUserId: myUserId);

  static Future<Set<String>> getSessionsWithUnreadMentions(
    Map<String, int> unreadCountBySession, {
    required String userId,
  }) => LocalDbMessageRepository.getSessionsWithUnreadMentions(
    unreadCountBySession,
    userId: userId,
  );

  static Future<void> updateAckMsg(
    String localSeq,
    String msgId,
    int inboxSeq, {
    int? createdAt,
  }) => LocalDbMessageRepository.updateAckMsg(
    localSeq,
    msgId,
    inboxSeq,
    createdAt: createdAt,
  );

  static Future<void> updateMessageStatusByLocalSeq(
    String localSeq,
    String status,
  ) => LocalDbMessageRepository.updateMessageStatusByLocalSeq(localSeq, status);

  static Future<void> updateAgentDeliveryStatusByMsgId(
    String msgId,
    String agentDeliveryStatus,
  ) => LocalDbMessageRepository.updateAgentDeliveryStatusByMsgId(
    msgId,
    agentDeliveryStatus,
  );

  static Future<void> updateAgentDeliveryStatusBatch(
    List<MapEntry<String, String>> entries,
  ) => LocalDbMessageRepository.updateAgentDeliveryStatusBatch(entries);

  static Future<Map<String, dynamic>?> getMessageByMsgId(String msgId) =>
      LocalDbMessageRepository.getMessageByMsgId(msgId);

  static Future<Map<String, dynamic>?> getMessageByLocalSeq(String localSeq) =>
      LocalDbMessageRepository.getMessageByLocalSeq(localSeq);

  static Future<List<Map<String, dynamic>>> getPendingOutboundMessages({
    int limit = 200,
  }) => LocalDbMessageRepository.getPendingOutboundMessages(limit: limit);

  // --- Search ---

  static Future<List<MatchedSession>> searchSessions(List<String> keywords) =>
      LocalDbSearchRepository.searchSessions(keywords);

  static Future<List<Map<String, dynamic>>> searchSessionRecords(
    List<String> keywords,
  ) => LocalDbSearchRepository.searchSessionRecords(keywords);

  static Future<List<MatchedMessage>> searchMessages(
    List<String> keywords, {
    int limit = 200,
  }) => LocalDbSearchRepository.searchMessages(keywords, limit: limit);

  static Future<LocalSearchResult> search(
    List<String> keywords, {
    int messageLimit = 200,
  }) => LocalDbSearchRepository.search(keywords, messageLimit: messageLimit);
}
