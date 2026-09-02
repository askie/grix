import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../data/providers/auth_service.dart';
import '../../data/providers/oss_service.dart';
import '../../shared/utils/hardware_facade.dart';
import '../themes/app_theme.dart';

class ChatBackgroundStyle {
  const ChatBackgroundStyle({
    required this.color,
    required this.imageUrl,
    this.isDefault = false,
  });

  static const Color defaultColor = Color(0xFFF2F2F2);
  static const ChatBackgroundStyle defaultStyle = ChatBackgroundStyle(
    color: defaultColor,
    imageUrl: '',
    isDefault: true,
  );

  final Color color;
  final String imageUrl;

  /// True when the user has never customized the background (or explicitly
  /// reset it). A default style follows the app's light/dark theme via
  /// [resolveColor] instead of always painting [defaultColor].
  final bool isDefault;

  bool get hasImage => imageUrl.trim().isNotEmpty;

  /// The color to actually paint for the given theme brightness. A
  /// user-chosen [color] always wins; only the untouched/reset default
  /// follows the current theme (light stays [defaultColor], dark switches
  /// to [AppTheme.darkBg]).
  Color resolveColor(Brightness brightness) {
    if (!isDefault) {
      return color;
    }
    return brightness == Brightness.dark ? AppTheme.darkBg : defaultColor;
  }

  ChatBackgroundStyle copyWith({Color? color, String? imageUrl}) {
    return ChatBackgroundStyle(
      color: color ?? this.color,
      imageUrl: imageUrl ?? this.imageUrl,
      isDefault: false,
    );
  }
}

enum ChatBackgroundUploadResult { success, canceled, failed }

class ChatBackgroundService extends GetxService {
  static const String _prefsKeyPrefix = 'chat_background_style_v1';
  static const String _payloadColorKey = 'color';
  static const String _payloadImageUrlKey = 'image_url';
  static const String _payloadIsDefaultKey = 'is_default';
  static const String _anonymousUserKey = '__anonymous__';
  static bool _prefsUnavailableLogged = false;

  static const List<Color> presetColors = <Color>[
    Color(0xFFF2F2F2),
    Color(0xFFFFFFFF),
    Color(0xFFFFF3E0),
    Color(0xFFE8F5E9),
    Color(0xFFE3F2FD),
    Color(0xFFEDE7F6),
    Color(0xFFFFEBEE),
    Color(0xFFFFFDE7),
    Color(0xFFCFD8DC),
    Color(0xFFE0F2F1),
    Color(0xFFF3E5F5),
    Color(0xFFFFF8E1),
  ];

  ChatBackgroundService({String? Function()? userIdResolver})
    : _userIdResolver = userIdResolver ?? _defaultUserIdResolver;

  final String? Function() _userIdResolver;
  final Rx<ChatBackgroundStyle> _style = ChatBackgroundStyle.defaultStyle.obs;
  final RxBool isUploadingImage = false.obs;

  String _loadedUserId = _anonymousUserKey;

  Future<ChatBackgroundService> init() async {
    await _loadForCurrentUser();
    return this;
  }

  ChatBackgroundStyle get style => _style.value;

  Color get color => _style.value.color;

  String get imageUrl => _style.value.imageUrl;

  bool get hasImage => _style.value.hasImage;

  void ensureSyncedWithCurrentUser() {
    final nextUserId = _resolveCurrentUserId();
    if (nextUserId == _loadedUserId) {
      return;
    }
    _loadedUserId = nextUserId;
    _style.value = ChatBackgroundStyle.defaultStyle;
    unawaited(_restoreStyleForUser(nextUserId));
  }

  Future<void> setColor(Color nextColor) async {
    ensureSyncedWithCurrentUser();
    _style.value = ChatBackgroundStyle(color: nextColor, imageUrl: '');
    await _persistCurrentStyle();
  }

  Future<void> setImageUrl(String nextImageUrl) async {
    final normalizedUrl = nextImageUrl.trim();
    if (normalizedUrl.isEmpty) {
      return;
    }

    ensureSyncedWithCurrentUser();
    _style.value = _style.value.copyWith(imageUrl: normalizedUrl);
    await _persistCurrentStyle();
  }

  Future<void> resetToDefault() async {
    ensureSyncedWithCurrentUser();
    _style.value = ChatBackgroundStyle.defaultStyle;
    await _persistCurrentStyle();
  }

