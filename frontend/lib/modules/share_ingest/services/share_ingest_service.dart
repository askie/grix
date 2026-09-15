import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../data/providers/auth_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../models/share_inbox_manifest.dart';
import '../share_ingest_page.dart';
import 'share_ingest_native_bridge.dart';

class ShareIngestService extends GetxService {
  ShareIngestService({AuthService? authService})
    : _authService = authService ?? Get.find<AuthService>();

  final AuthService _authService;

  StreamSubscription<String>? _eventSub;
  bool _isPresenting = false;
  ShareInboxManifest? _queuedManifest;

  Future<ShareIngestService> init() async {
    if (kIsWeb) {
      return this;
    }
    _eventSub = ShareIngestNativeBridge.shareReceivedEvents().listen(
      (_) => unawaited(_pollAndPresent()),
    );
    _authService.isLoggedInRx.listen((loggedIn) {
      if (loggedIn) {
        unawaited(_pollAndPresent());
      }
    });
    unawaited(_pollAndPresent());
    return this;
  }

  Future<void> consumePendingOnLaunch() async {
    await _pollAndPresent();
  }

  Future<void> _pollAndPresent() async {
    if (kIsWeb) return;
    final pending = await ShareIngestNativeBridge.consumePending();
    if (pending.isEmpty) {
      return;
    }
    for (final manifest in pending) {
      await _presentManifest(manifest);
    }
  }

  Future<void> _presentManifest(ShareInboxManifest manifest) async {
    if (!_authService.isLoggedIn) {
      _queuedManifest = manifest;
      if (Get.context != null) {
        CustomToast.show('share_ingest_login_required'.tr, isError: false);
      }
      return;
    }
    if (_isPresenting) {
      _queuedManifest = manifest;
      return;
    }
    final ctx = Get.context;
    if (ctx == null) {
      _queuedManifest = manifest;
      return;
    }
    _isPresenting = true;
    try {
      await Navigator.of(ctx).push<void>(
        MaterialPageRoute<void>(
          builder: (_) => ShareIngestPage(manifest: manifest),
          fullscreenDialog: true,
        ),
      );
    } finally {
      _isPresenting = false;
      final queued = _queuedManifest;
      _queuedManifest = null;
      if (queued != null && queued.id != manifest.id) {
        unawaited(_presentManifest(queued));
      }
    }
  }

  @override
  void onClose() {
    _eventSub?.cancel();
    super.onClose();
  }
}
