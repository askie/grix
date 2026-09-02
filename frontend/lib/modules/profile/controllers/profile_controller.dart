import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';
import '../../../app/settings/theme_preference_service.dart';
import '../../../data/providers/auth_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../app/themes/app_theme.dart';
import '../../../shared/utils/hardware_facade.dart';
import '../../../shared/utils/image_url_builder.dart';
import '../../../shared/utils/toast_util.dart';
import '../../../shared/widgets/app_dialog_style.dart';
import '../../../shared/widgets/avatar_network_image.dart';
import '../../home/services/friend_qr_flow_service.dart';
import '../services/avatar_cropper_service.dart';
import '../services/logout_flow_service.dart';

typedef ProfileToast = void Function(String message, {bool isError});

class ProfileController extends GetxController {
  ProfileController({ProfileToast? showToast})
    : _showToast = showToast ?? CustomToast.show;

  final ProfileToast _showToast;
  final AuthService authService = Get.find<AuthService>();
  final ImService imService = Get.find<ImService>();
  final AvatarCropperService avatarCropperService =
      Get.find<AvatarCropperService>();
  final ThemePreferenceService themePreferenceService =
      Get.find<ThemePreferenceService>();
  final FriendQrFlowService? _friendQrFlowService =
      Get.isRegistered<FriendQrFlowService>()
      ? Get.find<FriendQrFlowService>()
      : null;
  final RxBool isUpdatingProfile = false.obs;
  final RxBool isUploadingAvatar = false.obs;
  final RxInt avatarRenderVersion = 0.obs;
  static final RegExp _usernamePattern = RegExp(r'^[a-zA-Z0-9_]{4,20}$');

  @override
  void onReady() {
    super.onReady();
    unawaited(_ensureEmailLoaded());
  }

  Future<void> _ensureEmailLoaded() async {
    final email = authService.user?.email.trim() ?? '';
    if (email.isNotEmpty) {
      return;
    }
    await authService.fetchCurrentUserProfile();
  }

  Future<void> toggleTheme() {
    return themePreferenceService.toggle();
  }

  Future<void> showEditProfileDialog() async {
    final user = authService.user;
    if (user == null) {
      _showToast('common_error'.tr);
      return;
    }

    isUpdatingProfile.value = false;

    await showAppGetDialog(
      _EditProfileDialog(controller: this, user: user),
      barrierDismissible: false,
    );
    isUpdatingProfile.value = false;
  }

  Future<void> showAvatarEditSheet() async {
    await showAppActionSheet(
      context: Get.context!,
      items: [
        AppActionSheetItem(
          label: 'profile_avatar_pick_gallery'.tr,
          icon: Icons.photo_library_outlined,
          onTap: () => _pickCropAndUploadAvatar(fromCamera: false),
        ),
        AppActionSheetItem(
          label: 'profile_avatar_pick_camera'.tr,
          icon: Icons.photo_camera_outlined,
          onTap: () => _pickCropAndUploadAvatar(fromCamera: true),
        ),
      ],
    );
  }

  Future<void> showAvatarPreviewDialog() async {
    final horizontalInset = (Get.width * 0.02).clamp(8.0, 20.0).toDouble();
    final verticalInset = (Get.height * 0.08).clamp(40.0, 96.0).toDouble();
    final dialogWidth = (Get.width - horizontalInset * 2).toDouble();
    final avatarSize = (dialogWidth - 40).clamp(220.0, 420.0).toDouble();

    await showAppGetDialog(
      Obx(() {
        final user = authService.user;
        final nickname = user?.nickname.trim() ?? '';
        final avatarUrl = buildAvatarDisplayUrl(user?.avatarUrl);
        return Dialog(
          shape: appDialogShape,
          insetPadding: EdgeInsets.symmetric(
            horizontal: horizontalInset,
            vertical: verticalInset,
          ),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(20, 18, 20, 20),
            child: SizedBox(
              width: double.infinity,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    'profile_avatar_preview_title'.tr,
                    style: const TextStyle(
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const SizedBox(height: 16),
                  GestureDetector(
                    onTap: isUploadingAvatar.value
                        ? null
                        : () async {
                            Get.back();
                            await showAvatarEditSheet();
                          },
                    child: Stack(
                      alignment: Alignment.center,
                      children: [
                        Container(
                          width: avatarSize,
                          height: avatarSize,
                          clipBehavior: Clip.antiAlias,
                          decoration: BoxDecoration(
                            borderRadius: BorderRadius.circular(20),
                            gradient: const LinearGradient(
                              begin: Alignment.topLeft,
                              end: Alignment.bottomRight,
                              colors: [
                                AppTheme.primaryColor,
                                AppTheme.primaryDark,
                              ],
                            ),
                          ),
                          child: avatarUrl.isEmpty
                              ? _buildAvatarFallback(nickname)
                              : _buildCachedAvatarImage(
                                  avatarUrl: avatarUrl,
                                  fallback: _buildAvatarFallback(nickname),
                                ),
                        ),
                        if (isUploadingAvatar.value)
                          Container(
                            width: avatarSize,
                            height: avatarSize,
                            decoration: BoxDecoration(
                              color: Colors.black.withValues(alpha: 0.3),
                              borderRadius: BorderRadius.circular(20),
                            ),
                            child: const Center(
                              child: SizedBox(
                                width: 24,
                                height: 24,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                ),
                              ),
                            ),
                          ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 14),
                  Text(
                    'profile_avatar_preview_hint'.tr,
                    style: TextStyle(
                      fontSize: 12,
                      color: Get.theme.colorScheme.secondary,
                    ),
                  ),
                ],
              ),
            ),
          ),
        );
      }),
    );
  }

