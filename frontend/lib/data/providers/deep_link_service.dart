import 'dart:async';

import 'package:app_links/app_links.dart';
import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../app/routes/app_routes.dart';
import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';
import 'friend_qr_service.dart';
import 'group_qr_service.dart';

enum DeepLinkScanStatus {
  handled,
  queued,
  invalidCode,
  resolveFailed,
  unsupported,
}

class DeepLinkScanResult {
  const DeepLinkScanResult._({required this.status, this.message = ''});

  final DeepLinkScanStatus status;
  final String message;

  bool get accepted => status != DeepLinkScanStatus.unsupported;

  const DeepLinkScanResult.handled()
    : this._(status: DeepLinkScanStatus.handled);

  const DeepLinkScanResult.queued() : this._(status: DeepLinkScanStatus.queued);

  const DeepLinkScanResult.invalidCode()
    : this._(status: DeepLinkScanStatus.invalidCode);

  const DeepLinkScanResult.resolveFailed({required String message})
    : this._(status: DeepLinkScanStatus.resolveFailed, message: message);

  const DeepLinkScanResult.unsupported()
    : this._(status: DeepLinkScanStatus.unsupported);
}

enum _PendingLinkType { friend, group }

class _PendingLink {
  const _PendingLink({required this.type, required this.code});

  const _PendingLink.none() : type = null, code = '';

  final _PendingLinkType? type;
  final String code;

  bool get isEmpty => type == null || code.trim().isEmpty;
}

class DeepLinkService extends GetxService {
  DeepLinkService({
    AppLinks? appLinks,
    AuthService? authService,
    FriendQrService? friendQrService,
    GroupQrService? groupQrService,
  }) : _appLinks = appLinks ?? AppLinks(),
       _authService = authService ?? Get.find<AuthService>(),
       _friendQrService = friendQrService ?? Get.find<FriendQrService>(),
       _groupQrService = groupQrService ?? Get.find<GroupQrService>();

  final AppLinks _appLinks;
  final AuthService _authService;
  final FriendQrService _friendQrService;
  final GroupQrService _groupQrService;

  StreamSubscription<Uri>? _linkSub;
  Worker? _loginStateWorker;
  bool _isHandlingPendingCode = false;
  _PendingLink _pendingLink = const _PendingLink.none();

  late final Uri? _configuredFriendPrefixUri;
  late final Uri? _configuredGroupPrefixUri;

  Future<DeepLinkService> init() async {
    _configuredFriendPrefixUri = _parsePrefixUri(
      AppRuntimeEndpoints.friendQrLinkPrefix,
    );
    _configuredGroupPrefixUri = _parsePrefixUri(
      AppRuntimeEndpoints.groupQrLinkPrefix,
    );

    _loginStateWorker = ever<bool>(_authService.isLoggedInRx, (isLoggedIn) {
      if (!isLoggedIn) return;
      unawaited(consumePendingLink());
    });

    _linkSub = _appLinks.uriLinkStream.listen(
      _handleIncomingUri,
      onError: (error) {
        debugPrint('DeepLinkService uriLinkStream error: $error');
      },
    );

    try {
      final initialUri = await _appLinks.getInitialLink().timeout(
        const Duration(seconds: 3),
        onTimeout: () {
          debugPrint('DeepLinkService getInitialLink timeout after 3s');
          return null;
        },
      );
      if (initialUri != null) {
        _handleIncomingUri(initialUri);
      }
    } catch (error) {
      debugPrint('DeepLinkService getInitialLink error: $error');
    }

    return this;
  }

  @override
  void onClose() {
    _loginStateWorker?.dispose();
    _linkSub?.cancel();
    super.onClose();
  }

  Future<bool> consumePendingLink() async {
    final result = await consumePendingLinkDetailed();
    return result.status == DeepLinkScanStatus.handled;
  }

