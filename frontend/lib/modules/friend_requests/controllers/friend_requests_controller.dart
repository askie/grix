import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:get/get.dart';

import '../../../data/providers/friend_service.dart';
import '../../../shared/utils/toast_util.dart';

class FriendRequestsController extends GetxController {
  FriendRequestsController({FriendService? friendService})
    : _friendService = friendService ?? Get.find<FriendService>();

  final FriendService _friendService;

  final isLoading = false.obs;
  final processingRequestIds = <String, bool>{}.obs;

  FriendService get friendService => _friendService;

  @override
  void onInit() {
    super.onInit();
    unawaited(refreshRequests());
  }

  Future<void> refreshRequests() async {
    isLoading.value = true;
    try {
      await _friendService.loadFriendRequests();
    } finally {
      isLoading.value = false;
    }
  }

  bool isProcessing(String requestId) {
    return processingRequestIds[requestId] == true;
  }

  Future<bool> handleRequest(FriendRequestItem request, bool accept) async {
    final requestId = request.id.trim();
    if (request.status != 0 || requestId.isEmpty || isProcessing(requestId)) {
      return false;
    }

    processingRequestIds[requestId] = true;
    try {
      final success = await _friendService.handleFriendRequest(
        requestId,
        accept,
      );
      if (success && accept) {
        _showAcceptedToast(request);
      }
      return success;
    } catch (error, stackTrace) {
      debugPrint('Friend request handling failed: $error');
      debugPrintStack(stackTrace: stackTrace);
      CustomToast.show('common_unknown_error'.tr);
      return false;
    } finally {
      processingRequestIds.remove(requestId);
    }
  }

  void _showAcceptedToast(FriendRequestItem request) {
    final displayName = request.nickname.trim().isNotEmpty
        ? request.nickname.trim()
        : request.username.trim();
    if (displayName.isEmpty) return;
    CustomToast.show('${'friend_accepted'.tr} $displayName', isError: false);
  }
}
