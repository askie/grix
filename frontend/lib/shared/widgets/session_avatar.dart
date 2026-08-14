import 'package:flutter/material.dart';

import '../../app/themes/app_theme.dart';
import '../models/session_avatar_member.dart';
import 'avatar_network_image.dart';

class SessionAvatar extends StatefulWidget {
  const SessionAvatar({
    super.key,
    required this.isGroup,
    required this.avatarTitle,
    required this.avatarColor,
    this.members = const <SessionAvatarMember>[],
    this.avatarUrl = '',
    this.memberFallbackColor,
    this.size = 50,
    this.borderRadius = 0,
  });

  final bool isGroup;
  final String avatarTitle;
  final Color avatarColor;
  final List<SessionAvatarMember> members;
  final String avatarUrl;
  final Color? memberFallbackColor;
  final double size;
  final double borderRadius;

  @override
  State<SessionAvatar> createState() => _SessionAvatarState();
}

class _SessionAvatarState extends State<SessionAvatar> {
  String _cacheKey = '';
  Widget? _cachedChild;

  @override
  Widget build(BuildContext context) {
    final nextKey = _buildCacheKey(widget);
    if (_cachedChild == null || _cacheKey != nextKey) {
      _cacheKey = nextKey;
      _cachedChild = _buildAvatar(widget);
    }

    return RepaintBoundary(child: _cachedChild!);
  }

  Widget _buildAvatar(SessionAvatar widget) {
    if (widget.isGroup && widget.members.isNotEmpty) {
      return _GroupSessionAvatar(
        members: widget.members,
        avatarColor: widget.avatarColor,
        memberFallbackColor: widget.memberFallbackColor,
        size: widget.size,
        borderRadius: widget.borderRadius,
      );
    }

    final fallback = _buildSingleAvatarFallback(widget);

    return ClipRRect(
      borderRadius: BorderRadius.zero,
      child: SizedBox(
        width: widget.size,
        height: widget.size,
        child: AvatarNetworkImage(
          avatarUrl: widget.avatarUrl,
          fallback: fallback,
          width: widget.size,
          height: widget.size,
        ),
      ),
    );
  }

  Widget _buildSingleAvatarFallback(SessionAvatar widget) {
    return Container(
      width: widget.size,
      height: widget.size,
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            widget.avatarColor,
            widget.avatarColor.withValues(alpha: 0.8),
          ],
        ),
        borderRadius: BorderRadius.zero,
        boxShadow: [
          BoxShadow(
            color: widget.avatarColor.withValues(alpha: 0.2),
            blurRadius: widget.size * 0.16,
            offset: Offset(0, widget.size * 0.08),
          ),
        ],
      ),
      child: Center(
        child: Text(
          firstAvatarGlyph(widget.avatarTitle),
          style: TextStyle(
            color: Colors.white,
            fontSize: widget.size * 0.36,
            fontWeight: FontWeight.w700,
            height: 1,
          ),
        ),
      ),
    );
  }

  String _buildCacheKey(SessionAvatar widget) {
    final buffer = StringBuffer()
      ..write(widget.isGroup ? '1' : '0')
      ..write('|')
      ..write(widget.avatarTitle.trim())
      ..write('|')
      ..write(widget.avatarColor.toARGB32())
      ..write('|')
      ..write(widget.memberFallbackColor?.toARGB32() ?? '')
      ..write('|')
      ..write(widget.avatarUrl.trim())
      ..write('|')
      ..write(widget.size)
      ..write('|')
      ..write(widget.borderRadius);

    final visibleMembers = widget.members.take(9);
    for (final member in visibleMembers) {
      buffer
        ..write('|')
        ..write(member.memberId)
        ..write(':')
        ..write(member.memberType)
        ..write(':')
        ..write(member.displayName)
        ..write(':')
        ..write(member.avatarUrl);
    }
    return buffer.toString();
  }
}

class _GroupSessionAvatar extends StatelessWidget {
  const _GroupSessionAvatar({
    required this.members,
    required this.avatarColor,
    required this.memberFallbackColor,
    required this.size,
    required this.borderRadius,
  });

  final List<SessionAvatarMember> members;
  final Color avatarColor;
  final Color? memberFallbackColor;
  final double size;
  final double borderRadius;

  @override
  Widget build(BuildContext context) {
    final visibleMembers = members.take(9).toList(growable: false);
    final gridDimension =
        visibleMembers.length <= 1 ? 1 : (visibleMembers.length <= 4 ? 2 : 3);
    const itemSpacing = 1.0;
    final contentPadding = size * (gridDimension == 3 ? 0.08 : 0.1);

    return Container(
      width: size,
      height: size,
      padding: EdgeInsets.all(contentPadding),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            avatarColor.withValues(alpha: 0.18),
            avatarColor.withValues(alpha: 0.1),
          ],
        ),
        borderRadius: BorderRadius.zero,
        boxShadow: [
          BoxShadow(
            color: avatarColor.withValues(alpha: 0.16),
            blurRadius: size * 0.16,
            offset: Offset(0, size * 0.08),
          ),
        ],
      ),
      child: _buildGrid(
        visibleMembers,
        gridDimension,
        contentPadding,
        itemSpacing,
      ),
    );
  }

  Widget _buildGrid(
    List<SessionAvatarMember> visibleMembers,
    int gridDimension,
    double contentPadding,
    double itemSpacing,
  ) {
    final totalSpacing = itemSpacing * (gridDimension - 1);
    final innerSize = size - contentPadding * 2;
    final itemSize = (innerSize - totalSpacing) / gridDimension;
    return Wrap(
      spacing: itemSpacing,
      runSpacing: itemSpacing,
      children: [
        for (final member in visibleMembers)
          SizedBox(
            width: itemSize,
            height: itemSize,
            child: _SessionAvatarCell(
              member: member,
              fallbackColor: memberFallbackColor,
              fontSize: itemSize * 0.48,
              cellSize: itemSize,
            ),
          ),
      ],
    );
  }
}

class _SessionAvatarCell extends StatelessWidget {
  const _SessionAvatarCell({
    required this.member,
    required this.fallbackColor,
    required this.fontSize,
    required this.cellSize,
  });

  final SessionAvatarMember member;
  final Color? fallbackColor;
  final double fontSize;
  final double cellSize;

  @override
  Widget build(BuildContext context) {
    final avatarUrl = member.avatarUrl.trim();
    if (avatarUrl.isNotEmpty) {
      return ColoredBox(
        color: Colors.white.withValues(alpha: 0.9),
        child: AvatarNetworkImage(
          avatarUrl: avatarUrl,
          fallback: _buildFallback(),
          width: cellSize,
          height: cellSize,
        ),
      );
    }

    return _buildFallback();
  }

  Widget _buildFallback() {
    final resolvedFallbackColor =
        fallbackColor ?? AppTheme.getAvatarColor(member.avatarSeed);
    return ColoredBox(
      color: resolvedFallbackColor.withValues(alpha: 0.9),
      child: Center(
        child: Text(
          firstAvatarGlyph(member.displayName),
          style: TextStyle(
            color: Colors.white,
            fontSize: fontSize,
            fontWeight: FontWeight.w700,
            height: 1,
          ),
        ),
      ),
    );
  }
}

String firstAvatarGlyph(String text) {
  final normalized = text.trim();
  if (normalized.isEmpty) return '?';
  return String.fromCharCode(normalized.runes.first).toUpperCase();
}