  Future<DeepLinkScanResult> consumePendingLinkDetailed() async {
    if (_isHandlingPendingCode) {
      return const DeepLinkScanResult.queued();
    }
    if (!_authService.isLoggedIn) {
      return const DeepLinkScanResult.queued();
    }

    final pending = _pendingLink;
    if (pending.isEmpty) {
      return const DeepLinkScanResult.unsupported();
    }

    _isHandlingPendingCode = true;
    try {
      switch (pending.type) {
        case _PendingLinkType.friend:
          return _consumeFriendQrCode(pending.code);
        case _PendingLinkType.group:
          return _consumeGroupQrCode(pending.code);
        case null:
          return const DeepLinkScanResult.unsupported();
      }
    } finally {
      _isHandlingPendingCode = false;
    }
  }

  Future<DeepLinkScanResult> _consumeFriendQrCode(String rawCode) async {
    final code = rawCode.trim();
    if (code.isEmpty) {
      _pendingLink = const _PendingLink.none();
      return const DeepLinkScanResult.unsupported();
    }

    final resolvedResp = await _friendQrService.resolveCodeDetailed(code);
    if (!resolvedResp.ok || resolvedResp.data == null) {
      _pendingLink = const _PendingLink.none();
      if (resolvedResp.errorType == FriendQrResolveErrorType.invalidCode) {
        return const DeepLinkScanResult.invalidCode();
      }
      return DeepLinkScanResult.resolveFailed(message: resolvedResp.message);
    }

    final resolved = resolvedResp.data!;
    if (resolved.userId.trim().isEmpty) {
      _pendingLink = const _PendingLink.none();
      return const DeepLinkScanResult.invalidCode();
    }

    _pendingLink = const _PendingLink.none();
    final routePayload = <String, String>{
      'peer_id': resolved.userId,
      'peer_type': '1',
      'nickname': resolved.nickname,
      'username': resolved.username,
      'avatar_url': resolved.avatarUrl,
    };
    Get.toNamed(
      AppRoutes.accountInfo,
      parameters: routePayload,
      arguments: routePayload,
    );
    return const DeepLinkScanResult.handled();
  }

  Future<DeepLinkScanResult> _consumeGroupQrCode(String rawCode) async {
    final code = rawCode.trim();
    if (code.isEmpty) {
      _pendingLink = const _PendingLink.none();
      return const DeepLinkScanResult.unsupported();
    }

    final resolvedResp = await _groupQrService.resolveCodeDetailed(code);
    if (!resolvedResp.ok || resolvedResp.data == null) {
      _pendingLink = const _PendingLink.none();
      if (resolvedResp.errorType == GroupQrErrorType.invalidCode) {
        return const DeepLinkScanResult.invalidCode();
      }
      return DeepLinkScanResult.resolveFailed(message: resolvedResp.message);
    }

    final resolved = resolvedResp.data!;
    if (resolved.code.trim().isEmpty) {
      _pendingLink = const _PendingLink.none();
      return const DeepLinkScanResult.invalidCode();
    }

    _pendingLink = const _PendingLink.none();
    final routePayload = <String, String>{
      'code': resolved.code.trim().isEmpty ? code : resolved.code.trim(),
      'session_id': resolved.sessionId,
      'group_name': resolved.groupName,
      'owner_nickname': resolved.ownerNickname,
      'member_count': '${resolved.memberCount}',
      'is_member': resolved.isMember ? '1' : '0',
    };
    Get.toNamed(
      AppRoutes.groupInvite,
      parameters: routePayload,
      arguments: routePayload,
    );
    return const DeepLinkScanResult.handled();
  }

  Future<DeepLinkScanResult> handleScannedText(String rawContent) async {
    final normalized = rawContent.trim();
    if (normalized.isEmpty) {
      return const DeepLinkScanResult.unsupported();
    }

    final parsed = Uri.tryParse(normalized);
    if (parsed == null) {
      return const DeepLinkScanResult.unsupported();
    }

    final accepted = _queueIncomingUri(parsed);
    if (!accepted) {
      return const DeepLinkScanResult.unsupported();
    }
    if (!_authService.isLoggedIn) {
      return const DeepLinkScanResult.queued();
    }
    return consumePendingLinkDetailed();
  }