  Future<void> _pickCropAndUploadAvatar({required bool fromCamera}) async {
    if (isUploadingAvatar.value) return;

    final picked = await HardwareFacade.pickImage(fromCamera: fromCamera);
    if (picked == null) {
      return;
    }

    AvatarCropResult? cropResult;
    try {
      cropResult = await avatarCropperService.cropSquareAvatar(
        sourcePath: picked.path,
        webContext: Get.context,
      );
    } catch (_) {
      _showToast('profile_avatar_crop_failed'.tr);
      return;
    }
    if (cropResult == null) {
      return;
    }

    try {
      isUploadingAvatar.value = true;
      final result = await authService.uploadAvatar(
        bytes: cropResult.bytes,
        filename: picked.name.trim().isEmpty ? 'avatar.jpg' : picked.name,
      );
      if (!result.ok) {
        _showToast(
          result.message.isNotEmpty
              ? result.message
              : 'profile_avatar_upload_failed'.tr,
        );
        return;
      }
      _bumpAvatarRenderVersion();
      unawaited(_syncCurrentUserProfileAfterAvatarUpload());
      _showToast('profile_avatar_upload_success'.tr, isError: false);
    } catch (_) {
      _showToast('profile_avatar_upload_failed'.tr);
    } finally {
      isUploadingAvatar.value = false;
    }
  }

  Widget _buildAvatarFallback(String nickname) {
    final firstLetter = nickname.isNotEmpty ? nickname[0].toUpperCase() : 'U';
    return Center(
      child: Text(
        firstLetter,
        style: const TextStyle(
          color: Colors.white,
          fontSize: 72,
          fontWeight: FontWeight.w700,
        ),
      ),
    );
  }

  Widget _buildCachedAvatarImage({
    required String avatarUrl,
    required Widget fallback,
  }) {
    return AvatarNetworkImage(avatarUrl: avatarUrl, fallback: fallback);
  }

  String buildAvatarDisplayUrl(String? avatarUrl) {
    return appendVersionQueryParameter(
      avatarUrl?.trim() ?? '',
      avatarRenderVersion.value,
    );
  }

  Future<void> _syncCurrentUserProfileAfterAvatarUpload() async {
    final result = await authService.fetchCurrentUserProfile();
    if (!result.ok) {
      return;
    }
    _bumpAvatarRenderVersion();
  }

  void _bumpAvatarRenderVersion() {
    avatarRenderVersion.value = DateTime.now().millisecondsSinceEpoch;
  }

  Future<void> _submitProfileUpdate({
    required TextEditingController nicknameController,
    required TextEditingController introductionController,
    required TextEditingController usernameController,
  }) async {
    if (isUpdatingProfile.value) return;

    final nickname = nicknameController.text.trim();
    final introduction = introductionController.text
        .replaceAll('\r\n', '\n')
        .replaceAll('\r', '\n')
        .trim();
    final username = usernameController.text.trim();
    if (nickname.isEmpty) {
      _showToast('profile_edit_nickname_required'.tr);
      return;
    }

    final user = authService.user;
    if (user == null) {
      _showToast('common_error'.tr);
      return;
    }

    final usernameChanged = username != user.username.trim();
    if (usernameChanged) {
      if (user.usernameModified) {
        _showToast('profile_edit_username_tip_used'.tr);
        return;
      }
      if (!_usernamePattern.hasMatch(username)) {
        _showToast('profile_edit_username_rule'.tr);
        return;
      }
    }

    final profileChanged =
        nickname != user.nickname.trim() ||
        introduction != user.introduction.trim();
    final unchanged =
        nickname == user.nickname.trim() &&
        introduction == user.introduction.trim() &&
        username == user.username.trim();
    if (unchanged) {
      _closeEditDialogIfOpen();
      return;
    }

    isUpdatingProfile.value = true;
    var profileSaved = false;
    if (profileChanged) {
      final profileResult = await authService.updateProfile(
        nickname: nickname,
        introduction: introduction,
      );
      if (!profileResult.ok) {
        isUpdatingProfile.value = false;
        _showToast(
          _resolveProfileUpdateFailureMessage(
            profileResult,
            fallbackMessage: 'profile_edit_update_failed'.tr,
          ),
        );
        return;
      }
      profileSaved = true;
    }

    if (usernameChanged) {
      final usernameResult = await authService.updateUsername(
        username: username,
      );
      if (!usernameResult.ok) {
        isUpdatingProfile.value = false;
        _showToast(
          _resolveUsernameUpdateFailureMessage(
            usernameResult,
            profileSaved: profileSaved,
          ),
        );
        return;
      }
    }

    isUpdatingProfile.value = false;

    _closeEditDialogIfOpen();
    _showToast('common_save_success'.tr, isError: false);
  }

