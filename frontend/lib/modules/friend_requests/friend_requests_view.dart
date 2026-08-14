import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/themes/app_theme.dart';
import '../../data/providers/friend_service.dart';
import 'controllers/friend_requests_controller.dart';

class FriendRequestsView extends GetView<FriendRequestsController> {
  const FriendRequestsView({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      backgroundColor: theme.scaffoldBackgroundColor,
      appBar: AppBar(
        title: Text(
          'friend_requests'.tr,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          softWrap: false,
        ),
      ),
      body: Obx(() {
        final requests = controller.friendService.friendRequests;
        if (controller.isLoading.value && requests.isEmpty) {
          return const Center(child: CircularProgressIndicator());
        }

        return RefreshIndicator(
          onRefresh: controller.refreshRequests,
          child: requests.isEmpty
              ? ListView(
                  physics: const AlwaysScrollableScrollPhysics(),
                  padding: const EdgeInsets.fromLTRB(24, 96, 24, 24),
                  children: [
                    _EmptyFriendRequestsState(
                      title: 'friend_requests_empty'.tr,
                    ),
                  ],
                )
              : ListView.separated(
                  physics: const AlwaysScrollableScrollPhysics(),
                  padding: const EdgeInsets.fromLTRB(12, 12, 12, 24),
                  itemCount: requests.length,
                  separatorBuilder: (_, __) => const SizedBox(height: 10),
                  itemBuilder: (context, index) {
                    final request = requests[index];
                    return Obx(() {
                      return _FriendRequestCard(
                        request: request,
                        isProcessing: controller.isProcessing(request.id),
                        onAccept: request.status == 0
                            ? () => controller.handleRequest(request, true)
                            : null,
                        onReject: request.status == 0
                            ? () => controller.handleRequest(request, false)
                            : null,
                      );
                    });
                  },
                ),
        );
      }),
    );
  }
}

class _EmptyFriendRequestsState extends StatelessWidget {
  const _EmptyFriendRequestsState({
    required this.title,
  });

  final String title;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            Icons.mark_email_unread_outlined,
            size: 44,
            color: theme.colorScheme.secondary.withValues(alpha: 0.3),
          ),
          const SizedBox(height: 10),
          Text(
            title,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            softWrap: false,
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.secondary.withValues(alpha: 0.65),
            ),
          ),
        ],
      ),
    );
  }
}

class _FriendRequestCard extends StatelessWidget {
  const _FriendRequestCard({
    required this.request,
    required this.isProcessing,
    required this.onAccept,
    required this.onReject,
  });

  final FriendRequestItem request;
  final bool isProcessing;
  final Future<bool> Function()? onAccept;
  final Future<bool> Function()? onReject;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final displayName = request.nickname.trim().isNotEmpty
        ? request.nickname.trim()
        : request.username.trim();
    final username = '@${request.username.trim()}';
    final message = request.message.trim();

    return Container(
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(14),
      ),
      padding: const EdgeInsets.all(14),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _FriendRequestAvatar(title: displayName),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  displayName,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  softWrap: false,
                  style: theme.textTheme.bodyLarge?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  username,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  softWrap: false,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.secondary.withValues(alpha: 0.8),
                  ),
                ),
                if (message.isNotEmpty) ...[
                  const SizedBox(height: 4),
                  Text(
                    message,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    softWrap: false,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color:
                          theme.colorScheme.secondary.withValues(alpha: 0.68),
                    ),
                  ),
                ],
              ],
            ),
          ),
          const SizedBox(width: 12),
          request.status == 0
              ? _PendingRequestActions(
                  isProcessing: isProcessing,
                  onAccept: onAccept,
                  onReject: onReject,
                )
              : _RequestStatusBadge(status: request.status),
        ],
      ),
    );
  }
}

class _FriendRequestAvatar extends StatelessWidget {
  const _FriendRequestAvatar({
    required this.title,
  });

  final String title;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 48,
      height: 48,
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            AppTheme.warningColor,
            AppTheme.warningColor.withValues(alpha: 0.72),
          ],
        ),
        borderRadius: BorderRadius.circular(
          AppTheme.listAvatarCornerRadius(48),
        ),
      ),
      child: Center(
        child: Text(
          title.isNotEmpty ? title[0].toUpperCase() : '?',
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          softWrap: false,
          style: const TextStyle(
            color: Colors.white,
            fontSize: 16,
            fontWeight: FontWeight.w700,
          ),
        ),
      ),
    );
  }
}

class _PendingRequestActions extends StatelessWidget {
  const _PendingRequestActions({
    required this.isProcessing,
    required this.onAccept,
    required this.onReject,
  });

  final bool isProcessing;
  final Future<bool> Function()? onAccept;
  final Future<bool> Function()? onReject;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        SizedBox(
          width: 68,
          height: 34,
          child: ElevatedButton(
            onPressed: isProcessing || onAccept == null
                ? null
                : () async {
                    await onAccept!.call();
                  },
            style: ElevatedButton.styleFrom(
              backgroundColor: AppTheme.successColor,
              foregroundColor: Colors.white,
              padding: EdgeInsets.zero,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(9),
              ),
              elevation: 0,
            ),
            child: isProcessing
                ? const SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      valueColor: AlwaysStoppedAnimation<Color>(Colors.white),
                    ),
                  )
                : Text(
                    'friend_accept'.tr,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    softWrap: false,
                    style: const TextStyle(fontSize: 12),
                  ),
          ),
        ),
        const SizedBox(width: 8),
        SizedBox(
          width: 68,
          height: 34,
          child: OutlinedButton(
            onPressed: isProcessing || onReject == null
                ? null
                : () async {
                    await onReject!.call();
                  },
            style: OutlinedButton.styleFrom(
              padding: EdgeInsets.zero,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(9),
              ),
            ),
            child: Text(
              'friend_reject'.tr,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              softWrap: false,
              style: const TextStyle(fontSize: 12),
            ),
          ),
        ),
      ],
    );
  }
}

class _RequestStatusBadge extends StatelessWidget {
  const _RequestStatusBadge({
    required this.status,
  });

  final int status;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isAccepted = status == 1;

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: isAccepted
            ? AppTheme.successColor.withValues(alpha: 0.12)
            : theme.colorScheme.secondary.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Text(
        isAccepted ? 'friend_accepted'.tr : 'friend_rejected'.tr,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        softWrap: false,
        style: theme.textTheme.bodySmall?.copyWith(
          color: isAccepted
              ? AppTheme.successColor
              : theme.colorScheme.secondary.withValues(alpha: 0.72),
          fontWeight: FontWeight.w600,
        ),
      ),
    );
  }
}