  Future<ChatBackgroundUploadResult> pickAndUploadBackgroundImage() async {
    if (isUploadingImage.value) {
      return ChatBackgroundUploadResult.failed;
    }

    ensureSyncedWithCurrentUser();

    final pickedFile = await HardwareFacade.pickImage(fromCamera: false);
    if (pickedFile == null) {
      return ChatBackgroundUploadResult.canceled;
    }

    if (!Get.isRegistered<OssService>()) {
      debugPrint('OssService unavailable for chat background upload');
      return ChatBackgroundUploadResult.failed;
    }
    final ossService = Get.find<OssService>();

    isUploadingImage.value = true;
    try {
      final bytes = await pickedFile.readAsBytes();
      if (bytes.isEmpty) {
        return ChatBackgroundUploadResult.failed;
      }

      final filename = _resolveUploadFileName(pickedFile.name);
      final contentType = _resolveImageContentType(filename);
      final presignRes = await ossService.getPresignedUrl(
        filename,
        contentType,
      );
      if (presignRes == null) {
        return ChatBackgroundUploadResult.failed;
      }

      final uploadUrl = presignRes['uploadUrl']?.trim() ?? '';
      final accessUrl = presignRes['accessUrl']?.trim() ?? '';
      if (uploadUrl.isEmpty || accessUrl.isEmpty) {
        return ChatBackgroundUploadResult.failed;
      }

      final uploaded = await ossService.uploadToOss(
        uploadUrl,
        bytes,
        contentType: contentType,
      );
      if (!uploaded) {
        return ChatBackgroundUploadResult.failed;
      }

      await setImageUrl(accessUrl);
      return ChatBackgroundUploadResult.success;
    } catch (error) {
      debugPrint('pickAndUploadBackgroundImage error: $error');
      return ChatBackgroundUploadResult.failed;
    } finally {
      isUploadingImage.value = false;
    }
  }

  Future<void> _loadForCurrentUser() async {
    final userId = _resolveCurrentUserId();
    _loadedUserId = userId;
    _style.value = await _readStyleForUser(userId);
  }

  Future<void> _restoreStyleForUser(String userId) async {
    _style.value = await _readStyleForUser(userId);
  }

  Future<ChatBackgroundStyle> _readStyleForUser(String userId) async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) {
      return ChatBackgroundStyle.defaultStyle;
    }
    final raw = prefs.getString(_prefsKeyForUser(userId))?.trim() ?? '';
    if (raw.isEmpty) {
      return ChatBackgroundStyle.defaultStyle;
    }

    try {
      final decoded = jsonDecode(raw);
      if (decoded is! Map<String, dynamic>) {
        return ChatBackgroundStyle.defaultStyle;
      }
      final colorRaw = decoded[_payloadColorKey];
      if (colorRaw is! int) {
        return ChatBackgroundStyle.defaultStyle;
      }
      final imageUrlRaw = decoded[_payloadImageUrlKey];
      final imageUrl = imageUrlRaw is String ? imageUrlRaw.trim() : '';
      // Older persisted payloads predate `is_default` and always recorded an
      // explicit user pick, so default missing/invalid values to false.
      final isDefault = decoded[_payloadIsDefaultKey] == true;
      return ChatBackgroundStyle(
        color: Color(colorRaw),
        imageUrl: imageUrl,
        isDefault: isDefault,
      );
    } catch (_) {
      return ChatBackgroundStyle.defaultStyle;
    }
  }

  Future<void> _persistCurrentStyle() async {
    final prefs = await _safeGetPrefs();
    if (prefs == null) {
      return;
    }

    final payload = <String, dynamic>{
      _payloadColorKey: _style.value.color.toARGB32(),
      _payloadImageUrlKey: _style.value.imageUrl,
      _payloadIsDefaultKey: _style.value.isDefault,
    };

    await prefs.setString(_prefsKeyForUser(_loadedUserId), jsonEncode(payload));
  }

  String _prefsKeyForUser(String userId) {
    return '$_prefsKeyPrefix:$userId';
  }

  String _resolveCurrentUserId() {
    final rawUserId = _userIdResolver()?.trim() ?? '';
    if (rawUserId.isEmpty) {
      return _anonymousUserKey;
    }
    return rawUserId;
  }

  static String? _defaultUserIdResolver() {
    if (!Get.isRegistered<AuthService>()) {
      return null;
    }
    return Get.find<AuthService>().userId;
  }

  String _resolveUploadFileName(String rawName) {
    final trimmed = rawName.trim();
    if (trimmed.isNotEmpty) {
      return trimmed;
    }
    return 'chat_background_${DateTime.now().millisecondsSinceEpoch}.png';
  }

  String _resolveImageContentType(String fileName) {
    final dotIndex = fileName.lastIndexOf('.');
    if (dotIndex < 0 || dotIndex >= fileName.length - 1) {
      return 'image/png';
    }

    final ext = fileName.substring(dotIndex + 1).toLowerCase();
    switch (ext) {
      case 'jpg':
      case 'jpeg':
        return 'image/jpeg';
      case 'png':
        return 'image/png';
      case 'webp':
        return 'image/webp';
      case 'gif':
        return 'image/gif';
      case 'heic':
        return 'image/heic';
      case 'heif':
        return 'image/heif';
      case 'bmp':
        return 'image/bmp';
      default:
        return 'image/png';
    }
  }

  Future<SharedPreferences?> _safeGetPrefs() async {
    try {
      return await SharedPreferences.getInstance();
    } on MissingPluginException catch (error) {
      _logPrefsUnavailable(error);
      return null;
    } on PlatformException catch (error) {
      _logPrefsUnavailable(error);
      return null;
    }
  }

  void _logPrefsUnavailable(Object error) {
    if (_prefsUnavailableLogged) {
      return;
    }
    _prefsUnavailableLogged = true;
    debugPrint(
      'SharedPreferences unavailable, skip chat background persistence: $error',
    );
  }
}
