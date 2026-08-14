import 'dart:convert';

import 'package:flutter/foundation.dart';

import 'auth_session_store.dart';

/// 一条本机保存的已登录账号记录。
///
/// [refreshToken] 为空表示凭证已失效（主动退出 / 被服务端吊销），
/// 条目保留用于展示，切换时需引导重新登录。
class SavedAccount {
  final String userId;
  final String username;
  final String nickname;
  final String email;
  final String avatarUrl;
  final String introduction;
  final bool usernameModified;
  final String phoneE164;
  final String phoneCountry;
  final String accessToken;
  final String refreshToken;
  final int accessExpiresAtMs;
  final String region;
  final String apiEndpoint;
  final String wsEndpoint;
  final int lastActiveAtMs;

  const SavedAccount({
    required this.userId,
    this.username = '',
    this.nickname = '',
    this.email = '',
    this.avatarUrl = '',
    this.introduction = '',
    this.usernameModified = false,
    this.phoneE164 = '',
    this.phoneCountry = '',
    this.accessToken = '',
    this.refreshToken = '',
    this.accessExpiresAtMs = 0,
    this.region = '',
    this.apiEndpoint = '',
    this.wsEndpoint = '',
    this.lastActiveAtMs = 0,
  });

  bool get needsRelogin => refreshToken.trim().isEmpty;

  String get displayName => nickname.trim().isNotEmpty ? nickname : username;

  SavedAccount copyWith({
    String? accessToken,
    String? refreshToken,
    int? accessExpiresAtMs,
    int? lastActiveAtMs,
  }) {
    return SavedAccount(
      userId: userId,
      username: username,
      nickname: nickname,
      email: email,
      avatarUrl: avatarUrl,
      introduction: introduction,
      usernameModified: usernameModified,
      phoneE164: phoneE164,
      phoneCountry: phoneCountry,
      accessToken: accessToken ?? this.accessToken,
      refreshToken: refreshToken ?? this.refreshToken,
      accessExpiresAtMs: accessExpiresAtMs ?? this.accessExpiresAtMs,
      region: region,
      apiEndpoint: apiEndpoint,
      wsEndpoint: wsEndpoint,
      lastActiveAtMs: lastActiveAtMs ?? this.lastActiveAtMs,
    );
  }

  Map<String, dynamic> toJson() => {
    'user_id': userId,
    'username': username,
    'nickname': nickname,
    'email': email,
    'avatar_url': avatarUrl,
    'introduction': introduction,
    'username_modified': usernameModified,
    'phone_e164': phoneE164,
    'phone_country': phoneCountry,
    'access_token': accessToken,
    'refresh_token': refreshToken,
    'access_expires_at_ms': accessExpiresAtMs,
    'region': region,
    'api_endpoint': apiEndpoint,
    'ws_endpoint': wsEndpoint,
    'last_active_at_ms': lastActiveAtMs,
  };

  static SavedAccount? fromJson(Map<String, dynamic> json) {
    final userId = json['user_id']?.toString().trim() ?? '';
    if (userId.isEmpty) return null;
    return SavedAccount(
      userId: userId,
      username: json['username']?.toString() ?? '',
      nickname: json['nickname']?.toString() ?? '',
      email: json['email']?.toString() ?? '',
      avatarUrl: json['avatar_url']?.toString() ?? '',
      introduction: json['introduction']?.toString() ?? '',
      usernameModified: json['username_modified'] == true,
      phoneE164: json['phone_e164']?.toString() ?? '',
      phoneCountry: json['phone_country']?.toString() ?? '',
      accessToken: json['access_token']?.toString() ?? '',
      refreshToken: json['refresh_token']?.toString() ?? '',
      accessExpiresAtMs: _toInt(json['access_expires_at_ms']),
      region: json['region']?.toString() ?? '',
      apiEndpoint: json['api_endpoint']?.toString() ?? '',
      wsEndpoint: json['ws_endpoint']?.toString() ?? '',
      lastActiveAtMs: _toInt(json['last_active_at_ms']),
    );
  }

  static int _toInt(dynamic value) {
    if (value is int) return value;
    if (value is num) return value.toInt();
    return int.tryParse(value?.toString() ?? '') ?? 0;
  }
}

/// 本机"已登录账号列表"的持久化，按最近使用排序。
///
/// 存储与当前登录态的单槽 key 相互独立：退出登录只清单槽，
/// 列表条目保留，直到用户在账号列表里显式移除。
class SavedAccountStore {
  SavedAccountStore(this._store);

  final AuthSessionStore _store;

  static const String storageKey = 'saved_accounts_v1';

  /// 串行化读-改-写，避免并发调用互相覆盖。
  Future<void> _pending = Future<void>.value();

  Future<T> _serialized<T>(Future<T> Function() action) {
    final result = _pending.then((_) => action());
    _pending = result.then((_) {}, onError: (_) {});
    return result;
  }

  Future<List<SavedAccount>> list() => _serialized(_read);

  Future<SavedAccount?> find(String userId) async {
    final normalized = userId.trim();
    if (normalized.isEmpty) return null;
    final accounts = await list();
    for (final account in accounts) {
      if (account.userId == normalized) return account;
    }
    return null;
  }

  Future<void> upsert(SavedAccount account) {
    if (account.userId.trim().isEmpty) return Future.value();
    return _serialized(() async {
      final accounts = await _read();
      accounts.removeWhere((item) => item.userId == account.userId);
      accounts.add(account);
      await _write(accounts);
    });
  }

  Future<void> remove(String userId) {
    final normalized = userId.trim();
    if (normalized.isEmpty) return Future.value();
    return _serialized(() async {
      final accounts = await _read();
      accounts.removeWhere((item) => item.userId == normalized);
      await _write(accounts);
    });
  }

  /// 清除某账号的登录凭证但保留条目（退出登录 / 凭证被吊销时调用）。
  Future<void> clearCredentials(String userId) {
    final normalized = userId.trim();
    if (normalized.isEmpty) return Future.value();
    return _serialized(() async {
      final accounts = await _read();
      var changed = false;
      for (var i = 0; i < accounts.length; i++) {
        if (accounts[i].userId == normalized) {
          accounts[i] = accounts[i].copyWith(
            accessToken: '',
            refreshToken: '',
            accessExpiresAtMs: 0,
          );
          changed = true;
        }
      }
      if (changed) {
        await _write(accounts);
      }
    });
  }

  Future<List<SavedAccount>> _read() async {
    try {
      final raw = await _store.getString(storageKey);
      if (raw == null || raw.trim().isEmpty) return <SavedAccount>[];
      final decoded = jsonDecode(raw);
      if (decoded is! List) return <SavedAccount>[];
      final accounts = <SavedAccount>[];
      for (final item in decoded) {
        if (item is! Map) continue;
        final account = SavedAccount.fromJson(Map<String, dynamic>.from(item));
        if (account != null) accounts.add(account);
      }
      accounts.sort((a, b) => b.lastActiveAtMs.compareTo(a.lastActiveAtMs));
      return accounts;
    } catch (e) {
      debugPrint('SavedAccountStore read error: $e');
      return <SavedAccount>[];
    }
  }

  Future<void> _write(List<SavedAccount> accounts) async {
    final payload = jsonEncode(
      accounts.map((account) => account.toJson()).toList(),
    );
    await _store.setString(storageKey, payload);
  }
}