  void _handleIncomingUri(Uri uri) {
    if (!_queueIncomingUri(uri)) {
      return;
    }
    unawaited(consumePendingLink());
  }

  bool _queueIncomingUri(Uri uri) {
    final friendCode = _extractCodeByPrefix(uri, _configuredFriendPrefixUri);
    if (friendCode.isNotEmpty) {
      _pendingLink = _PendingLink(
        type: _PendingLinkType.friend,
        code: friendCode,
      );
      return true;
    }

    final groupCode = _extractCodeByPrefix(uri, _configuredGroupPrefixUri);
    if (groupCode.isNotEmpty) {
      _pendingLink = _PendingLink(
        type: _PendingLinkType.group,
        code: groupCode,
      );
      return true;
    }

    return false;
  }

  Uri? _parsePrefixUri(String rawPrefix) {
    return parsePrefixUri(rawPrefix);
  }

  String _extractCodeByPrefix(Uri incomingUri, Uri? prefixUri) {
    if (prefixUri == null) {
      return '';
    }
    return extractQrCodeWithPrefix(
      incomingUri: incomingUri,
      prefixUri: prefixUri,
    );
  }

  @visibleForTesting
  static Uri? parsePrefixUri(String rawPrefix) {
    final normalized = rawPrefix.trim();
    if (normalized.isEmpty) {
      return null;
    }
    final parsed = Uri.tryParse(normalized);
    if (parsed == null ||
        parsed.scheme.trim().isEmpty ||
        parsed.host.trim().isEmpty) {
      return null;
    }
    return parsed;
  }

  @visibleForTesting
  static String extractQrCodeWithPrefix({
    required Uri incomingUri,
    required Uri prefixUri,
  }) {
    final incomingScheme = incomingUri.scheme.trim().toLowerCase();
    final expectedScheme = prefixUri.scheme.trim().toLowerCase();
    if (incomingScheme.isEmpty || expectedScheme.isEmpty) {
      return '';
    }
    if (incomingScheme != expectedScheme) {
      return '';
    }

    final incomingHost = incomingUri.host.trim().toLowerCase();
    final expectedHost = prefixUri.host.trim().toLowerCase();
    if (incomingHost.isEmpty || expectedHost.isEmpty) {
      return '';
    }
    if (incomingHost != expectedHost) {
      return '';
    }

    final incomingPort = incomingUri.hasPort ? incomingUri.port : 0;
    final expectedPort = prefixUri.hasPort ? prefixUri.port : 0;
    if (incomingPort != expectedPort) {
      return '';
    }

    final expectedPathSegments = prefixUri.pathSegments
        .where((segment) => segment.trim().isNotEmpty)
        .toList();
    final incomingPathSegments = incomingUri.pathSegments
        .where((segment) => segment.trim().isNotEmpty)
        .toList();

    if (incomingPathSegments.length != expectedPathSegments.length + 1) {
      return '';
    }

    for (var i = 0; i < expectedPathSegments.length; i++) {
      if (incomingPathSegments[i] != expectedPathSegments[i]) {
        return '';
      }
    }

    return incomingPathSegments.last.trim();
  }

  @visibleForTesting
  static String extractFriendQrCodeWithPrefix({
    required Uri incomingUri,
    required Uri prefixUri,
  }) {
    return extractQrCodeWithPrefix(
      incomingUri: incomingUri,
      prefixUri: prefixUri,
    );
  }

  @visibleForTesting
  static String extractGroupQrCodeWithPrefix({
    required Uri incomingUri,
    required Uri prefixUri,
  }) {
    return extractQrCodeWithPrefix(
      incomingUri: incomingUri,
      prefixUri: prefixUri,
    );
  }
}
