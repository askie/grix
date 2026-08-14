import 'dart:math' as math;

import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';

import '../utils/user_image_cache_manager.dart';

class AvatarNetworkImage extends StatelessWidget {
  const AvatarNetworkImage({
    super.key,
    required this.avatarUrl,
    required this.fallback,
    this.fit = BoxFit.cover,
    this.filterQuality = FilterQuality.low,
    this.width,
    this.height,
  });

  final String avatarUrl;
  final Widget fallback;
  final BoxFit fit;
  final FilterQuality filterQuality;
  final double? width;
  final double? height;

  @override
  Widget build(BuildContext context) {
    final normalizedAvatarUrl = avatarUrl.trim();
    if (normalizedAvatarUrl.isEmpty) {
      return fallback;
    }

    final resolvedWidth = width;
    final resolvedHeight = height;
    final devicePixelRatio =
        MediaQuery.maybeOf(context)?.devicePixelRatio ?? 1.0;
    final memCacheWidth = _resolveCacheExtent(
      logicalExtent: resolvedWidth,
      devicePixelRatio: devicePixelRatio,
    );
    final memCacheHeight = _resolveCacheExtent(
      logicalExtent: resolvedHeight,
      devicePixelRatio: devicePixelRatio,
    );
    final cacheManager = UserImageCacheManager.current();
    final cacheKey = UserImageCacheManager.cacheKeyForImageUrl(
      normalizedAvatarUrl,
    );

    if (cacheManager == null) {
      return Image.network(
        normalizedAvatarUrl,
        width: resolvedWidth,
        height: resolvedHeight,
        fit: fit,
        filterQuality: filterQuality,
        cacheWidth: memCacheWidth,
        cacheHeight: memCacheHeight,
        gaplessPlayback: true,
        loadingBuilder: (_, child, loadingProgress) {
          if (loadingProgress == null) {
            return child;
          }
          return fallback;
        },
        errorBuilder: (_, __, ___) => fallback,
      );
    }

    return CachedNetworkImage(
      imageUrl: normalizedAvatarUrl,
      cacheKey: cacheKey.isEmpty ? null : cacheKey,
      cacheManager: cacheManager,
      width: resolvedWidth,
      height: resolvedHeight,
      fit: fit,
      filterQuality: filterQuality,
      memCacheWidth: memCacheWidth,
      memCacheHeight: memCacheHeight,
      useOldImageOnUrlChange: true,
      fadeInDuration: Duration.zero,
      fadeOutDuration: Duration.zero,
      placeholderFadeInDuration: Duration.zero,
      placeholder: (_, __) => fallback,
      errorWidget: (_, __, ___) => fallback,
    );
  }

  int? _resolveCacheExtent({
    required double? logicalExtent,
    required double devicePixelRatio,
  }) {
    if (logicalExtent == null ||
        !logicalExtent.isFinite ||
        logicalExtent <= 0) {
      return null;
    }
    return math.max(1, (logicalExtent * devicePixelRatio).round());
  }
}
