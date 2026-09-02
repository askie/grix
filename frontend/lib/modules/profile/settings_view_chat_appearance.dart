part of 'settings_view.dart';

extension _SettingsViewChatAppearance on SettingsView {
  Future<void> _showChatFontSizeSheet(
    BuildContext context,
    ChatFontSizeService service,
  ) async {
    await showModalBottomSheet<void>(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) {
        return SafeArea(
          child: Obx(() {
            final currentLevel = service.levelRx.value;
            return Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const SizedBox(height: 8),
                Container(
                  width: 40,
                  height: 4,
                  decoration: BoxDecoration(
                    color: Theme.of(
                      sheetContext,
                    ).colorScheme.outline.withValues(alpha: 0.25),
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
                const SizedBox(height: 8),
                ListTile(
                  title: Text('settings_font_size_small'.tr),
                  trailing: currentLevel == ChatFontSizeLevel.small
                      ? Icon(
                          Icons.check_rounded,
                          color: Theme.of(sheetContext).primaryColor,
                        )
                      : null,
                  onTap: () =>
                      _onFontLevelSelected(service, ChatFontSizeLevel.small),
                ),
                ListTile(
                  title: Text('settings_font_size_medium'.tr),
                  trailing: currentLevel == ChatFontSizeLevel.medium
                      ? Icon(
                          Icons.check_rounded,
                          color: Theme.of(sheetContext).primaryColor,
                        )
                      : null,
                  onTap: () =>
                      _onFontLevelSelected(service, ChatFontSizeLevel.medium),
                ),
                ListTile(
                  title: Text('settings_font_size_large'.tr),
                  trailing: currentLevel == ChatFontSizeLevel.large
                      ? Icon(
                          Icons.check_rounded,
                          color: Theme.of(sheetContext).primaryColor,
                        )
                      : null,
                  onTap: () =>
                      _onFontLevelSelected(service, ChatFontSizeLevel.large),
                ),
                const SizedBox(height: 8),
              ],
            );
          }),
        );
      },
    );
  }

  Future<void> _onFontLevelSelected(
    ChatFontSizeService service,
    ChatFontSizeLevel level,
  ) async {
    await service.setLevel(level);
    if (Get.isBottomSheetOpen ?? false) {
      Get.back();
    }
  }

  Widget _buildFontSizeTile({
    required BuildContext context,
    required ChatFontSizeService? service,
  }) {
    final theme = Theme.of(context);
    final currentLevel = service?.levelRx.value;
    final subtitle = currentLevel == null
        ? ''
        : service!.translationKeyForLevel(currentLevel).tr;
    return ListTile(
      leading: Container(
        width: 36,
        height: 36,
        decoration: BoxDecoration(
          color: AppTheme.successColor.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(10),
        ),
        child: const Icon(
          Icons.text_fields_rounded,
          color: AppTheme.successColor,
          size: 20,
        ),
      ),
      title: Text('settings_font_size'.tr),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (subtitle.isNotEmpty)
            Text(
              subtitle,
              style: TextStyle(
                fontSize: 13,
                color: theme.colorScheme.secondary,
              ),
            ),
          Icon(
            Icons.chevron_right_rounded,
            color: theme.colorScheme.secondary.withValues(alpha: 0.4),
          ),
        ],
      ),
      onTap: service == null
          ? null
          : () => _showChatFontSizeSheet(context, service),
    );
  }

  Widget _buildChatBackgroundTile({
    required BuildContext context,
    required ChatBackgroundService? service,
  }) {
    final theme = Theme.of(context);
    final isUploading = service?.isUploadingImage.value ?? false;
    final hasImage = service?.hasImage ?? false;
    final selectedColor = (service?.style ?? ChatBackgroundStyle.defaultStyle)
        .resolveColor(theme.brightness);
    final statusLabel = isUploading
        ? 'settings_chat_background_uploading'.tr
        : hasImage
        ? 'settings_chat_background_mode_image'.tr
        : '${'settings_chat_background_mode_color'.tr} ${_colorHex(selectedColor)}';

    return ListTile(
      leading: Container(
        width: 36,
        height: 36,
        decoration: BoxDecoration(
          color: AppTheme.primaryColor.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(10),
        ),
        child: const Icon(
          Icons.wallpaper_rounded,
          color: AppTheme.primaryColor,
          size: 20,
        ),
      ),
      title: Text('settings_chat_background'.tr),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (isUploading)
            SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: theme.primaryColor,
              ),
            )
          else if (!hasImage)
            Container(
              width: 14,
              height: 14,
              decoration: BoxDecoration(
                color: selectedColor,
                shape: BoxShape.circle,
                border: Border.all(
                  color: theme.colorScheme.outline.withValues(alpha: 0.5),
                  width: 1,
                ),
              ),
            )
          else
            Icon(
              Icons.image_rounded,
              size: 16,
              color: theme.colorScheme.secondary,
            ),
          const SizedBox(width: 8),
          Text(
            statusLabel,
            style: TextStyle(fontSize: 13, color: theme.colorScheme.secondary),
          ),
          Icon(
            Icons.chevron_right_rounded,
            color: theme.colorScheme.secondary.withValues(alpha: 0.4),
          ),
        ],
      ),
      onTap: service == null || isUploading
          ? null
          : () => _showChatBackgroundSheet(context, service),
    );
  }

  Future<void> _showChatBackgroundSheet(
    BuildContext context,
    ChatBackgroundService service,
  ) async {
    await showModalBottomSheet<void>(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) {
        return SafeArea(
          child: Obx(() {
            final uploading = service.isUploadingImage.value;
            return Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const SizedBox(height: 8),
                Container(
                  width: 40,
                  height: 4,
                  decoration: BoxDecoration(
                    color: Theme.of(
                      sheetContext,
                    ).colorScheme.outline.withValues(alpha: 0.25),
                    borderRadius: BorderRadius.circular(2),
                  ),
                ),
                const SizedBox(height: 8),
                ListTile(
                  leading: const Icon(Icons.palette_outlined),
                  title: Text('settings_chat_background_set_color'.tr),
                  onTap: uploading
                      ? null
                      : () async {
                          Get.back();
                          await _showChatBackgroundColorSheet(context, service);
                        },
                ),
                ListTile(
                  leading: const Icon(Icons.photo_library_outlined),
                  title: Text('settings_chat_background_upload_image'.tr),
                  onTap: uploading
                      ? null
                      : () async {
                          Get.back();
                          await _uploadBackgroundImage(service);
                        },
                ),
                ListTile(
                  leading: const Icon(Icons.restart_alt_rounded),
                  title: Text('settings_chat_background_reset'.tr),
                  onTap: uploading
                      ? null
                      : () async {
                          await service.resetToDefault();
                          if (Get.isBottomSheetOpen ?? false) {
                            Get.back();
                          }
                        },
                ),
                const SizedBox(height: 8),
              ],
            );
          }),
        );
      },
    );
  }

  Future<void> _showChatBackgroundColorSheet(
    BuildContext context,
    ChatBackgroundService service,
  ) async {
    await showModalBottomSheet<void>(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetContext) {
        final theme = Theme.of(sheetContext);
        return SafeArea(
          child: Obx(() {
            final selectedColor = service.style.resolveColor(theme.brightness);
            return Padding(
              padding: const EdgeInsets.fromLTRB(20, 12, 20, 20),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'settings_chat_background_set_color'.tr,
                    style: const TextStyle(
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const SizedBox(height: 12),
                  Wrap(
                    spacing: 10,
                    runSpacing: 10,
                    children: ChatBackgroundService.presetColors
                        .map(
                          (color) => InkWell(
                            borderRadius: BorderRadius.circular(24),
                            onTap: () =>
                                _onBackgroundColorSelected(service, color),
                            child: Container(
                              width: 36,
                              height: 36,
                              decoration: BoxDecoration(
                                color: color,
                                shape: BoxShape.circle,
                                border: Border.all(
                                  color:
                                      selectedColor.toARGB32() ==
                                          color.toARGB32()
                                      ? theme.primaryColor
                                      : theme.colorScheme.outline.withValues(
                                          alpha: 0.45,
                                        ),
                                  width:
                                      selectedColor.toARGB32() ==
                                          color.toARGB32()
                                      ? 2
                                      : 1,
                                ),
                              ),
                              child:
                                  selectedColor.toARGB32() == color.toARGB32()
                                  ? Icon(
                                      Icons.check_rounded,
                                      size: 18,
                                      color:
                                          AppTheme.readableTextColorForBackground(
                                            color,
                                          ),
                                    )
                                  : null,
                            ),
                          ),
                        )
                        .toList(),
                  ),
                ],
              ),
            );
          }),
        );
      },
    );
  }

  Future<void> _onBackgroundColorSelected(
    ChatBackgroundService service,
    Color color,
  ) async {
    await service.setColor(color);
    if (Get.isBottomSheetOpen ?? false) {
      Get.back();
    }
  }

  Future<void> _uploadBackgroundImage(ChatBackgroundService service) async {
    final result = await service.pickAndUploadBackgroundImage();
    if (result == ChatBackgroundUploadResult.success) {
      CustomToast.show(
        'settings_chat_background_upload_success'.tr,
        isError: false,
      );
      return;
    }
    if (result == ChatBackgroundUploadResult.failed) {
      CustomToast.show('settings_chat_background_upload_failed'.tr);
    }
  }

  String _colorHex(Color color) {
    final rgb = color.toARGB32() & 0x00FFFFFF;
    return '#${rgb.toRadixString(16).padLeft(6, '0').toUpperCase()}';
  }
}
