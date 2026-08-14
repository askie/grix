import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_cache_manager/flutter_cache_manager.dart';
import '../../data/providers/local_db.dart';

class UserImageCacheManager {
  static const Duration _stalePeriod = Duration(days: 365);
  static const int _maxNrOfCacheObjects = 500;
  static const Set<String> _volatileQueryParameterNames = {
    'expires',
    'googleaccessid',
    'ossaccesskeyid',
    'policy',
    'q-ak',
    'q-header-list',
    'q-key-time',
    'q-sign-algorithm',
    'q-sign-time',
    'q-signature',
    'q-url-param-list',
    'security-token',
    'signature',
    'x-cos-security-token',
    'x-oss-additional-headers',
    'x-oss-algorithm',
    'x-oss-credential',
    'x-oss-date',
    'x-oss-expires',
    'x-oss-security-token',
    'x-oss-signature',
    'x-oss-signature-version',
  };
  static final Map<String, BaseCacheManager> _userManagers = {};
  static bool _disabledForTest = false;
  static Future<void> Function(String imageUrl)? _evictOverrideForTest;

  static BaseCacheManager? current() {
    if (_disabledForTest) {
      return null;
    }
    final normalizedUserId = LocalDb.activeUserId?.trim() ?? '';
    if (normalizedUserId.isEmpty) {
      return DefaultCacheManager();
    }
    return _resolveUserManager(normalizedUserId);
  }

  static Future<void> evictUserImage(String userId, String imageUrl) {
    return evictUserImages(userId, <String>[imageUrl]);
  }

  static String cacheKeyForImageUrl(String imageUrl) {
    final trimmed = imageUrl.trim();
    if (trimmed.isEmpty) {
      return '';
    }

    Uri uri;
    try {
      uri = Uri.parse(trimmed);
    } catch (_) {
      return trimmed;
    }

    if (!uri.hasScheme || (uri.scheme != 'http' && uri.scheme != 'https')) {
      return trimmed;
    }

    final retainedQueryParameters = <MapEntry<String, String>>[];
    uri.queryParametersAll.forEach((name, values) {
      if (_isVolatileQueryParameterName(name)) {
        return;
      }
      for (final value in values) {
        retainedQueryParameters.add(MapEntry<String, String>(name, value));
      }
    });
    retainedQueryParameters.sort((a, b) {
      final nameCompare = a.key.compareTo(b.key);
      if (nameCompare != 0) {
        return nameCompare;
      }
      return a.value.compareTo(b.value);
    });

    final normalizedQuery = retainedQueryParameters.isEmpty
        ? null
        : retainedQueryParameters
              .map(
                (entry) =>
                    '${Uri.encodeQueryComponent(entry.key)}='
                    '${Uri.encodeQueryComponent(entry.value)}',
              )
              .join('&');
    final buffer = StringBuffer()
      ..write(uri.scheme)
      ..write('://')
      ..write(uri.authority)
      ..write(uri.path);
    if (normalizedQuery != null) {
      buffer
        ..write('?')
        ..write(normalizedQuery);
    }
    return buffer.toString();
  }

  static Future<void> evictUserImages(
    String userId,
    Iterable<String> imageUrls,
  ) async {
    final normalizedUserId = userId.trim();
    if (normalizedUserId.isEmpty) {
      return;
    }

    final normalizedUrls = _normalizeImageUrls(imageUrls);
    if (normalizedUrls.isEmpty) {
      return;
    }

    final evictOverride = _evictOverrideForTest;
    if (evictOverride != null) {
      for (final imageUrl in normalizedUrls) {
        await evictOverride(imageUrl);
      }
      return;
    }

    final manager = _resolveUserManager(normalizedUserId);
    for (final imageUrl in normalizedUrls) {
      final cacheKey = cacheKeyForImageUrl(imageUrl);
      await CachedNetworkImage.evictFromCache(
        imageUrl,
        cacheKey: cacheKey.isEmpty ? null : cacheKey,
        cacheManager: manager,
      );
      if (cacheKey.isNotEmpty) {
        await CachedNetworkImageProvider(
          imageUrl,
          cacheKey: cacheKey,
          cacheManager: manager,
        ).evict();
      }
    }
  }

  static void setDisabledForTest(bool disabled) {
    _disabledForTest = disabled;
  }

  @visibleForTesting
  static void setEvictOverrideForTest(
    Future<void> Function(String imageUrl)? evict,
  ) {
    _evictOverrideForTest = evict;
  }

  static Future<void> clearUserCache(String userId) async {
    final normalizedUserId = userId.trim();
    if (normalizedUserId.isEmpty) {
      return;
    }
    final manager = _resolveUserManager(normalizedUserId);
    await manager.emptyCache();
    _userManagers.remove(normalizedUserId);
  }

  static BaseCacheManager _resolveUserManager(String userId) {
    return _userManagers.putIfAbsent(
      userId,
      () => CacheManager(_buildConfig(userId)),
    );
  }

  static Config _buildConfig(String userId) {
    return Config(
      'grix_user_${userId}_img_cache',
      stalePeriod: _stalePeriod,
      maxNrOfCacheObjects: _maxNrOfCacheObjects,
      fileService: HttpFileService(),
    );
  }

  static List<String> _normalizeImageUrls(Iterable<String> imageUrls) {
    final normalized = <String>[];
    final dedup = <String>{};
    for (final imageUrl in imageUrls) {
      final trimmed = imageUrl.trim();
      if (trimmed.isEmpty || !dedup.add(trimmed)) {
        continue;
      }
      normalized.add(trimmed);
    }
    return normalized;
  }

  static bool _isVolatileQueryParameterName(String name) {
    final normalized = name.trim().toLowerCase();
    return _volatileQueryParameterNames.contains(normalized) ||
        normalized.startsWith('x-amz-') ||
        normalized.startsWith('response-');
  }
}
