import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../app/themes/app_theme.dart';
import '../../../shared/widgets/app_dialog_style.dart';
import '../controllers/contacts_controller.dart';

enum ContactQuickAction { addFriend, newGroup, scanUserQr }

class ContactQuickActions {
  const ContactQuickActions._();

  static void handleSelection(
    BuildContext context,
    ContactsController controller,
    ContactQuickAction action,
  ) {
    switch (action) {
      case ContactQuickAction.addFriend:
        showAddFriendDialog(context, controller);
        return;
      case ContactQuickAction.newGroup:
        showNewGroupDialog(context, controller);
        return;
      case ContactQuickAction.scanUserQr:
        return;
    }
  }

  static void showAddFriendDialog(
    BuildContext context,
    ContactsController controller,
  ) {
    controller.resetSearch();
    final theme = Theme.of(context);

    showAppGetDialog(
      Dialog(
        shape: appDialogShape,
        child: Container(
          width: 460,
          constraints: const BoxConstraints(maxHeight: 500),
          padding: const EdgeInsets.all(20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(
                    Icons.person_add_alt_1_rounded,
                    color: theme.primaryColor,
                    size: 24,
                  ),
                  const SizedBox(width: 10),
                  Text(
                    'contacts_add_friend'.tr,
                    style: const TextStyle(
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const Spacer(),
                  IconButton(
                    onPressed: () => Get.back(),
                    icon: Icon(
                      Icons.close_rounded,
                      color: theme.colorScheme.secondary.withValues(alpha: 0.5),
                    ),
                    constraints: const BoxConstraints(),
                    padding: EdgeInsets.zero,
                  ),
                ],
              ),
              const SizedBox(height: 16),
              TextField(
                controller: controller.searchController,
                decoration: InputDecoration(
                  hintText: 'friend_search_hint'.tr,
                  contentPadding: const EdgeInsets.symmetric(
                    horizontal: 14,
                    vertical: 12,
                  ),
                  suffixIcon: IconButton(
                    icon: Icon(Icons.search_rounded, color: theme.primaryColor),
                    onPressed: () => controller.searchUsers(
                      controller.searchController.text,
                    ),
                  ),
                ),
                onSubmitted: controller.searchUsers,
                autofocus: true,
              ),
              const SizedBox(height: 12),
              Flexible(
                child: Obx(() {
                  if (controller.isSearching.value) {
                    return const Center(
                      child: Padding(
                        padding: EdgeInsets.all(20),
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                    );
                  }
                  if (controller.searchResults.isEmpty) {
                    return Center(
                      child: Padding(
                        padding: const EdgeInsets.all(20),
                        child: Text(
                          controller.searchController.text.isNotEmpty
                              ? 'friend_no_result'.tr
                              : '',
                          style: TextStyle(
                            color: theme.colorScheme.secondary.withValues(
                              alpha: 0.5,
                            ),
                          ),
                        ),
                      ),
                    );
                  }
                  return ListView.separated(
                    shrinkWrap: true,
                    itemCount: controller.searchResults.length,
                    separatorBuilder: (_, __) => Divider(
                      height: 1,
                      color: theme.colorScheme.outline.withValues(alpha: 0.1),
                    ),
                    itemBuilder: (context, index) {
                      final user = controller.searchResults[index];
                      final isSent = controller.sentUsernames.contains(
                        user.username,
                      );
                      final isAlreadyFriend = controller
                          .friendService
                          .friendList
                          .any((f) => f.username == user.username);
                      return ListTile(
                        dense: true,
                        leading: Container(
                          width: 40,
                          height: 40,
                          decoration: BoxDecoration(
                            gradient: LinearGradient(
                              colors: [
                                theme.primaryColor,
                                theme.primaryColor.withValues(alpha: 0.7),
                              ],
                            ),
                            borderRadius: BorderRadius.circular(10),
                          ),
                          child: Center(
                            child: Text(
                              user.nickname.isNotEmpty
                                  ? user.nickname[0].toUpperCase()
                                  : '?',
                              style: const TextStyle(
                                color: Colors.white,
                                fontSize: 15,
                                fontWeight: FontWeight.w700,
                              ),
                            ),
                          ),
                        ),
                        title: Text(
                          user.nickname.isNotEmpty
                              ? user.nickname
                              : user.username,
                          style: const TextStyle(
                            fontWeight: FontWeight.w500,
                            fontSize: 13,
                          ),
                        ),
                        subtitle: Text(
                          '@${user.username}',
                          style: TextStyle(
                            fontSize: 12,
                            color: theme.colorScheme.secondary.withValues(
                              alpha: 0.6,
                            ),
                          ),
                        ),
                        trailing: isAlreadyFriend
                            ? Container(
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 12,
                                  vertical: 6,
                                ),
                                decoration: BoxDecoration(
                                  color: theme.colorScheme.secondary.withValues(
                                    alpha: 0.1,
                                  ),
                                  borderRadius: BorderRadius.circular(8),
                                ),
                                child: Text(
                                  'friend_already_friend'.tr,
                                  style: TextStyle(
                                    fontSize: 12,
                                    color: theme.colorScheme.secondary
                                        .withValues(alpha: 0.6),
                                  ),
                                ),
                              )
                            : isSent
                            ? Container(
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 12,
                                  vertical: 6,
                                ),
                                decoration: BoxDecoration(
                                  color: AppTheme.successColor.withValues(
                                    alpha: 0.1,
                                  ),
                                  borderRadius: BorderRadius.circular(8),
                                ),
                                child: Text(
                                  'friend_request_sent'.tr,
                                  style: const TextStyle(
                                    fontSize: 11,
                                    color: AppTheme.successColor,
                                  ),
                                ),
                              )
                            : SizedBox(
                                height: 32,
                                child: ElevatedButton(
                                  onPressed: () async {
                                    final success = await controller
                                        .sendFriendRequest(user);
                                    if (success && Get.isDialogOpen == true) {
                                      Get.back();
                                    }
                                  },
                                  style: ElevatedButton.styleFrom(
                                    backgroundColor: theme.primaryColor,
                                    foregroundColor: Colors.white,
                                    padding: const EdgeInsets.symmetric(
                                      horizontal: 14,
                                    ),
                                    minimumSize: const Size(0, 32),
                                    tapTargetSize:
                                        MaterialTapTargetSize.shrinkWrap,
                                    shape: RoundedRectangleBorder(
                                      borderRadius: BorderRadius.circular(8),
                                    ),
                                    elevation: 0,
                                  ),
                                  child: Text(
                                    'friend_send_request'.tr,
                                    style: const TextStyle(fontSize: 12),
                                  ),
                                ),
                              ),
                      );
                    },
                  );
                }),
              ),
            ],
          ),
        ),
      ),
    );
  }

  static void showNewGroupDialog(
    BuildContext context,
    ContactsController controller,
  ) {
    final textController = TextEditingController();
    final theme = Theme.of(context);

    showAppGetDialog(
      AlertDialog(
        title: Text(
          'contacts_new_group'.tr,
          style: const TextStyle(fontWeight: FontWeight.w600),
        ),
        content: TextField(
          controller: textController,
          decoration: InputDecoration(hintText: 'contacts_new_group_hint'.tr),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Get.back(),
            child: Text('common_cancel'.tr),
          ),
          ElevatedButton(
            onPressed: () => controller.createGroup(textController.text),
            style: ElevatedButton.styleFrom(
              backgroundColor: theme.primaryColor,
              foregroundColor: Colors.white,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(10),
              ),
            ),
            child: Text('common_confirm'.tr),
          ),
        ],
      ),
    );
  }
}