  void _closeEditDialogIfOpen() {
    if (Get.isDialogOpen ?? false) {
      Get.back();
    }
  }

  bool _isUnauthorizedResult(ServiceResult<void> result) {
    return result.httpStatus == 401 || result.code == 401;
  }

  String _resolveProfileUpdateFailureMessage(
    ServiceResult<void> result, {
    required String fallbackMessage,
  }) {
    if (_isUnauthorizedResult(result)) {
      return 'profile_edit_session_expired'.tr;
    }
    return result.message.isNotEmpty ? result.message : fallbackMessage;
  }

  String _resolveUsernameUpdateFailureMessage(
    ServiceResult<void> result, {
    required bool profileSaved,
  }) {
    final detail = _resolveProfileUpdateFailureMessage(
      result,
      fallbackMessage: 'profile_edit_username_update_failed'.tr,
    );
    if (!profileSaved) {
      return detail;
    }
    return 'profile_edit_partial_success'.trParams({'detail': detail});
  }

  void openMyFriendQr() {
    final friendQrFlowService = _friendQrFlowService;
    if (friendQrFlowService == null) {
      _showToast('common_unknown_error'.tr);
      return;
    }
    friendQrFlowService.openMyFriendQr();
  }

  void showLogoutConfirm() {
    showLogoutConfirmDialog();
  }
}

class _EditProfileDialog extends StatefulWidget {
  const _EditProfileDialog({required this.controller, required this.user});

  final ProfileController controller;
  final User user;

  @override
  State<_EditProfileDialog> createState() => _EditProfileDialogState();
}

class _EditProfileDialogState extends State<_EditProfileDialog> {
  late final TextEditingController _nicknameController;
  late final TextEditingController _introductionController;
  late final TextEditingController _usernameController;

  bool get _canEditUsername => !widget.user.usernameModified;

  @override
  void initState() {
    super.initState();
    _nicknameController = TextEditingController(text: widget.user.nickname);
    _introductionController = TextEditingController(
      text: widget.user.introduction,
    );
    _usernameController = TextEditingController(text: widget.user.username);
  }

  @override
  void dispose() {
    _nicknameController.dispose();
    _introductionController.dispose();
    _usernameController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      titlePadding: const EdgeInsets.fromLTRB(24, 24, 24, 8),
      contentPadding: const EdgeInsets.fromLTRB(24, 8, 24, 0),
      actionsPadding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
      title: Text(
        'me_edit_profile'.tr,
        style: const TextStyle(fontWeight: FontWeight.w600),
      ),
      content: SizedBox(
        width: resolveDialogConstraints(
          context,
          size: AppDialogSize.wide,
        ).maxWidth,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: _nicknameController,
                maxLength: 32,
                textInputAction: TextInputAction.next,
                decoration: InputDecoration(
                  labelText: 'profile_edit_nickname'.tr,
                  helperText: 'profile_edit_nickname_tip'.tr,
                  helperMaxLines: 2,
                  hintText: 'profile_edit_nickname_hint'.tr,
                ),
              ),
              const SizedBox(height: 18),
              TextField(
                controller: _introductionController,
                maxLength: 300,
                minLines: 3,
                maxLines: 5,
                textInputAction: TextInputAction.newline,
                decoration: InputDecoration(
                  labelText: 'profile_edit_introduction'.tr,
                  helperText: 'profile_edit_introduction_tip'.tr,
                  helperMaxLines: 2,
                  hintText: 'profile_edit_introduction_hint'.tr,
                  alignLabelWithHint: true,
                ),
              ),
              const SizedBox(height: 18),
              TextField(
                controller: _usernameController,
                enabled: _canEditUsername,
                maxLength: 20,
                textInputAction: TextInputAction.done,
                decoration: InputDecoration(
                  labelText: 'profile_edit_username'.tr,
                  hintText: 'profile_edit_username_hint'.tr,
                  helperText: _canEditUsername
                      ? 'profile_edit_username_tip_once'.tr
                      : 'profile_edit_username_tip_used'.tr,
                  helperMaxLines: 2,
                ),
              ),
            ],
          ),
        ),
      ),
      actions: [
        Obx(
          () => TextButton(
            onPressed: widget.controller.isUpdatingProfile.value
                ? null
                : () => Get.back(),
            child: Text('common_cancel'.tr),
          ),
        ),
        Obx(
          () => ElevatedButton(
            onPressed: widget.controller.isUpdatingProfile.value
                ? null
                : () => widget.controller._submitProfileUpdate(
                    nicknameController: _nicknameController,
                    introductionController: _introductionController,
                    usernameController: _usernameController,
                  ),
            child: widget.controller.isUpdatingProfile.value
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Text('common_save'.tr),
          ),
        ),
      ],
    );
  }
}
